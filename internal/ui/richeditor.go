package ui

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/richtext"
)

// richEditor is a GtkTextView with a formatting toolbar. Styles live in
// text tags; Spans() turns them into richtext spans for HTML export.
type richEditor struct {
	view    *gtk.TextView
	buf     *gtk.TextBuffer
	toolbar *gtk.Box
	tags    map[string]*gtk.TextTag // bold, italic, underline, strike
	active  map[string]bool         // styles applied to newly typed text
	toggles map[string]*gtk.ToggleButton
	links   map[uintptr]string // link tag (native pointer) -> URL
	parent  gtk.Widgetter
	spellSt *spellState    // nil: no spell checking
	sig     *htmlSignature // nil: no HTML signature
}

func newRichEditor(parent gtk.Widgetter) *richEditor {
	e := &richEditor{
		view: gtk.NewTextView(), tags: map[string]*gtk.TextTag{}, active: map[string]bool{},
		toggles: map[string]*gtk.ToggleButton{}, links: map[uintptr]string{}, parent: parent,
	}
	e.buf = e.view.Buffer()
	e.view.SetWrapMode(gtk.WrapWordChar)
	e.view.SetTopMargin(12)
	e.view.SetBottomMargin(12)
	e.view.SetLeftMargin(12)
	e.view.SetRightMargin(12)
	e.view.SetVExpand(true)

	table := e.buf.TagTable()
	mk := func(name string, set func(t *gtk.TextTag)) {
		t := gtk.NewTextTag(name)
		set(t)
		table.Add(t)
		e.tags[name] = t
	}
	mk("bold", func(t *gtk.TextTag) { t.SetObjectProperty("weight", int(pango.WeightBold)) })
	mk("italic", func(t *gtk.TextTag) { t.SetObjectProperty("style", pango.StyleItalic) })
	mk("underline", func(t *gtk.TextTag) { t.SetObjectProperty("underline", pango.UnderlineSingle) })
	mk("strike", func(t *gtk.TextTag) { t.SetObjectProperty("strikethrough", true) })

	e.toolbar = gtk.NewBox(gtk.OrientationHorizontal, 2)
	e.toolbar.AddCSSClass("toolbar")
	for _, b := range []struct{ name, icon, tip, accel string }{
		{"bold", "format-text-bold-symbolic", i18n.T("Bold (Ctrl+B)"), "<Control>b"},
		{"italic", "format-text-italic-symbolic", i18n.T("Italic (Ctrl+I)"), "<Control>i"},
		{"underline", "format-text-underline-symbolic", i18n.T("Underline (Ctrl+U)"), "<Control>u"},
		{"strike", "format-text-strikethrough-symbolic", i18n.T("Strikethrough"), ""},
	} {
		b := b
		tb := gtk.NewToggleButton()
		tb.SetIconName(b.icon)
		tb.SetTooltipText(b.tip)
		tb.AddCSSClass("flat")
		tb.ConnectToggled(func() { e.setStyle(b.name, tb.Active()) })
		e.toggles[b.name] = tb
		e.toolbar.Append(tb)
		if b.accel != "" {
			e.shortcut(b.accel, func() { tb.SetActive(!tb.Active()) })
		}
	}
	sep := gtk.NewSeparator(gtk.OrientationVertical)
	e.toolbar.Append(sep)
	list := gtk.NewButtonFromIconName("view-list-bullet-symbolic")
	list.SetTooltipText(i18n.T("Bulleted List"))
	list.AddCSSClass("flat")
	list.ConnectClicked(e.toggleBullets)
	e.toolbar.Append(list)
	link := gtk.NewButtonFromIconName("insert-link-symbolic")
	link.SetTooltipText(i18n.T("Link (Ctrl+K)"))
	link.AddCSSClass("flat")
	link.ConnectClicked(e.insertLink)
	e.toolbar.Append(link)
	e.shortcut("<Control>k", e.insertLink)
	clear := gtk.NewButtonFromIconName("edit-clear-all-symbolic")
	clear.SetTooltipText(i18n.T("Clear Formatting"))
	clear.AddCSSClass("flat")
	clear.ConnectClicked(func() {
		if s, en, ok := e.buf.SelectionBounds(); ok {
			e.buf.RemoveAllTags(s, en)
		}
		for name, tb := range e.toggles {
			e.active[name] = false
			tb.SetActive(false)
		}
	})
	e.toolbar.Append(clear)

	// Typed text takes the active styles.
	e.buf.ConnectInsertText(func(loc *gtk.TextIter, text string, _ int) {
		start := loc.Offset()
		n := utf8.RuneCountInString(text)
		var names []string
		for name, on := range e.active {
			if on {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return
		}
		glib.IdleAdd(func() {
			s, en := e.buf.IterAtOffset(start), e.buf.IterAtOffset(start+n)
			for _, name := range names {
				e.buf.ApplyTag(e.tags[name], s, en)
			}
		})
	})
	// Toolbar reflects the style at the cursor.
	e.buf.NotifyProperty("cursor-position", func() {
		if _, _, sel := e.buf.SelectionBounds(); sel {
			return
		}
		it := e.buf.IterAtMark(e.buf.GetInsert())
		if it.Offset() > 0 {
			it.BackwardChar()
		}
		for name, tb := range e.toggles {
			on := it.HasTag(e.tags[name])
			e.active[name] = on
			if tb.Active() != on {
				tb.SetActive(on)
			}
		}
	})
	return e
}

func (e *richEditor) shortcut(accel string, f func()) {
	sc := gtk.NewShortcutController()
	sc.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString(accel),
		gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { f(); return true })))
	e.view.AddController(sc)
}

// setStyle applies a style to the selection, or to text typed next.
func (e *richEditor) setStyle(name string, on bool) {
	e.active[name] = on
	if s, en, ok := e.buf.SelectionBounds(); ok {
		if on {
			e.buf.ApplyTag(e.tags[name], s, en)
		} else {
			e.buf.RemoveTag(e.tags[name], s, en)
		}
	}
	e.view.GrabFocus()
}

// toggleBullets adds or removes "• " at the start of the selected lines.
func (e *richEditor) toggleBullets() {
	s, en, ok := e.buf.SelectionBounds()
	if !ok {
		s = e.buf.IterAtMark(e.buf.GetInsert())
		en = s.Copy()
	}
	first, last := s.Line(), en.Line()
	all := true
	for l := first; l <= last; l++ {
		it, _ := e.buf.IterAtLine(l)
		end := it.Copy()
		end.ForwardChars(len([]rune(richtext.Bullet)))
		if e.buf.Text(it, end, false) != richtext.Bullet {
			all = false
		}
	}
	for l := first; l <= last; l++ {
		it, _ := e.buf.IterAtLine(l)
		if all {
			end := it.Copy()
			end.ForwardChars(len([]rune(richtext.Bullet)))
			e.buf.Delete(it, end)
		} else {
			end := it.Copy()
			end.ForwardChars(len([]rune(richtext.Bullet)))
			if e.buf.Text(it, end, false) != richtext.Bullet {
				e.buf.Insert(it, richtext.Bullet)
			}
		}
	}
	e.view.GrabFocus()
}

// insertLink turns the selection into a link, or inserts a new one.
func (e *richEditor) insertLink() {
	s, en, hasSel := e.buf.SelectionBounds()
	selText := ""
	if hasSel {
		selText = e.buf.Text(s, en, false)
	}
	startOff, endOff := 0, 0
	if hasSel {
		startOff, endOff = s.Offset(), en.Offset()
	}
	d := adw.NewAlertDialog(i18n.T("Insert Link"), "")
	box := gtk.NewBox(gtk.OrientationVertical, 6)
	textEntry := gtk.NewEntry()
	textEntry.SetPlaceholderText(i18n.T("Link text"))
	textEntry.SetText(selText)
	urlEntry := gtk.NewEntry()
	urlEntry.SetPlaceholderText("https://…")
	urlEntry.SetActivatesDefault(true)
	if strings.HasPrefix(selText, "http") {
		urlEntry.SetText(selText)
	}
	box.Append(textEntry)
	box.Append(urlEntry)
	d.SetExtraChild(box)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("ok", i18n.T("Insert"))
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetDefaultResponse("ok")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "ok" {
			return
		}
		target := strings.TrimSpace(urlEntry.Text())
		if u, err := url.Parse(target); err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
			if !strings.Contains(target, "://") && target != "" {
				target = "https://" + target
			} else {
				return
			}
		}
		text := textEntry.Text()
		if text == "" {
			text = target
		}
		tag := gtk.NewTextTag("")
		tag.SetObjectProperty("underline", pango.UnderlineSingle)
		tag.SetObjectProperty("foreground", "#1c71d8")
		e.buf.TagTable().Add(tag)
		e.links[tag.Native()] = target
		if hasSel {
			a, b := e.buf.IterAtOffset(startOff), e.buf.IterAtOffset(endOff)
			e.buf.Delete(a, b)
		}
		at := e.buf.IterAtOffset(startOff)
		if !hasSel {
			at = e.buf.IterAtMark(e.buf.GetInsert())
		}
		off := at.Offset()
		e.buf.Insert(at, text)
		e.buf.ApplyTag(tag, e.buf.IterAtOffset(off), e.buf.IterAtOffset(off+utf8.RuneCountInString(text)))
		e.view.GrabFocus()
	})
	d.Present(e.parent)
}

// SetText replaces the content with unformatted text.
func (e *richEditor) SetText(s string) { e.buf.SetText(s) }

// Text returns the plain content (as typed, without link targets).
func (e *richEditor) Text() string {
	s, en := e.buf.Bounds()
	return e.buf.Text(s, en, false)
}

// Spans exports the content with its styles.
func (e *richEditor) Spans() []richtext.Span {
	var spans []richtext.Span
	it := e.buf.StartIter()
	for !it.IsEnd() {
		if sp, ok := e.signatureSpan(it); ok {
			spans = append(spans, sp)
			it.ForwardChar()
			continue
		}
		if it.ChildAnchor() != nil {
			it.ForwardChar() // another embedded widget: not part of the text
			continue
		}
		var sp richtext.Span
		// gotk4 returns a fresh wrapper per call, so compare native pointers.
		for _, t := range it.Tags() {
			switch t.Native() {
			case e.tags["bold"].Native():
				sp.Bold = true
			case e.tags["italic"].Native():
				sp.Italic = true
			case e.tags["underline"].Native():
				sp.Underline = true
			case e.tags["strike"].Native():
				sp.Strike = true
			default:
				if u, ok := e.links[t.Native()]; ok {
					sp.Link = u
				}
			}
		}
		sp.Text = string(rune(it.Char()))
		if n := len(spans); n > 0 {
			last := &spans[n-1]
			if last.HTML == "" && last.Bold == sp.Bold && last.Italic == sp.Italic && last.Underline == sp.Underline &&
				last.Strike == sp.Strike && last.Link == sp.Link {
				last.Text += sp.Text
				it.ForwardChar()
				continue
			}
		}
		spans = append(spans, sp)
		it.ForwardChar()
	}
	return spans
}
