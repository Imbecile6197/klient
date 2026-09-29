package ui

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/demo"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/relnotes"
	"github.com/Imbecile6197/klient/internal/secrets"
	"github.com/Imbecile6197/klient/internal/spam"
	"github.com/Imbecile6197/klient/internal/update"
)

// The demo mode (KLIENT_DEMO=1) runs Klient with a made-up account, for
// screenshots and for trying it out. It uses its own temporary settings,
// data and in-memory secrets, so it never touches the user's accounts,
// keyring or configuration, and a separate application ID, so it can run
// next to the user's Klient. KLIENT_SCREENSHOTS=<dir> additionally saves
// screenshots of the main screens there and quits.

func demoMode() bool { return os.Getenv("KLIENT_DEMO") != "" }

// PrepareDemo points the settings, data and cache directories to a fresh
// temporary directory and keeps secrets in memory. It must run before the
// configuration is loaded.
func PrepareDemo() error {
	realData := config.DataDir()
	dir, err := os.MkdirTemp("", "klient-demo-")
	if err != nil {
		return err
	}
	for env, sub := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
		os.Setenv(env, filepath.Join(dir, sub))
	}
	secrets.UseMemory()
	// The public blocklists make the filter status look real.
	src := filepath.Join(realData, "blocklists")
	dst := filepath.Join(config.DataDir(), "blocklists")
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if b, err := os.ReadFile(path); err == nil {
			_ = os.WriteFile(filepath.Join(dst, rel), b, 0o644)
		}
		return nil
	})
	return nil
}

// startDemo shows the demo account instead of resuming real ones.
func (a *App) startDemo() {
	a.cfg.AssistantProvider, a.cfg.AssistantModel = ai.ProviderOllama, "qwen3.5:4b"
	a.cfg.SpamProvider, a.cfg.SpamModel = ai.ProviderOllama, "qwen3.5:4b"
	a.cfg.LocalOnly = true
	a.initAI()
	_ = a.lists.LoadCached(a.cfg.Feeds)

	acc := demo.New()
	for _, v := range acc.Verdicts() {
		a.filter.Remember(spam.Decision{
			MessageID: v.ID, From: v.From, Subject: v.Subject, Spam: v.Spam, Source: "ai", Time: time.Now(),
			Verdict: ai.Verdict{SpamProbability: v.Probability, Category: v.Category, Reason: v.Reason},
		})
	}
	a.cfg.Accounts = []string{acc.Username()}
	a.addSession(acc)
	a.switchAccount(acc)
	if dir := os.Getenv("KLIENT_SCREENSHOTS"); dir != "" {
		a.demoScreenshots(dir)
	}
}

// demoScreenshots walks through the main screens and saves each one.
func (a *App) demoScreenshots(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	shot := func(w gtk.Widgetter, name string) {
		if w == gtk.Widgetter(a.win) {
			a.win.Present()
		}
		path := filepath.Join(dir, name+".png")
		if err := saveWidgetPNG(w, path, 1.25); err != nil {
			log.Printf("screenshot %s: %v", name, err)
			return
		}
		log.Printf("screenshot %s", path)
	}
	openFirst := func() {
		if a.mv == nil || a.mv.msgList == nil {
			return
		}
		if row := a.mv.msgList.RowAtIndex(0); row != nil {
			a.mv.msgList.SelectRow(row)
			row.Activate()
		}
	}
	var dialog interface{ Close() bool }
	steps := []struct {
		wait time.Duration
		do   func()
	}{
		{3 * time.Second, openFirst},
		{3 * time.Second, func() { shot(a.win, "main") }},
		{0, func() { a.mv.reply(protonmail.ActionReply) }},
		{2 * time.Second, func() {
			if w := newestToplevel(a.win); w != nil {
				shot(w, "compose")
				if d, ok := w.(interface{ Destroy() }); ok {
					d.Destroy()
				}
			} else {
				log.Print("screenshot compose: no compose window")
			}
			a.win.Present()
		}},
		{0, func() {
			d := a.preferencesDialog()
			d.SetVisiblePageName("ai")
			dialog = d
		}},
		{2 * time.Second, func() { shot(a.win, "preferences"); dialog.Close() }},
		{0, func() {
			rels := relnotes.Embedded(i18n.Lang(), "", Version, update.Newer)
			if len(rels) > 1 {
				rels = rels[:1]
			}
			dialog = a.notesDialog(fmt.Sprintf(i18n.T("What's New in Klient %s"), Version), rels, "", nil)
		}},
		{2 * time.Second, func() { shot(a.win, "whats-new"); dialog.Close() }},
		{0, func() {
			a.win.Present()
			a.mv.openFolder(protonmail.FolderByID(protonmail.SpamID))
		}},
		{2 * time.Second, openFirst},
		{time.Second, func() { a.win.Present(); a.win.QueueResize() }},
		{3 * time.Second, func() { shot(a.win, "spam") }},
		{0, func() {
			adw.StyleManagerGetDefault().SetColorScheme(adw.ColorSchemeForceDark)
			a.mv.openFolder(protonmail.FolderByID(protonmail.InboxID))
		}},
		{2 * time.Second, openFirst},
		{3 * time.Second, func() { shot(a.win, "main-dark") }},
		{time.Second, func() { a.quit() }},
	}
	var run func(i int)
	run = func(i int) {
		if i >= len(steps) {
			return
		}
		st := steps[i]
		glib.TimeoutAdd(uint(st.wait.Milliseconds()), func() bool {
			st.do()
			run(i + 1)
			return false
		})
	}
	run(0)
}

// newestToplevel returns a visible window other than main (e.g. the
// compose window).
func newestToplevel(main *adw.ApplicationWindow) gtk.Widgetter {
	mainPtr := coreglib.InternObject(main).Native()
	list := gtk.WindowListToplevels()
	for i := len(list) - 1; i >= 0; i-- {
		w := list[i]
		if coreglib.InternObject(w).Native() != mainPtr && gtk.BaseWidget(w).IsVisible() {
			return w
		}
	}
	return nil
}
