package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// The Outbox keeps messages whose sending failed, so nothing written is
// lost. After a network failure Klient sends them again by itself as soon as
// the connection returns; other failures wait for the user (send again,
// edit or delete). Each account keeps its messages in its own encrypted
// cache, so they survive a restart.

const outboxKey = "outbox"

// outboxRetry is how often waiting messages are tried again.
var outboxRetry = time.Minute

type outboxItem struct {
	ID      string
	Draft   protonmail.Draft
	Error   string
	Waiting bool // failed for lack of connection: sent again automatically
	Added   time.Time

	acc     mailbox.Account
	sending bool
}

// networkError reports whether sending failed only because the server could
// not be reached (worth trying again later without asking).
func networkError(err error) bool {
	if err == nil {
		return false
	}
	if protonmail.IsOffline(err) || imapmail.IsOffline(err) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

func newOutboxID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// loadOutbox reads an account's waiting messages (when its session starts).
func (a *App) loadOutbox(acc mailbox.Account) {
	if acc.Cache() == nil {
		return
	}
	var items []*outboxItem
	if !acc.Cache().Get(outboxKey, &items) {
		return
	}
	for _, it := range items {
		it.acc = acc
		a.outbox = append(a.outbox, it)
	}
	a.outboxChanged()
}

// saveOutbox stores an account's waiting messages in its encrypted cache.
func (a *App) saveOutbox(acc mailbox.Account) {
	if acc == nil || acc.Cache() == nil {
		return
	}
	items := []*outboxItem{}
	for _, it := range a.outbox {
		if it.acc == acc {
			items = append(items, it)
		}
	}
	if err := acc.Cache().Set(outboxKey, items); err != nil {
		a.toast(i18n.T("The Outbox could not be saved: ") + err.Error())
	}
}

// addToOutbox keeps a message that could not be sent.
func (a *App) addToOutbox(acc mailbox.Account, d *protonmail.Draft, err error) *outboxItem {
	it := &outboxItem{ID: newOutboxID(), Draft: *d, Error: err.Error(), Waiting: networkError(err), Added: time.Now(), acc: acc}
	it.Draft.DeliveryTime = time.Time{}
	a.outbox = append(a.outbox, it)
	a.saveOutbox(acc)
	a.outboxChanged()
	if it.Waiting {
		a.toastWithAction(i18n.T("No connection – the message waits in the Outbox and will be sent automatically"), i18n.T("Show"), a.showOutbox)
	} else {
		a.toastWithAction(i18n.T("Sending failed – the message waits in the Outbox"), i18n.T("Show"), a.showOutbox)
	}
	return it
}

func (a *App) removeFromOutbox(it *outboxItem) {
	for i, x := range a.outbox {
		if x == it {
			a.outbox = append(a.outbox[:i], a.outbox[i+1:]...)
			break
		}
	}
	a.saveOutbox(it.acc)
	a.outboxChanged()
}

// shownOutbox are the waiting messages of the accounts the window shows.
func (a *App) shownOutbox() []*outboxItem {
	var out []*outboxItem
	for _, it := range a.outbox {
		if a.sessionOf(it.acc) != nil && a.showing(it.acc) {
			out = append(out, it)
		}
	}
	return out
}

// outboxChanged updates everything that shows the Outbox.
func (a *App) outboxChanged() {
	if a.mv != nil {
		a.mv.updateOutboxButton()
	}
	if a.outboxRefresh != nil {
		a.outboxRefresh()
	}
}

// sendFromOutbox tries to send a waiting message again.
func (a *App) sendFromOutbox(it *outboxItem, manual bool) {
	if it.sending || a.sessionOf(it.acc) == nil {
		return
	}
	it.sending = true
	a.outboxChanged()
	d := it.Draft
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
		defer cancel()
		err := it.acc.Send(ctx, &d)
		ui(func() {
			it.sending = false
			if err == nil {
				a.removeFromOutbox(it)
				a.toast(fmt.Sprintf(i18n.T("Sent from the Outbox: %s"), orDefault(d.Subject, i18n.T("(no subject)"))))
				if a.mv != nil {
					a.mv.scheduleRefresh()
				}
				return
			}
			it.Draft.ID = d.ID // the draft may have been saved on the server meanwhile
			it.Error = err.Error()
			it.Waiting = networkError(err)
			a.saveOutbox(it.acc)
			a.outboxChanged()
			if ce := certError(err); ce != nil && manual {
				if im, ok := it.acc.(*imapmail.Account); ok {
					a.askTrustCert(a.win, ce, func() {
						a.trustCert(im.Settings().ID(), ce)
						a.sendFromOutbox(it, true)
					})
				}
				return
			}
			if manual {
				a.toast(i18n.T("Sending failed: ") + err.Error())
			}
		})
	}()
}

// outboxLoop sends waiting messages again once the connection returns.
func (a *App) outboxLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(outboxRetry):
		}
		ui(func() {
			for _, it := range a.outbox {
				if it.Waiting && !it.sending {
					a.sendFromOutbox(it, false)
				}
			}
		})
	}
}

// showOutbox lists the waiting messages with Send Again, Edit and Delete.
func (a *App) showOutbox() {
	d := adw.NewDialog()
	d.SetTitle(i18n.T("Outbox"))
	d.SetContentWidth(620)
	d.SetContentHeight(460)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())
	page := adw.NewPreferencesPage()
	group := adw.NewPreferencesGroup()
	group.SetDescription(i18n.T("Messages that could not be sent. After a connection failure they are sent again automatically every minute; otherwise send them again, edit them or delete them."))
	page.Add(group)
	empty := adw.NewStatusPage()
	empty.SetIconName("mail-send-symbolic")
	empty.SetTitle(i18n.T("The Outbox Is Empty"))
	stack := gtk.NewStack()
	stack.AddNamed(page, "list")
	stack.AddNamed(empty, "empty")
	tv.SetContent(stack)
	d.SetChild(tv)

	var rows []gtk.Widgetter
	refresh := func() {
		for _, r := range rows {
			group.Remove(r)
		}
		rows = nil
		items := a.shownOutbox()
		if len(items) == 0 {
			stack.SetVisibleChildName("empty")
			return
		}
		stack.SetVisibleChildName("list")
		for _, it := range items {
			it := it
			row := adw.NewActionRow()
			row.SetTitle(orDefault(it.Draft.Subject, i18n.T("(no subject)")))
			var to []string
			for _, r := range append(append([]*mail.Address{}, it.Draft.To...), it.Draft.CC...) {
				to = append(to, r.Address)
			}
			status := it.Error
			switch {
			case it.sending:
				status = i18n.T("Sending…")
			case it.Waiting:
				status = i18n.T("Waiting for the connection – ") + it.Error
			}
			sub := i18n.T("To: ") + strings.Join(to, ", ")
			if len(a.sessions) > 1 {
				sub += " · " + it.acc.Email()
			}
			row.SetSubtitle(sub + "\n" + status)
			row.SetSubtitleLines(3)
			row.SetTitleLines(1)

			send := gtk.NewButtonFromIconName("mail-send-symbolic")
			send.SetTooltipText(i18n.T("Send Again"))
			send.SetVAlign(gtk.AlignCenter)
			send.AddCSSClass("flat")
			send.SetSensitive(!it.sending)
			send.ConnectClicked(func() { a.sendFromOutbox(it, true) })
			edit := gtk.NewButtonFromIconName("document-edit-symbolic")
			edit.SetTooltipText(i18n.T("Edit"))
			edit.SetVAlign(gtk.AlignCenter)
			edit.AddCSSClass("flat")
			edit.SetSensitive(!it.sending)
			edit.ConnectClicked(func() {
				a.removeFromOutbox(it)
				d.Close()
				dr := it.Draft
				a.composerFor(it.acc, &dr, nil, protonmail.ActionNew)
			})
			del := gtk.NewButtonFromIconName("user-trash-symbolic")
			del.SetTooltipText(i18n.T("Delete"))
			del.SetVAlign(gtk.AlignCenter)
			del.AddCSSClass("flat")
			del.SetSensitive(!it.sending)
			del.ConnectClicked(func() {
				confirm := adw.NewAlertDialog(i18n.T("Delete the Message?"), i18n.T("The message will not be sent and cannot be recovered."))
				confirm.AddResponse("cancel", i18n.T("Cancel"))
				confirm.AddResponse("delete", i18n.T("Delete"))
				confirm.SetResponseAppearance("delete", adw.ResponseDestructive)
				confirm.SetCloseResponse("cancel")
				confirm.ConnectResponse(func(r string) {
					if r == "delete" {
						a.removeFromOutbox(it)
					}
				})
				confirm.Present(d)
			})
			row.AddSuffix(send)
			row.AddSuffix(edit)
			row.AddSuffix(del)
			group.Add(row)
			rows = append(rows, row)
		}
	}
	refresh()
	a.outboxRefresh = refresh
	d.ConnectClosed(func() { a.outboxRefresh = nil })
	d.Present(a.win)
}

// updateOutboxButton shows the Outbox entry in the sidebar while it has
// messages.
func (m *mainView) updateOutboxButton() {
	if m.outboxBtn == nil {
		return
	}
	n := len(m.a.shownOutbox())
	m.outboxBtn.SetVisible(n > 0)
	m.outboxLabel.SetText(fmt.Sprintf(i18n.T("Outbox (%d)"), n))
}

// outboxButton is the sidebar entry under the folders.
func (m *mainView) outboxButton() gtk.Widgetter {
	m.outboxBtn = gtk.NewButton()
	m.outboxBtn.AddCSSClass("flat")
	box := gtk.NewBox(gtk.OrientationHorizontal, 12)
	box.Append(gtk.NewImageFromIconName("mail-send-symbolic"))
	m.outboxLabel = gtk.NewLabel("")
	m.outboxLabel.SetXAlign(0)
	m.outboxLabel.SetEllipsize(pango.EllipsizeEnd)
	m.outboxLabel.AddCSSClass("warning")
	box.Append(m.outboxLabel)
	m.outboxBtn.SetChild(box)
	m.outboxBtn.SetMarginStart(6)
	m.outboxBtn.SetMarginEnd(6)
	m.outboxBtn.SetMarginBottom(6)
	m.outboxBtn.SetTooltipText(i18n.T("Messages that could not be sent"))
	m.outboxBtn.ConnectClicked(m.a.showOutbox)
	m.updateOutboxButton()
	return m.outboxBtn
}
