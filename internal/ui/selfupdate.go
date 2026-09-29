package ui

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/update"
)

// pendingUpdate is a downloaded and verified package waiting to be installed.
type pendingUpdate struct {
	version, path string
	busy          bool // a check, download or install is running
}

func updateDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(dir, "klient", "updates")
}

// selfUpdateLoop checks GitHub once a day (not on a metered connection) and
// downloads a newer release in the background; installing it is one click
// and the system password.
func (a *App) selfUpdateLoop() {
	select {
	case <-a.ctx.Done():
		return
	case <-time.After(3 * time.Minute): // let the mail load first
	}
	for {
		due := make(chan bool)
		ui(func() {
			last, _ := time.Parse(time.RFC3339, a.cfg.UpdateLastCheck)
			due <- a.cfg.AutoUpdate && time.Since(last) > 24*time.Hour
		})
		if <-due && !gio.NetworkMonitorGetDefault().NetworkMetered() {
			a.checkUpdate(nil)
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(3 * time.Hour):
		}
	}
}

// checkUpdate looks for a new release and downloads it. report (may be nil)
// gets status texts on the UI thread. Runs off the UI thread.
func (a *App) checkUpdate(report func(string)) {
	say := func(s string) {
		if report != nil {
			ui(func() { report(s) })
		}
	}
	busy := make(chan bool)
	ui(func() {
		b := a.update.busy
		a.update.busy = true
		busy <- b
	})
	if <-busy {
		say("Kontrola už probíhá…")
		return
	}
	defer ui(func() { a.update.busy = false })

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	say("Zjišťuji nejnovější verzi…")
	rel, err := update.Latest(ctx)
	ui(func() {
		a.cfg.UpdateLastCheck = time.Now().Format(time.RFC3339)
		a.saveConfig()
	})
	if err != nil {
		say("Kontrola selhala: " + err.Error())
		return
	}
	if !update.Newer(rel.Version, Version) {
		say("Máte nejnovější verzi " + Version)
		_ = os.RemoveAll(updateDir()) // leftovers of an installed update
		return
	}
	if !update.Installable() {
		// Built from source: only tell about it.
		say("Je dostupná verze " + rel.Version + " – tento Klient není nainstalovaný z balíčku, stáhněte ji z GitHubu")
		return
	}
	say("Stahuji verzi " + rel.Version + "…")
	path, err := update.Download(ctx, rel, updateDir(), nil)
	if err != nil {
		say("Stažení selhalo: " + err.Error())
		return
	}
	say("Verze " + rel.Version + " je stažená a ověřená")
	ui(func() {
		a.update.version, a.update.path = rel.Version, path
		n := gio.NewNotification("Je připravená nová verze Klienta")
		n.SetBody("Verze " + rel.Version + " je stažená z GitHubu a ověřená. Instalace si vyžádá heslo správce.")
		n.SetDefaultAction("app.install-update")
		n.AddButton("Nainstalovat", "app.install-update")
		a.app.SendNotification("self-update", n)
		a.toastWithAction("Je připravená verze "+rel.Version, "Nainstalovat", a.installUpdate)
	})
}

// installUpdate installs the downloaded package and restarts Klient.
func (a *App) installUpdate() {
	if a.update.path == "" || a.update.busy {
		return
	}
	a.update.busy = true
	path, version := a.update.path, a.update.version
	a.app.WithdrawNotification("self-update")
	a.toast("Instaluji verzi " + version + "…")
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
		defer cancel()
		err := update.Install(ctx, path)
		ui(func() {
			a.update.busy = false
			if err != nil {
				a.toastWithAction(err.Error(), "Zkusit znovu", a.installUpdate)
				return
			}
			a.update.path = ""
			_ = os.RemoveAll(updateDir())
			a.restartApp()
		})
	}()
}

// updateGroup is the "Aktualizace" group in the preferences.
func (a *App) updateGroup() *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle("Aktualizace")
	g.SetDescription("Nové verze se stahují z GitHubu (" + update.Repo + "). Balíček se před instalací ověří kontrolním součtem, instalace si vyžádá heslo správce.")
	auto := adw.NewSwitchRow()
	auto.SetTitle("Automaticky kontrolovat aktualizace")
	auto.SetSubtitle("Jednou denně, ne na měřeném připojení; nová verze se rovnou stáhne")
	auto.SetActive(a.cfg.AutoUpdate)
	auto.NotifyProperty("active", func() {
		a.cfg.AutoUpdate = auto.Active()
		a.saveConfig()
	})
	g.Add(auto)

	check := adw.NewActionRow()
	check.SetTitle("Nainstalovaná verze " + Version)
	if last, err := time.Parse(time.RFC3339, a.cfg.UpdateLastCheck); err == nil {
		check.SetSubtitle("Naposledy zkontrolováno " + last.Format("2. 1. 15:04"))
	}
	btn := gtk.NewButtonWithLabel("Zkontrolovat")
	btn.SetVAlign(gtk.AlignCenter)
	install := gtk.NewButtonWithLabel("Nainstalovat")
	install.SetVAlign(gtk.AlignCenter)
	install.AddCSSClass("suggested-action")
	install.SetVisible(a.update.path != "")
	install.ConnectClicked(a.installUpdate)
	btn.ConnectClicked(func() {
		btn.SetSensitive(false)
		go func() {
			a.checkUpdate(func(s string) { check.SetSubtitle(s) })
			ui(func() {
				btn.SetSensitive(true)
				install.SetVisible(a.update.path != "")
			})
		}()
	})
	check.AddSuffix(install)
	check.AddSuffix(btn)
	g.Add(check)
	return g
}
