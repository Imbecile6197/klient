package ui

import (
	"fmt"
	"slices"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// Buttons in the new-mail notification. GNOME shows at most three.
type notifyButton struct {
	id    string
	label func() string
}

var notifyButtons = []notifyButton{
	{"read", func() string { return i18n.T("Mark as Read") }},
	{"archive", func() string { return i18n.C("action", "Archive") }},
	{"trash", func() string { return i18n.C("action", "Delete") }},
	{"spam", func() string { return i18n.T("Spam") }},
}

// defaultNotifyButtons are used until the user chooses.
var defaultNotifyButtons = []string{"read", "archive", "trash"}

const maxNotifyButtons = 3

func (a *App) notifyButtonIDs() []string {
	if a.cfg.NotifyButtons == nil {
		return defaultNotifyButtons
	}
	return a.cfg.NotifyButtons
}

// addNotifyButtons puts the chosen buttons on a new-mail notification.
func (a *App) addNotifyButtons(n *gio.Notification, id string) {
	chosen := a.notifyButtonIDs()
	for _, b := range notifyButtons {
		if slices.Contains(chosen, b.id) {
			n.AddButtonWithTarget(b.label(), "app.notify-"+b.id, glib.NewVariantString(id))
		}
	}
}

// installNotifyActions registers the actions behind the buttons.
func (a *App) installNotifyActions() {
	for _, b := range notifyButtons {
		b := b
		act := gio.NewSimpleAction("notify-"+b.id, glib.NewVariantType("s"))
		act.ConnectActivate(func(p *glib.Variant) {
			if p != nil {
				a.notifyAction(b.id, p.String())
			}
		})
		a.app.AddAction(act)
	}
}

// notifyAction handles a button of a new-mail notification, without opening
// the window.
func (a *App) notifyAction(action, id string) {
	n, ok := a.notified[id]
	a.app.WithdrawNotification("new-mail-" + id)
	if !ok || a.sessionOf(n.acc) == nil {
		return
	}
	delete(a.notified, id)
	acc := n.acc
	from := ""
	if n.meta.Sender != nil {
		from = n.meta.Sender.Address
	}
	go func() {
		var err error
		switch action {
		case "read":
			err = acc.MarkRead(a.ctx, id)
		case "archive":
			err = acc.Move(a.ctx, protonmail.ArchiveID, id)
		case "trash":
			err = acc.Move(a.ctx, protonmail.TrashID, id)
		case "spam":
			a.filter.Feedback(id, from, true)
			err = acc.Move(a.ctx, protonmail.SpamID, id)
		}
		ui(func() {
			if err != nil {
				// The window may be hidden: say it in a notification.
				fail := gio.NewNotification(i18n.T("The action failed"))
				fail.SetBody(err.Error())
				a.app.SendNotification("notify-failed", fail)
				return
			}
			if a.mv != nil && a.showing(acc) {
				a.mv.scheduleRefresh()
			}
			if s := a.sessionOf(acc); s != nil {
				a.refreshUnread(s)
			}
		})
	}()
}

// notifyGroup is the Preferences group that chooses the buttons.
func (a *App) notifyGroup(d *adw.PreferencesDialog) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Notifications"))
	g.SetDescription(fmt.Sprintf(i18n.T("Buttons in the new-mail notification (at most %d)"), maxNotifyButtons))
	for _, b := range notifyButtons {
		b := b
		row := adw.NewSwitchRow()
		row.SetTitle(b.label())
		row.SetActive(slices.Contains(a.notifyButtonIDs(), b.id))
		row.NotifyProperty("active", func() {
			chosen := slices.Clone(a.notifyButtonIDs())
			has := slices.Contains(chosen, b.id)
			switch {
			case row.Active() && !has:
				if len(chosen) >= maxNotifyButtons {
					d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("GNOME shows at most %d buttons – turn another one off first"), maxNotifyButtons)))
					row.SetActive(false)
					return
				}
				chosen = append(chosen, b.id)
			case !row.Active() && has:
				chosen = slices.DeleteFunc(chosen, func(x string) bool { return x == b.id })
			default:
				return
			}
			a.cfg.NotifyButtons = chosen
			a.saveConfig()
		})
		g.Add(row)
	}
	return g
}
