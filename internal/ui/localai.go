package ui

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/ollama"
)

// Suggested local models for a CPU-only laptop (sizes of the download).
var localModels = []struct {
	name, label, size string
	defaultOn         bool
}{
	{"qwen3.5:4b", "Qwen 3.5 4B (Alibaba)", "3,4 GB", true},
	{"gemma4:e2b-it-qat", "Gemma 4 E2B (Google)", "4,3 GB", true},
	{"gemma4:e4b-it-qat", "Gemma 4 E4B (Google) – lepší čeština, potřebuje víc paměti", "6,1 GB", false},
	{"ministral-3:3b", "Ministral 3 3B (Mistral AI)", "3,0 GB", true},
	{"ministral-3:8b", "Ministral 3 8B (Mistral AI) – chytřejší, pomalejší", "6,0 GB", false},
}

func (a *App) localClient() *ollama.Client { return ollama.NewClient(a.ollama.Addr()) }

// ---- Auto-updater --------------------------------------------------------------

// ollamaUpdateLoop keeps Ollama and the used local models current: once a
// day it checks GitHub for a new release (only on a non-metered connection)
// and re-pulls the models in use, which downloads only changed parts.
func (a *App) ollamaUpdateLoop() {
	select {
	case <-a.ctx.Done():
		return
	case <-time.After(2 * time.Minute): // let the mail load first
	}
	for {
		due := make(chan bool)
		ui(func() {
			last, _ := time.Parse(time.RFC3339, a.cfg.OllamaLastCheck)
			due <- a.cfg.OllamaAutoUpdate && a.ollama.Installed() != "" && time.Since(last) > 24*time.Hour
		})
		if <-due && !gio.NetworkMonitorGetDefault().NetworkMetered() {
			a.updateOllama(false, nil)
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(3 * time.Hour):
		}
	}
}

// updateOllama checks for and installs a new Ollama and refreshes the used
// models. Runs off the UI thread; report gets status texts (may be nil).
func (a *App) updateOllama(manual bool, report func(text string, fraction float64)) {
	say := func(s string) {
		if report != nil {
			ui(func() { report(s, -1) })
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Hour)
	defer cancel()
	installed := a.ollama.Installed()
	say("Zjišťuji nejnovější verzi…")
	rel, err := ollama.Latest(ctx)
	if err != nil {
		if report != nil {
			ui(func() { report("Hotovo s chybou – kontrola aktualizací selhala: "+err.Error(), 1) })
		}
		return
	}
	updated := ""
	if ollama.Newer(rel.Version, installed) {
		err = a.ollama.Install(ctx, rel, func(done, total int64) {
			if report != nil {
				text := fmt.Sprintf("Stahuji Ollamu %s: %s z %s", rel.Version, humanSize(done), humanSize(total))
				ui(func() { report(text, float64(done)/float64(max(total, 1))) })
			}
		})
		if err != nil {
			if report != nil {
				ui(func() { report("Hotovo s chybou – instalace selhala: "+err.Error(), 1) })
			}
			log.Printf("ollama update: %v", err)
			return
		}
		updated = rel.Version
	}
	// Refresh the models in use.
	var models []string
	wait := make(chan struct{})
	ui(func() {
		for _, pair := range [][2]string{{a.cfg.AssistantProvider, a.cfg.AssistantModel}, {a.cfg.SpamProvider, a.cfg.SpamModel}} {
			if ai.IsLocal(pair[0]) && !slices.Contains(models, pair[1]) {
				models = append(models, pair[1])
			}
		}
		close(wait)
	})
	<-wait
	if len(models) > 0 {
		if release, err := a.ollama.Acquire(ctx); err == nil {
			for _, m := range models {
				say("Kontroluji aktualizace modelu " + m + "…")
				_ = a.localClient().Pull(ctx, m, nil)
			}
			release()
		}
	}
	ui(func() {
		a.cfg.OllamaLastCheck = time.Now().Format(time.RFC3339)
		a.saveConfig()
		switch {
		case updated != "":
			if installed == "" {
				a.toast("Ollama " + updated + " je nainstalovaná")
			} else {
				a.toast("Lokální AI aktualizována na Ollamu " + updated)
			}
			if report != nil {
				report("Hotovo: Ollama "+updated, 1)
			}
		case manual && report != nil:
			report("Hotovo: Ollama "+installed+" je aktuální", 1)
		}
	})
}

// ---- Preferences ----------------------------------------------------------------

// localAIGroup is the "Lokální AI" section of the AI preferences: install,
// updates, models, comparison and the local-only switch.
func (a *App) localAIGroup(d *adw.PreferencesDialog, refreshRoles func()) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle("Lokální AI (Ollama)")
	g.SetDescription("Model běží přímo v tomto počítači, obsah zpráv nikam neodchází. Bez grafické karty je pomalejší: posouzení zprávy trvá desítky sekund.")

	status := adw.NewActionRow()
	status.SetTitle("Ollama")
	status.SetSubtitleLines(3)
	action := gtk.NewButton()
	action.SetVAlign(gtk.AlignCenter)
	progress := gtk.NewProgressBar()
	progress.SetVisible(false)
	progress.SetVAlign(gtk.AlignCenter)
	progress.SetSizeRequest(120, -1)
	status.AddSuffix(progress)
	status.AddSuffix(action)
	g.Add(status)

	localOnly := adw.NewSwitchRow()
	localOnly.SetTitle("Jen lokálně")
	localOnly.SetSubtitle("Nic se neposílá cloudové AI (Gemini, ChatGPT, Claude, Mistral), i když jsou uložené klíče")
	localOnly.SetActive(a.cfg.LocalOnly)
	localOnly.NotifyProperty("active", func() {
		a.cfg.LocalOnly = localOnly.Active()
		if a.cfg.LocalOnly {
			model := a.preferredLocalModel()
			if !ai.IsLocal(a.cfg.AssistantProvider) {
				a.cfg.AssistantProvider, a.cfg.AssistantModel = ai.ProviderOllama, model
			}
			if !ai.IsLocal(a.cfg.SpamProvider) {
				a.cfg.SpamProvider, a.cfg.SpamModel = ai.ProviderOllama, model
			}
		}
		a.saveConfig()
		a.initAI()
		refreshRoles()
		if a.mv != nil {
			a.mv.updateFilterStatus()
		}
	})
	g.Add(localOnly)

	auto := adw.NewSwitchRow()
	auto.SetTitle("Automaticky aktualizovat")
	auto.SetSubtitle("Jednou denně zkontroluje novou verzi Ollamy a modelů (ne na měřeném připojení)")
	auto.SetActive(a.cfg.OllamaAutoUpdate)
	auto.NotifyProperty("active", func() {
		a.cfg.OllamaAutoUpdate = auto.Active()
		a.saveConfig()
	})
	g.Add(auto)

	pre := adw.NewSwitchRow()
	pre.SetTitle("Připravovat shrnutí předem")
	pre.SetSubtitle("Nová pošta se shrne na pozadí, jen při napájení ze sítě a když model nic jiného nedělá; při otevření zprávy je shrnutí hned")
	pre.SetActive(a.cfg.PrecomputeSummaries)
	pre.NotifyProperty("active", func() {
		a.cfg.PrecomputeSummaries = pre.Active()
		a.saveConfig()
	})
	g.Add(pre)

	models := adw.NewExpanderRow()
	models.SetTitle("Stažené modely")
	g.Add(models)
	var modelRows []gtk.Widgetter

	compare := adw.NewButtonRow()
	compare.SetTitle("Porovnat modely na mé poště…")
	compare.SetStartIconName("view-dual-symbolic")
	compare.ConnectActivated(func() { a.compareModels(d) })
	g.Add(compare)

	busy := false
	var refresh func()
	refresh = func() {
		ver := a.ollama.Installed()
		prog, mods := a.ollama.DiskUsage()
		switch {
		case busy:
		case ver == "":
			status.SetSubtitle("Není nainstalovaná. Stáhne se oficiální verze z GitHubu (asi 1,4 GB, na disku zůstane jen část pro procesor).")
			action.SetLabel("Nainstalovat")
			action.AddCSSClass("suggested-action")
		default:
			sub := fmt.Sprintf("Verze %s · program %s, modely %s", ver, humanSize(prog), humanSize(mods))
			if p := a.ollama.PendingRestart(); p != "" {
				sub += " · nová verze " + p + " se použije při dalším dotazu"
			}
			status.SetSubtitle(sub)
			action.SetLabel("Zkontrolovat aktualizace")
			action.RemoveCSSClass("suggested-action")
		}
		compare.SetSensitive(ver != "" && !busy)
		for _, r := range modelRows {
			models.Remove(r)
		}
		modelRows = nil
		models.SetSensitive(ver != "")
		if ver == "" {
			models.SetSubtitle("")
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
			defer cancel()
			var list []ollama.Model
			release, err := a.ollama.Acquire(ctx)
			if err == nil {
				list, err = a.localClient().List(ctx)
				release()
			}
			ui(func() {
				if err != nil {
					models.SetSubtitle("Ollamu nejde spustit: " + err.Error())
					return
				}
				models.SetSubtitle(fmt.Sprintf("%d stažených", len(list)))
				if len(list) == 0 {
					models.SetSubtitle("Zatím žádný – stáhněte ho porovnáním níže")
				}
				for _, m := range list {
					m := m
					row := adw.NewActionRow()
					row.SetTitle(m.Name)
					sub := humanSize(m.Size)
					if (ai.IsLocal(a.cfg.AssistantProvider) && a.cfg.AssistantModel == m.Name) ||
						(ai.IsLocal(a.cfg.SpamProvider) && a.cfg.SpamModel == m.Name) {
						sub += " · používá se"
					}
					row.SetSubtitle(sub)
					use := gtk.NewButtonWithLabel("Použít")
					use.SetVAlign(gtk.AlignCenter)
					use.AddCSSClass("flat")
					use.ConnectClicked(func() {
						a.useLocalModel(m.Name)
						refreshRoles()
						refresh()
						d.AddToast(adw.NewToast("Asistent i spamfiltr teď používají " + m.Name))
					})
					del := gtk.NewButtonFromIconName("user-trash-symbolic")
					del.SetVAlign(gtk.AlignCenter)
					del.AddCSSClass("flat")
					del.SetTooltipText("Smazat model")
					del.ConnectClicked(func() {
						go func() {
							err := a.localClient().Delete(a.ctx, m.Name)
							ui(func() {
								if err != nil {
									d.AddToast(adw.NewToast(err.Error()))
								}
								refresh()
							})
						}()
					})
					row.AddSuffix(use)
					row.AddSuffix(del)
					models.AddRow(row)
					modelRows = append(modelRows, row)
				}
			})
		}()
	}
	action.ConnectClicked(func() {
		if busy {
			return
		}
		busy = true
		action.SetSensitive(false)
		progress.SetVisible(true)
		progress.SetFraction(0)
		go a.updateOllama(true, func(s string, fraction float64) {
			status.SetSubtitle(s)
			if fraction >= 0 {
				progress.SetFraction(fraction)
			} else {
				progress.Pulse()
			}
			if strings.HasPrefix(s, "Hotovo") {
				busy = false
				action.SetSensitive(true)
				progress.SetVisible(false)
				refresh()
				status.SetSubtitle(strings.TrimPrefix(strings.TrimPrefix(s, "Hotovo s chybou – "), "Hotovo: "))
			}
		})
	})
	refresh()
	return g
}

// preferredLocalModel is the downloaded model to use by default.
func (a *App) preferredLocalModel() string {
	if ai.IsLocal(a.cfg.AssistantProvider) {
		return a.cfg.AssistantModel
	}
	if ai.IsLocal(a.cfg.SpamProvider) {
		return a.cfg.SpamModel
	}
	return localModels[0].name
}

func (a *App) useLocalModel(name string) {
	a.cfg.AssistantProvider, a.cfg.AssistantModel = ai.ProviderOllama, name
	a.cfg.SpamProvider, a.cfg.SpamModel = ai.ProviderOllama, name
	a.saveConfig()
	a.initAI()
	if a.mv != nil {
		a.mv.updateFilterStatus()
	}
}
