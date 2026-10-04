package ui

import (
	_ "embed"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/mailbox"
)

// Service logos come from the system icon themes (GNOME Online Accounts
// ships Google's, Papirus has Proton's); services without one get a small
// badge drawn for Klient. No brand files are bundled.

//go:embed icons/provider-seznam.svg
var seznamBadge []byte

//go:embed icons/provider-imap.svg
var imapBadge []byte

var providerIcons = map[mailbox.Kind][]string{
	mailbox.KindProton: {"appimagekit-protonmail-desktop-unofficial", "proton-mail", AppID},
	mailbox.KindGmail:  {"goa-account-google", "gmail"},
}

// providerLogo returns an image of the service logo at the given size.
func providerLogo(kind mailbox.Kind, size int) *gtk.Image {
	if kind == mailbox.KindUnified {
		img := gtk.NewImageFromIconName("system-users-symbolic")
		img.SetPixelSize(size)
		return img
	}
	theme := gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault())
	for _, name := range providerIcons[kind] {
		if theme.HasIcon(name) {
			img := gtk.NewImageFromIconName(name)
			img.SetPixelSize(size)
			return img
		}
	}
	data := imapBadge
	if kind == mailbox.KindSeznam {
		data = seznamBadge
	}
	tex, err := gdk.NewTextureFromBytes(glib.NewBytesWithGo(data))
	if err != nil {
		img := gtk.NewImageFromIconName("mail-unread-symbolic")
		img.SetPixelSize(size)
		return img
	}
	img := gtk.NewImageFromPaintable(tex)
	img.SetPixelSize(size)
	return img
}

// providerName is the service name shown next to accounts.
func providerName(kind mailbox.Kind) string {
	switch kind {
	case mailbox.KindProton:
		return "Proton Mail"
	case mailbox.KindGmail:
		return "Gmail"
	case mailbox.KindSeznam:
		return "Seznam.cz"
	}
	return "IMAP"
}
