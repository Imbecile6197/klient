package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
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
		say(i18n.T("A check is already running…"))
		return
	}
	defer ui(func() { a.update.busy = false })

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	say(i18n.T("Finding the latest version…"))
	rel, err := update.Latest(ctx)
	ui(func() {
		a.cfg.UpdateLastCheck = time.Now().Format(time.RFC3339)
		a.saveConfig()
	})
	if err != nil {
		say(i18n.T("The update check failed: ") + err.Error())
		return
	}
	if !update.Newer(rel.Version, Version) {
		say(fmt.Sprintf(i18n.T("You have the latest version, %s"), Version))
		_ = os.RemoveAll(updateDir()) // leftovers of an installed update
		return
	}
	if !update.Installable() {
		// Built from source: only tell about it.
		say(fmt.Sprintf(i18n.T("Version %s is available – this Klient was not installed from the package, so download it from GitHub"), rel.Version))
		return
	}
	say(fmt.Sprintf(i18n.T("Downloading version %s…"), rel.Version))
	path, err := update.Download(ctx, rel, updateDir(), nil)
	if err != nil {
		say(i18n.T("The download failed: ") + err.Error())
		return
	}
	say(fmt.Sprintf(i18n.T("Version %s is downloaded and verified"), rel.Version))
	ui(func() {
		a.update.version, a.update.path = rel.Version, path
		n := gio.NewNotification(i18n.T("A New Version of Klient Is Ready"))
		n.SetBody(fmt.Sprintf(i18n.T("Version %s has been downloaded from GitHub and verified. Installing it will ask for the administrator password."), rel.Version))
		n.SetDefaultAction("app.install-update")
		n.AddButton(i18n.T("Install"), "app.install-update")
		a.app.SendNotification("self-update", n)
		a.toastWithAction(fmt.Sprintf(i18n.T("Version %s is ready"), rel.Version), i18n.T("Install"), a.installUpdate)
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
	a.toast(fmt.Sprintf(i18n.T("Installing version %s…"), version))
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
		defer cancel()
		err := update.Install(ctx, path)
		ui(func() {
			a.update.busy = false
			if err != nil {
				a.toastWithAction(err.Error(), i18n.T("Try Again"), a.installUpdate)
				return
			}
			a.update.path = ""
			_ = os.RemoveAll(updateDir())
			a.restartApp()
		})
	}()
}

// updateGroup is the i18n.T("Updates") group in the preferences.
func (a *App) updateGroup() *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Updates"))
	g.SetDescription(fmt.Sprintf(i18n.T("New versions are downloaded from GitHub (%s). The package is verified by its checksum before installation, and installing asks for the administrator password."), update.Repo))
	auto := adw.NewSwitchRow()
	auto.SetTitle(i18n.T("Check for updates automatically"))
	auto.SetSubtitle(i18n.T("Once a day, not on a metered connection; a new version is downloaded right away"))
	auto.SetActive(a.cfg.AutoUpdate)
	auto.NotifyProperty("active", func() {
		a.cfg.AutoUpdate = auto.Active()
		a.saveConfig()
	})
	g.Add(auto)

	check := adw.NewActionRow()
	check.SetTitle(fmt.Sprintf(i18n.T("Installed version %s"), Version))
	if last, err := time.Parse(time.RFC3339, a.cfg.UpdateLastCheck); err == nil {
		check.SetSubtitle(fmt.Sprintf(i18n.T("Last checked %s"), last.Format(i18n.T("Jan 2 15:04"))))
	}
	btn := gtk.NewButtonWithLabel(i18n.T("Check"))
	btn.SetVAlign(gtk.AlignCenter)
	install := gtk.NewButtonWithLabel(i18n.T("Install"))
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
