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
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/rules"
	"github.com/Imbecile6197/klient/internal/secrets"
)

func (a *App) openPreferences() { a.preferencesDialog() }

func (a *App) preferencesDialog() *adw.PreferencesDialog {
	d := adw.NewPreferencesDialog()
	d.SetTitle(i18n.T("Preferences"))
	// Same order for every account: the application itself first, then AI
	// and mail handling, then the account's encryption.
	d.Add(a.generalPage(d))
	d.Add(a.aiPage(d))
	d.Add(a.spamPage(d))
	d.Add(a.messagesPage(d))
	d.Add(a.rulesPage(d))
	if a.acc != nil {
		d.Add(a.pgpPage(d))
	}
	d.Present(a.win)
	return d
}

func (a *App) aiPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle("AI")
	p.SetIconName("applications-science-symbolic")
	p.SetName("ai")

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
		combo.SetTitle(i18n.T("Provider"))
		combo.SetModel(gtk.NewStringList(names))
		combo.SetSelected(indexOf(*provider))
		modelRow := adw.NewEntryRow()
		modelRow.SetTitle(i18n.T("Model"))
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
				d.AddToast(adw.NewToast(i18n.T("“Local only” mode is on – cloud AI cannot be selected")))
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
			d.AddToast(adw.NewToast(i18n.T("Model saved")))
		})
		pick := gtk.NewButtonFromIconName("view-list-bullet-symbolic")
		pick.SetTooltipText(i18n.T("Choose from the models available for your account"))
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
							d.AddToast(adw.NewToast(i18n.T("No local model is downloaded – download one in the Local AI section")))
							return
						}
						a.pickModel(d, names, *model, func(m string) {
							*model = m
							modelRow.SetText(m)
							a.saveConfig()
							a.initAI()
							d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("Model %s saved"), m)))
						})
					})
				}()
				return
			}
			key, _ := secrets.LoadAPIKey(*provider)
			if key == "" {
				d.AddToast(adw.NewToast(i18n.T("Save this provider's API key first")))
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
						d.AddToast(adw.NewToast(i18n.T("Model list: ") + ai.Explain(prov, err).Text))
						return
					}
					a.pickModel(d, models, *model, func(m string) {
						*model = m
						modelRow.SetText(m)
						a.saveConfig()
						a.initAI()
						d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("Model %s saved"), m)))
					})
				})
			}()
		})
		modelRow.AddSuffix(pick)
		g.Add(combo)
		g.Add(modelRow)
		return g
	}
	p.Add(role(i18n.T("Assistant"), i18n.T("Message summaries, reply suggestions and text editing."),
		&a.cfg.AssistantProvider, &a.cfg.AssistantModel, func(pr ai.ProviderInfo) string { return pr.AssistantModel }))
	p.Add(role(i18n.T("Spam Filter"), i18n.T("Judges every new message – a cheaper and faster model is usually enough here."),
		&a.cfg.SpamProvider, &a.cfg.SpamModel, func(pr ai.ProviderInfo) string { return pr.SpamModel }))

	keys := adw.NewPreferencesGroup()
	keys.SetTitle(i18n.T("API Keys"))
	keys.SetDescription(i18n.T("The keys are stored in the system keyring. The content of the messages the AI processes is sent to the chosen provider."))
	for _, pr := range ai.Providers {
		pr := pr
		if ai.IsLocal(pr.ID) {
			continue // no key needed
		}
		row := adw.NewPasswordEntryRow()
		stored, _ := secrets.LoadAPIKey(pr.ID)
		setTitle := func(has bool) {
			if has {
				row.SetTitle(fmt.Sprintf(i18n.T("%s – saved"), pr.Name))
			} else {
				row.SetTitle(pr.Name)
			}
		}
		setTitle(stored != "")
		row.SetShowApplyButton(true)
		row.ConnectApply(func() {
			v := strings.TrimSpace(row.Text())
			if err := secrets.SaveAPIKey(pr.ID, v); err != nil {
				d.AddToast(adw.NewToast(i18n.T("Saving failed: ") + err.Error()))
				return
			}
			row.SetText("")
			setTitle(v != "")
			a.initAI()
			if a.mv != nil {
				a.mv.updateFilterStatus()
			}
			if v == "" {
				d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("%s key removed"), pr.Name)))
			} else {
				d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("%s key saved"), pr.Name)))
			}
		})
		link := gtk.NewButtonFromIconName("web-browser-symbolic")
		link.SetTooltipText(i18n.T("Get a key: ") + pr.KeyURL)
		link.SetVAlign(gtk.AlignCenter)
		link.AddCSSClass("flat")
		link.ConnectClicked(func() {
			gtk.NewURILauncher(pr.KeyURL).Launch(context.Background(), a.gtkWindow(), nil)
		})
		test := gtk.NewButtonWithLabel(i18n.T("Test"))
		test.SetVAlign(gtk.AlignCenter)
		test.AddCSSClass("flat")
		test.SetTooltipText(i18n.T("Sends a short test request"))
		test.ConnectClicked(func() {
			key, _ := secrets.LoadAPIKey(pr.ID)
			if key == "" {
				d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("No key is saved for %s"), pr.Name)))
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
			test.SetLabel(i18n.T("Testing…"))
			go func() {
				ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
				defer cancel()
				err := ai.Test(ctx, pr.ID, key, model)
				ui(func() {
					test.SetSensitive(true)
					test.SetLabel(i18n.T("Test"))
					if err != nil {
						ex := ai.Explain(pr.ID, err)
						dlg := adw.NewAlertDialog(fmt.Sprintf(i18n.T("%s: Test Failed"), pr.Name), ex.Text)
						details := gtk.NewLabel(fmt.Sprintf(i18n.T("Model %s"), model) + "\n" + err.Error())
						details.SetWrap(true)
						details.SetSelectable(true)
						details.AddCSSClass("caption")
						details.AddCSSClass("dim-label")
						exp := gtk.NewExpander(i18n.T("Details"))
						exp.SetChild(details)
						dlg.SetExtraChild(exp)
						dlg.AddResponse("close", i18n.T("Close"))
						if ex.URL != "" {
							dlg.AddResponse("open", i18n.T("Open Page"))
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
					d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("%s works (model %s)"), pr.Name, model)))
				})
			}()
		})
		row.AddSuffix(test)
		row.AddSuffix(link)
		keys.Add(row)
	}
	p.Add(keys)

	fg := adw.NewPreferencesGroup()
	fg.SetTitle(i18n.T("Automatic Features"))
	fg.SetDescription(i18n.T("They use the assistant model. With local AI the content of messages stays on this computer; with cloud AI it is sent to the provider."))
	autoLabel := adw.NewSwitchRow()
	autoLabel.SetTitle(i18n.T("Sort new mail into labels"))
	autoLabel.SetSubtitle(i18n.T("The AI adds the best matching of your labels to a new message (only when it is confident and no rule has applied)"))
	autoLabel.SetActive(a.cfg.AutoLabel)
	autoLabel.NotifyProperty("active", func() {
		a.cfg.AutoLabel = autoLabel.Active()
		a.saveConfig()
	})
	fg.Add(autoLabel)
	digest := adw.NewExpanderRow()
	digest.SetTitle(i18n.T("Morning mail overview"))
	digest.SetSubtitle(i18n.T("A daily notification summarizing the unread mail of all accounts"))
	digest.SetShowEnableSwitch(true)
	digest.SetEnableExpansion(a.cfg.Digest)
	digest.NotifyProperty("enable-expansion", func() {
		a.cfg.Digest = digest.EnableExpansion()
		a.saveConfig()
	})
	hour := adw.NewSpinRowWithRange(0, 23, 1)
	hour.SetTitle(i18n.T("Hour"))
	hour.SetSubtitle(i18n.T("The overview arrives in the first minute after this hour (Klient must be running, for example in the background)"))
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
	dlg := adw.NewAlertDialog(i18n.T("Choose a Model"), fmt.Sprintf(i18n.N("%d model is available for your account", "%d models are available for your account", len(models)), len(models)))
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
	dlg.AddResponse("close", i18n.T("Cancel"))
	dlg.SetCloseResponse("close")
	dlg.Present(parent)
}

// generalPage holds the settings of the application itself, unrelated to
// mail: language, desktop integration, background mode and updates.
func (a *App) generalPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle(i18n.T("General"))
	p.SetIconName("preferences-system-symbolic")
	p.SetName("general")
	p.Add(a.languageGroup(d))

	ig := adw.NewPreferencesGroup()
	ig.SetTitle(i18n.T("Desktop Integration"))
	defRow := adw.NewActionRow()
	defRow.SetTitle(i18n.T("Default email application"))
	setDef := func() {
		if isDefaultMailApp() {
			defRow.SetSubtitle(i18n.T("Klient opens mailto: links throughout the system"))
		} else {
			defRow.SetSubtitle(i18n.T("Another application opens mailto: links"))
		}
	}
	setDef()
	defBtn := gtk.NewButtonWithLabel(i18n.T("Make Default"))
	defBtn.SetVAlign(gtk.AlignCenter)
	defBtn.ConnectClicked(func() {
		if err := setDefaultMailApp(); err != nil {
			d.AddToast(adw.NewToast(err.Error()))
			return
		}
		setDef()
		d.AddToast(adw.NewToast(i18n.T("Klient is the default email application")))
	})
	defRow.AddSuffix(defBtn)
	ig.Add(defRow)
	p.Add(ig)

	bg := adw.NewPreferencesGroup()
	bg.SetTitle(i18n.T("Background"))
	bg.SetDescription(i18n.T("In GNOME, the icon in the top bar requires the AppIndicator extension (the gnome-shell-extension-appindicator package)."))
	background := adw.NewSwitchRow()
	background.SetTitle(i18n.T("Run in the background after closing the window"))
	background.SetSubtitle(i18n.T("Klient keeps watching your mail, filtering spam and showing notifications; the icon in the top bar opens the window again"))
	background.SetActive(a.cfg.RunInBackground)
	background.NotifyProperty("active", func() {
		a.cfg.RunInBackground = background.Active()
		a.saveConfig()
		if a.cfg.RunInBackground {
			a.startTray()
		} else if a.tray.started {
			d.AddToast(adw.NewToast(i18n.T("The icon disappears from the top bar after Klient restarts")))
		}
	})
	bg.Add(background)
	autostart := adw.NewSwitchRow()
	autostart.SetTitle(i18n.T("Start after login"))
	autostart.SetSubtitle(i18n.T("Klient starts hidden in the background, with just the icon in the top bar"))
	autostart.SetActive(autostartEnabled())
	autostart.NotifyProperty("active", func() {
		if err := setAutostart(autostart.Active()); err != nil {
			d.AddToast(adw.NewToast(i18n.T("Setting up automatic start failed: ") + err.Error()))
			return
		}
		if autostart.Active() && !background.Active() {
			background.SetActive(true)
		}
	})
	bg.Add(autostart)
	p.Add(bg)
	p.Add(a.updateGroup())
	return p
}

func (a *App) messagesPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle(i18n.T("Messages"))
	p.SetIconName("mail-message-new-symbolic")

	lg := adw.NewPreferencesGroup()
	lg.SetTitle(i18n.T("Message List"))
	threads := adw.NewSwitchRow()
	threads.SetTitle(i18n.T("Group messages into threads"))
	threads.SetSubtitle(i18n.T("Replies are shown together as a conversation, just like on the Proton website"))
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
	sg.SetTitle(i18n.T("Sending"))
	delay := adw.NewSpinRowWithRange(0, 60, 5)
	delay.SetTitle(i18n.T("Send delay (s)"))
	delay.SetSubtitle(i18n.T("During this time sending can be undone with the Undo button; 0 = send immediately"))
	delay.SetValue(float64(a.cfg.SendDelay))
	delay.NotifyProperty("value", func() {
		a.cfg.SendDelay = int(delay.Value())
		a.saveConfig()
	})
	sg.Add(delay)
	attachKey := adw.NewSwitchRow()
	attachKey.SetTitle(i18n.T("Always attach my public key"))
	attachKey.SetSubtitle(i18n.T("The default state of the switch in the message window; recipients with PGP can then send you encrypted mail"))
	attachKey.SetActive(a.cfg.AttachPublicKey)
	attachKey.NotifyProperty("active", func() {
		a.cfg.AttachPublicKey = attachKey.Active()
		a.saveConfig()
	})
	sg.Add(attachKey)
	p.Add(sg)

	og := adw.NewPreferencesGroup()
	og.SetTitle(i18n.T("Offline"))
	og.SetDescription(i18n.T("The newest messages are stored in an encrypted cache (AES-256 with the key in the keyring, bodies also PGP-encrypted as on the server), so you can read them without a connection."))
	count := adw.NewSpinRowWithRange(0, 5000, 100)
	count.SetTitle(i18n.T("Messages kept for offline reading"))
	count.SetSubtitle(i18n.T("0 = cache turned off"))
	count.SetValue(float64(a.cfg.OfflineMessages))
	count.NotifyProperty("value", func() {
		a.cfg.OfflineMessages = int(count.Value())
		a.saveConfig()
	})
	og.Add(count)
	stats := adw.NewActionRow()
	stats.SetTitle(i18n.T("Stored"))
	setStats := func() {
		if a.acc == nil || a.acc.Cache() == nil {
			stats.SetSubtitle(i18n.T("the cache is not available"))
			return
		}
		msgs, bodies := a.acc.Cache().Stats()
		stats.SetSubtitle(fmt.Sprintf(i18n.N("%d message, %d of them with content", "%d messages, %d of them with content", msgs), msgs, bodies))
	}
	setStats()
	clearBtn := gtk.NewButtonWithLabel(i18n.T("Clear"))
	clearBtn.SetVAlign(gtk.AlignCenter)
	clearBtn.AddCSSClass("destructive-action")
	clearBtn.ConnectClicked(func() {
		if a.acc != nil && a.acc.Cache() != nil {
			_ = a.acc.Cache().Clear()
			setStats()
			d.AddToast(adw.NewToast(i18n.T("Offline cache cleared")))
		}
	})
	stats.AddSuffix(clearBtn)
	og.Add(stats)
	p.Add(og)

	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Signature"))
	g.SetDescription(i18n.T("Added below new messages, replies and forwards."))
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
	save := gtk.NewButtonWithLabel(i18n.T("Save Signature"))
	save.SetHAlign(gtk.AlignEnd)
	save.SetMarginTop(6)
	save.ConnectClicked(func() {
		buf := view.Buffer()
		start, end := buf.Bounds()
		a.cfg.Signature = strings.TrimSpace(buf.Text(start, end, false))
		a.saveConfig()
		d.AddToast(adw.NewToast(i18n.T("Signature saved")))
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
	p.SetTitle(i18n.T("Spam Filter"))
	p.SetIconName("mail-mark-junk-symbolic")

	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("AI Decisions"))
	g.SetDescription(i18n.T("The AI judges every new message in the inbox. As evidence it gets the SPF/DKIM/DMARC results, blocklist hits and your lists of allowed and blocked senders."))
	enabled := adw.NewSwitchRow()
	enabled.SetTitle(i18n.T("Filter incoming mail with AI"))
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
	threshold.SetTitle(i18n.T("Threshold for moving to spam"))
	threshold.SetSubtitle(i18n.T("The spam probability at which the AI moves a message"))
	threshold.SetDigits(2)
	threshold.SetValue(a.cfg.SpamThreshold)
	threshold.NotifyProperty("value", func() {
		a.cfg.SpamThreshold = threshold.Value()
		a.saveConfig()
	})
	g.Add(threshold)
	hold := adw.NewSwitchRow()
	hold.SetTitle(i18n.T("Show new mail only after it is checked"))
	hold.SetSubtitle(i18n.T("A new message appears in the inbox once the spam filter has judged it, so spam never flashes up. Mail is delayed by the time the check takes (about a minute with local AI, at most 6 minutes)."))
	hold.SetActive(a.cfg.HoldUntilChecked)
	hold.NotifyProperty("active", func() {
		a.cfg.HoldUntilChecked = hold.Active()
		a.saveConfig()
	})
	g.Add(hold)
	p.Add(g)

	bg := adw.NewPreferencesGroup()
	bg.SetTitle(i18n.T("Blocklists"))
	bg.SetDescription(fmt.Sprintf(i18n.T("They are downloaded automatically every %s."), strings.TrimSuffix(a.cfg.UpdateInterval.String(), "0m0s")))
	status := adw.NewActionRow()
	status.SetTitle(i18n.T("Status"))
	setStatus := func() {
		nets, domains := a.lists.Stats()
		upd := i18n.T("never")
		if !a.lists.LastUpdate.IsZero() {
			upd = a.lists.LastUpdate.Format(i18n.T("Jan 2, 2006 15:04"))
		}
		status.SetSubtitle(fmt.Sprintf(i18n.T("%s, %s · last updated %s"), fmt.Sprintf(i18n.N("%d IP range", "%d IP ranges", nets), nets), fmt.Sprintf(i18n.N("%d domain", "%d domains", domains), domains), upd))
	}
	setStatus()
	upd := gtk.NewButtonWithLabel(i18n.T("Update Now"))
	upd.SetVAlign(gtk.AlignCenter)
	upd.ConnectClicked(func() {
		upd.SetSensitive(false)
		upd.SetLabel(i18n.T("Downloading…"))
		go func() {
			err := a.lists.Update(context.Background(), a.cfg.Feeds)
			ui(func() {
				upd.SetSensitive(true)
				upd.SetLabel(i18n.T("Update Now"))
				setStatus()
				if a.mv != nil {
					a.mv.updateFilterStatus()
				}
				if err != nil {
					d.AddToast(adw.NewToast(err.Error()))
				} else {
					d.AddToast(adw.NewToast(i18n.T("Blocklists updated")))
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
	own.SetTitle(i18n.T("My Public Keys"))
	own.SetDescription(i18n.T("Send them to contacts outside Proton so they can send you encrypted mail."))
	own.SetVisible(a.acc.Kind() == mailbox.KindProton)
	for _, addr := range a.acc.SendAddresses() {
		addr := addr
		row := adw.NewActionRow()
		row.SetTitle(addr.Email)
		copyBtn := gtk.NewButtonFromIconName("edit-copy-symbolic")
		copyBtn.SetTooltipText(i18n.T("Copy to Clipboard"))
		copyBtn.SetVAlign(gtk.AlignCenter)
		copyBtn.AddCSSClass("flat")
		copyBtn.ConnectClicked(func() {
			key, err := a.acc.OwnPublicKey(addr.ID)
			if err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			a.win.Clipboard().SetText(key)
			d.AddToast(adw.NewToast(i18n.T("Public key copied")))
		})
		saveBtn := gtk.NewButtonFromIconName("document-save-symbolic")
		saveBtn.SetTooltipText(i18n.T("Save to File"))
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
	contacts.SetTitle(i18n.T("Contact Keys"))
	contacts.SetDescription(i18n.T("Keys of recipients outside Proton who do not publish their key through WKD. Messages to them are encrypted with PGP."))
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
			del.SetTooltipText(i18n.T("Remove Key"))
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
	importBtn.SetTooltipText(i18n.T("Import Public Key…"))
	importBtn.AddCSSClass("flat")
	importBtn.ConnectClicked(func() {
		dlg := gtk.NewFileDialog()
		dlg.SetTitle(i18n.T("Import Public Key"))
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
			d.AddToast(adw.NewToast(i18n.T("Imported for: ") + strings.Join(emails, ", ")))
			refresh()
		})
	})
	contacts.SetHeaderSuffix(importBtn)
	refresh()
	p.Add(contacts)

	if a.acc.Kind() != mailbox.KindProton {
		lg := adw.NewPreferencesGroup()
		lg.SetTitle(i18n.T("Finding Recipients' Keys"))
		lg.SetDescription(i18n.T("Klient always asks the recipient's domain (Web Key Directory) and learns keys from the Autocrypt header of received mail."))
		vks := adw.NewSwitchRow()
		vks.SetTitle(i18n.T("Also search keys.openpgp.org"))
		vks.SetSubtitle(i18n.T("Finds more keys, but the server learns the addresses you write to"))
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
	p.SetTitle(i18n.T("Rules"))
	p.SetIconName("edit-find-replace-symbolic")
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Rules for Incoming Mail"))
	g.SetDescription(i18n.T("They apply to every new message in the inbox (after the spam filter). You can also create a rule for a sender from the ⋯ menu of a message."))
	add := gtk.NewButtonFromIconName("list-add-symbolic")
	add.SetTooltipText(i18n.T("Add Rule"))
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
			row.SetTitle(orDefault(r.Name, i18n.T("Rule")))
			sub := fmt.Sprintf(i18n.T("%s contains “%s” → %s"), rules.FieldName(r.Field), r.Contains, rules.ActionName(r.Action))
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
			row.SetTitle(i18n.T("No rules yet"))
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

// languageGroup picks the interface language; it applies after a restart.
func (a *App) languageGroup(d *adw.PreferencesDialog) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Language"))
	row := adw.NewComboRow()
	row.SetTitle(i18n.T("Interface language"))
	row.SetSubtitle(i18n.T("AI answers, summaries and suggested replies follow it too"))
	codes := []string{""}
	names := []string{i18n.T("System language")}
	for _, l := range i18n.Languages {
		codes = append(codes, l.Code)
		names = append(names, l.Name)
	}
	row.SetModel(gtk.NewStringList(names))
	for i, c := range codes {
		if c == a.cfg.Language {
			row.SetSelected(uint(i))
		}
	}
	row.NotifyProperty("selected", func() {
		c := codes[row.Selected()]
		if c == a.cfg.Language {
			return
		}
		a.cfg.Language = c
		a.saveConfig()
		t := adw.NewToast(i18n.T("The language changes after Klient restarts"))
		t.SetButtonLabel(i18n.T("Restart"))
		t.ConnectButtonClicked(a.restartApp)
		d.AddToast(t)
	})
	g.Add(row)
	return g
}
