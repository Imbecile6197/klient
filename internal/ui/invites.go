package ui

import (
	"context"
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

	"github.com/Imbecile6197/klient/internal/ical"
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
	kind := "Pozvánka na událost"
	if ev.Method == "CANCEL" {
		kind = "Událost byla zrušena"
	}
	kl := gtk.NewLabel(kind)
	kl.AddCSSClass("caption-heading")
	kl.AddCSSClass("dim-label")
	head.Append(kl)
	inner.Append(head)

	title := gtk.NewLabel(orDefault(ev.Summary, "(bez názvu)"))
	title.AddCSSClass("title-3")
	title.SetXAlign(0)
	title.SetWrap(true)
	if ev.Method == "CANCEL" {
		title.SetMarkup("<s>" + escapeMarkup(orDefault(ev.Summary, "(bez názvu)")) + "</s>")
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
		line("avatar-default-symbolic", "Pořádá "+org)
	}
	if n := len(ev.Attendees); n > 0 {
		line("system-users-symbolic", fmt.Sprintf("Pozváno: %d", n))
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
			{"Přijmout", "ACCEPTED", "suggested-action"},
			{"Možná", "TENTATIVE", ""},
			{"Odmítnout", "DECLINED", "destructive-action"},
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
		add := gtk.NewButtonWithLabel("Přidat do kalendáře")
		add.SetTooltipText("Otevře událost ve výchozím kalendáři (např. Kalendář GNOME)")
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
	date := czDays[s.Weekday()] + " " + s.Format("2. 1. 2006")
	if ev.AllDay {
		last := e.AddDate(0, 0, -1)
		if last.After(s) {
			return date + " – " + czDays[last.Weekday()] + " " + last.Format("2. 1. 2006") + " (celý den)"
		}
		return date + " (celý den)"
	}
	if s.Format("20060102") == e.Format("20060102") {
		return date + ", " + s.Format("15:04") + "–" + e.Format("15:04")
	}
	return date + " " + s.Format("15:04") + " – " + czDays[e.Weekday()] + " " + e.Format("2. 1. 2006 15:04")
}

func partStatText(s string) string {
	switch s {
	case "ACCEPTED":
		return "Vaše odpověď: přijato"
	case "TENTATIVE":
		return "Vaše odpověď: možná"
	case "DECLINED":
		return "Vaše odpověď: odmítnuto"
	}
	return "Zatím jste neodpověděli"
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
	verb := map[string]string{"ACCEPTED": "Přijato", "TENTATIVE": "Možná", "DECLINED": "Odmítnuto"}[stat]
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
	d.Body = fmt.Sprintf("%s odpověděl(a) na pozvánku „%s“ (%s): %s.", name, ev.Summary, eventWhen(ev), strings.ToLower(verb))
	reply := ev.Reply(me, name, stat, time.Now())
	d.Attachments = []*protonmail.Outgoing{{Name: "invite.ics", MIMEType: "text/calendar", Data: reply, Size: int64(len(reply))}}
	buttons.SetSensitive(false)
	status.SetText("Odesílám odpověď…")
	acc := a.acc
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		err := acc.Send(ctx, d)
		ui(func() {
			buttons.SetSensitive(true)
			if err != nil {
				status.SetText("Odpověď se nepodařilo odeslat: " + err.Error())
				return
			}
			status.SetText(partStatText(stat) + " (odesláno pořadateli)")
			a.toast("Odpověď na pozvánku odeslána")
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
	b.SetTooltipText("Importovat veřejný klíč – zprávy pro tohoto odesílatele se pak budou šifrovat")
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
					err = fmt.Errorf("příloha neobsahuje veřejný PGP klíč")
				} else {
					emails, err = pgp.ImportKey(string(data))
				}
			}
			ui(func() {
				b.SetSensitive(true)
				if err != nil {
					m.a.toast("Import klíče selhal: " + err.Error())
					return
				}
				b.SetIconName("object-select-symbolic")
				m.a.toast("Klíč importován pro " + strings.Join(emails, ", "))
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
	d.SetTitle("Automatická odpověď")
	d.SetContentWidth(560)
	d.SetContentHeight(640)
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	save := gtk.NewButtonWithLabel("Uložit")
	save.AddCSSClass("suggested-action")
	save.SetSensitive(false)
	hb.PackEnd(save)
	tv.AddTopBar(hb)
	toasts := adw.NewToastOverlay()

	page := adw.NewPreferencesPage()
	g := adw.NewPreferencesGroup()
	g.SetDescription("Proton odpovídá sám na serveru, i když máte počítač vypnutý. Odpověď dostane každý odesílatel nejvýš jednou za čas. Funkce vyžaduje placený tarif Proton.")
	enabled := adw.NewSwitchRow()
	enabled.SetTitle("Odpovídat automaticky")
	enabled.SetSubtitle(a.acc.Email())
	subject := adw.NewEntryRow()
	subject.SetTitle("Předmět")
	g.Add(enabled)
	g.Add(subject)
	page.Add(g)

	tg := adw.NewPreferencesGroup()
	tg.SetTitle("Doba")
	untilRow := adw.NewSwitchRow()
	untilRow.SetTitle("Ukončit automaticky")
	untilRow.SetSubtitle("Jinak platí, dokud ji nevypnete")
	cal := gtk.NewCalendar()
	calRow := adw.NewActionRow()
	calRow.SetTitle("Poslední den")
	dateBtn := gtk.NewMenuButton()
	dateBtn.SetVAlign(gtk.AlignCenter)
	pop := gtk.NewPopover()
	pop.SetChild(cal)
	dateBtn.SetPopover(pop)
	calRow.AddSuffix(dateBtn)
	end := time.Now().AddDate(0, 0, 7)
	setEnd := func(t time.Time) {
		end = endOfDay(t)
		dateBtn.SetLabel(czDays[end.Weekday()] + " " + end.Format("2. 1. 2006"))
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
	mg.SetTitle("Text odpovědi")
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
			subject.SetText(orDefault(ar.Subject, "Nejsem k zastižení"))
			msg := ar.Message
			if msg == "" {
				msg = "Dobrý den,\n\ndo %s nejsem k zastižení a e-maily čtu jen občas. Odpovím po návratu.\n\nDěkuji za pochopení."
				msg = fmt.Sprintf(msg, end.Format("2. 1."))
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
				toasts.AddToast(adw.NewToast("Poslední den už uplynul"))
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
					a.toast("Automatická odpověď je zapnutá")
				} else {
					a.toast("Automatická odpověď je vypnutá")
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
