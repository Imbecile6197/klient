package ui

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/rules"
)

// ---- Unsubscribe --------------------------------------------------------

// unsubscribeButton returns an i18n.T("Unsubscribe") button for mailing-list
// messages, or nil.
func (m *mainView) unsubscribeButton(msg *protonmail.Message) gtk.Widgetter {
	u := mailparse.ParseUnsubscribe(msg.Headers)
	if !u.Any() {
		return nil
	}
	b := gtk.NewButton()
	content := adw.NewButtonContent()
	content.SetIconName("mail-mark-junk-symbolic")
	content.SetLabel(i18n.T("Unsubscribe"))
	b.SetChild(content)
	b.AddCSSClass("unsubscribe-button")
	b.SetTooltipText(i18n.T("This message comes from a mailing list – you can unsubscribe from it"))
	b.SetHAlign(gtk.AlignStart)
	b.ConnectClicked(func() { m.a.unsubscribe(msg, u, b) })
	return b
}

func (a *App) unsubscribe(msg *protonmail.Message, u mailparse.Unsubscribe, b *gtk.Button) {
	sender := ""
	if msg.Meta.Sender != nil {
		sender = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	var body string
	switch {
	case u.OneClick:
		body = i18n.T("Klient sends the sender an unsubscribe request (one click, as defined by RFC 8058). The sender will see your IP address, just as when you click a link.")
	case u.Mailto != "":
		body = i18n.T("A prefilled unsubscribe request opens for you to send.")
	default:
		body = i18n.T("The sender's unsubscribe page opens in the browser.")
	}
	d := adw.NewAlertDialog(fmt.Sprintf(i18n.T("Unsubscribe from %s?"), sender), body)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("ok", i18n.T("Unsubscribe"))
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "ok" {
			return
		}
		switch {
		case u.OneClick:
			b.SetSensitive(false)
			go func() {
				err := oneClickUnsubscribe(a.ctx, u.HTTP)
				ui(func() {
					if err != nil {
						b.SetSensitive(true)
						a.toast(i18n.T("Unsubscribing failed: ") + err.Error())
						return
					}
					b.SetLabel(i18n.T("Unsubscribed"))
					a.toast(i18n.T("Unsubscribe request sent"))
				})
			}()
		case u.Mailto != "":
			a.openMailto(u.Mailto)
		default:
			gtk.NewURILauncher(u.HTTP).Launch(context.Background(), a.gtkWindow(), nil)
		}
	})
	d.Present(a.win)
}

func oneClickUnsubscribe(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf(i18n.T("the server responded with %s"), res.Status)
	}
	return nil
}

// ---- Print & export -------------------------------------------------------

// printMessage shows a print preview (the message with a header block) and
// the system print dialog.
func (a *App) printMessage(msg *protonmail.Message) {
	meta := msg.Meta
	var to []string
	for _, r := range meta.ToList {
		to = append(to, mailparse.DisplayAddress(r))
	}
	head := fmt.Sprintf(`<table style="font-family:sans-serif;font-size:12px;margin-bottom:12px">
<tr><td><b>%s</b></td><td>%s</td></tr><tr><td><b>%s</b></td><td>%s</td></tr>
<tr><td><b>%s</b></td><td>%s</td></tr><tr><td><b>%s</b></td><td>%s</td></tr></table><hr>`,
		i18n.T("From:"), html.EscapeString(mailparse.DisplayAddress(meta.Sender)),
		i18n.T("To:"), html.EscapeString(strings.Join(to, ", ")),
		i18n.T("Date:"), time.Unix(meta.Time, 0).Format(i18n.T("Jan 2, 2006 15:04")),
		i18n.T("Subject:"), html.EscapeString(meta.Subject))
	body := msg.HTML
	if body == "" {
		body = mailparse.PlainToHTML(msg.Text)
	}
	sender := ""
	if meta.Sender != nil {
		sender = meta.Sender.Address
	}
	doc := mailparse.PrepareHTML(head+body, nil, a.remoteAllowed(sender))

	d := adw.NewDialog()
	d.SetTitle(i18n.T("Print"))
	d.SetContentWidth(760)
	d.SetContentHeight(820)
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	printBtn := gtk.NewButtonWithLabel(i18n.C("button", "Print…"))
	printBtn.AddCSSClass("suggested-action")
	hb.PackEnd(printBtn)
	tv.AddTopBar(hb)
	view := newHTMLView(doc, func(string) {})
	sw := gtk.NewScrolledWindow()
	sw.SetChild(view)
	sw.SetVExpand(true)
	tv.SetContent(sw)
	d.SetChild(tv)
	printBtn.ConnectClicked(func() {
		webkit.NewPrintOperation(view).RunDialog(a.gtkWindow())
	})
	d.Present(a.win)
}

func safeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		s = "zprava"
	}
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

// exportMessage saves the message as a standard .eml file.
func (a *App) exportMessage(msg *protonmail.Message) {
	dlg := gtk.NewFileDialog()
	dlg.SetInitialName(safeFilename(msg.Meta.Subject) + ".eml")
	dlg.Save(context.Background(), a.gtkWindow(), func(res gio.AsyncResulter) {
		file, err := dlg.SaveFinish(res)
		if err != nil || file == nil {
			return
		}
		path := file.Path()
		go func() {
			data, err := a.acc.ExportEML(a.ctx, msg.Meta.ID)
			if err == nil {
				err = os.WriteFile(path, data, 0o600)
			}
			ui(func() {
				if err != nil {
					a.toast(i18n.T("Export failed: ") + err.Error())
					return
				}
				a.toast(fmt.Sprintf(i18n.T("Message saved as %s"), path))
			})
		}()
	})
}

// messageMenu is the "more" button of a message card.
func (m *mainView) messageMenu(msg *protonmail.Message) gtk.Widgetter {
	pop := gtk.NewPopover()
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	item := func(icon, label string, f func()) {
		b := gtk.NewButton()
		content := adw.NewButtonContent()
		content.SetIconName(icon)
		content.SetLabel(label)
		content.SetHAlign(gtk.AlignStart)
		b.SetChild(content)
		b.AddCSSClass("flat")
		b.ConnectClicked(func() {
			pop.Popdown()
			f()
		})
		box.Append(b)
	}
	item("document-print-symbolic", i18n.T("Print…"), func() { m.a.printMessage(msg) })
	item("document-save-as-symbolic", i18n.T("Save as .eml…"), func() { m.a.exportMessage(msg) })
	item("bookmark-new-symbolic", i18n.T("Suggest a Label (AI)"), func() { m.suggestLabelFor(msg) })
	if m.a.acc.Caps().Contacts {
		item("contact-new-symbolic", i18n.T("Add the Sender to Contacts…"), func() {
			name, email := "", ""
			if msg.Meta.Sender != nil {
				name, email = mailparse.DisplayName(msg.Meta.Sender), msg.Meta.Sender.Address
				if name == email {
					name = ""
				}
			}
			m.a.addContactDialog(m.a.win, name, email, nil)
		})
	}
	item("edit-find-replace-symbolic", i18n.T("Create a Rule for the Sender…"), func() {
		from := ""
		if msg.Meta.Sender != nil {
			from = msg.Meta.Sender.Address
		}
		m.a.editRule(-1, rules.Rule{Name: fmt.Sprintf(i18n.T("From %s"), from), Field: rules.FieldFrom, Contains: from, Action: rules.ActionMove, Enabled: true}, nil)
	})
	pop.SetChild(box)
	btn := gtk.NewMenuButton()
	btn.SetIconName("view-more-symbolic")
	btn.SetTooltipText(i18n.T("More Actions"))
	btn.AddCSSClass("flat")
	btn.SetPopover(pop)
	return btn
}

// ---- Rules ----------------------------------------------------------------

func ruleMessage(meta protonmail.Summary) rules.Message {
	return rules.Message{From: meta.Sender, To: append(append([]*mail.Address{}, meta.ToList...), meta.CCList...), Subject: meta.Subject}
}

// applyRules runs the user's rules on one message and returns what was done.
func (a *App) applyRules(ctx context.Context, acc mailbox.Account, meta protonmail.Summary) []string {
	var done []string
	for _, r := range rules.Matching(a.cfg.Rules, ruleMessage(meta)) {
		var err error
		switch r.Action {
		case rules.ActionMove:
			err = acc.Move(ctx, r.Target, meta.ID)
		case rules.ActionLabel:
			err = acc.SetLabel(ctx, r.Target, true, meta.ID)
		case rules.ActionRead:
			err = acc.MarkRead(ctx, meta.ID)
		case rules.ActionStar:
			err = acc.SetLabel(ctx, protonmail.StarredID, true, meta.ID)
		}
		if err == nil {
			done = append(done, orDefault(r.Name, rules.ActionName(r.Action)))
		}
	}
	return done
}

// applyRulesToInbox runs the rules over the newest inbox messages.
func (a *App) applyRulesToInbox() {
	if len(a.cfg.Rules) == 0 {
		a.toast(i18n.T("You have no rules (Preferences → Rules)"))
		return
	}
	a.toast(i18n.T("Applying the rules to the inbox…"))
	accs := []mailbox.Account{a.acc}
	if a.isUnified() {
		accs = a.openAccounts()
	}
	go func() {
		n := 0
		var err error
		for _, acc := range accs {
			var msgs []protonmail.Summary
			if msgs, err = acc.List(a.ctx, protonmail.InboxID, 0, 150); err != nil {
				break
			}
			for _, s := range msgs {
				if len(a.applyRules(a.ctx, acc, s)) > 0 {
					n++
				}
			}
		}
		ui(func() {
			if err != nil {
				a.toast(i18n.T("The rules failed: ") + err.Error())
				return
			}
			a.toast(fmt.Sprintf(i18n.N("Rules applied to %d message", "Rules applied to %d messages", n), n))
			if a.mv != nil {
				a.mv.scheduleRefresh()
			}
		})
	}()
}

// editRule shows the rule editor; index -1 adds a new rule.
func (a *App) editRule(index int, r rules.Rule, saved func()) {
	var labels []protonmail.UserLabel
	if a.mv != nil {
		labels = a.mv.labels
	}
	type target struct{ id, name string }
	var folders, labelTargets []target
	for _, f := range []target{{protonmail.InboxID, i18n.T("Inbox")}, {protonmail.ArchiveID, i18n.T("Archive")}, {protonmail.SpamID, i18n.T("Spam")}, {protonmail.TrashID, i18n.T("Trash")}} {
		folders = append(folders, f)
	}
	for _, l := range labels {
		if l.Folder {
			folders = append(folders, target{l.ID, l.Name})
		} else {
			labelTargets = append(labelTargets, target{l.ID, l.Name})
		}
	}

	d := adw.NewAlertDialog(i18n.T("Rule"), i18n.T("It applies to every new message in the inbox."))
	group := adw.NewPreferencesGroup()
	name := adw.NewEntryRow()
	name.SetTitle(i18n.C("rule", "Name"))
	name.SetText(r.Name)
	fields := []rules.Field{rules.FieldFrom, rules.FieldTo, rules.FieldSubject}
	fieldRow := adw.NewComboRow()
	fieldRow.SetTitle(i18n.T("When"))
	var fieldNames []string
	for i, f := range fields {
		fieldNames = append(fieldNames, rules.FieldName(f))
		if f == r.Field {
			defer fieldRow.SetSelected(uint(i))
		}
	}
	fieldRow.SetModel(gtk.NewStringList(fieldNames))
	contains := adw.NewEntryRow()
	contains.SetTitle(i18n.T("contains"))
	contains.SetText(r.Contains)
	actions := []rules.Action{rules.ActionMove, rules.ActionLabel, rules.ActionRead, rules.ActionStar}
	actionRow := adw.NewComboRow()
	actionRow.SetTitle(i18n.T("Then"))
	var actionNames []string
	for _, ac := range actions {
		actionNames = append(actionNames, rules.ActionName(ac))
	}
	actionRow.SetModel(gtk.NewStringList(actionNames))
	targetRow := adw.NewComboRow()
	targetRow.SetTitle(i18n.T("Target"))
	var current []target
	setTargets := func() {
		switch actions[actionRow.Selected()] {
		case rules.ActionMove:
			current = folders
		case rules.ActionLabel:
			current = labelTargets
		default:
			current = nil
		}
		var names []string
		sel := uint(0)
		for i, t := range current {
			names = append(names, t.name)
			if t.id == r.Target {
				sel = uint(i)
			}
		}
		targetRow.SetModel(gtk.NewStringList(names))
		targetRow.SetSelected(sel)
		targetRow.SetVisible(len(current) > 0)
	}
	for i, ac := range actions {
		if ac == r.Action {
			actionRow.SetSelected(uint(i))
		}
	}
	actionRow.NotifyProperty("selected", setTargets)
	setTargets()
	for _, w := range []gtk.Widgetter{name, fieldRow, contains, actionRow, targetRow} {
		group.Add(w)
	}
	d.SetExtraChild(group)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("save", i18n.T("Save"))
	d.SetResponseAppearance("save", adw.ResponseSuggested)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(resp string) {
		if resp != "save" {
			return
		}
		nr := rules.Rule{
			Name: strings.TrimSpace(name.Text()), Field: fields[fieldRow.Selected()],
			Contains: strings.TrimSpace(contains.Text()), Action: actions[actionRow.Selected()], Enabled: true,
		}
		if len(current) > 0 {
			if i := int(targetRow.Selected()); i >= 0 && i < len(current) {
				nr.Target = current[i].id
			}
		}
		if nr.Contains == "" || ((nr.Action == rules.ActionMove || nr.Action == rules.ActionLabel) && nr.Target == "") {
			a.toast(i18n.T("The rule needs a condition text and a target"))
			return
		}
		if index >= 0 && index < len(a.cfg.Rules) {
			a.cfg.Rules[index] = nr
		} else {
			a.cfg.Rules = append(a.cfg.Rules, nr)
		}
		a.saveConfig()
		a.toast(i18n.T("Rule saved"))
		if saved != nil {
			saved()
		}
	})
	d.Present(a.win)
}

// ---- Default mail application ---------------------------------------------

const desktopID = AppID + ".desktop"

func isDefaultMailApp() bool {
	info := gio.AppInfoGetDefaultForURIScheme("mailto")
	return info != nil && info.ID() == desktopID
}

// setDefaultMailApp registers Klient for mailto: links (needs the installed
// .desktop file, see `make install`).
func setDefaultMailApp() error {
	out, err := exec.Command("gio", "mime", "x-scheme-handler/mailto", desktopID).CombinedOutput()
	if err != nil {
		return fmt.Errorf(i18n.T("%v: %s (install the application with make install)"), err, strings.TrimSpace(string(out)))
	}
	return nil
}
