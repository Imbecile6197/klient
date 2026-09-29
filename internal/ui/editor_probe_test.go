//go:build probe

package ui

import (
	"fmt"
	"os"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/richtext"
)

func TestEditorProbe(t *testing.T) {
	app := gtk.NewApplication("eu.libormacak.KlientProbe2", gio.ApplicationNonUnique)
	app.ConnectActivate(func() {
		win := gtk.NewApplicationWindow(app)
		e := newRichEditor(win)
		win.SetChild(e.view)
		e.SetText("Ahoj světe\n• bod\nodkaz")
		e.buf.ApplyTag(e.tags["bold"], e.buf.IterAtOffset(5), e.buf.IterAtOffset(10))
		e.buf.ApplyTag(e.tags["italic"], e.buf.IterAtOffset(0), e.buf.IterAtOffset(4))
		tag := gtk.NewTextTag("")
		tag.SetObjectProperty("underline", pango.UnderlineSingle)
		tag.SetObjectProperty("foreground", "#1c71d8")
		e.buf.TagTable().Add(tag)
		e.links[tag.Native()] = "https://example.com"
		e.buf.ApplyTag(tag, e.buf.IterAtOffset(17), e.buf.IterAtOffset(22))
		spans := e.Spans()
		fmt.Fprintln(os.Stderr, "HTML:", richtext.HTML(spans))
		fmt.Fprintln(os.Stderr, "PLAIN:", richtext.Plain(spans))
		app.Quit()
	})
	app.Run(nil)
}
