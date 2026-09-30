package ui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/ical"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- Calendar invitations --------------------------------------------------------

func isCalendar(att protonmail.Attachment) bool {
	return strings.HasPrefix(strings.ToLower(att.MIMEType), "text/calendar") ||
		strings.HasSuffix(strings.ToLower(att.Name), ".ics")
}

// inviteCards adds a card for every invitation attached to msg (decrypted
// and parsed in the background).
func (m *mainView) inviteCards(msg *protonmail.Message, box *gtk.Box) {
	for _, att := range msg.Attachments {
		if !isCalendar(att) {
			continue
		}
		holder := gtk.NewBox(gtk.OrientationVertical, 0)
		box.Append(holder)
		att := att
		acc := m.a.acc
		go func() {
			data, err := acc.AttachmentData(m.a.ctx, att)
			var ev *ical.Event
			if err == nil {
				ev, err = ical.Parse(data)
			}
			ui(func() {
				if err != nil || ev.Method == "REPLY" {
					holder.SetVisible(false)
					return
				}
				holder.Append(m.inviteCard(msg, ev, data))
			})
		}()
	}
}

func (m *mainView) inviteCard(msg *protonmail.Message, ev *ical.Event, data []byte) gtk.Widgetter {
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("card")
	card.AddCSSClass("invite-card")
	inner := gtk.NewBox(gtk.OrientationVertical, 6)
	inner.SetMarginTop(12)
	inner.SetMarginBottom(12)
	inner.SetMarginStart(12)
	inner.SetMarginEnd(12)
	card.Append(inner)

	head := gtk.NewBox(gtk.OrientationHorizontal, 10)
	head.Append(gtk.NewImageFromIconName("x-office-calendar-symbolic"))
	kind := i18n.T("Event invitation")
	if ev.Method == "CANCEL" {
		kind = i18n.T("The event has been cancelled")
	}
	kl := gtk.NewLabel(kind)
	kl.AddCSSClass("caption-heading")
	kl.AddCSSClass("dim-label")
	head.Append(kl)
	inner.Append(head)

	title := gtk.NewLabel(orDefault(ev.Summary, i18n.T("(untitled)")))
	title.AddCSSClass("title-3")
	title.SetXAlign(0)
	title.SetWrap(true)
	if ev.Method == "CANCEL" {
		title.SetMarkup("<s>" + escapeMarkup(orDefault(ev.Summary, i18n.T("(untitled)"))) + "</s>")
	}
	inner.Append(title)

	line := func(icon, text string) {
		if text == "" {
			return
		}
		b := gtk.NewBox(gtk.OrientationHorizontal, 8)
		b.Append(gtk.NewImageFromIconName(icon))
		l := gtk.NewLabel(text)
		l.SetXAlign(0)
		l.SetWrap(true)
		l.SetWrapMode(pango.WrapWordChar)
		l.SetSelectable(true)
		b.Append(l)
		inner.Append(b)
	}
	line("alarm-symbolic", eventWhen(ev))
	line("find-location-symbolic", ev.Location)
	org := ev.Organizer.Name
	if org == "" {
		org = ev.Organizer.Email
	} else if ev.Organizer.Email != "" {
		org += " <" + ev.Organizer.Email + ">"
	}
	if org != "" {
		line("avatar-default-symbolic", fmt.Sprintf(i18n.T("Organized by %s"), org))
	}
	if n := len(ev.Attendees); n > 0 {
		line("system-users-symbolic", fmt.Sprintf(i18n.N("%d person invited", "%d people invited", n), n))
	}

	me := m.inviteeAddress(msg, ev)
	status := gtk.NewLabel("")
	status.SetXAlign(0)
	status.AddCSSClass("dim-label")
	if a, ok := ev.Attendee(me); ok {
		status.SetText(partStatText(a.PartStat))
	}
	inner.Append(status)

	buttons := gtk.NewBox(gtk.OrientationHorizontal, 6)
	buttons.SetMarginTop(6)
	if ev.Method == "REQUEST" && ev.Organizer.Email != "" && !m.a.acc.IsOwnAddress(ev.Organizer.Email) {
		for _, r := range []struct{ label, stat, css string }{
			{i18n.T("Accept"), "ACCEPTED", "suggested-action"},
			{i18n.T("Maybe"), "TENTATIVE", ""},
			{i18n.T("Decline"), "DECLINED", "destructive-action"},
		} {
			r := r
			b := gtk.NewButtonWithLabel(r.label)
			if r.css != "" {
				b.AddCSSClass(r.css)
			}
			b.ConnectClicked(func() {
				m.a.answerInvite(msg, ev, me, r.stat, buttons, status)
			})
			buttons.Append(b)
		}
	}
	if ev.Method != "CANCEL" {
		add := gtk.NewButtonWithLabel(i18n.T("Add to Calendar"))
		add.SetTooltipText(i18n.T("Opens the event in the default calendar (e.g. GNOME Calendar)"))
		add.ConnectClicked(func() { m.a.openInCalendar(ev, data) })
		buttons.Append(add)
	}
	inner.Append(buttons)
	return card
}

func escapeMarkup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func eventWhen(ev *ical.Event) string {
	s, e := ev.Start.Local(), ev.End.Local()
	date := dayDate(s)
	if ev.AllDay {
		last := e.AddDate(0, 0, -1)
		if last.After(s) {
			return date + " – " + dayDate(last) + i18n.T(" (all day)")
		}
		return date + i18n.T(" (all day)")
	}
	if s.Format("20060102") == e.Format("20060102") {
		return date + ", " + s.Format("15:04") + "–" + e.Format("15:04")
	}
	return date + " " + s.Format("15:04") + " – " + dayDate(e) + " " + e.Format("15:04")
}

func partStatText(s string) string {
	switch s {
	case "ACCEPTED":
		return i18n.T("Your answer: accepted")
	case "TENTATIVE":
		return i18n.T("Your answer: maybe")
	case "DECLINED":
		return i18n.T("Your answer: declined")
	}
	return i18n.T("You have not answered yet")
}

// inviteeAddress is the user's address the invitation was sent to.
func (m *mainView) inviteeAddress(msg *protonmail.Message, ev *ical.Event) string {
	for _, a := range ev.Attendees {
		if m.a.acc.IsOwnAddress(a.Email) {
			return a.Email
		}
	}
	for _, ad := range m.a.acc.SendAddresses() {
		if ad.ID == msg.Meta.AddressID {
			return ad.Email
		}
	}
	return m.a.acc.Email()
}

// answerInvite sends the iTIP reply to the organizer.
func (a *App) answerInvite(msg *protonmail.Message, ev *ical.Event, me, stat string, buttons *gtk.Box, status *gtk.Label) {
	verb := map[string]string{"ACCEPTED": i18n.T("Accepted"), "TENTATIVE": i18n.T("Tentative"), "DECLINED": i18n.T("Declined")}[stat]
	name := a.acc.DisplayName()
	d := &protonmail.Draft{
		FromAddressID: msg.Meta.AddressID,
		To:            []*mail.Address{{Name: ev.Organizer.Name, Address: ev.Organizer.Email}},
		Subject:       verb + ": " + ev.Summary,
		SignExternal:  true,
	}
	for _, ad := range a.acc.SendAddresses() {
		if strings.EqualFold(ad.Email, me) {
			d.FromAddressID = ad.ID
		}
	}
	d.Body = fmt.Sprintf(i18n.T("%s has replied to the invitation “%s” (%s): %s."), name, ev.Summary, eventWhen(ev), strings.ToLower(verb))
	reply := ev.Reply(me, name, stat, time.Now())
	d.Attachments = []*protonmail.Outgoing{{Name: "invite.ics", MIMEType: "text/calendar", Data: reply, Size: int64(len(reply))}}
	buttons.SetSensitive(false)
	status.SetText(i18n.T("Sending the answer…"))
	acc := a.acc
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		err := acc.Send(ctx, d)
		ui(func() {
			buttons.SetSensitive(true)
			if err != nil {
				status.SetText(i18n.T("The answer could not be sent: ") + err.Error())
				if ce := certError(err); ce != nil {
					if im, ok := acc.(*imapmail.Account); ok {
						a.askTrustCert(a.win, ce, func() { a.trustCert(im.Settings().ID(), ce) })
					}
				}
				return
			}
			status.SetText(partStatText(stat) + i18n.T(" (sent to the organizer)"))
			a.toast(i18n.T("Your answer to the invitation has been sent"))
		})
	}()
}

// openInCalendar hands the .ics to the default calendar app.
func (a *App) openInCalendar(ev *ical.Event, data []byte) {
	dir := filepath.Join(os.TempDir(), "klient-"+fmt.Sprint(os.Getuid()))
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, safeFilename(orDefault(ev.Summary, "udalost"))+".ics")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		a.toast(err.Error())
		return
	}
	gtk.NewFileLauncher(gio.NewFileForPath(path)).Launch(context.Background(), a.gtkWindow(), nil)
}

// ---- PGP keys in attachments --------------------------------------------------------

func isKeyAttachment(att protonmail.Attachment) bool {
	n := strings.ToLower(att.Name)
	return att.MIMEType == "application/pgp-keys" || strings.HasSuffix(n, ".asc") ||
		strings.HasSuffix(n, ".pub") || (strings.HasSuffix(n, ".key") && strings.Contains(n, "pgp")) ||
		strings.HasPrefix(n, "publickey")
}

// importKeyButton imports a public key sent as an attachment.
func (m *mainView) importKeyButton(att protonmail.Attachment) gtk.Widgetter {
	b := gtk.NewButtonFromIconName("channel-secure-symbolic")
	b.SetTooltipText(i18n.T("Import the public key – messages to this sender will then be encrypted"))
	b.SetVAlign(gtk.AlignCenter)
	b.AddCSSClass("flat")
	acc := m.a.acc
	b.ConnectClicked(func() {
		b.SetSensitive(false)
		go func() {
			data, err := acc.AttachmentData(m.a.ctx, att)
			var emails []string
			if err == nil {
				if !strings.Contains(string(data), "BEGIN PGP PUBLIC KEY BLOCK") {
					err = errors.New(i18n.T("the attachment does not contain a public PGP key"))
				} else {
					emails, err = pgp.ImportKey(string(data))
				}
			}
			ui(func() {
				b.SetSensitive(true)
				if err != nil {
					m.a.toast(i18n.T("Importing the key failed: ") + err.Error())
					return
				}
				b.SetIconName("object-select-symbolic")
				m.a.toast(fmt.Sprintf(i18n.T("Key imported for %s"), strings.Join(emails, ", ")))
			})
		}()
	})
	return b
}

// ---- Vacation auto-reply --------------------------------------------------------------

func (a *App) openAutoReply() {
	if a.acc == nil {
		return
	}
	d := adw.NewDialog()
	d.SetTitle(i18n.T("Automatic Reply"))
	d.SetContentWidth(560)
	d.SetContentHeight(640)
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	save := gtk.NewButtonWithLabel(i18n.T("Save"))
	save.AddCSSClass("suggested-action")
	save.SetSensitive(false)
	hb.PackEnd(save)
	tv.AddTopBar(hb)
	toasts := adw.NewToastOverlay()

	page := adw.NewPreferencesPage()
	g := adw.NewPreferencesGroup()
	g.SetDescription(i18n.T("Proton replies on its own on the server, even when your computer is off. Each sender gets the reply at most once in a while. This feature requires a paid Proton plan."))
	enabled := adw.NewSwitchRow()
	enabled.SetTitle(i18n.T("Reply automatically"))
	enabled.SetSubtitle(a.acc.Email())
	subject := adw.NewEntryRow()
	subject.SetTitle(i18n.T("Subject"))
	g.Add(enabled)
	g.Add(subject)
	page.Add(g)

	tg := adw.NewPreferencesGroup()
	tg.SetTitle(i18n.T("Duration"))
	untilRow := adw.NewSwitchRow()
	untilRow.SetTitle(i18n.T("End automatically"))
	untilRow.SetSubtitle(i18n.T("Otherwise it stays on until you turn it off"))
	cal := gtk.NewCalendar()
	calRow := adw.NewActionRow()
	calRow.SetTitle(i18n.T("Last day"))
	dateBtn := gtk.NewMenuButton()
	dateBtn.SetVAlign(gtk.AlignCenter)
	pop := gtk.NewPopover()
	pop.SetChild(cal)
	dateBtn.SetPopover(pop)
	calRow.AddSuffix(dateBtn)
	end := time.Now().AddDate(0, 0, 7)
	setEnd := func(t time.Time) {
		end = endOfDay(t)
		dateBtn.SetLabel(dayDate(end))
	}
	setEnd(end)
	cal.ConnectDaySelected(func() {
		dt := cal.Date()
		setEnd(time.Date(dt.Year(), time.Month(dt.Month()), dt.DayOfMonth(), 0, 0, 0, 0, time.Local))
		pop.Popdown()
	})
	untilRow.NotifyProperty("active", func() { calRow.SetSensitive(untilRow.Active()) })
	calRow.SetSensitive(false)
	tg.Add(untilRow)
	tg.Add(calRow)
	page.Add(tg)

	mg := adw.NewPreferencesGroup()
	mg.SetTitle(i18n.T("Reply Text"))
	view := gtk.NewTextView()
	view.SetWrapMode(gtk.WrapWordChar)
	for _, f := range []func(int){view.SetTopMargin, view.SetBottomMargin, view.SetLeftMargin, view.SetRightMargin} {
		f(10)
	}
	view.SetSizeRequest(-1, 160)
	frame := gtk.NewFrame("")
	frame.SetChild(view)
	mg.Add(frame)
	page.Add(mg)

	stack := gtk.NewStack()
	stack.AddNamed(spinnerBox(), "loading")
	stack.AddNamed(page, "form")
	toasts.SetChild(stack)
	tv.SetContent(toasts)
	d.SetChild(tv)

	acc := a.acc
	go func() {
		ar, err := acc.AutoReply(a.ctx)
		ui(func() {
			stack.SetVisibleChildName("form")
			save.SetSensitive(true)
			if err != nil {
				toasts.AddToast(adw.NewToast(err.Error()))
			}
			enabled.SetActive(ar.Enabled)
			subject.SetText(orDefault(ar.Subject, i18n.T("Out of office")))
			msg := ar.Message
			if msg == "" {
				msg = i18n.T("Hello,\n\nI am away until %s and only read email occasionally. I will reply when I am back.\n\nThank you for your understanding.")
				msg = fmt.Sprintf(msg, end.Format(i18n.T("Jan 2")))
			}
			view.Buffer().SetText(htmlToPlain(msg))
			if !ar.End.IsZero() {
				untilRow.SetActive(true)
				setEnd(ar.End)
				cal.SelectDay(glibDate(ar.End))
			}
		})
	}()

	save.ConnectClicked(func() {
		buf := view.Buffer()
		st, en := buf.Bounds()
		r := protonmail.AutoReply{
			Enabled: enabled.Active(),
			Subject: strings.TrimSpace(subject.Text()),
			Message: plainToHTMLParagraphs(buf.Text(st, en, false)),
		}
		if untilRow.Active() {
			if end.Before(time.Now()) {
				toasts.AddToast(adw.NewToast(i18n.T("The last day has already passed")))
				return
			}
			r.End = end
		}
		save.SetSensitive(false)
		go func() {
			err := acc.SetAutoReply(a.ctx, r)
			ui(func() {
				save.SetSensitive(true)
				if err != nil {
					toasts.AddToast(adw.NewToast(err.Error()))
					return
				}
				d.Close()
				if r.Enabled {
					a.toast(i18n.T("The automatic reply is on"))
				} else {
					a.toast(i18n.T("The automatic reply is off"))
				}
			})
		}()
	})
	d.Present(a.win)
}

func htmlToPlain(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	text, _ := mailparse.HTMLToText(s)
	return strings.TrimSpace(text)
}

// plainToHTMLParagraphs turns the typed text into the simple HTML Proton
// stores for the auto-reply.
func plainToHTMLParagraphs(s string) string {
	s = html.EscapeString(strings.TrimSpace(s))
	return "<div>" + strings.ReplaceAll(s, "\n", "<br>") + "</div>"
}

func glibDate(t time.Time) *glib.DateTime {
	return glib.NewDateTimeLocal(t.Year(), int(t.Month()), t.Day(), 0, 0, 0)
}
