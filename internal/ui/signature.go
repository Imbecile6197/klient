package ui

import (
	"context"
	"html"
	"os"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/richtext"
)

// htmlSignature is an HTML signature placed in the composer: a preview of
// it sits in the text at an anchor and goes into the message as HTML.
type htmlSignature struct {
	anchor *gtk.TextChildAnchor
	html   string // cleaned, wrapped for the message body
	plain  string // the text/plain version, with the "-- " separator
}

// htmlSignatureOf returns the user's HTML signature ready for a message, or
// "" when the plain-text one is used.
func (a *App) htmlSignatureOf() (html, plain string) {
	if !a.cfg.SignatureUseHTML {
		return "", ""
	}
	sig := richtext.CleanSignature(a.cfg.SignatureHTML)
	if sig == "" {
		return "", ""
	}
	text, _ := mailparse.HTMLToText(sig)
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return richtext.SignatureHTML(sig), "-- \n" + strings.Join(lines, "\n")
}

// signaturePreview renders a signature as the recipients will see it.
// Remote images are shown: it is the user's own content.
func signaturePreview(html string) gtk.Widgetter {
	view := newHTMLView(mailparse.PrepareHTML(html, nil, true), func(string) {})
	view.SetCanTarget(false) // links are not followed from the preview
	return view
}

// insertSignature puts the HTML signature at a character offset.
func (e *richEditor) insertSignature(offset int, html, plain string) {
	anchor := e.buf.CreateChildAnchor(e.buf.IterAtOffset(offset))
	e.sig = &htmlSignature{anchor: anchor, html: html, plain: plain}

	box := gtk.NewBox(gtk.OrientationVertical, 2)
	box.SetSizeRequest(520, -1)
	head := gtk.NewBox(gtk.OrientationHorizontal, 6)
	title := gtk.NewLabel(i18n.T("Signature"))
	title.AddCSSClass("dim-label")
	title.AddCSSClass("caption")
	title.SetHExpand(true)
	title.SetXAlign(0)
	remove := gtk.NewButtonFromIconName("window-close-symbolic")
	remove.AddCSSClass("flat")
	remove.AddCSSClass("circular")
	remove.SetTooltipText(i18n.T("Remove Signature"))
	remove.ConnectClicked(func() { e.removeSignature() })
	head.Append(title)
	head.Append(remove)
	box.Append(head)
	box.Append(signaturePreview(html))
	e.view.AddChildAtAnchor(box, anchor)
}

func (e *richEditor) removeSignature() {
	if e.sig == nil || e.sig.anchor.Deleted() {
		return
	}
	start := e.buf.IterAtChildAnchor(e.sig.anchor)
	end := e.buf.IterAtChildAnchor(e.sig.anchor)
	end.ForwardChar()
	e.buf.Delete(start, end)
	e.sig = nil
}

// signatureSpan is the span of the signature when it is at the iterator.
func (e *richEditor) signatureSpan(it *gtk.TextIter) (richtext.Span, bool) {
	a := it.ChildAnchor()
	if a == nil || e.sig == nil || a.Native() != e.sig.anchor.Native() {
		return richtext.Span{}, false
	}
	return richtext.Span{Text: e.sig.plain, HTML: e.sig.html}, true
}

// signatureGroup is the Preferences group for the signature: plain text, or
// HTML with a live preview.
func (a *App) signatureGroup(d *adw.PreferencesDialog) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Signature"))
	g.SetDescription(i18n.T("Added below new messages, replies and forwards."))

	format := adw.NewComboRow()
	format.SetTitle(i18n.T("Format"))
	format.SetModel(gtk.NewStringList([]string{i18n.T("Plain Text"), i18n.T("HTML")}))
	g.Add(format)

	textView := sourceView(a.cfg.Signature, false)
	htmlView := sourceView(a.cfg.SignatureHTML, true)

	// HTML: the source, buttons, and the preview below.
	preview := newHTMLView(mailparse.PrepareHTML("", nil, true), func(string) {})
	preview.SetCanTarget(false)
	remoteNote := gtk.NewLabel(i18n.T("Images from the internet are blocked by many recipients' mail apps until they allow them."))
	remoteNote.AddCSSClass("dim-label")
	remoteNote.AddCSSClass("caption")
	remoteNote.SetWrap(true)
	remoteNote.SetXAlign(0)
	var pending glib.SourceHandle
	refresh := func() {
		sig := richtext.CleanSignature(bufferText(htmlView.Buffer()))
		preview.LoadHtml(mailparse.PrepareHTML(richtext.SignatureHTML(sig), nil, true), "about:blank")
		remoteNote.SetVisible(mailparse.HasRemoteContent(sig))
	}
	htmlView.Buffer().ConnectChanged(func() {
		if pending != 0 {
			glib.SourceRemove(pending)
		}
		pending = glib.TimeoutAdd(400, func() bool {
			pending = 0
			refresh()
			return false
		})
	})
	refresh()

	open := gtk.NewButtonWithLabel(i18n.T("Open File…"))
	open.ConnectClicked(func() {
		dlg := gtk.NewFileDialog()
		dlg.SetTitle(i18n.T("HTML Signature"))
		filter := gtk.NewFileFilter()
		filter.SetName(i18n.T("Web Pages"))
		filter.AddMIMEType("text/html")
		filter.AddSuffix("html")
		filter.AddSuffix("htm")
		filters := gio.NewListStore(gtk.GTypeFileFilter)
		filters.Append(filter.Object)
		dlg.SetFilters(filters)
		dlg.Open(context.Background(), a.gtkWindow(), func(res gio.AsyncResulter) {
			file, err := dlg.OpenFinish(res)
			if err != nil || file == nil {
				return
			}
			b, err := os.ReadFile(file.Path())
			if err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			htmlView.Buffer().SetText(richtext.CleanSignature(string(b)))
		})
	})
	template := gtk.NewButtonWithLabel(i18n.T("Use a Template"))
	template.ConnectClicked(func() {
		htmlView.Buffer().SetText(a.signatureTemplate())
	})
	buttons := gtk.NewBox(gtk.OrientationHorizontal, 6)
	buttons.SetMarginTop(6)
	buttons.Append(open)
	buttons.Append(template)

	previewTitle := gtk.NewLabel(i18n.T("Preview"))
	previewTitle.AddCSSClass("heading")
	previewTitle.SetXAlign(0)
	previewTitle.SetMarginTop(12)
	previewFrame := gtk.NewFrame("")
	previewFrame.SetChild(preview)

	htmlBox := gtk.NewBox(gtk.OrientationVertical, 4)
	// The source scrolls: a long signature must not stretch the page.
	src := gtk.NewScrolledWindow()
	src.SetChild(htmlView)
	src.SetSizeRequest(-1, 220)
	srcFrame := gtk.NewFrame("")
	srcFrame.SetChild(src)
	htmlBox.Append(srcFrame)
	htmlBox.Append(buttons)
	htmlBox.Append(previewTitle)
	htmlBox.Append(previewFrame)
	htmlBox.Append(remoteNote)

	stack := gtk.NewStack()
	stack.SetVhomogeneous(false)
	stack.SetMarginTop(12)
	stack.AddNamed(framed(textView, 110), "text")
	stack.AddNamed(htmlBox, "html")
	showMode := func() {
		if format.Selected() == 1 {
			stack.SetVisibleChildName("html")
		} else {
			stack.SetVisibleChildName("text")
		}
	}
	if a.cfg.SignatureUseHTML {
		format.SetSelected(1)
	}
	showMode()
	format.NotifyProperty("selected", showMode)

	save := gtk.NewButtonWithLabel(i18n.T("Save Signature"))
	save.SetHAlign(gtk.AlignEnd)
	save.SetMarginTop(6)
	save.ConnectClicked(func() {
		a.cfg.Signature = strings.TrimSpace(bufferText(textView.Buffer()))
		a.cfg.SignatureHTML = richtext.CleanSignature(bufferText(htmlView.Buffer()))
		a.cfg.SignatureUseHTML = format.Selected() == 1
		a.saveConfig()
		d.AddToast(adw.NewToast(i18n.T("Signature saved")))
	})
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(stack)
	box.Append(save)
	g.Add(box)
	return g
}

func sourceView(text string, code bool) *gtk.TextView {
	view := gtk.NewTextView()
	view.SetWrapMode(gtk.WrapWordChar)
	view.SetTopMargin(10)
	view.SetBottomMargin(10)
	view.SetLeftMargin(10)
	view.SetRightMargin(10)
	view.SetMonospace(code)
	view.Buffer().SetText(text)
	return view
}

func framed(view *gtk.TextView, height int) *gtk.Frame {
	view.SetSizeRequest(-1, height)
	frame := gtk.NewFrame("")
	frame.SetChild(view)
	return frame
}

func bufferText(buf *gtk.TextBuffer) string {
	start, end := buf.Bounds()
	return buf.Text(start, end, false)
}

// signatureTemplate is a starting point filled in with the user's first
// sending address: initials in a circle, the name and the address.
func (a *App) signatureTemplate() string {
	name, email := i18n.T("Your Name"), "name@example.com"
	if a.acc != nil {
		if addrs := a.acc.SendAddresses(); len(addrs) > 0 {
			email = addrs[0].Email
			if addrs[0].DisplayName != "" {
				name = addrs[0].DisplayName
			}
		}
	}
	initials := ""
	for _, w := range strings.Fields(name) {
		if r := []rune(w); len(r) > 0 && len([]rune(initials)) < 2 {
			initials += strings.ToUpper(string(r[0]))
		}
	}
	esc := html.EscapeString
	return `<table cellpadding="0" cellspacing="0" border="0" role="presentation" style="border-collapse:collapse;font-family:Arial,Helvetica,sans-serif;color:#2e3436">
  <tr>
    <td style="padding:0 14px 0 0;border-right:3px solid #3584e4;vertical-align:middle">
      <table cellpadding="0" cellspacing="0" border="0" role="presentation" style="border-collapse:collapse"><tr>
        <td width="54" height="54" align="center" valign="middle" style="width:54px;height:54px;border-radius:27px;background:#3584e4;color:#ffffff;font-size:21px;font-weight:bold">` + esc(initials) + `</td>
      </tr></table>
    </td>
    <td style="padding:0 0 0 14px;vertical-align:middle">
      <div style="font-size:16px;font-weight:bold;line-height:22px">` + esc(name) + `</div>
      <div style="font-size:13px;line-height:20px;color:#5e5c64">
        <a href="mailto:` + esc(email) + `" style="color:#1c71d8;text-decoration:none">` + esc(email) + `</a>
      </div>
    </td>
  </tr>
</table>`
}
