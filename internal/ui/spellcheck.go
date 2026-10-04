package ui

/*
#cgo pkg-config: gtk4
#include <stdint.h>
#include <gtk/gtk.h>

// The Go bindings cannot pass a GdkRGBA property: set it here.
static void klient_underline_red(uintptr_t tag) {
	GdkRGBA red = {0.88, 0.11, 0.14, 1.0}; // GNOME's error red
	g_object_set((gpointer)tag, "underline-rgba", &red, NULL);
}
*/
import "C"

import (
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/spell"
)

// spellChecker returns the spell checker (nil when it is off or no
// dictionary is installed). The interface language comes first, English is
// always checked too: a word is right in either.
func (a *App) spellChecker() *spell.Checker {
	if !a.cfg.SpellCheck {
		return nil
	}
	if a.spell == nil && !a.spellTried {
		a.spellTried = true
		langs := []string{"en_US", "en_GB"}
		if i18n.Lang() == "cs" {
			langs = append([]string{"cs_CZ"}, langs...)
		} else {
			langs = append(langs, "cs_CZ")
		}
		a.spell = spell.New(langs)
	}
	return a.spell
}

// spellState is the spell checking of one editor.
type spellState struct {
	checker *spell.Checker
	tag     *gtk.TextTag
	pending glib.SourceHandle
	// The word under the last right click, for the menu actions.
	start, end int
	word       string
}

// enableSpell underlines misspelled words and offers corrections in the
// right-click menu.
func (e *richEditor) enableSpell(c *spell.Checker) {
	if c == nil {
		return
	}
	s := &spellState{checker: c}
	e.spellSt = s
	s.tag = gtk.NewTextTag("misspelled")
	s.tag.SetObjectProperty("underline", pango.UnderlineError)
	C.klient_underline_red(C.uintptr_t(s.tag.Native()))
	e.buf.TagTable().Add(s.tag)

	// Check again a moment after typing stops.
	e.buf.ConnectChanged(func() {
		if s.pending != 0 {
			glib.SourceRemove(s.pending)
		}
		s.pending = glib.TimeoutAdd(400, func() bool {
			s.pending = 0
			e.checkSpelling()
			return false
		})
	})

	group := gio.NewSimpleActionGroup()
	replace := gio.NewSimpleAction("replace", glib.NewVariantType("s"))
	replace.ConnectActivate(func(p *glib.Variant) {
		if p == nil {
			return
		}
		start, end := e.buf.IterAtOffset(s.start), e.buf.IterAtOffset(s.end)
		if e.buf.Text(start, end, false) != s.word {
			return // the text changed meanwhile
		}
		e.buf.BeginUserAction()
		e.buf.Delete(start, end)
		at := e.buf.IterAtOffset(s.start)
		e.buf.Insert(at, p.String())
		e.buf.EndUserAction()
	})
	add := gio.NewSimpleAction("add", nil)
	add.ConnectActivate(func(*glib.Variant) {
		s.checker.Add(s.word)
		e.checkSpelling()
	})
	ignore := gio.NewSimpleAction("ignore", nil)
	ignore.ConnectActivate(func(*glib.Variant) {
		s.checker.Ignore(s.word)
		e.checkSpelling()
	})
	group.AddAction(replace)
	group.AddAction(add)
	group.AddAction(ignore)
	e.view.InsertActionGroup("spell", group)

	// Right click: put the corrections of the word under the pointer at the
	// top of the text view's own menu.
	click := gtk.NewGestureClick()
	click.SetButton(gdk.BUTTON_SECONDARY)
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(n int, x, y float64) {
		bx, by := e.view.WindowToBufferCoords(gtk.TextWindowWidget, int(x), int(y))
		it, ok := e.view.IterAtLocation(bx, by)
		e.view.SetExtraMenu(nil)
		if !ok || !it.HasTag(s.tag) {
			return
		}
		off := it.Offset()
		for _, w := range spell.Words(e.allText()) {
			if off < w.Start || off >= w.End {
				continue
			}
			s.start, s.end, s.word = w.Start, w.End, w.Text
			e.view.SetExtraMenu(spellMenu(s.checker.Suggest(w.Text, 6)))
			return
		}
	})
	e.view.AddController(click)
	e.checkSpelling()
}

func spellMenu(suggestions []string) *gio.Menu {
	menu := gio.NewMenu()
	fixes := gio.NewMenu()
	if len(suggestions) == 0 {
		item := gio.NewMenuItem(i18n.T("(no suggestions)"), "spell.none")
		fixes.AppendItem(item)
	}
	for _, s := range suggestions {
		item := gio.NewMenuItem(s, "")
		item.SetActionAndTargetValue("spell.replace", glib.NewVariantString(s))
		fixes.AppendItem(item)
	}
	menu.AppendSection("", fixes)
	more := gio.NewMenu()
	more.Append(i18n.T("Add to Dictionary"), "spell.add")
	more.Append(i18n.T("Ignore"), "spell.ignore")
	menu.AppendSection("", more)
	return menu
}

func (e *richEditor) allText() string {
	return e.buf.Text(e.buf.StartIter(), e.buf.EndIter(), false)
}

// checkSpelling underlines every misspelled word again.
func (e *richEditor) checkSpelling() {
	s := e.spellSt
	if s == nil {
		return
	}
	e.buf.RemoveTag(s.tag, e.buf.StartIter(), e.buf.EndIter())
	cursor := e.buf.IterAtMark(e.buf.GetInsert()).Offset()
	for _, w := range spell.Words(e.allText()) {
		// Not the word being typed right now.
		if cursor > w.Start && cursor <= w.End && cursor == e.buf.CharCount() {
			continue
		}
		if !s.checker.Check(w.Text) {
			e.buf.ApplyTag(s.tag, e.buf.IterAtOffset(w.Start), e.buf.IterAtOffset(w.End))
		}
	}
}

// langNames turns dictionary tags into names in the interface language.
func langNames(tags []string) []string {
	names := map[string]string{
		"cs_CZ": i18n.T("Czech"), "en_US": i18n.T("English (US)"), "en_GB": i18n.T("English (UK)"),
		"sk_SK": i18n.T("Slovak"), "de_DE": i18n.T("German"),
	}
	var out []string
	for _, t := range tags {
		if n, ok := names[t]; ok {
			out = append(out, n)
		} else {
			out = append(out, t)
		}
	}
	return out
}
