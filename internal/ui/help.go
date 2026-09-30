package ui

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/docs"
	"github.com/Imbecile6197/klient/internal/i18n"
)

// showHelp opens the user documentation in the interface language, in a
// window of its own. The pages are embedded in the binary and written to the
// cache directory, so they always match the running version. They are not
// handed to the web browser: a sandboxed browser (the Firefox snap on
// Ubuntu) receives a single file through the document portal and cannot
// follow the link to the other language or load the screenshots.
func (a *App) showHelp() {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	dir = filepath.Join(dir, "klient", "help")
	err = fs.WalkDir(docs.Files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := docs.Files.ReadFile(name)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	})
	if err != nil {
		a.toast(i18n.T("The help could not be prepared: ") + err.Error())
		return
	}
	page := gio.NewFileForPath(filepath.Join(dir, docs.Page(i18n.Lang()))).URI()
	if a.helpWin != nil {
		a.helpView.LoadURI(page)
		a.helpWin.Present()
		return
	}
	a.helpWin, a.helpView = a.helpWindow(dir, page)
	a.helpWin.Present()
}

// helpWindow shows the documentation pages from dir. Links between them stay
// in the window; web and mailto: links go to openLink.
func (a *App) helpWindow(dir, page string) (*adw.Window, *webkit.WebView) {
	win := adw.NewWindow()
	win.SetTitle(i18n.T("Help"))
	win.SetDefaultSize(1100, 800)
	win.SetHideOnClose(true)

	view := webkit.NewWebView()
	s := view.Settings()
	s.SetEnableDeveloperExtras(false)
	root := gio.NewFileForPath(dir).URI() + "/"
	view.ConnectDecidePolicy(func(d webkit.PolicyDecisioner, t webkit.PolicyDecisionType) bool {
		if t != webkit.PolicyDecisionTypeNavigationAction && t != webkit.PolicyDecisionTypeNewWindowAction {
			return false
		}
		nd, ok := d.(*webkit.NavigationPolicyDecision)
		if !ok {
			webkit.BasePolicyDecision(d).Ignore()
			return true
		}
		uri := nd.NavigationAction().Request().URI()
		if t == webkit.PolicyDecisionTypeNavigationAction && strings.HasPrefix(uri, root) {
			return false // our own pages and their anchors
		}
		webkit.BasePolicyDecision(d).Ignore()
		a.openLink(uri)
		return true
	})

	back := gtk.NewButtonFromIconName("go-previous-symbolic")
	back.SetTooltipText(i18n.T("Back"))
	back.SetSensitive(false)
	back.ConnectClicked(view.GoBack)
	view.ConnectLoadChanged(func(webkit.LoadEvent) { back.SetSensitive(view.CanGoBack()) })

	header := adw.NewHeaderBar()
	header.PackStart(back)
	tv := adw.NewToolbarView()
	tv.AddTopBar(header)
	view.SetVExpand(true)
	tv.SetContent(view)
	win.SetContent(tv)

	esc := gtk.NewShortcutController()
	esc.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { win.Close(); return true })))
	win.AddController(esc)

	view.LoadURI(page)
	return win, view
}
