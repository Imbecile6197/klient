package ui

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/systray"

	"github.com/Imbecile6197/klient/internal/i18n"
)

//go:embed icons/tray.png
var trayIcon []byte

//go:embed icons/tray-unread.png
var trayIconUnread []byte

// tray is the icon in the panel (StatusNotifierItem; on GNOME shown by the
// AppIndicator extension). It can only be started once per process.
type tray struct {
	started bool
	ready   bool
	unread  int
	compose *systray.MenuItem
}

func (a *App) startTray() {
	if a.tray.started {
		return
	}
	a.tray.started = true
	start, _ := systray.RunWithExternalLoop(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("Klient")
		systray.SetTooltip("Klient")
		systray.SetOnTapped(func() { ui(a.showWindow) })
		open := systray.AddMenuItem(i18n.T("Open Klient"), "")
		a.tray.compose = systray.AddMenuItem(i18n.T("New Message"), "")
		systray.AddSeparator()
		quit := systray.AddMenuItem(i18n.T("Quit"), "")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					ui(a.showWindow)
				case <-a.tray.compose.ClickedCh:
					ui(func() {
						if a.acc != nil {
							a.showWindow()
							a.openCompose(nil)
						}
					})
				case <-quit.ClickedCh:
					ui(a.quit)
				}
			}
		}()
		ui(func() {
			a.tray.ready = true
			a.setTrayUnread(a.tray.unread)
		})
	}, nil)
	start()
}

// setTrayUnread shows a dot on the icon while there is unread mail.
func (a *App) setTrayUnread(n int) {
	a.tray.unread = n
	if !a.tray.ready {
		return
	}
	if n > 0 {
		systray.SetIcon(trayIconUnread)
		systray.SetTooltip(fmt.Sprintf("Klient – %s", unreadText(n)))
	} else {
		systray.SetIcon(trayIcon)
		systray.SetTooltip(i18n.T("Klient – no unread mail"))
	}
}

// unreadText is "1 unread message", "5 unread messages".
func unreadText(n int) string {
	return fmt.Sprintf(i18n.N("%d unread message", "%d unread messages", n), n)
}

func (a *App) showWindow() {
	if a.win == nil {
		return
	}
	a.win.SetVisible(true)
	a.win.Present()
}

// ---- Autostart --------------------------------------------------------------

func autostartPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "autostart", AppID+".desktop")
}

func autostartEnabled() bool {
	b, err := os.ReadFile(autostartPath())
	return err == nil && !strings.Contains(string(b), "Hidden=true")
}

// setAutostart starts Klient hidden (tray only) after logging in to GNOME.
func setAutostart(on bool) error {
	p := autostartPath()
	if !on {
		err := os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe := "klient"
	if path, err := os.Executable(); err == nil && !strings.HasPrefix(path, "/usr/") {
		exe = path // a development build
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Klient
Comment=Klient mail in the background
Comment[cs]=Klient – pošta na pozadí
Exec=%s --background
Icon=%s
X-GNOME-Autostart-enabled=true
X-GNOME-Autostart-Delay=5
NoDisplay=true
`, exe, AppID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(entry), 0o644)
}
