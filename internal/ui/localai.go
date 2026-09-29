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
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/ollama"
)

// Suggested local models for a CPU-only laptop (sizes of the download).
var localModels = []struct {
	name, label, size string
	defaultOn         bool
}{
	{"qwen3.5:4b", "Qwen 3.5 4B (Alibaba)", i18n.T("3.4 GB"), true},
	{"gemma4:e2b-it-qat", "Gemma 4 E2B (Google)", i18n.T("4.3 GB"), true},
	{"gemma4:e4b-it-qat", i18n.T("Gemma 4 E4B (Google) – better at Czech, needs more memory"), i18n.T("6.1 GB"), false},
	{"ministral-3:3b", "Ministral 3 3B (Mistral AI)", i18n.T("3.0 GB"), true},
	{"ministral-3:8b", i18n.T("Ministral 3 8B (Mistral AI) – smarter, slower"), i18n.T("6.0 GB"), false},
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

// doneMark starts the last status text of updateOllama.
const doneMark = "done:"

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
	say(i18n.T("Finding the latest version…"))
	rel, err := ollama.Latest(ctx)
	if err != nil {
		if report != nil {
			ui(func() { report(doneMark+fmt.Sprintf(i18n.T("Checking for updates failed: %s"), err.Error()), 1) })
		}
		return
	}
	updated := ""
	if ollama.Newer(rel.Version, installed) {
		err = a.ollama.Install(ctx, rel, func(done, total int64) {
			if report != nil {
				text := fmt.Sprintf(i18n.T("Downloading Ollama %s: %s of %s"), rel.Version, humanSize(done), humanSize(total))
				ui(func() { report(text, float64(done)/float64(max(total, 1))) })
			}
		})
		if err != nil {
			if report != nil {
				ui(func() { report(doneMark+fmt.Sprintf(i18n.T("Installation failed: %s"), err.Error()), 1) })
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
				say(fmt.Sprintf(i18n.T("Checking model %s for updates…"), m))
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
				a.toast(fmt.Sprintf(i18n.T("Ollama %s is installed"), updated))
			} else {
				a.toast(fmt.Sprintf(i18n.T("Local AI updated to Ollama %s"), updated))
			}
			if report != nil {
				report(doneMark+fmt.Sprintf(i18n.T("Ollama %s is installed"), updated), 1)
			}
		case manual && report != nil:
			report(doneMark+fmt.Sprintf(i18n.T("Ollama %s is up to date"), installed), 1)
		}
	})
}

// ---- Preferences ----------------------------------------------------------------

// localAIGroup is the "Local AI" section of the AI preferences: install,
// updates, models, comparison and the local-only switch.
func (a *App) localAIGroup(d *adw.PreferencesDialog, refreshRoles func()) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Local AI (Ollama)"))
	g.SetDescription(i18n.T("The model runs right on this computer; the content of your messages goes nowhere. Without a graphics card it is slower: judging a message takes tens of seconds."))

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
	localOnly.SetTitle(i18n.T("Local only"))
	localOnly.SetSubtitle(i18n.T("Nothing is sent to cloud AI (Gemini, ChatGPT, Claude, Mistral), even if keys are saved"))
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
	auto.SetTitle(i18n.T("Update automatically"))
	auto.SetSubtitle(i18n.T("Checks for a new version of Ollama and the models once a day (not on a metered connection)"))
	auto.SetActive(a.cfg.OllamaAutoUpdate)
	auto.NotifyProperty("active", func() {
		a.cfg.OllamaAutoUpdate = auto.Active()
		a.saveConfig()
	})
	g.Add(auto)

	pre := adw.NewSwitchRow()
	pre.SetTitle(i18n.T("Prepare summaries in advance"))
	pre.SetSubtitle(i18n.T("New mail is summarized in the background, only on mains power and when the model is idle; the summary is ready as soon as you open the message"))
	pre.SetActive(a.cfg.PrecomputeSummaries)
	pre.NotifyProperty("active", func() {
		a.cfg.PrecomputeSummaries = pre.Active()
		a.saveConfig()
	})
	g.Add(pre)

	models := adw.NewExpanderRow()
	models.SetTitle(i18n.T("Downloaded models"))
	g.Add(models)
	var modelRows []gtk.Widgetter

	compare := adw.NewButtonRow()
	compare.SetTitle(i18n.T("Compare Models on My Mail…"))
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
			status.SetSubtitle(i18n.T("Not installed. The official version is downloaded from GitHub (about 1.4 GB; only the part for the processor stays on disk)."))
			action.SetLabel(i18n.T("Install"))
			action.AddCSSClass("suggested-action")
		default:
			sub := fmt.Sprintf(i18n.T("Version %s · program %s, models %s"), ver, humanSize(prog), humanSize(mods))
			if p := a.ollama.PendingRestart(); p != "" {
				sub += " · " + fmt.Sprintf(i18n.T("the new version %s will be used for the next request"), p)
			}
			status.SetSubtitle(sub)
			action.SetLabel(i18n.T("Check for Updates"))
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
					models.SetSubtitle(i18n.T("Ollama cannot be started: ") + err.Error())
					return
				}
				models.SetSubtitle(fmt.Sprintf(i18n.N("%d downloaded", "%d downloaded", len(list)), len(list)))
				if len(list) == 0 {
					models.SetSubtitle(i18n.T("None yet – download one with the comparison below"))
				}
				for _, m := range list {
					m := m
					row := adw.NewActionRow()
					row.SetTitle(m.Name)
					sub := humanSize(m.Size)
					if (ai.IsLocal(a.cfg.AssistantProvider) && a.cfg.AssistantModel == m.Name) ||
						(ai.IsLocal(a.cfg.SpamProvider) && a.cfg.SpamModel == m.Name) {
						sub += i18n.T(" · in use")
					}
					row.SetSubtitle(sub)
					use := gtk.NewButtonWithLabel(i18n.T("Use"))
					use.SetVAlign(gtk.AlignCenter)
					use.AddCSSClass("flat")
					use.ConnectClicked(func() {
						a.useLocalModel(m.Name)
						refreshRoles()
						refresh()
						d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("The assistant and the spam filter now use %s"), m.Name)))
					})
					del := gtk.NewButtonFromIconName("user-trash-symbolic")
					del.SetVAlign(gtk.AlignCenter)
					del.AddCSSClass("flat")
					del.SetTooltipText(i18n.T("Delete Model"))
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
			status.SetSubtitle(strings.TrimPrefix(s, doneMark))
			if fraction >= 0 {
				progress.SetFraction(fraction)
			} else {
				progress.Pulse()
			}
			if strings.HasPrefix(s, doneMark) {
				busy = false
				action.SetSensitive(true)
				progress.SetVisible(false)
				refresh()
				status.SetSubtitle(strings.TrimPrefix(s, doneMark))
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
