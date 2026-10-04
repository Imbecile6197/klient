package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/pgp"
)

func keySourceName(src string) string {
	switch src {
	case pgp.SourceAutocrypt:
		return i18n.T("from the Autocrypt header of their mail")
	case pgp.SourceWKD:
		return i18n.T("from their domain (Web Key Directory)")
	}
	return i18n.T("imported by you")
}

func keyDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format(i18n.T("Jan 2, 2006 15:04"))
}

// contactKeySubtitle sums up a stored contact key for the list.
func contactKeySubtitle(email string) string {
	m := pgp.Meta(email)
	parts := []string{shortFP(strings.ToUpper(pgp.Details(pgp.StoredKey(email)).Fingerprint))}
	if m.Verified {
		parts = append(parts, i18n.T("verified"))
	}
	if pgp.PendingKey(email) != nil {
		parts = append(parts, i18n.T("a new key is waiting"))
	}
	return strings.Join(parts, " · ")
}

// keyRows adds the facts about a key to a group.
func keyRows(g *adw.PreferencesGroup, key *crypto.Key) {
	det := pgp.Details(key)
	fp := adw.NewActionRow()
	fp.SetTitle(i18n.T("Fingerprint"))
	fp.SetSubtitle(pgp.GroupFingerprint(det.Fingerprint))
	fp.SetSubtitleSelectable(true)
	fp.AddCSSClass("property")
	copyBtn := gtk.NewButtonFromIconName("edit-copy-symbolic")
	copyBtn.SetTooltipText(i18n.T("Copy to Clipboard"))
	copyBtn.SetVAlign(gtk.AlignCenter)
	copyBtn.AddCSSClass("flat")
	copyBtn.ConnectClicked(func() {
		copyBtn.Clipboard().SetText(det.Fingerprint)
	})
	fp.AddSuffix(copyBtn)
	g.Add(fp)
	prop := func(title, value string) {
		r := adw.NewActionRow()
		r.SetUseMarkup(false) // user IDs contain "<address>"
		r.SetTitle(title)
		r.SetSubtitle(value)
		r.SetSubtitleSelectable(true)
		r.AddCSSClass("property")
		g.Add(r)
	}
	prop(i18n.T("User IDs"), strings.Join(det.UserIDs, "\n"))
	prop(i18n.T("Algorithm"), det.Algorithm)
	prop(i18n.T("Created"), keyDate(det.Created))
	expires := i18n.T("never")
	if !det.Expires.IsZero() {
		expires = keyDate(det.Expires)
	}
	switch {
	case det.Revoked:
		expires = i18n.T("revoked")
	case det.Expired:
		expires += " – " + i18n.T("expired")
	}
	prop(i18n.T("Expires"), expires)
}

// contactKeyDialog shows a contact's key: fingerprint to compare with them,
// where it came from, whether it is verified, and a new key waiting for a
// decision.
func (a *App) contactKeyDialog(email string, onChange func()) {
	d := adw.NewDialog()
	d.SetTitle(email)
	d.SetContentWidth(520)
	d.SetContentHeight(640)
	var build func()
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())
	toasts := adw.NewToastOverlay()
	toasts.SetChild(tv)
	d.SetChild(toasts)
	toast := func(s string) { toasts.AddToast(adw.NewToast(s)) }
	changed := func() {
		if onChange != nil {
			onChange()
		}
		build()
	}
	build = func() {
		page := adw.NewPreferencesPage()
		m := pgp.Meta(email)
		key := pgp.StoredKey(email)

		if pending := pgp.PendingKey(email); pending != nil {
			pg := adw.NewPreferencesGroup()
			pg.SetTitle(i18n.T("New Key Waiting"))
			pg.SetDescription(fmt.Sprintf(i18n.T("On %s Klient saw a different key for this address (%s). Contacts get a new key when they set up a new computer or lose the old key – but a forged message can bring a key too. Accept it only if you expect the change, best after checking the fingerprint with the contact."), keyDate(m.PendingSeen), keySourceName(m.PendingSource)))
			keyRows(pg, pending)
			btns := gtk.NewBox(gtk.OrientationHorizontal, 6)
			btns.SetHAlign(gtk.AlignEnd)
			btns.SetMarginTop(8)
			keep := gtk.NewButtonWithLabel(i18n.T("Keep the Old Key"))
			keep.ConnectClicked(func() {
				_ = pgp.RejectPending(email)
				changed()
			})
			accept := gtk.NewButtonWithLabel(i18n.T("Use the New Key"))
			accept.AddCSSClass("suggested-action")
			accept.ConnectClicked(func() {
				if err := pgp.AcceptPending(email); err != nil {
					toast(err.Error())
				}
				changed()
			})
			btns.Append(keep)
			btns.Append(accept)
			pg.Add(btns)
			page.Add(pg)
		}

		g := adw.NewPreferencesGroup()
		g.SetTitle(i18n.T("Key in Use"))
		g.SetDescription(fmt.Sprintf(i18n.T("Added %s, %s."), keyDate(m.Added), keySourceName(m.Source)))
		if key != nil {
			keyRows(g, key)
		}
		if m.ReplacedFP != "" {
			r := adw.NewActionRow()
			r.SetTitle(i18n.T("Previous key"))
			r.SetSubtitle(fmt.Sprintf(i18n.T("%s, replaced %s"), shortFP(m.ReplacedFP), keyDate(m.ReplacedAt)))
			r.AddCSSClass("property")
			g.Add(r)
		}
		ver := adw.NewSwitchRow()
		ver.SetTitle(i18n.T("Verified"))
		ver.SetSubtitle(i18n.T("You compared the fingerprint with the contact (in person, by phone). A verified key is never replaced automatically."))
		ver.SetActive(m.Verified)
		ver.NotifyProperty("active", func() {
			if ver.Active() == pgp.Meta(email).Verified {
				return
			}
			if err := pgp.SetVerified(email, ver.Active()); err != nil {
				toast(err.Error())
				return
			}
			if onChange != nil {
				onChange()
			}
		})
		g.Add(ver)
		page.Add(g)

		dg := adw.NewPreferencesGroup()
		del := adw.NewButtonRow()
		del.SetTitle(i18n.T("Remove Key"))
		del.AddCSSClass("destructive-action")
		del.ConnectActivated(func() {
			if err := pgp.DeleteKey(email); err != nil {
				toast(err.Error())
				return
			}
			if onChange != nil {
				onChange()
			}
			d.Close()
		})
		dg.Add(del)
		page.Add(dg)
		tv.SetContent(page)
	}
	build()
	d.Present(a.gtkWindow())
}

// senderKeyNotice warns under a message when the sender's key changed: a
// new key waits for the user, or one was replaced in the last two weeks.
func (a *App) senderKeyNotice(sender string, onChange func()) gtk.Widgetter {
	if sender == "" || pgp.StoredKey(sender) == nil {
		return nil
	}
	m := pgp.Meta(sender)
	var text string
	warn := false
	switch {
	case pgp.PendingKey(sender) != nil:
		text, warn = i18n.T("The sender uses a new key that Klient has not accepted yet."), true
	case m.ReplacedFP != "" && time.Since(m.ReplacedAt) < 14*24*time.Hour:
		text = fmt.Sprintf(i18n.T("The sender's key changed on %s."), keyDate(m.ReplacedAt))
	default:
		return nil
	}
	box := gtk.NewBox(gtk.OrientationHorizontal, 6)
	icon := "dialog-information-symbolic"
	if warn {
		icon = "dialog-warning-symbolic"
	}
	box.Append(gtk.NewImageFromIconName(icon))
	l := gtk.NewLabel(text)
	l.AddCSSClass("caption")
	if warn {
		l.AddCSSClass("warning")
	}
	l.SetWrap(true)
	l.SetXAlign(0)
	box.Append(l)
	btn := gtk.NewButtonWithLabel(i18n.T("Show Key…"))
	btn.AddCSSClass("flat")
	btn.AddCSSClass("caption")
	btn.ConnectClicked(func() { a.contactKeyDialog(sender, onChange) })
	box.Append(btn)
	return box
}
