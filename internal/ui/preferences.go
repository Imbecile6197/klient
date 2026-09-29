package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/rules"
	"github.com/Imbecile6197/klient/internal/secrets"
)

func (a *App) openPreferences() {
	d := adw.NewPreferencesDialog()
	d.SetTitle("Předvolby")
	// Same order for every account: general settings first, then mail
	// handling, then the account's encryption.
	d.Add(a.aiPage(d))
	d.Add(a.spamPage(d))
	d.Add(a.messagesPage(d))
	d.Add(a.rulesPage(d))
	if a.acc != nil {
		d.Add(a.pgpPage(d))
	}
	d.Present(a.win)
}

func (a *App) aiPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle("AI")
	p.SetIconName("applications-science-symbolic")

	var names []string
	for _, pr := range ai.Providers {
		names = append(names, pr.Name)
	}
	indexOf := func(id string) uint {
		for i, pr := range ai.Providers {
			if pr.ID == id {
				return uint(i)
			}
		}
		return 0
	}

	// One group per role: provider + model.
	var refreshers []func()
	refreshRoles := func() {
		for _, f := range refreshers {
			f()
		}
	}
	p.Add(a.localAIGroup(d, refreshRoles))
	role := func(title, desc string, provider, model *string, defaultModel func(ai.ProviderInfo) string) *adw.PreferencesGroup {
		g := adw.NewPreferencesGroup()
		g.SetTitle(title)
		g.SetDescription(desc)
		combo := adw.NewComboRow()
		combo.SetTitle("Poskytovatel")
		combo.SetModel(gtk.NewStringList(names))
		combo.SetSelected(indexOf(*provider))
		modelRow := adw.NewEntryRow()
		modelRow.SetTitle("Model")
		modelRow.SetText(*model)
		modelRow.SetShowApplyButton(true)
		refreshers = append(refreshers, func() {
			combo.SetSelected(indexOf(*provider))
			modelRow.SetText(*model)
		})
		combo.NotifyProperty("selected", func() {
			pr := ai.Providers[combo.Selected()]
			if pr.ID == *provider {
				return
			}
			if a.cfg.LocalOnly && !ai.IsLocal(pr.ID) {
				d.AddToast(adw.NewToast("Je zapnutý režim „Jen lokálně“ – cloudovou AI nejde zvolit"))
				combo.SetSelected(indexOf(*provider))
				return
			}
			*provider = pr.ID
			*model = defaultModel(pr)
			modelRow.SetText(*model)
			a.saveConfig()
			a.initAI()
			if a.mv != nil {
				a.mv.updateFilterStatus()
			}
		})
		modelRow.ConnectApply(func() {
			*model = strings.TrimSpace(modelRow.Text())
			a.saveConfig()
			a.initAI()
			d.AddToast(adw.NewToast("Model uložen"))
		})
		pick := gtk.NewButtonFromIconName("view-list-bullet-symbolic")
		pick.SetTooltipText("Vybrat z modelů dostupných pro váš účet")
		pick.SetVAlign(gtk.AlignCenter)
		pick.AddCSSClass("flat")
		pick.ConnectClicked(func() {
			if ai.IsLocal(*provider) {
				pick.SetSensitive(false)
				go func() {
					ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
					defer cancel()
					var names []string
					release, err := a.ollama.Acquire(ctx)
					if err == nil {
						var list []ollama.Model
						list, err = a.localClient().List(ctx)
						for _, m := range list {
							names = append(names, m.Name)
						}
						release()
					}
					ui(func() {
						pick.SetSensitive(true)
						if err != nil || len(names) == 0 {
							d.AddToast(adw.NewToast("Žádný stažený lokální model – stáhněte ho v sekci Lokální AI"))
							return
						}
						a.pickModel(d, names, *model, func(m string) {
							*model = m
							modelRow.SetText(m)
							a.saveConfig()
							a.initAI()
							d.AddToast(adw.NewToast("Model " + m + " uložen"))
						})
					})
				}()
				return
			}
			key, _ := secrets.LoadAPIKey(*provider)
			if key == "" {
				d.AddToast(adw.NewToast("Nejdřív uložte API klíč tohoto poskytovatele"))
				return
			}
			pick.SetSensitive(false)
			prov := *provider
			go func() {
				ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
				defer cancel()
				models, err := ai.ListModels(ctx, prov, key)
				ui(func() {
					pick.SetSensitive(true)
					if err != nil {
						d.AddToast(adw.NewToast("Seznam modelů: " + ai.Explain(prov, err).Text))
						return
					}
					a.pickModel(d, models, *model, func(m string) {
						*model = m
						modelRow.SetText(m)
						a.saveConfig()
						a.initAI()
						d.AddToast(adw.NewToast("Model " + m + " uložen"))
					})
				})
			}()
		})
		modelRow.AddSuffix(pick)
		g.Add(combo)
		g.Add(modelRow)
		return g
	}
	p.Add(role("Asistent", "Shrnutí zpráv, návrhy odpovědí a úpravy textu.",
		&a.cfg.AssistantProvider, &a.cfg.AssistantModel, func(pr ai.ProviderInfo) string { return pr.AssistantModel }))
	p.Add(role("Spamfiltr", "Posuzuje každou novou zprávu – levnější a rychlejší model tu obvykle stačí.",
		&a.cfg.SpamProvider, &a.cfg.SpamModel, func(pr ai.ProviderInfo) string { return pr.SpamModel }))

	keys := adw.NewPreferencesGroup()
	keys.SetTitle("API klíče")
	keys.SetDescription("Klíče se ukládají do klíčenky systému. Obsah zpráv, které AI zpracovává, se posílá zvolenému poskytovateli.")
	for _, pr := range ai.Providers {
		pr := pr
		if ai.IsLocal(pr.ID) {
			continue // no key needed
		}
		row := adw.NewPasswordEntryRow()
		stored, _ := secrets.LoadAPIKey(pr.ID)
		setTitle := func(has bool) {
			if has {
				row.SetTitle(pr.Name + " – uložen")
			} else {
				row.SetTitle(pr.Name)
			}
		}
		setTitle(stored != "")
		row.SetShowApplyButton(true)
		row.ConnectApply(func() {
			v := strings.TrimSpace(row.Text())
			if err := secrets.SaveAPIKey(pr.ID, v); err != nil {
				d.AddToast(adw.NewToast("Uložení selhalo: " + err.Error()))
				return
			}
			row.SetText("")
			setTitle(v != "")
			a.initAI()
			if a.mv != nil {
				a.mv.updateFilterStatus()
			}
			if v == "" {
				d.AddToast(adw.NewToast("Klíč " + pr.Name + " odstraněn"))
			} else {
				d.AddToast(adw.NewToast("Klíč " + pr.Name + " uložen"))
			}
		})
		link := gtk.NewButtonFromIconName("web-browser-symbolic")
		link.SetTooltipText("Získat klíč: " + pr.KeyURL)
		link.SetVAlign(gtk.AlignCenter)
		link.AddCSSClass("flat")
		link.ConnectClicked(func() {
			gtk.NewURILauncher(pr.KeyURL).Launch(context.Background(), a.gtkWindow(), nil)
		})
		test := gtk.NewButtonWithLabel("Vyzkoušet")
		test.SetVAlign(gtk.AlignCenter)
		test.AddCSSClass("flat")
		test.SetTooltipText("Pošle krátký testový dotaz")
		test.ConnectClicked(func() {
			key, _ := secrets.LoadAPIKey(pr.ID)
			if key == "" {
				d.AddToast(adw.NewToast("Pro " + pr.Name + " není uložen klíč"))
				return
			}
			// Test with the model configured for a role using this provider,
			// otherwise with the provider's default assistant model.
			model := pr.AssistantModel
			if a.cfg.SpamProvider == pr.ID {
				model = a.cfg.SpamModel
			}
			if a.cfg.AssistantProvider == pr.ID {
				model = a.cfg.AssistantModel
			}
			test.SetSensitive(false)
			test.SetLabel("Zkouším…")
			go func() {
				ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
				defer cancel()
				err := ai.Test(ctx, pr.ID, key, model)
				ui(func() {
					test.SetSensitive(true)
					test.SetLabel("Vyzkoušet")
					if err != nil {
						ex := ai.Explain(pr.ID, err)
						dlg := adw.NewAlertDialog(pr.Name+": test selhal", ex.Text)
						details := gtk.NewLabel("Model " + model + "\n" + err.Error())
						details.SetWrap(true)
						details.SetSelectable(true)
						details.AddCSSClass("caption")
						details.AddCSSClass("dim-label")
						exp := gtk.NewExpander("Podrobnosti")
						exp.SetChild(details)
						dlg.SetExtraChild(exp)
						dlg.AddResponse("close", "Zavřít")
						if ex.URL != "" {
							dlg.AddResponse("open", "Otevřít stránku")
							dlg.SetResponseAppearance("open", adw.ResponseSuggested)
							dlg.SetDefaultResponse("open")
						}
						dlg.SetCloseResponse("close")
						dlg.ConnectResponse(func(r string) {
							if r == "open" {
								gtk.NewURILauncher(ex.URL).Launch(context.Background(), a.gtkWindow(), nil)
							}
						})
						dlg.Present(d)
						return
					}
					d.AddToast(adw.NewToast(pr.Name + " funguje (model " + model + ")"))
				})
			}()
		})
		row.AddSuffix(test)
		row.AddSuffix(link)
		keys.Add(row)
	}
	p.Add(keys)

	fg := adw.NewPreferencesGroup()
	fg.SetTitle("Automatické funkce")
	fg.SetDescription("Používají model asistenta. S lokální AI zůstává obsah zpráv v počítači, s cloudovou se posílá poskytovateli.")
	autoLabel := adw.NewSwitchRow()
	autoLabel.SetTitle("Třídit novou poštu do štítků")
	autoLabel.SetSubtitle("AI přidá nové zprávě nejvhodnější z vašich štítků (jen když si je jistá a žádné pravidlo nezasáhlo)")
	autoLabel.SetActive(a.cfg.AutoLabel)
	autoLabel.NotifyProperty("active", func() {
		a.cfg.AutoLabel = autoLabel.Active()
		a.saveConfig()
	})
	fg.Add(autoLabel)
	digest := adw.NewExpanderRow()
	digest.SetTitle("Ranní přehled pošty")
	digest.SetSubtitle("Jednou denně oznámení se shrnutím nepřečtené pošty ze všech účtů")
	digest.SetShowEnableSwitch(true)
	digest.SetEnableExpansion(a.cfg.Digest)
	digest.NotifyProperty("enable-expansion", func() {
		a.cfg.Digest = digest.EnableExpansion()
		a.saveConfig()
	})
	hour := adw.NewSpinRowWithRange(0, 23, 1)
	hour.SetTitle("Hodina")
	hour.SetSubtitle("Přehled přijde první minutu po této hodině (Klient musí běžet, třeba na pozadí)")
	hour.SetValue(float64(a.cfg.DigestHour))
	hour.NotifyProperty("value", func() {
		a.cfg.DigestHour = int(hour.Value())
		a.saveConfig()
	})
	digest.AddRow(hour)
	fg.Add(digest)
	p.Add(fg)
	return p
}

// pickModel shows a list of model names to choose from.
func (a *App) pickModel(parent gtk.Widgetter, models []string, current string, done func(string)) {
	dlg := adw.NewAlertDialog("Vyberte model", fmt.Sprintf("%d modelů dostupných pro váš účet", len(models)))
	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list")
	for _, m := range models {
		row := adw.NewActionRow()
		row.SetTitle(m)
		row.SetActivatable(true)
		if m == current {
			row.AddSuffix(gtk.NewImageFromIconName("object-select-symbolic"))
		}
		list.Append(row)
	}
	list.ConnectRowActivated(func(r *gtk.ListBoxRow) {
		if i := r.Index(); i >= 0 && i < len(models) {
			done(models[i])
		}
		dlg.Close()
	})
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetMinContentHeight(320)
	sw.SetChild(list)
	dlg.SetExtraChild(sw)
	dlg.AddResponse("close", "Zrušit")
	dlg.SetCloseResponse("close")
	dlg.Present(parent)
}

func (a *App) messagesPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle("Zprávy")
	p.SetIconName("mail-message-new-symbolic")

	lg := adw.NewPreferencesGroup()
	lg.SetTitle("Seznam zpráv")
	threads := adw.NewSwitchRow()
	threads.SetTitle("Seskupovat zprávy do vláken")
	threads.SetSubtitle("Odpovědi se zobrazí pohromadě jako konverzace, stejně jako na webu Protonu")
	threads.SetActive(a.cfg.Threads)
	threads.NotifyProperty("active", func() {
		a.cfg.Threads = threads.Active()
		a.saveConfig()
		if a.mv != nil {
			a.mv.clearReader()
			a.mv.rebuildList()
		}
	})
	lg.Add(threads)
	p.Add(lg)

	sg := adw.NewPreferencesGroup()
	sg.SetTitle("Odesílání")
	delay := adw.NewSpinRowWithRange(0, 60, 5)
	delay.SetTitle("Zpoždění odeslání (s)")
	delay.SetSubtitle("Po tuto dobu jde odeslání vrátit tlačítkem Zpět; 0 = odeslat hned")
	delay.SetValue(float64(a.cfg.SendDelay))
	delay.NotifyProperty("value", func() {
		a.cfg.SendDelay = int(delay.Value())
		a.saveConfig()
	})
	sg.Add(delay)
	attachKey := adw.NewSwitchRow()
	attachKey.SetTitle("Přikládat můj veřejný klíč")
	attachKey.SetSubtitle("Výchozí stav přepínače v okně zprávy; příjemci s PGP vám pak mohou psát šifrovaně")
	attachKey.SetActive(a.cfg.AttachPublicKey)
	attachKey.NotifyProperty("active", func() {
		a.cfg.AttachPublicKey = attachKey.Active()
		a.saveConfig()
	})
	sg.Add(attachKey)
	defRow := adw.NewActionRow()
	defRow.SetTitle("Výchozí e-mailová aplikace")
	setDef := func() {
		if isDefaultMailApp() {
			defRow.SetSubtitle("Klient otevírá odkazy mailto: v celém systému")
		} else {
			defRow.SetSubtitle("Odkazy mailto: otevírá jiná aplikace")
		}
	}
	setDef()
	defBtn := gtk.NewButtonWithLabel("Nastavit")
	defBtn.SetVAlign(gtk.AlignCenter)
	defBtn.ConnectClicked(func() {
		if err := setDefaultMailApp(); err != nil {
			d.AddToast(adw.NewToast(err.Error()))
			return
		}
		setDef()
		d.AddToast(adw.NewToast("Klient je výchozí e-mailová aplikace"))
	})
	defRow.AddSuffix(defBtn)
	sg.Add(defRow)
	p.Add(sg)

	bg := adw.NewPreferencesGroup()
	bg.SetTitle("Na pozadí")
	bg.SetDescription("Ikona v horní liště vyžaduje v GNOME rozšíření AppIndicator (balíček gnome-shell-extension-appindicator).")
	background := adw.NewSwitchRow()
	background.SetTitle("Běžet na pozadí po zavření okna")
	background.SetSubtitle("Klient dál hlídá poštu, filtruje spam a ukazuje oznámení; ikona v liště okno zase otevře")
	background.SetActive(a.cfg.RunInBackground)
	background.NotifyProperty("active", func() {
		a.cfg.RunInBackground = background.Active()
		a.saveConfig()
		if a.cfg.RunInBackground {
			a.startTray()
		} else if a.tray.started {
			d.AddToast(adw.NewToast("Ikona z lišty zmizí po restartu Klienta"))
		}
	})
	bg.Add(background)
	autostart := adw.NewSwitchRow()
	autostart.SetTitle("Spouštět po přihlášení")
	autostart.SetSubtitle("Klient se spustí skrytý na pozadí, jen s ikonou v liště")
	autostart.SetActive(autostartEnabled())
	autostart.NotifyProperty("active", func() {
		if err := setAutostart(autostart.Active()); err != nil {
			d.AddToast(adw.NewToast("Nastavení automatického spuštění selhalo: " + err.Error()))
			return
		}
		if autostart.Active() && !background.Active() {
			background.SetActive(true)
		}
	})
	bg.Add(autostart)
	p.Add(bg)
	p.Add(a.updateGroup())

	og := adw.NewPreferencesGroup()
	og.SetTitle("Offline")
	og.SetDescription("Nejnovější zprávy se ukládají do cache zašifrované (AES-256 s klíčem v klíčence, těla navíc PGP jako na serveru), takže je lze číst i bez připojení.")
	count := adw.NewSpinRowWithRange(0, 5000, 100)
	count.SetTitle("Počet zpráv pro offline čtení")
	count.SetSubtitle("0 = cache vypnutá")
	count.SetValue(float64(a.cfg.OfflineMessages))
	count.NotifyProperty("value", func() {
		a.cfg.OfflineMessages = int(count.Value())
		a.saveConfig()
	})
	og.Add(count)
	stats := adw.NewActionRow()
	stats.SetTitle("Uloženo")
	setStats := func() {
		if a.acc == nil || a.acc.Cache() == nil {
			stats.SetSubtitle("cache není k dispozici")
			return
		}
		msgs, bodies := a.acc.Cache().Stats()
		stats.SetSubtitle(fmt.Sprintf("%d zpráv, z toho %d i s obsahem", msgs, bodies))
	}
	setStats()
	clearBtn := gtk.NewButtonWithLabel("Vymazat")
	clearBtn.SetVAlign(gtk.AlignCenter)
	clearBtn.AddCSSClass("destructive-action")
	clearBtn.ConnectClicked(func() {
		if a.acc != nil && a.acc.Cache() != nil {
			_ = a.acc.Cache().Clear()
			setStats()
			d.AddToast(adw.NewToast("Offline cache vymazána"))
		}
	})
	stats.AddSuffix(clearBtn)
	og.Add(stats)
	p.Add(og)

	g := adw.NewPreferencesGroup()
	g.SetTitle("Podpis")
	g.SetDescription("Připojí se pod nové zprávy, odpovědi a přeposlání.")
	view := gtk.NewTextView()
	view.SetWrapMode(gtk.WrapWordChar)
	view.SetTopMargin(10)
	view.SetBottomMargin(10)
	view.SetLeftMargin(10)
	view.SetRightMargin(10)
	view.SetSizeRequest(-1, 110)
	view.Buffer().SetText(a.cfg.Signature)
	frame := gtk.NewFrame("")
	frame.SetChild(view)
	save := gtk.NewButtonWithLabel("Uložit podpis")
	save.SetHAlign(gtk.AlignEnd)
	save.SetMarginTop(6)
	save.ConnectClicked(func() {
		buf := view.Buffer()
		start, end := buf.Bounds()
		a.cfg.Signature = strings.TrimSpace(buf.Text(start, end, false))
		a.saveConfig()
		d.AddToast(adw.NewToast("Podpis uložen"))
	})
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(frame)
	box.Append(save)
	g.Add(box)
	p.Add(g)
	return p
}

func (a *App) spamPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle("Spamfiltr")
	p.SetIconName("mail-mark-junk-symbolic")

	g := adw.NewPreferencesGroup()
	g.SetTitle("Rozhodování AI")
	g.SetDescription("Každou novou zprávu v doručené poště posoudí AI. Jako podklady dostane výsledky SPF/DKIM/DMARC, zásahy v blocklistech a vaše seznamy povolených a blokovaných odesílatelů.")
	enabled := adw.NewSwitchRow()
	enabled.SetTitle("Filtrovat příchozí poštu pomocí AI")
	enabled.SetActive(a.cfg.SpamFilterEnabled)
	enabled.NotifyProperty("active", func() {
		a.cfg.SpamFilterEnabled = enabled.Active()
		a.saveConfig()
		if a.mv != nil {
			a.mv.updateFilterStatus()
		}
	})
	g.Add(enabled)

	threshold := adw.NewSpinRowWithRange(0.3, 0.99, 0.05)
	threshold.SetTitle("Práh pro přesun do spamu")
	threshold.SetSubtitle("Pravděpodobnost spamu, od které AI zprávu přesune")
	threshold.SetDigits(2)
	threshold.SetValue(a.cfg.SpamThreshold)
	threshold.NotifyProperty("value", func() {
		a.cfg.SpamThreshold = threshold.Value()
		a.saveConfig()
	})
	g.Add(threshold)
	hold := adw.NewSwitchRow()
	hold.SetTitle("Zobrazovat novou poštu až po kontrole")
	hold.SetSubtitle("Nová zpráva se v doručené poště objeví, až ji spamfiltr posoudí – spam tak vůbec neprobleskne. Pošta se tím zpozdí o dobu kontroly (s lokální AI asi minutu, nejvýš 6 minut).")
	hold.SetActive(a.cfg.HoldUntilChecked)
	hold.NotifyProperty("active", func() {
		a.cfg.HoldUntilChecked = hold.Active()
		a.saveConfig()
	})
	g.Add(hold)
	p.Add(g)

	bg := adw.NewPreferencesGroup()
	bg.SetTitle("Blocklisty")
	bg.SetDescription(fmt.Sprintf("Stahují se automaticky každých %s.", strings.TrimSuffix(a.cfg.UpdateInterval.String(), "0m0s")))
	status := adw.NewActionRow()
	status.SetTitle("Stav")
	setStatus := func() {
		nets, domains := a.lists.Stats()
		upd := "nikdy"
		if !a.lists.LastUpdate.IsZero() {
			upd = a.lists.LastUpdate.Format("2. 1. 2006 15:04")
		}
		status.SetSubtitle(fmt.Sprintf("%d IP rozsahů, %d domén · poslední aktualizace %s", nets, domains, upd))
	}
	setStatus()
	upd := gtk.NewButtonWithLabel("Aktualizovat nyní")
	upd.SetVAlign(gtk.AlignCenter)
	upd.ConnectClicked(func() {
		upd.SetSensitive(false)
		upd.SetLabel("Stahuji…")
		go func() {
			err := a.lists.Update(context.Background(), a.cfg.Feeds)
			ui(func() {
				upd.SetSensitive(true)
				upd.SetLabel("Aktualizovat nyní")
				setStatus()
				if a.mv != nil {
					a.mv.updateFilterStatus()
				}
				if err != nil {
					d.AddToast(adw.NewToast(err.Error()))
				} else {
					d.AddToast(adw.NewToast("Blocklisty aktualizovány"))
				}
			})
		}()
	})
	status.AddSuffix(upd)
	bg.Add(status)

	for i := range a.cfg.Feeds {
		i := i
		f := a.cfg.Feeds[i]
		row := adw.NewSwitchRow()
		row.SetTitle(f.Name)
		row.SetSubtitle(f.URL)
		row.SetActive(f.Enabled)
		row.NotifyProperty("active", func() {
			a.cfg.Feeds[i].Enabled = row.Active()
			a.saveConfig()
		})
		bg.Add(row)
	}
	p.Add(bg)
	return p
}

func (a *App) pgpPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle("PGP")
	p.SetIconName("channel-secure-symbolic")

	if a.acc.Kind() != mailbox.KindProton {
		p.Add(a.ownKeyGroup(d, p))
	}
	own := adw.NewPreferencesGroup()
	own.SetTitle("Moje veřejné klíče")
	own.SetDescription("Pošlete je kontaktům mimo Proton, aby vám mohli psát šifrovaně.")
	own.SetVisible(a.acc.Kind() == mailbox.KindProton)
	for _, addr := range a.acc.SendAddresses() {
		addr := addr
		row := adw.NewActionRow()
		row.SetTitle(addr.Email)
		copyBtn := gtk.NewButtonFromIconName("edit-copy-symbolic")
		copyBtn.SetTooltipText("Kopírovat do schránky")
		copyBtn.SetVAlign(gtk.AlignCenter)
		copyBtn.AddCSSClass("flat")
		copyBtn.ConnectClicked(func() {
			key, err := a.acc.OwnPublicKey(addr.ID)
			if err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			a.win.Clipboard().SetText(key)
			d.AddToast(adw.NewToast("Veřejný klíč zkopírován"))
		})
		saveBtn := gtk.NewButtonFromIconName("document-save-symbolic")
		saveBtn.SetTooltipText("Uložit do souboru")
		saveBtn.SetVAlign(gtk.AlignCenter)
		saveBtn.AddCSSClass("flat")
		saveBtn.ConnectClicked(func() {
			key, err := a.acc.OwnPublicKey(addr.ID)
			if err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			dlg := gtk.NewFileDialog()
			dlg.SetInitialName(addr.Email + ".asc")
			dlg.Save(context.Background(), a.gtkWindow(), func(res gio.AsyncResulter) {
				file, err := dlg.SaveFinish(res)
				if err != nil || file == nil {
					return
				}
				if err := os.WriteFile(file.Path(), []byte(key), 0o644); err != nil {
					d.AddToast(adw.NewToast(err.Error()))
				}
			})
		})
		row.AddSuffix(copyBtn)
		row.AddSuffix(saveBtn)
		own.Add(row)
	}
	p.Add(own)

	contacts := adw.NewPreferencesGroup()
	contacts.SetTitle("Klíče kontaktů")
	contacts.SetDescription("Klíče adresátů mimo Proton, kteří nezveřejňují klíč přes WKD. Zprávy pro ně se zašifrují PGP.")
	var rows []gtk.Widgetter
	var refresh func()
	refresh = func() {
		for _, r := range rows {
			contacts.Remove(r)
		}
		rows = nil
		for _, email := range pgp.LocalKeys() {
			email := email
			row := adw.NewActionRow()
			row.SetTitle(email)
			row.SetSubtitle(shortFP(pgp.Fingerprint(pgp.LocalKey(email))))
			del := gtk.NewButtonFromIconName("user-trash-symbolic")
			del.SetTooltipText("Odstranit klíč")
			del.SetVAlign(gtk.AlignCenter)
			del.AddCSSClass("flat")
			del.ConnectClicked(func() {
				if err := pgp.DeleteKey(email); err != nil {
					d.AddToast(adw.NewToast(err.Error()))
				}
				refresh()
			})
			row.AddSuffix(del)
			contacts.Add(row)
			rows = append(rows, row)
		}
	}
	importBtn := gtk.NewButtonFromIconName("list-add-symbolic")
	importBtn.SetTooltipText("Importovat veřejný klíč…")
	importBtn.AddCSSClass("flat")
	importBtn.ConnectClicked(func() {
		dlg := gtk.NewFileDialog()
		dlg.SetTitle("Importovat veřejný klíč")
		dlg.Open(context.Background(), a.gtkWindow(), func(res gio.AsyncResulter) {
			file, err := dlg.OpenFinish(res)
			if err != nil || file == nil {
				return
			}
			b, err := os.ReadFile(file.Path())
			if err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			emails, err := pgp.ImportKey(string(b))
			if err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			d.AddToast(adw.NewToast("Importováno pro: " + strings.Join(emails, ", ")))
			refresh()
		})
	})
	contacts.SetHeaderSuffix(importBtn)
	refresh()
	p.Add(contacts)

	if a.acc.Kind() != mailbox.KindProton {
		lg := adw.NewPreferencesGroup()
		lg.SetTitle("Hledání klíčů příjemců")
		lg.SetDescription("Klient se vždy zeptá domény příjemce (Web Key Directory) a naučí se klíče z hlavičky Autocrypt v přijaté poště.")
		vks := adw.NewSwitchRow()
		vks.SetTitle("Hledat i na keys.openpgp.org")
		vks.SetSubtitle("Najde víc klíčů, ale server se dozví adresy, kterým píšete")
		vks.SetActive(a.cfg.KeyServerLookup)
		vks.NotifyProperty("active", func() {
			a.cfg.KeyServerLookup = vks.Active()
			imapmail.SetKeyServer(a.cfg.KeyServerLookup)
			a.saveConfig()
		})
		lg.Add(vks)
		p.Add(lg)
	}
	return p
}

func (a *App) rulesPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle("Pravidla")
	p.SetIconName("edit-find-replace-symbolic")
	g := adw.NewPreferencesGroup()
	g.SetTitle("Pravidla pro příchozí poštu")
	g.SetDescription("Použijí se na každou novou zprávu v doručené poště (po spamfiltru). Pravidlo pro odesílatele vytvoříte i z nabídky ⋯ u zprávy.")
	add := gtk.NewButtonFromIconName("list-add-symbolic")
	add.SetTooltipText("Přidat pravidlo")
	add.AddCSSClass("flat")
	g.SetHeaderSuffix(add)
	var rows []gtk.Widgetter
	var refresh func()
	refresh = func() {
		for _, r := range rows {
			g.Remove(r)
		}
		rows = nil
		for i, r := range a.cfg.Rules {
			i, r := i, r
			row := adw.NewActionRow()
			row.SetTitle(orDefault(r.Name, "Pravidlo"))
			sub := fmt.Sprintf("%s obsahuje „%s“ → %s", rules.FieldName(r.Field), r.Contains, rules.ActionName(r.Action))
			row.SetSubtitle(sub)
			on := gtk.NewSwitch()
			on.SetActive(r.Enabled)
			on.SetVAlign(gtk.AlignCenter)
			on.NotifyProperty("active", func() {
				a.cfg.Rules[i].Enabled = on.Active()
				a.saveConfig()
			})
			edit := gtk.NewButtonFromIconName("document-edit-symbolic")
			edit.AddCSSClass("flat")
			edit.SetVAlign(gtk.AlignCenter)
			edit.ConnectClicked(func() { a.editRule(i, a.cfg.Rules[i], refresh) })
			del := gtk.NewButtonFromIconName("user-trash-symbolic")
			del.AddCSSClass("flat")
			del.SetVAlign(gtk.AlignCenter)
			del.ConnectClicked(func() {
				a.cfg.Rules = append(a.cfg.Rules[:i], a.cfg.Rules[i+1:]...)
				a.saveConfig()
				refresh()
			})
			row.AddSuffix(on)
			row.AddSuffix(edit)
			row.AddSuffix(del)
			g.Add(row)
			rows = append(rows, row)
		}
		if len(a.cfg.Rules) == 0 {
			row := adw.NewActionRow()
			row.SetTitle("Zatím žádná pravidla")
			g.Add(row)
			rows = append(rows, row)
		}
	}
	add.ConnectClicked(func() {
		a.editRule(-1, rules.Rule{Field: rules.FieldFrom, Action: rules.ActionMove, Enabled: true}, refresh)
	})
	refresh()
	p.Add(g)
	return p
}
