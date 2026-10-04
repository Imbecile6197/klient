package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// unifiedID is the value of Config.ActiveAccount for the combined view.
const unifiedID = "unified"

// openAccounts are the accounts with a running session.
func (a *App) openAccounts() []mailbox.Account {
	out := make([]mailbox.Account, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s.acc)
	}
	return out
}

// isUnified reports whether the window shows all accounts together.
func (a *App) isUnified() bool {
	return a.acc != nil && a.acc.Kind() == mailbox.KindUnified
}

// showing reports whether the window currently shows mail of acc (alone or
// in the combined view).
func (a *App) showing(acc mailbox.Account) bool {
	if a.acc == acc {
		return true
	}
	return a.isUnified() && a.sessionOf(acc) != nil
}

// ownAccount returns the real account for work that belongs to one account:
// for a message of the combined view its own account, otherwise the account
// shown (in the combined view the first one).
func (a *App) ownAccount(id string) mailbox.Account {
	if !a.isUnified() {
		return a.acc
	}
	if id != "" {
		if acc := a.unified.Owner(id); acc != nil {
			return acc
		}
	}
	if len(a.sessions) > 0 {
		return a.sessions[0].acc
	}
	return nil
}

// ownID returns a message's ID as its own account knows it (the spam
// filter and the rules remember messages by it).
func (a *App) ownID(id string) string {
	if a.isUnified() {
		if _, real, ok := a.unified.Resolve(id); ok {
			return real
		}
	}
	return id
}

// ownMessage returns a message of the combined view with its account's IDs.
func (a *App) ownMessage(msg *protonmail.Message) (mailbox.Account, *protonmail.Message) {
	if a.isUnified() && msg != nil {
		if acc, real, err := a.unified.UnwrapMessage(msg); err == nil {
			return acc, real
		}
	}
	return a.acc, msg
}

// unifiedButton is the "All Accounts" entry of the account switcher.
func (m *mainView) unifiedButton(pop *gtk.Popover) gtk.Widgetter {
	b := gtk.NewButton()
	b.AddCSSClass("flat")
	row := gtk.NewBox(gtk.OrientationHorizontal, 10)
	icon := gtk.NewImageFromIconName("system-users-symbolic")
	icon.SetPixelSize(24)
	icon.SetSizeRequest(32, 32)
	row.Append(icon)
	texts := gtk.NewBox(gtk.OrientationVertical, 0)
	name := gtk.NewLabel(i18n.T("All Accounts"))
	name.SetXAlign(0)
	name.AddCSSClass("heading")
	sub := gtk.NewLabel(i18n.T("Inbox, Sent, Archive, Spam and Trash of every account together"))
	sub.SetXAlign(0)
	sub.AddCSSClass("dim-label")
	sub.AddCSSClass("caption")
	sub.SetEllipsize(pango.EllipsizeEnd)
	texts.Append(name)
	texts.Append(sub)
	texts.SetHExpand(true)
	row.Append(texts)
	if m.a.isUnified() {
		row.Append(gtk.NewImageFromIconName("object-select-symbolic"))
	}
	b.SetChild(row)
	b.ConnectClicked(func() {
		pop.Popdown()
		if !m.a.isUnified() {
			m.a.switchAccount(m.a.unified)
		}
	})
	return b
}

// accountTag is the small label in a list row of the combined view that
// says which account the message belongs to.
func (m *mainView) accountTag(id string) gtk.Widgetter {
	if !m.a.isUnified() {
		return nil
	}
	acc := m.a.unified.Owner(id)
	if acc == nil {
		return nil
	}
	l := gtk.NewLabel(acc.Email())
	l.AddCSSClass("caption")
	l.AddCSSClass("dim-label")
	l.SetXAlign(0)
	l.SetEllipsize(pango.EllipsizeEnd)
	return l
}
