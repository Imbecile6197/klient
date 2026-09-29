package ui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/relnotes"
	"github.com/Imbecile6197/klient/internal/update"
)

// releaseNotes describes the current version for About → What's New.
func releaseNotes() string {
	rels := relnotes.Embedded(i18n.Lang(), "", Version, update.Newer)
	if len(rels) == 0 || rels[0].Version != Version {
		return ""
	}
	return relnotes.AppStream(rels[0].Blocks)
}

// checkWhatsNew runs at start: after an update it shows what changed since
// the version that ran last (in the window, or as a notification when
// Klient starts in the background). A fresh installation shows nothing.
func (a *App) checkWhatsNew() {
	if strings.Contains(Version, "dev") {
		return
	}
	last := a.cfg.LastRunVersion
	if last == Version {
		return
	}
	a.cfg.LastRunVersion = Version
	a.saveConfig()
	var rels []relnotes.Release
	switch {
	case last == "" && len(a.cfg.Accounts) == 0:
		return // first start ever
	case last == "":
		// Updated from a version that did not remember itself yet.
		rels = relnotes.Embedded(i18n.Lang(), "", Version, update.Newer)
		if len(rels) > 1 {
			rels = rels[:1]
		}
	case update.Newer(Version, last):
		rels = relnotes.Embedded(i18n.Lang(), last, Version, update.Newer)
	}
	if len(rels) == 0 {
		return
	}
	a.whatsNew = rels
	if a.win.IsVisible() {
		glib.TimeoutSecondsAdd(1, func() bool {
			a.showWhatsNew()
			return false
		})
		return
	}
	n := gio.NewNotification(fmt.Sprintf(i18n.T("Klient has been updated to version %s"), Version))
	n.SetBody(i18n.T("Click to see what's new."))
	n.SetDefaultAction("app.whats-new")
	a.app.SendNotification("whats-new", n)
}

// showWhatsNew shows the changes of the last update, or of this version.
func (a *App) showWhatsNew() {
	rels := a.whatsNew
	if len(rels) == 0 {
		rels = relnotes.Embedded(i18n.Lang(), "", Version, update.Newer)
		if len(rels) > 1 {
			rels = rels[:1]
		}
	}
	a.showWindow()
	a.app.WithdrawNotification("whats-new")
	a.notesDialog(fmt.Sprintf(i18n.T("What's New in Klient %s"), Version), rels, "", nil)
}

// showUpdateNotes shows the notes of a downloaded update before installing.
func (a *App) showUpdateNotes() {
	if a.update.path == "" {
		return
	}
	rels := []relnotes.Release{{Version: a.update.version, Blocks: relnotes.Parse(a.update.notes, i18n.Lang())}}
	a.showWindow()
	a.notesDialog(fmt.Sprintf(i18n.T("What's New in Version %s"), a.update.version), rels, i18n.T("Install"), a.installUpdate)
}

// notesDialog lists release notes, newest first. action (may be empty)
// adds a suggested button that runs do and closes the dialog.
func (a *App) notesDialog(title string, rels []relnotes.Release, action string, do func()) {
	d := adw.NewDialog()
	d.SetTitle(title)
	d.SetContentWidth(520)
	d.SetContentHeight(560)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())

	box := gtk.NewBox(gtk.OrientationVertical, 6)
	box.SetMarginTop(12)
	box.SetMarginBottom(18)
	box.SetMarginStart(18)
	box.SetMarginEnd(18)
	label := func(markup string, classes ...string) *gtk.Label {
		l := gtk.NewLabel("")
		l.SetMarkup(markup)
		l.SetWrap(true)
		l.SetXAlign(0)
		l.SetSelectable(true)
		for _, c := range classes {
			l.AddCSSClass(c)
		}
		return l
	}
	for i, r := range rels {
		if len(rels) > 1 || r.Version != Version {
			h := label(glib.MarkupEscapeText(fmt.Sprintf(i18n.T("Version %s"), r.Version)), "title-4")
			if i > 0 {
				h.SetMarginTop(12)
			}
			box.Append(h)
		}
		for _, b := range r.Blocks {
			if b.Bullet {
				row := gtk.NewBox(gtk.OrientationHorizontal, 8)
				dot := gtk.NewLabel("•")
				dot.SetVAlign(gtk.AlignStart)
				row.Append(dot)
				row.Append(label(relnotes.Markup(b.Text)))
				box.Append(row)
			} else {
				p := label(relnotes.Markup(b.Text))
				p.SetMarginBottom(4)
				box.Append(p)
			}
		}
	}
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetVExpand(true)
	sw.SetChild(box)
	tv.SetContent(sw)

	if action != "" && do != nil {
		btn := gtk.NewButtonWithLabel(action)
		btn.AddCSSClass("suggested-action")
		btn.AddCSSClass("pill")
		btn.SetHAlign(gtk.AlignCenter)
		btn.SetMarginTop(6)
		btn.SetMarginBottom(12)
		btn.ConnectClicked(func() {
			d.Close()
			do()
		})
		tv.AddBottomBar(btn)
	}
	d.SetChild(tv)
	d.Present(a.win)
}
