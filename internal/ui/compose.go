package ui

import (
	"context"
	"fmt"
	"mime"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/richtext"
)

func parseAddrs(s string) ([]*mail.Address, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ","))
	if s == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		return nil, fmt.Errorf("neplatná adresa v „%s“", s)
	}
	return list, nil
}

func formatAddrs(list []*mail.Address) string {
	var parts []string
	for _, a := range list {
		parts = append(parts, mailparse.DisplayAddress(a))
	}
	return strings.Join(parts, ", ")
}

func quote(msg *protonmail.Message) string {
	from := ""
	if msg.Meta.Sender != nil {
		from = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\nDne %s napsal(a) %s:\n", time.Unix(msg.Meta.Time, 0).Format("2. 1. 2006 v 15:04"), from)
	for _, line := range strings.Split(strings.TrimRight(msg.Text, "\n"), "\n") {
		sb.WriteString("> " + line + "\n")
	}
	return sb.String()
}

func forwardHeader(msg *protonmail.Message) string {
	from := ""
	if msg.Meta.Sender != nil {
		from = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	return fmt.Sprintf("\n\n---------- Přeposlaná zpráva ----------\nOd: %s\nDatum: %s\nPředmět: %s\nKomu: %s\n\n%s",
		from, time.Unix(msg.Meta.Time, 0).Format("2. 1. 2006 15:04"), msg.Meta.Subject,
		formatAddrs(msg.Meta.ToList), msg.Text)
}

func prefixSubject(prefix, subj string) string {
	if strings.HasPrefix(strings.ToLower(subj), strings.ToLower(prefix)) {
		return subj
	}
	return prefix + " " + subj
}

// openCompose opens an empty message window.
func (a *App) openCompose(_ *protonmail.Message) {
	a.composer(&protonmail.Draft{SignExternal: true}, nil, protonmail.ActionNew)
}

// openComposeFor starts a reply, reply-all or forward of msg.
func (a *App) openComposeFor(msg *protonmail.Message, action protonmail.ComposeAction) {
	d := &protonmail.Draft{
		FromAddressID: msg.Meta.AddressID, ParentID: msg.Meta.ID,
		Action: action, SignExternal: true,
	}
	switch action {
	case protonmail.ActionReply, protonmail.ActionReplyAll:
		d.Subject = prefixSubject("Re:", msg.Meta.Subject)
		rcpt := msg.Meta.Sender
		if len(msg.Meta.ReplyTos) > 0 {
			rcpt = msg.Meta.ReplyTos[0]
		}
		// Replying to our own sent message goes to its recipients.
		if rcpt != nil && a.acc.IsOwnAddress(rcpt.Address) {
			d.To = msg.Meta.ToList
		} else if rcpt != nil {
			d.To = []*mail.Address{rcpt}
		}
		if action == protonmail.ActionReplyAll {
			seen := map[string]bool{}
			for _, r := range d.To {
				seen[strings.ToLower(r.Address)] = true
			}
			add := func(dst *[]*mail.Address, list []*mail.Address) {
				for _, r := range list {
					k := strings.ToLower(r.Address)
					if seen[k] || a.acc.IsOwnAddress(r.Address) {
						continue
					}
					seen[k] = true
					*dst = append(*dst, r)
				}
			}
			add(&d.To, msg.Meta.ToList)
			add(&d.CC, msg.Meta.CCList)
		}
	case protonmail.ActionForward:
		d.Subject = prefixSubject("Fwd:", msg.Meta.Subject)
	}
	a.composer(d, msg, action)
}

// openMailto opens the composer prefilled from a mailto: URI (RFC 6068).
func (a *App) openMailto(uri string) {
	if a.acc == nil {
		return
	}
	u, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(u.Scheme, "mailto") {
		return
	}
	d := &protonmail.Draft{SignExternal: true}
	q := u.Query()
	addrs := func(s string) []*mail.Address {
		l, _ := parseAddrs(s)
		return l
	}
	to := u.Opaque
	if to == "" {
		to = u.Path
	}
	if dec, err := url.PathUnescape(to); err == nil {
		to = dec
	}
	d.To = addrs(strings.Join(append([]string{to}, q["to"]...), ","))
	d.CC = addrs(strings.Join(q["cc"], ","))
	d.BCC = addrs(strings.Join(q["bcc"], ","))
	d.Subject = q.Get("subject")
	d.Body = q.Get("body")
	a.composer(d, nil, protonmail.ActionNew)
}

// openDraft loads a saved draft into the composer.
func (a *App) openDraft(id string) {
	go func() {
		d, _, err := a.acc.OpenDraft(a.ctx, id)
		ui(func() {
			if err != nil {
				a.toast("Koncept se nepodařilo otevřít: " + err.Error())
				return
			}
			a.composer(d, nil, protonmail.ActionNew)
		})
	}()
}

// composer is the message window. orig is the message being replied to or
// forwarded (nil for new messages and reopened drafts).
func (a *App) composer(d *protonmail.Draft, orig *protonmail.Message, action protonmail.ComposeAction) {
	// The composer keeps its account even if the window switches to another.
	acc := a.acc
	win := adw.NewWindow()
	win.SetTransientFor(a.gtkWindow())
	win.SetDefaultSize(780, 760)

	toasts := adw.NewToastOverlay()
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	titleText := map[protonmail.ComposeAction]string{
		protonmail.ActionNew: "Nová zpráva", protonmail.ActionReply: "Odpověď",
		protonmail.ActionReplyAll: "Odpověď všem", protonmail.ActionForward: "Přeposlání",
	}[action]
	if d.ID != "" {
		titleText = "Koncept"
	}
	title := adw.NewWindowTitle(titleText, "")
	win.SetTitle(titleText)
	hb.SetTitleWidget(title)

	send := adw.NewSplitButton()
	send.SetLabel("Odeslat")
	send.AddCSSClass("suggested-action")
	send.SetTooltipText("Odeslat (Ctrl+Enter)")
	send.SetDropdownTooltip("Naplánovat odeslání")
	attachBtn := gtk.NewButtonFromIconName("mail-attachment-symbolic")
	attachBtn.SetTooltipText("Přiložit soubory")
	saveBtn := gtk.NewButtonFromIconName("document-save-symbolic")
	saveBtn.SetTooltipText("Uložit koncept (Ctrl+S)")
	discardBtn := gtk.NewButtonFromIconName("user-trash-symbolic")
	discardBtn.SetTooltipText("Zahodit")
	hb.PackEnd(send)
	hb.PackEnd(attachBtn)
	hb.PackStart(discardBtn)
	hb.PackStart(saveBtn)
	tv.AddTopBar(hb)

	// --- header fields
	group := adw.NewPreferencesGroup()
	addrs := acc.SendAddresses()
	var fromNames []string
	for _, ad := range addrs {
		fromNames = append(fromNames, ad.Email)
	}
	from := adw.NewComboRow()
	from.SetTitle("Od")
	from.SetModel(gtk.NewStringList(fromNames))
	for i, ad := range addrs {
		if ad.ID == d.FromAddressID {
			from.SetSelected(uint(i))
		}
	}
	to := adw.NewEntryRow()
	to.SetTitle("Komu")
	to.SetText(formatAddrs(d.To))
	cc := adw.NewEntryRow()
	cc.SetTitle("Kopie")
	cc.SetText(formatAddrs(d.CC))
	bcc := adw.NewEntryRow()
	bcc.SetTitle("Skrytá kopie")
	bcc.SetText(formatAddrs(d.BCC))
	subject := adw.NewEntryRow()
	subject.SetTitle("Předmět")
	subject.SetText(d.Subject)
	for _, w := range []gtk.Widgetter{from, to, cc, bcc, subject} {
		group.Add(w)
	}
	suggestions := a.addressCompletion([]*adw.EntryRow{to, cc, bcc})

	optGroup := adw.NewPreferencesGroup()
	sign := adw.NewSwitchRow()
	sign.SetTitle("Podepsat PGP i nešifrované zprávy")
	sign.SetSubtitle("Příjemci s PGP ověří, že zpráva je od vás a nebyla změněna")
	sign.SetActive(d.SignExternal)
	security := adw.NewActionRow()
	security.SetTitle("Zabezpečení")
	security.SetSubtitle("Zadejte příjemce")
	security.SetSubtitleLines(6)
	check := gtk.NewButtonWithLabel("Zkontrolovat")
	check.SetVAlign(gtk.AlignCenter)
	check.AddCSSClass("flat")
	security.AddSuffix(check)
	attachKey := adw.NewSwitchRow()
	attachKey.SetTitle("Přiložit můj veřejný klíč")
	attachKey.SetSubtitle("Příjemce s PGP vám pak může odpovědět šifrovaně")
	attachKey.SetActive(a.cfg.AttachPublicKey && d.ID == "")
	optGroup.Add(sign)
	optGroup.Add(attachKey)
	optGroup.Add(security)
	// PGP options only where the service handles keys (Proton for now).
	e2e := acc.Caps().E2E
	optGroup.SetVisible(e2e)
	if !e2e {
		attachKey.SetActive(false)
		sign.SetActive(false)
	}

	// --- attachments
	attGroup := adw.NewPreferencesGroup()
	attGroup.SetTitle("Přílohy")
	var attRows []gtk.Widgetter
	var refreshAtts func()
	refreshAtts = func() {
		for _, r := range attRows {
			attGroup.Remove(r)
		}
		attRows = nil
		var total int64
		for _, att := range d.Attachments {
			att := att
			total += att.Size
			row := adw.NewActionRow()
			row.SetTitle(att.Name)
			row.SetSubtitle(humanSize(att.Size))
			row.AddPrefix(gtk.NewImageFromIconName("mail-attachment-symbolic"))
			rm := gtk.NewButtonFromIconName("list-remove-symbolic")
			rm.SetTooltipText("Odebrat")
			rm.SetVAlign(gtk.AlignCenter)
			rm.AddCSSClass("flat")
			rm.ConnectClicked(func() {
				var keep []*protonmail.Outgoing
				for _, x := range d.Attachments {
					if x != att {
						keep = append(keep, x)
					}
				}
				if att.UploadedID != "" {
					d.Recreate = true
					// Recreating the draft re-uploads everything, so keep data.
				}
				d.Attachments = keep
				refreshAtts()
			})
			row.AddSuffix(rm)
			attGroup.Add(row)
			attRows = append(attRows, row)
		}
		attGroup.SetVisible(len(d.Attachments) > 0)
		if total > protonmail.MaxAttachmentsSize {
			attGroup.SetDescription(fmt.Sprintf("Celkem %s – překračuje limit Protonu 25 MB!", humanSize(total)))
		} else {
			attGroup.SetDescription(fmt.Sprintf("Celkem %s, šifrují se stejně jako text zprávy", humanSize(total)))
		}
	}

	// --- body
	editor := newRichEditor(win)
	body := editor.view
	bodySW := gtk.NewScrolledWindow()
	bodySW.SetChild(body)
	bodySW.SetVExpand(true)
	bodySW.SetMinContentHeight(260)
	bodyBox := gtk.NewBox(gtk.OrientationVertical, 0)
	bodyBox.Append(editor.toolbar)
	bodyBox.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	bodyBox.Append(bodySW)
	bodyFrame := gtk.NewFrame("")
	bodyFrame.SetChild(bodyBox)

	// Text below the user's own writing: signature and quoted/forwarded message.
	tail := ""
	if d.ID == "" {
		if sig := strings.TrimSpace(a.cfg.Signature); sig != "" {
			tail += "\n\n-- \n" + sig
		}
		switch {
		case orig != nil && action == protonmail.ActionForward:
			tail += forwardHeader(orig)
		case orig != nil:
			tail += quote(orig)
		}
		editor.SetText(d.Body + tail) // d.Body is set for mailto: links
	} else {
		editor.SetText(d.Body)
	}

	aiEntry := gtk.NewEntry()
	aiEntry.SetPlaceholderText("Pokyn pro AI, např. „zdvořile odmítni“ nebo „přelož do angličtiny“")
	aiEntry.SetHExpand(true)
	aiLabel := "Napsat s AI"
	if orig != nil && action != protonmail.ActionForward {
		aiLabel = "Navrhnout odpověď"
	}
	aiBtn := gtk.NewButtonWithLabel(aiLabel)
	aiBox := gtk.NewBox(gtk.OrientationHorizontal, 0)
	aiBox.AddCSSClass("linked")
	aiBox.Append(aiEntry)
	aiBox.Append(aiBtn)

	content := gtk.NewBox(gtk.OrientationVertical, 12)
	content.SetMarginTop(12)
	content.SetMarginBottom(12)
	content.SetMarginStart(12)
	content.SetMarginEnd(12)
	content.Append(group)
	content.Append(suggestions)
	content.Append(optGroup)
	content.Append(attGroup)
	content.Append(aiBox)
	content.Append(bodyFrame)
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(920)
	clamp.SetChild(content)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetChild(clamp)
	tv.SetContent(sw)
	toasts.SetChild(tv)
	win.SetContent(toasts)
	refreshAtts()

	getBody := editor.Text
	localToast := func(s string) { toasts.AddToast(adw.NewToast(s)) }

	// Forwarded attachments are downloaded and decrypted in the background.
	if orig != nil && action == protonmail.ActionForward && len(orig.Attachments) > 0 {
		attGroup.SetVisible(true)
		attGroup.SetDescription("Načítám přílohy přeposílané zprávy…")
		send.SetSensitive(false)
		go func() {
			atts, err := acc.ForwardAttachments(a.ctx, orig)
			ui(func() {
				send.SetSensitive(true)
				if err != nil {
					localToast("Přílohy se nepodařilo načíst: " + err.Error())
				}
				d.Attachments = append(d.Attachments, atts...)
				refreshAtts()
			})
		}()
	}

	// collect copies the form into d.
	collect := func() error {
		t, err := parseAddrs(to.Text())
		if err != nil {
			return err
		}
		c, err := parseAddrs(cc.Text())
		if err != nil {
			return err
		}
		b, err := parseAddrs(bcc.Text())
		if err != nil {
			return err
		}
		sel := int(from.Selected())
		if sel < 0 || sel >= len(addrs) {
			return fmt.Errorf("vyberte adresu odesílatele")
		}
		d.FromAddressID = addrs[sel].ID
		d.To, d.CC, d.BCC = t, c, b
		d.Subject = subject.Text()
		spans := editor.Spans()
		d.Body = richtext.Plain(spans)
		d.HTML = ""
		if richtext.Formatted(spans) {
			d.HTML = richtext.HTML(spans)
		}
		d.SignExternal = sign.Active()
		a.syncKeyAttachment(acc, d, attachKey.Active())
		refreshAtts()
		return nil
	}
	allRecipients := func() []*mail.Address {
		return append(append(append([]*mail.Address{}, d.To...), d.CC...), d.BCC...)
	}

	describe := func(modes []protonmail.RecipientMode) (string, bool) {
		var lines []string
		allSecure := true
		for _, md := range modes {
			switch md.Scheme {
			case "proton":
				lines = append(lines, md.Address+": šifrováno end-to-end (Proton)")
			case "pgp":
				lines = append(lines, md.Address+": šifrováno PGP ("+shortFP(md.KeyFP)+")")
			default:
				allSecure = false
				lines = append(lines, md.Address+": BEZ ŠIFROVÁNÍ (není znám klíč)")
			}
		}
		return strings.Join(lines, "\n"), allSecure
	}

	check.ConnectClicked(func() {
		if err := collect(); err != nil {
			localToast(err.Error())
			return
		}
		all := allRecipients()
		if len(all) == 0 {
			return
		}
		security.SetSubtitle("Zjišťuji klíče příjemců…")
		go func() {
			modes := acc.PlanEncryption(a.ctx, all)
			ui(func() {
				text, _ := describe(modes)
				security.SetSubtitle(text)
			})
		}()
	})

	attachBtn.ConnectClicked(func() {
		dlg := gtk.NewFileDialog()
		dlg.SetTitle("Přiložit soubory")
		dlg.OpenMultiple(context.Background(), &win.Window, func(res gio.AsyncResulter) {
			files, err := dlg.OpenMultipleFinish(res)
			if err != nil || files == nil {
				return
			}
			for i := uint(0); i < files.NItems(); i++ {
				f, ok := files.Item(i).Cast().(gio.Filer)
				if !ok {
					continue
				}
				out, err := readAttachment(f.Path())
				if err != nil {
					localToast(err.Error())
					continue
				}
				d.Attachments = append(d.Attachments, out)
			}
			refreshAtts()
		})
	})

	aiBtn.ConnectClicked(func() {
		if !a.ai.HasAssistant() {
			localToast("AI asistent není nastavený (Předvolby → AI)")
			return
		}
		instruction := strings.TrimSpace(aiEntry.Text())
		draft := getBody()
		userPart := strings.TrimSpace(strings.TrimSuffix(draft, tail))
		isReply := orig != nil && action != protonmail.ActionForward
		if !isReply && instruction == "" {
			localToast("Napište pokyn pro AI")
			return
		}
		aiBtn.SetSensitive(false)
		aiBtn.SetLabel("Píšu…")
		subj := subject.Text()
		go func() {
			var out string
			var err error
			if isReply && userPart == "" {
				sender := ""
				if orig.Meta.Sender != nil {
					sender = mailparse.DisplayAddress(orig.Meta.Sender)
				}
				out, err = a.ai.DraftReply(a.ctx, sender, orig.Meta.Subject, orig.Text, instruction)
			} else {
				if instruction == "" {
					instruction = "Vylepši text, oprav chyby a zachovej smysl."
				}
				text := userPart
				if text == "" {
					text = "(prázdný koncept – napiš nový e-mail podle pokynu; předmět: " + subj + ")"
				}
				out, err = a.ai.Improve(a.ctx, text, instruction)
			}
			ui(func() {
				aiBtn.SetSensitive(true)
				aiBtn.SetLabel(aiLabel)
				if err != nil {
					localToast("AI selhala: " + ai.Explain(a.cfg.AssistantProvider, err).Text)
					return
				}
				editor.SetText(strings.TrimSpace(out) + tail)
			})
		}()
	})

	sent := false
	busy := false
	saveDraft := func(done func(error)) {
		if err := collect(); err != nil {
			done(err)
			return
		}
		busy = true
		go func() {
			err := acc.SaveDraft(a.ctx, d)
			ui(func() {
				busy = false
				done(err)
			})
		}()
	}
	saveBtn.ConnectClicked(func() {
		saveDraft(func(err error) {
			if err != nil {
				localToast(err.Error())
				return
			}
			localToast("Koncept uložen")
		})
	})

	sendNow := func() {
		send.SetSensitive(false)
		send.SetLabel("Odesílám…")
		busy = true
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
			defer cancel()
			err := acc.Send(ctx, d)
			ui(func() {
				busy = false
				if err != nil {
					d.DeliveryTime = time.Time{}
					send.SetSensitive(true)
					send.SetLabel("Odeslat")
					win.SetVisible(true)
					win.Present()
					localToast(err.Error())
					a.toast("Odeslání selhalo, zpráva je znovu otevřená")
					return
				}
				for _, ad := range allRecipients() {
					a.filter.RecordContact(ad.Address)
				}
				sent = true
				win.Close()
				switch {
				case !d.DeliveryTime.IsZero() && !acc.Caps().ServerSchedule:
					a.toast("Naplánováno – odejde " + formatWhen(d.DeliveryTime) + ", pokud Klient poběží (třeba na pozadí)")
					if a.mv != nil && a.acc == acc {
						a.mv.reloadFolders()
					}
				case !d.DeliveryTime.IsZero():
					a.toast("Naplánováno – odejde " + formatWhen(d.DeliveryTime) + " (" + countdown(d.DeliveryTime) + ")")
				case a.cfg.SendDelay <= 0:
					a.toast("Zpráva odeslána")
				}
				if a.mv != nil {
					a.mv.scheduleRefresh()
				}
			})
		}()
	}

	var startDelay func(delay int, undone *bool)
	// doSend waits SendDelay seconds with an "Undo" toast. The window is only
	// hidden meanwhile, so undo brings it back exactly as it was.
	doSend := func() {
		delay := a.cfg.SendDelay
		if delay <= 0 {
			sendNow()
			return
		}
		undone := false
		busy = true
		win.SetVisible(false)
		// Keep the message safe as a draft first: if the app is closed during
		// the delay, it stays in Drafts instead of being lost.
		go func() {
			err := acc.SaveDraft(a.ctx, d)
			ui(func() {
				if err != nil {
					a.toast("Koncept se nepodařilo uložit: " + err.Error())
				}
				startDelay(delay, &undone)
			})
		}()
	}
	startDelay = func(delay int, undone *bool) {
		a.undoToast(delay, "Zpráva se odešle", func(ok bool) {
			if !ok {
				*undone = true
				busy = false
				win.SetVisible(true)
				win.Present()
				return
			}
			busy = false
			sendNow()
			a.toast("Zpráva odeslána")
		})
	}

	// trySend checks the message and sends it now (after the undo delay) or,
	// with a non-zero when, schedules it on the server.
	trySend := func(when time.Time) {
		if busy {
			return
		}
		if err := collect(); err != nil {
			localToast(err.Error())
			return
		}
		all := allRecipients()
		if len(all) == 0 {
			localToast("Zadejte alespoň jednoho příjemce")
			return
		}
		var total int64
		for _, att := range d.Attachments {
			total += att.Size
		}
		if total > protonmail.MaxAttachmentsSize {
			localToast("Přílohy překračují limit 25 MB")
			return
		}
		send.SetSensitive(false)
		if !e2e {
			// No end-to-end encryption on this service: nothing to warn about.
			send.SetSensitive(true)
			if when.IsZero() {
				doSend()
			} else {
				d.DeliveryTime = when
				sendNow()
			}
			return
		}
		// Warn before sending anything unencrypted.
		go func() {
			modes := acc.PlanEncryption(a.ctx, all)
			ui(func() {
				text, secure := describe(modes)
				security.SetSubtitle(text)
				send.SetSensitive(true)
				go2 := func() {
					if when.IsZero() {
						doSend()
						return
					}
					d.DeliveryTime = when
					sendNow()
				}
				if secure {
					go2()
					return
				}
				dlg := adw.NewAlertDialog("Odeslat bez šifrování?", "Někteří příjemci nemají známý veřejný klíč, zpráva jim dorazí nešifrovaně:\n\n"+text)
				dlg.AddResponse("cancel", "Zrušit")
				dlg.AddResponse("send", "Přesto odeslat")
				dlg.SetResponseAppearance("send", adw.ResponseDestructive)
				dlg.SetCloseResponse("cancel")
				dlg.ConnectResponse(func(r string) {
					if r == "send" {
						go2()
					}
				})
				dlg.Present(win)
			})
		}()
	}
	send.ConnectClicked(func() { trySend(time.Time{}) })
	if acc.Caps().CanSchedule() {
		send.SetPopover(a.presetPopover(sendPresets(), "Naplánovat odeslání", func() gtk.Widgetter { return win }, func(t time.Time) {
			if t.Before(time.Now().Add(2 * time.Minute)) {
				localToast("Naplánovat jde nejdřív za 2 minuty")
				return
			}
			trySend(t)
		}))
	}

	discardBtn.ConnectClicked(func() {
		dlg := adw.NewAlertDialog("Zahodit zprávu?", "Rozepsaný text i uložený koncept budou smazány.")
		dlg.AddResponse("cancel", "Zrušit")
		dlg.AddResponse("discard", "Zahodit")
		dlg.SetResponseAppearance("discard", adw.ResponseDestructive)
		dlg.SetCloseResponse("cancel")
		dlg.ConnectResponse(func(r string) {
			if r != "discard" {
				return
			}
			sent = true // nothing to save on close
			if d.ID != "" {
				id := d.ID
				go func() { _ = acc.DeleteDraft(a.ctx, id) }()
			}
			win.Close()
		})
		dlg.Present(win)
	})

	// Closing the window keeps the work as a draft.
	initialBody := getBody()
	win.ConnectCloseRequest(func() bool {
		if sent || busy {
			return false
		}
		if strings.TrimSpace(to.Text()) == "" && strings.TrimSpace(subject.Text()) == "" &&
			getBody() == initialBody && len(d.Attachments) == 0 {
			return false
		}
		if err := collect(); err != nil {
			// Invalid address: still keep the text, drop the recipient fields.
			d.To, d.CC, d.BCC = nil, nil, nil
			d.Subject, d.Body = subject.Text(), getBody()
		}
		go func() {
			err := acc.SaveDraft(context.Background(), d)
			ui(func() {
				if err != nil {
					a.toast("Koncept se nepodařilo uložit: " + err.Error())
					return
				}
				a.toast("Koncept uložen")
			})
		}()
		return false
	})

	// Files dropped on the window become attachments.
	drop := gtk.NewDropTarget(gdk.GTypeFileList, gdk.ActionCopy)
	drop.ConnectDrop(func(v *coreglib.Value, _, _ float64) bool {
		fl, ok := v.GoValue().(*gdk.FileList)
		if !ok {
			return false
		}
		for _, f := range fl.Files() {
			if out, err := readAttachment(f.Path()); err != nil {
				localToast(err.Error())
			} else {
				d.Attachments = append(d.Attachments, out)
			}
		}
		refreshAtts()
		return true
	})
	win.AddController(drop)

	// Window shortcuts.
	sc := gtk.NewShortcutController()
	sc.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("<Control>Return"),
		gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { trySend(time.Time{}); return true })))
	sc.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("<Control>s"),
		gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { saveBtn.Activate(); return true })))
	sc.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { win.Close(); return true })))
	win.AddController(sc)

	win.Present()
	switch {
	case d.ID == "" && len(d.To) == 0:
		to.GrabFocus()
	default:
		body.GrabFocus()
		buf := body.Buffer()
		buf.PlaceCursor(buf.StartIter())
	}
}

// addressCompletion returns a suggestion list shown under the address rows
// while typing; picking a contact completes the address being typed.
func (a *App) addressCompletion(rows []*adw.EntryRow) gtk.Widgetter {
	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list")
	list.SetSelectionMode(gtk.SelectionNone)
	rev := gtk.NewRevealer()
	rev.SetChild(list)
	rev.SetRevealChild(false)

	var active *adw.EntryRow
	var shown []protonmail.Contact
	var contacts []protonmail.Contact
	go func() {
		c := a.addressBook() // contacts + recent correspondents
		ui(func() { contacts = c })
	}()

	list.ConnectRowActivated(func(r *gtk.ListBoxRow) {
		i := r.Index()
		if active == nil || i < 0 || i >= len(shown) {
			return
		}
		text := active.Text()
		prefix := ""
		if j := strings.LastIndexByte(text, ','); j >= 0 {
			prefix = text[:j+1] + " "
		}
		addr := mailparse.DisplayAddress(&mail.Address{Name: shown[i].Name, Address: shown[i].Email})
		active.SetText(prefix + addr + ", ")
		active.GrabFocus()
		active.SetPosition(-1)
		rev.SetRevealChild(false)
	})

	for _, row := range rows {
		row := row
		row.ConnectChanged(func() {
			active = row
			text := row.Text()
			token := text
			if j := strings.LastIndexByte(text, ','); j >= 0 {
				token = text[j+1:]
			}
			token = strings.TrimSpace(token)
			clearListBox(list)
			shown = nil
			if len([]rune(token)) < 2 {
				rev.SetRevealChild(false)
				return
			}
			shown = matchContacts(contacts, token, 6)
			for _, c := range shown {
				ar := adw.NewActionRow()
				ar.SetTitle(orDefault(c.Name, c.Email))
				sub := c.Email
				if c.Recent {
					sub += " · nedávná korespondence"
				}
				ar.SetSubtitle(sub)
				ar.AddPrefix(adw.NewAvatar(28, orDefault(c.Name, c.Email), true))
				ar.SetActivatable(true)
				list.Append(ar)
			}
			rev.SetRevealChild(len(shown) > 0)
		})
	}
	return rev
}

// readAttachment loads a local file as an outgoing attachment.
func readAttachment(path string) (*protonmail.Outgoing, error) {
	if path == "" {
		return nil, fmt.Errorf("soubor nejde přiložit (není místní)")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s je složka – přiložit jde jen soubory", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if i := strings.IndexByte(mt, ';'); i > 0 {
		mt = mt[:i]
	}
	return &protonmail.Outgoing{Name: filepath.Base(path), MIMEType: mt, Data: data, Size: int64(len(data))}, nil
}

// syncKeyAttachment adds or removes the sender's public key attachment.
func (a *App) syncKeyAttachment(acc mailbox.Account, d *protonmail.Draft, want bool) {
	idx := -1
	for i, att := range d.Attachments {
		if att.PublicKey {
			idx = i
		}
	}
	switch {
	case want && idx >= 0 && d.Attachments[idx].KeyAddressID != d.FromAddressID:
		// Sender changed: replace with the key of the new address.
		a.syncKeyAttachment(acc, d, false)
		a.syncKeyAttachment(acc, d, true)
	case want && idx < 0:
		key, name, err := acc.PublicKeyAttachment(d.FromAddressID)
		if err != nil {
			return
		}
		d.Attachments = append(d.Attachments, &protonmail.Outgoing{
			Name: name, MIMEType: "application/pgp-keys", Data: []byte(key), Size: int64(len(key)),
			PublicKey: true, KeyAddressID: d.FromAddressID,
		})
	case !want && idx >= 0:
		if d.Attachments[idx].UploadedID != "" {
			d.Recreate = true
		}
		d.Attachments = append(d.Attachments[:idx], d.Attachments[idx+1:]...)
	}
}

func shortFP(fp string) string {
	if len(fp) > 16 {
		return "…" + fp[len(fp)-16:]
	}
	return fp
}
