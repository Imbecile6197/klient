package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// threadState is the conversation open in the reader.
type threadState struct {
	item  protonmail.Thread    // the list row (messages of the current folder)
	metas []protonmail.Summary // whole conversation, oldest first
	full  map[string]*protonmail.Message
	cards map[string]*messageCard
}

type messageCard struct {
	box      *gtk.Box
	revealer *gtk.Revealer
	detail   *gtk.Box
	preview  *gtk.Label
	loaded   bool
	loading  bool
}

func (m *mainView) threadsOn() bool {
	return m.a.cfg.Threads && m.folder.ID != protonmail.DraftsID
}

// visibleMsgs leaves out new mail still waiting for the spam filter.
func (m *mainView) visibleMsgs() []protonmail.Summary {
	var held map[string]bool
	if m.a.isUnified() {
		held = map[string]bool{}
		for _, s := range m.a.sessions {
			for id := range s.held() {
				held[m.a.unified.WrapID(s.acc, id)] = true
			}
		}
	} else if s := m.a.sessionOf(m.a.acc); s != nil {
		held = s.held()
	} else {
		return m.msgs
	}
	m.setChecking(len(held))
	if len(held) == 0 {
		return m.msgs
	}
	var out []protonmail.Summary
	for _, msg := range m.msgs {
		if !held[msg.ID] {
			out = append(out, msg)
		}
	}
	return out
}

// updateChecking refreshes the "checking" bar and hides newly held mail.
func (m *mainView) updateChecking() { m.rebuildList() }

func (m *mainView) setChecking(n int) {
	if m.checking == nil {
		return
	}
	m.checkLabel.SetText(fmt.Sprintf(i18n.N("The spam filter is checking %d new message…", "The spam filter is checking %d new messages…", n), n))
	m.checking.SetRevealChild(n > 0)
}

// rebuildList regroups the loaded messages and redraws the list.
func (m *mainView) rebuildList() {
	msgs := m.visibleMsgs()
	if m.threadsOn() {
		m.items = protonmail.GroupThreads(msgs)
		if m.unreadOnly {
			var keep []protonmail.Thread
			for _, t := range m.items {
				if t.Unread() {
					keep = append(keep, t)
				}
			}
			m.items = keep
		}
	} else {
		m.items = nil
		for _, s := range msgs {
			if !m.unreadOnly || bool(s.Unread) {
				m.items = append(m.items, protonmail.Thread{Latest: s, Messages: []protonmail.Summary{s}})
			}
		}
	}
	if m.emptyTitle != nil {
		m.emptyTitle.SetTitle(m.folder.Name)
		m.emptyTitle.SetDescription(fmt.Sprintf(i18n.N("%d conversation is loaded in this folder", "%d conversations are loaded in this folder", len(m.items)), len(m.items)))
	}
	clearListBox(m.msgList)
	m.countdowns = nil
	m.checks = nil
	m.selected = map[int]bool{}
	m.updateSelection()
	for _, t := range m.items {
		m.msgList.Append(m.threadRow(t))
	}
	if len(m.items) == 0 {
		m.listStack.SetVisibleChildName("empty")
	} else {
		m.listStack.SetVisibleChildName("list")
	}
	// Keep the open conversation selected after a refresh.
	if m.thread != nil {
		for i, t := range m.items {
			if t.Latest.ID == m.thread.item.Latest.ID ||
				(t.ConversationID != "" && t.ConversationID == m.thread.item.ConversationID) {
				m.msgList.SelectRow(m.msgList.RowAtIndex(i))
				m.thread.item = t
				break
			}
		}
	}
}

func (m *mainView) outgoingFolder() bool {
	return m.folder.ID == protonmail.SentID || m.folder.ID == protonmail.DraftsID
}

// threadRow renders one list row: participants, count, subject, date.
func (m *mainView) threadRow(t protonmail.Thread) gtk.Widgetter {
	s := t.Latest
	outer := gtk.NewBox(gtk.OrientationHorizontal, 10)
	outer.SetMarginTop(8)
	outer.SetMarginBottom(8)
	outer.SetMarginEnd(6)
	bar := gtk.NewBox(gtk.OrientationVertical, 0)
	if t.Unread() {
		bar.AddCSSClass("unread-bar")
	} else {
		bar.SetSizeRequest(4, -1)
	}
	box := gtk.NewBox(gtk.OrientationVertical, 3)
	box.SetHExpand(true)

	var names []string
	seen := map[string]bool{}
	for i := len(t.Messages) - 1; i >= 0; i-- { // oldest participant first
		msg := t.Messages[i]
		name := ""
		switch {
		case m.outgoingFolder() && len(msg.ToList) > 0:
			name = mailparse.DisplayName(msg.ToList[0])
		case msg.Sender != nil && m.a.acc.IsOwnAddress(msg.Sender.Address):
			name = i18n.T("Me")
		case msg.Sender != nil:
			name = mailparse.DisplayName(msg.Sender)
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	label := strings.Join(names, ", ")
	if label == "" {
		label = i18n.T("(no sender)")
	}
	if m.outgoingFolder() {
		label = i18n.T("To: ") + label
	}

	top := gtk.NewBox(gtk.OrientationHorizontal, 6)
	check := gtk.NewCheckButton()
	check.SetVisible(m.selecting)
	check.SetCanFocus(false)
	check.SetCanTarget(false) // the row click toggles it
	check.SetVAlign(gtk.AlignCenter)
	m.checks = append(m.checks, check)
	avatarName := strings.TrimPrefix(names0(names), i18n.T("To: "))
	avatar := adw.NewAvatar(36, avatarName, true)
	avatar.SetVAlign(gtk.AlignStart)
	outer.Append(bar)
	outer.Append(check)
	outer.Append(avatar)
	outer.Append(box)
	sender := gtk.NewLabel(label)
	sender.SetXAlign(0)
	sender.SetHExpand(true)
	sender.SetEllipsize(pango.EllipsizeEnd)
	if t.Unread() {
		sender.AddCSSClass("heading")
	}
	top.Append(sender)
	if len(t.Messages) > 1 {
		count := gtk.NewLabel(fmt.Sprintf("%d", len(t.Messages)))
		count.AddCSSClass("dim-label")
		count.AddCSSClass("caption-heading")
		count.SetTooltipText(fmt.Sprintf(i18n.N("%d message in the thread", "%d messages in the thread", len(t.Messages)), len(t.Messages)))
		top.Append(count)
	}
	if d, ok := m.a.filter.Decision(m.a.ownID(s.ID)); ok && d.Spam {
		icon := gtk.NewImageFromIconName("mail-mark-junk-symbolic")
		icon.SetTooltipText(i18n.T("AI spam filter: ") + categoryName(d.Verdict.Category))
		icon.AddCSSClass("warning")
		top.Append(icon)
	}

	if t.Starred() {
		top.Append(gtk.NewImageFromIconName("starred-symbolic"))
	}
	date := gtk.NewLabel(formatTime(s.Time))
	date.AddCSSClass("dim-label")
	date.AddCSSClass("caption")
	switch {
	case protonmail.IsScheduled(s):
		// Scheduled mail: Time is the delivery time; show a countdown.
		when := time.Unix(s.Time, 0)
		date.SetText(countdown(when))
		date.SetTooltipText(fmt.Sprintf(i18n.T("Will be sent %s"), formatWhen(when)))
		date.RemoveCSSClass("dim-label")
		date.AddCSSClass("accent")
		top.Append(gtk.NewImageFromIconName("appointment-soon-symbolic"))
		m.countdowns = append(m.countdowns, countdownLabel{date, when})
	case m.folder.ID == protonmail.SnoozedID:
		if until := m.a.acc.SnoozedUntil(t.ConversationID); !until.IsZero() {
			date.SetText(fmt.Sprintf(i18n.T("returns %s"), formatWhen(until)))
			date.RemoveCSSClass("dim-label")
			date.AddCSSClass("accent")
		}
	}
	top.Append(date)

	subject := gtk.NewLabel(orDefault(s.Subject, i18n.T("(no subject)")))
	subject.SetXAlign(0)
	subject.SetEllipsize(pango.EllipsizeEnd)
	if t.Unread() {
		subject.AddCSSClass("row-subject-unread")
	} else {
		subject.AddCSSClass("dim-label")
	}
	box.Append(top)
	box.Append(subject)
	if tag := m.accountTag(s.ID); tag != nil {
		box.Append(tag)
	}
	if n := attachmentCount(t); n > 0 {
		chip := gtk.NewBox(gtk.OrientationHorizontal, 4)
		chip.AddCSSClass("attach-chip")
		chip.SetHAlign(gtk.AlignStart)
		chip.Append(gtk.NewImageFromIconName("mail-attachment-symbolic"))
		chip.Append(gtk.NewLabel(attachmentsText(n)))
		box.Append(chip)
	}
	m.makeDraggable(outer, t)
	return outer
}

// attachmentsText is "1 attachment", "5 attachments".
func attachmentsText(n int) string {
	return fmt.Sprintf(i18n.N("%d attachment", "%d attachments", n), n)
}

func attachmentCount(t protonmail.Thread) int {
	n := 0
	for _, m := range t.Messages {
		n += m.NumAttachments
	}
	return n
}

func names0(names []string) string {
	if len(names) == 0 {
		return "?"
	}
	return names[len(names)-1] // the latest participant
}

// openThread shows a conversation: all its messages, the newest and the
// unread ones expanded.
func (m *mainView) openThread(t protonmail.Thread) {
	if len(t.Messages) == 1 && t.Latest.IsDraft() && !protonmail.IsScheduled(t.Latest) {
		m.a.openDraft(t.Latest.ID)
		return
	}
	m.loadSeq++
	seq := m.loadSeq
	m.readerStack.SetVisibleChildName("loading")
	m.setReaderActions(false)
	m.inner.SetShowContent(true)
	includeHidden := m.folder.ID == protonmail.TrashID || m.folder.ID == protonmail.SpamID
	useConv := m.threadsOn() && t.ConversationID != ""

	go func() {
		ctx, cancel := context.WithTimeout(m.a.ctx, 90*time.Second)
		defer cancel()
		var metas []protonmail.Summary
		var err error
		if useConv {
			metas, err = m.a.acc.ThreadMessages(ctx, t.ConversationID, includeHidden)
		}
		if !useConv || err != nil || len(metas) == 0 {
			metas = append([]protonmail.Summary{}, t.Messages...)
			sort.SliceStable(metas, func(i, j int) bool { return metas[i].Time < metas[j].Time })
			err = nil
		}
		// Decrypt the newest non-draft message and every unread one.
		expand := map[string]bool{}
		for i := len(metas) - 1; i >= 0; i-- {
			if !metas[i].IsDraft() {
				expand[metas[i].ID] = true
				break
			}
		}
		var unread []string
		for _, s := range metas {
			if bool(s.Unread) {
				expand[s.ID] = true
				unread = append(unread, s.ID)
			}
		}
		full := map[string]*protonmail.Message{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		var firstErr error
		for id := range expand {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				msg, e := m.a.acc.Get(ctx, id)
				mu.Lock()
				defer mu.Unlock()
				if e != nil {
					if firstErr == nil {
						firstErr = e
					}
					return
				}
				full[id] = msg
			}(id)
		}
		wg.Wait()
		if len(unread) > 0 {
			if m.a.acc.MarkRead(ctx, unread...) == nil {
				ui(m.updateCounts) // badges and the tray dot at once
			}
		}
		ui(func() {
			if seq != m.loadSeq {
				return
			}
			if len(full) == 0 {
				m.readerStack.SetVisibleChildName("empty")
				if firstErr != nil {
					m.a.toast(i18n.T("The message could not be opened: ") + firstErr.Error())
				}
				return
			}
			m.showThread(t, metas, full)
		})
	}()
}

func (m *mainView) showThread(t protonmail.Thread, metas []protonmail.Summary, full map[string]*protonmail.Message) {
	m.thread = &threadState{item: t, metas: metas, full: full, cards: map[string]*messageCard{}}
	m.current = nil
	for i := len(metas) - 1; i >= 0; i-- {
		if msg, ok := full[metas[i].ID]; ok && !metas[i].IsDraft() {
			m.current = msg
			break
		}
	}
	subject := t.Latest.Subject
	if m.current != nil {
		subject = m.current.Meta.Subject
	}
	m.readerPage.SetTitle(orDefault(subject, i18n.T("Message")))
	m.subject.SetText(orDefault(subject, i18n.T("(no subject)")))
	m.summaryCard.SetVisible(false)
	m.summaryBtn.SetSensitive(true)
	// A summary computed before (or in advance) shows up right away.
	if text, ok := cachedSummaryFor(m.a.acc, metas); ok {
		m.summaryLabel.SetText(text)
		m.summaryCard.SetVisible(true)
	}

	for {
		child := m.threadBox.FirstChild()
		if child == nil {
			break
		}
		m.threadBox.Remove(child)
	}
	for _, meta := range metas {
		card := m.messageCard(meta)
		m.thread.cards[meta.ID] = card
		m.threadBox.Append(card.box)
		if msg, ok := full[meta.ID]; ok {
			m.fillCard(card, msg)
			card.revealer.SetRevealChild(true)
		}
	}

	m.updateSpamUI()
	m.updateStar()
	if protonmail.IsScheduled(t.Latest) {
		m.showScheduledBanner(t.Latest)
	}
	m.setReaderActions(m.current != nil)
	m.readerStack.SetVisibleChildName("message")
}

// messageCard builds a collapsible card; the body is decrypted on first expand.
func (m *mainView) messageCard(meta protonmail.Summary) *messageCard {
	c := &messageCard{}
	c.box = gtk.NewBox(gtk.OrientationVertical, 0)
	c.box.AddCSSClass("card")
	c.box.AddCSSClass("message-card")
	if bool(meta.Unread) {
		c.box.AddCSSClass("unread")
	}

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.SetMarginTop(10)
	header.SetMarginBottom(10)
	header.SetMarginStart(12)
	header.SetMarginEnd(12)
	who := i18n.T("(no sender)")
	if meta.Sender != nil {
		who = mailparse.DisplayName(meta.Sender)
		if m.a.acc.IsOwnAddress(meta.Sender.Address) {
			who += i18n.T(" (me)")
		}
	}
	avatarName := who
	if meta.Sender != nil {
		avatarName = mailparse.DisplayName(meta.Sender)
	}
	header.Append(adw.NewAvatar(32, avatarName, true))
	name := gtk.NewLabel(who)
	name.AddCSSClass("heading")
	name.SetXAlign(0)
	name.SetEllipsize(pango.EllipsizeEnd)
	c.preview = gtk.NewLabel("")
	c.preview.AddCSSClass("dim-label")
	c.preview.SetXAlign(0)
	c.preview.SetHExpand(true)
	c.preview.SetEllipsize(pango.EllipsizeEnd)
	if meta.IsDraft() {
		c.preview.SetText(i18n.T("Draft – click to open"))
	} else if meta.NumAttachments > 0 {
		c.preview.SetText(attachmentsText(meta.NumAttachments))
	}
	date := gtk.NewLabel(time.Unix(meta.Time, 0).Format(i18n.T("Jan 2, 2006 15:04")))
	date.AddCSSClass("dim-label")
	date.AddCSSClass("caption")
	header.Append(name)
	header.Append(c.preview)
	header.Append(date)

	click := gtk.NewGestureClick()
	click.ConnectReleased(func(n int, x, y float64) {
		if meta.IsDraft() {
			m.a.openDraft(meta.ID)
			return
		}
		m.toggleCard(meta, c)
	})
	header.AddController(click)
	header.SetCursorFromName("pointer")

	c.detail = gtk.NewBox(gtk.OrientationVertical, 10)
	c.detail.SetMarginStart(12)
	c.detail.SetMarginEnd(12)
	c.detail.SetMarginBottom(12)
	c.revealer = gtk.NewRevealer()
	c.revealer.SetChild(c.detail)
	c.revealer.SetRevealChild(false)

	c.box.Append(header)
	c.box.Append(c.revealer)
	return c
}

func (m *mainView) toggleCard(meta protonmail.Summary, c *messageCard) {
	if c.revealer.RevealChild() {
		c.revealer.SetRevealChild(false)
		return
	}
	c.revealer.SetRevealChild(true)
	if c.loaded || c.loading {
		return
	}
	c.loading = true
	c.detail.Append(spinnerBox())
	th := m.thread
	go func() {
		msg, err := m.a.acc.Get(m.a.ctx, meta.ID)
		ui(func() {
			c.loading = false
			if m.thread != th {
				return
			}
			for {
				child := c.detail.FirstChild()
				if child == nil {
					break
				}
				c.detail.Remove(child)
			}
			if err != nil {
				l := gtk.NewLabel(i18n.T("The message could not be loaded: ") + err.Error())
				l.AddCSSClass("error")
				l.SetWrap(true)
				c.detail.Append(l)
				return
			}
			th.full[meta.ID] = msg
			m.fillCard(c, msg)
		})
	}()
}

// fillCard adds recipients, security info, body and attachments.
func (m *mainView) fillCard(c *messageCard, msg *protonmail.Message) {
	c.loaded = true
	label := func(text string, classes ...string) *gtk.Label {
		l := gtk.NewLabel(text)
		l.SetXAlign(0)
		l.SetWrap(true)
		l.SetWrapMode(pango.WrapWordChar)
		l.SetSelectable(true)
		for _, cl := range classes {
			l.AddCSSClass(cl)
		}
		return l
	}
	meta := msg.Meta
	if meta.Sender != nil {
		c.detail.Append(label(i18n.T("From: ")+mailparse.DisplayAddress(meta.Sender), "dim-label"))
	}
	var to []string
	for _, a := range meta.ToList {
		to = append(to, mailparse.DisplayAddress(a))
	}
	for _, a := range meta.CCList {
		to = append(to, mailparse.DisplayAddress(a)+i18n.T(" (cc)"))
	}
	if len(to) > 0 {
		c.detail.Append(label(i18n.T("To: ")+strings.Join(to, ", "), "dim-label"))
	}

	sec := gtk.NewBox(gtk.OrientationHorizontal, 6)
	secIcon := "channel-insecure-symbolic"
	if strings.HasPrefix(msg.Encryption, "End-to-end") {
		secIcon = "channel-secure-symbolic"
	}
	sec.Append(gtk.NewImageFromIconName(secIcon))
	sec.Append(label(msg.Encryption, "caption"))
	sec.Append(gtk.NewLabel("·"))
	sigIcon := "dialog-question-symbolic"
	sigText := msg.Signature.String()
	sigLabel := label(sigText, "caption")
	switch msg.Signature {
	case pgp.SigValid:
		sigIcon = "object-select-symbolic"
		if msg.SenderKeyFP != "" {
			sigLabel.SetText(fmt.Sprintf(i18n.T("%s (key %s)"), sigText, shortFP(msg.SenderKeyFP)))
		}
	case pgp.SigInvalid:
		sigIcon = "dialog-warning-symbolic"
		sigLabel.AddCSSClass("error")
	case pgp.SigOwn:
		sigIcon = "avatar-default-symbolic"
	}
	sec.Append(gtk.NewImageFromIconName(sigIcon))
	sec.Append(sigLabel)
	c.detail.Append(sec)
	if msg.Meta.Sender != nil && !m.a.acc.IsOwnAddress(msg.Meta.Sender.Address) {
		if n := m.a.senderKeyNotice(msg.Meta.Sender.Address, nil); n != nil {
			c.detail.Append(n)
		}
	}
	if unsub := m.unsubscribeButton(msg); unsub != nil {
		c.detail.Append(unsub)
	}

	if chips := m.attachmentChips(msg); chips != nil {
		c.detail.Append(chips)
	}
	m.inviteCards(msg, c.detail)
	c.detail.Append(m.messageBody(msg))

	if first := firstLine(msg.Text); first != "" && msg.Meta.NumAttachments == 0 {
		c.preview.SetText(first)
	}

	actions := gtk.NewBox(gtk.OrientationHorizontal, 6)
	actions.SetHAlign(gtk.AlignEnd)
	for _, a := range []struct {
		icon, tip string
		action    protonmail.ComposeAction
	}{
		{"mail-reply-sender-symbolic", i18n.T("Reply (This Message)"), protonmail.ActionReply},
		{"mail-reply-all-symbolic", i18n.T("Reply to All (This Message)"), protonmail.ActionReplyAll},
		{"mail-forward-symbolic", i18n.T("Forward (This Message)"), protonmail.ActionForward},
	} {
		a := a
		b := gtk.NewButtonFromIconName(a.icon)
		b.SetTooltipText(a.tip)
		b.AddCSSClass("flat")
		b.ConnectClicked(func() { m.a.openComposeFor(msg, a.action) })
		actions.Append(b)
	}
	actions.Append(m.messageMenu(msg))
	c.detail.Append(actions)
}

func plainBody(text string) *gtk.TextView {
	body := gtk.NewTextView()
	body.SetEditable(false)
	body.SetCursorVisible(false)
	body.SetWrapMode(gtk.WrapWordChar)
	body.AddCSSClass("inline")
	body.SetTopMargin(6)
	// A text view outside a scrolled window leaves a single wrapped line
	// blank (GTK sizes it before the width is known); a trailing newline
	// gives it a second line and it lays out like any longer message.
	if !strings.Contains(strings.TrimRight(text, "\n"), "\n") {
		text = strings.TrimRight(text, "\n") + "\n"
	}
	body.Buffer().SetText(text)
	return body
}

// messageBody shows HTML mail in a locked-down web view (remote content
// blocked until allowed) and plain-text mail as text.
func (m *mainView) messageBody(msg *protonmail.Message) gtk.Widgetter {
	if msg.HTML == "" {
		return plainBody(msg.Text)
	}
	sender := ""
	if msg.Meta.Sender != nil {
		sender = msg.Meta.Sender.Address
	}
	box := gtk.NewBox(gtk.OrientationVertical, 6)
	stack := gtk.NewStack()
	stack.SetVhomogeneous(false)
	stack.SetInterpolateSize(true)
	inline := map[string]string{}
	allow := m.a.remoteAllowed(sender)
	hasRemote := mailparse.HasRemoteContent(msg.HTML)

	var view gtk.Widgetter
	render := func() {
		if view != nil {
			stack.Remove(view)
		}
		view = newHTMLView(mailparse.PrepareHTML(msg.HTML, inline, allow), m.a.openLink)
		stack.AddNamed(view, "html")
		stack.SetVisibleChildName("html")
	}
	stack.AddNamed(plainBody(msg.Text), "text")

	// Bar offering remote content, like GNOME's other mail clients.
	bar := gtk.NewBox(gtk.OrientationHorizontal, 6)
	bar.AddCSSClass("remote-bar")
	info := gtk.NewLabel(i18n.T("Remote images are blocked for privacy."))
	info.SetXAlign(0)
	info.SetHExpand(true)
	info.SetWrap(true)
	info.AddCSSClass("caption")
	showOnce := gtk.NewButtonWithLabel(i18n.T("Show"))
	showOnce.AddCSSClass("flat")
	always := gtk.NewButtonWithLabel(i18n.T("Always From This Sender"))
	always.AddCSSClass("flat")
	bar.Append(gtk.NewImageFromIconName("dialog-information-symbolic"))
	bar.Append(info)
	bar.Append(showOnce)
	bar.Append(always)
	bar.SetVisible(hasRemote && !allow)
	showOnce.ConnectClicked(func() {
		allow = true
		bar.SetVisible(false)
		render()
	})
	always.ConnectClicked(func() {
		m.a.allowRemoteFor(sender)
		allow = true
		bar.SetVisible(false)
		render()
	})

	asText := gtk.NewToggleButton()
	asText.SetIconName("text-x-generic-symbolic")
	asText.SetTooltipText(i18n.T("Show as Plain Text"))
	asText.AddCSSClass("flat")
	asText.SetHAlign(gtk.AlignEnd)
	asText.ConnectToggled(func() {
		if asText.Active() {
			stack.SetVisibleChildName("text")
		} else {
			stack.SetVisibleChildName("html")
		}
	})

	box.Append(bar)
	box.Append(stack)
	box.Append(asText)
	render()

	// Inline images (cid:) are separate encrypted attachments; fetch and
	// re-render once they are decrypted.
	if strings.Contains(strings.ToLower(msg.HTML), "cid:") {
		go func() {
			imgs := m.a.acc.InlineImages(m.a.ctx, msg)
			if len(imgs) == 0 {
				return
			}
			ui(func() {
				inline = imgs
				render()
			})
		}()
	}
	return box
}

func firstLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, ">") {
			return l
		}
	}
	return ""
}

// attachmentChips shows the attachments at the top of a message as a row of
// chips: click opens the file, the arrow saves it.
func (m *mainView) attachmentChips(msg *protonmail.Message) gtk.Widgetter {
	var atts []protonmail.Attachment
	for _, att := range msg.Attachments {
		// Inline images of an HTML body are shown in the body itself.
		if att.ContentID != "" && strings.Contains(msg.HTML, att.ContentID) {
			continue
		}
		atts = append(atts, att)
	}
	if len(atts) == 0 {
		return nil
	}
	flow := gtk.NewFlowBox()
	flow.SetSelectionMode(gtk.SelectionNone)
	flow.SetMaxChildrenPerLine(8)
	flow.SetColumnSpacing(6)
	flow.SetRowSpacing(6)
	flow.SetHomogeneous(false)
	for _, att := range atts {
		att := att
		chip := gtk.NewBox(gtk.OrientationHorizontal, 0)
		chip.AddCSSClass("linked")
		open := gtk.NewButton()
		content := gtk.NewBox(gtk.OrientationHorizontal, 6)
		content.Append(gtk.NewImageFromIconName(attachmentIcon(att)))
		name := gtk.NewLabel(orDefault(att.Name, i18n.T("attachment")))
		name.SetEllipsize(pango.EllipsizeMiddle)
		name.SetMaxWidthChars(28)
		size := gtk.NewLabel(humanSize(att.Size))
		size.AddCSSClass("dim-label")
		size.AddCSSClass("caption")
		content.Append(name)
		content.Append(size)
		open.SetChild(content)
		open.SetTooltipText(fmt.Sprintf(i18n.T("Open %s"), orDefault(att.Name, i18n.T("the attachment"))))
		open.ConnectClicked(func() { m.openAttachment(att) })
		save := gtk.NewButtonFromIconName("document-save-symbolic")
		save.SetTooltipText(i18n.T("Save…"))
		save.ConnectClicked(func() { m.saveAttachment(att) })
		chip.Append(open)
		chip.Append(save)
		if isKeyAttachment(att) {
			chip.Append(m.importKeyButton(att))
		}
		flow.Append(chip)
	}
	return flow
}

func attachmentIcon(att protonmail.Attachment) string {
	mt := strings.ToLower(att.MIMEType)
	switch {
	case strings.HasPrefix(mt, "image/"):
		return "image-x-generic-symbolic"
	case mt == "application/pdf":
		return "x-office-document-symbolic"
	case strings.Contains(mt, "zip") || strings.Contains(mt, "compressed"):
		return "package-x-generic-symbolic"
	case strings.HasPrefix(mt, "text/calendar"):
		return "x-office-calendar-symbolic"
	case strings.HasPrefix(mt, "audio/"):
		return "audio-x-generic-symbolic"
	case strings.HasPrefix(mt, "video/"):
		return "video-x-generic-symbolic"
	}
	return "mail-attachment-symbolic"
}

// openAttachment decrypts the attachment to a private temporary folder and
// opens it in the default application.
func (m *mainView) openAttachment(att protonmail.Attachment) {
	acc := m.a.acc
	go func() {
		data, err := acc.AttachmentData(m.a.ctx, att)
		path := ""
		if err == nil {
			dir := filepath.Join(os.TempDir(), fmt.Sprintf("klient-%d", os.Getuid()))
			if err = os.MkdirAll(dir, 0o700); err == nil {
				path = filepath.Join(dir, safeFilename(orDefault(att.Name, "priloha")))
				err = os.WriteFile(path, data, 0o600)
			}
		}
		ui(func() {
			if err != nil {
				m.a.toast(i18n.T("The attachment cannot be opened: ") + err.Error())
				return
			}
			gtk.NewFileLauncher(gio.NewFileForPath(path)).Launch(context.Background(), m.a.gtkWindow(), nil)
		})
	}()
}

func (m *mainView) attachmentRow(att protonmail.Attachment) gtk.Widgetter {
	row := adw.NewActionRow()
	row.SetTitle(orDefault(att.Name, i18n.T("attachment")))
	row.SetSubtitle(fmt.Sprintf("%s · %s", att.MIMEType, humanSize(att.Size)))
	btn := gtk.NewButtonFromIconName("document-save-symbolic")
	btn.SetTooltipText(i18n.T("Save"))
	btn.SetVAlign(gtk.AlignCenter)
	btn.AddCSSClass("flat")
	btn.ConnectClicked(func() { m.saveAttachment(att) })
	if isKeyAttachment(att) {
		row.AddSuffix(m.importKeyButton(att))
	}
	row.AddSuffix(btn)
	return row
}

// threadIDs are the messages an action on the open conversation applies to:
// those of the conversation that are in the current folder.
func (m *mainView) threadIDs() []string {
	if m.thread == nil {
		return nil
	}
	return m.thread.item.IDs()
}

func (m *mainView) clearReader() {
	m.thread = nil
	m.current = nil
	m.banner.SetRevealed(false)
	m.readerStack.SetVisibleChildName("empty")
	m.setReaderActions(false)
	m.inner.SetShowContent(false)
}

// reply answers the newest message of the conversation.
func (m *mainView) reply(action protonmail.ComposeAction) {
	if m.current == nil {
		return
	}
	m.a.openComposeFor(m.current, action)
}

func (m *mainView) moveCurrent(folderID, done string) {
	ids := idsOf(m.targets())
	if len(ids) == 0 {
		return
	}
	m.leaveSelection()
	m.clearReader()
	go func() {
		err := m.a.acc.Move(m.a.ctx, folderID, ids...)
		ui(func() {
			if err != nil {
				m.a.toast(i18n.T("Moving failed: ") + err.Error())
				return
			}
			m.a.toast(done)
			m.scheduleRefresh()
		})
	}()
}

func (m *mainView) markUnread() {
	var ids []string
	if m.hasSelection() {
		ids = idsOf(m.targets())
	} else if m.current != nil {
		ids = []string{m.current.Meta.ID}
	}
	if len(ids) == 0 {
		return
	}
	m.leaveSelection()
	m.clearReader()
	go func() {
		err := m.a.acc.MarkUnread(m.a.ctx, ids...)
		ui(func() {
			if err != nil {
				m.a.toast(i18n.T("Marking failed: ") + err.Error())
				return
			}
			m.scheduleRefresh()
		})
	}()
}

func (m *mainView) threadHasLabel(labelID string) bool {
	if m.thread == nil {
		return false
	}
	return threadHas(m.thread.item, labelID)
}

func threadHas(t protonmail.Thread, labelID string) bool {
	for _, s := range t.Messages {
		if hasLabel(s, labelID) {
			return true
		}
	}
	return false
}

func (m *mainView) updateStar() {
	if m.threadHasLabel(protonmail.StarredID) {
		m.starBtn.SetIconName("starred-symbolic")
		m.starBtn.SetTooltipText(i18n.T("Remove Star (Ctrl+D)"))
	} else {
		m.starBtn.SetIconName("non-starred-symbolic")
		m.starBtn.SetTooltipText(i18n.T("Add Star (Ctrl+D)"))
	}
}

// toggleLabel adds or removes a label (or the star) on the conversation.
func (m *mainView) toggleLabel(labelID string) {
	targets := m.targets()
	ids := idsOf(targets)
	if len(ids) == 0 {
		return
	}
	// Add unless every target already has it.
	on := false
	for _, t := range targets {
		if !threadHas(t, labelID) {
			on = true
		}
	}
	if m.hasSelection() {
		m.leaveSelection()
		go func() {
			err := m.a.acc.SetLabel(m.a.ctx, labelID, on, ids...)
			ui(func() {
				if err != nil {
					m.a.toast(i18n.T("Changing the label failed: ") + err.Error())
				}
				m.scheduleRefresh()
			})
		}()
		return
	}
	// Update local state so the star button reacts immediately.
	for i := range m.thread.item.Messages {
		s := &m.thread.item.Messages[i]
		if on {
			s.LabelIDs = append(s.LabelIDs, labelID)
		} else {
			var keep []string
			for _, l := range s.LabelIDs {
				if l != labelID {
					keep = append(keep, l)
				}
			}
			s.LabelIDs = keep
		}
	}
	m.updateStar()
	go func() {
		err := m.a.acc.SetLabel(m.a.ctx, labelID, on, ids...)
		ui(func() {
			if err != nil {
				m.a.toast(i18n.T("Changing the label failed: ") + err.Error())
				return
			}
			if labelID != protonmail.StarredID {
				name := labelID
				for _, l := range m.labels {
					if l.ID == labelID {
						name = l.Name
					}
				}
				if on {
					m.a.toast(fmt.Sprintf(i18n.T("Label %s added"), name))
				} else {
					m.a.toast(fmt.Sprintf(i18n.T("Label %s removed"), name))
				}
			}
			m.scheduleRefresh()
		})
	}()
}

func (m *mainView) toggleStar() { m.toggleLabel(protonmail.StarredID) }

// latestIncomingSender is the sender used for spam feedback.
func (m *mainView) latestIncomingSender() string {
	if m.thread == nil {
		return ""
	}
	for i := len(m.thread.metas) - 1; i >= 0; i-- {
		s := m.thread.metas[i].Sender
		if s != nil && !m.a.acc.IsOwnAddress(s.Address) {
			return s.Address
		}
	}
	return ""
}

func (m *mainView) toggleSpam() {
	if m.hasSelection() {
		inSpam := m.folder.ID == protonmail.SpamID
		for _, t := range m.targets() {
			from := ""
			if t.Latest.Sender != nil {
				from = t.Latest.Sender.Address
			}
			if inSpam {
				m.a.markNotSpam(from, t.IDs()...)
			} else {
				m.a.markSpam(from, t.IDs()...)
			}
		}
		m.leaveSelection()
		m.clearReader()
		return
	}
	ids := m.threadIDs()
	if len(ids) == 0 {
		return
	}
	from := m.latestIncomingSender()
	inSpam := m.threadHasLabel(protonmail.SpamID)
	if inSpam {
		m.a.markNotSpam(from, ids...)
	} else {
		m.a.markSpam(from, ids...)
	}
	m.clearReader()
}

func (m *mainView) updateSpamUI() {
	if m.threadHasLabel(protonmail.SpamID) {
		m.spamBtn.SetIconName("mail-mark-notjunk-symbolic")
		m.spamBtn.SetTooltipText(i18n.T("Not Spam"))
	} else {
		m.spamBtn.SetIconName("mail-mark-junk-symbolic")
		m.spamBtn.SetTooltipText(i18n.T("Mark as Spam"))
	}
	if m.current == nil {
		m.banner.SetRevealed(false)
		return
	}
	msg := m.current
	from := ""
	if msg.Meta.Sender != nil {
		from = msg.Meta.Sender.Address
	}
	d, ok := m.a.filter.Decision(m.a.ownID(msg.Meta.ID))
	if !ok || d.Source == "none" {
		m.banner.SetRevealed(false)
		return
	}
	var text string
	switch d.Source {
	case "ai":
		text = fmt.Sprintf(i18n.T("AI spam filter: %s (%.0f %%) – %s"), categoryName(d.Verdict.Category), d.Verdict.SpamProbability*100, d.Verdict.Reason)
	case "dangerous-link":
		text = fmt.Sprintf(i18n.T("Spam filter: a link leads to a known phishing or malware page – %s"), strings.Join(d.Hits, "; "))
	case "user":
		if d.Spam {
			text = i18n.T("You marked this as spam")
		} else {
			text = i18n.T("You marked this as legitimate")
		}
	default:
		text = fmt.Sprintf(i18n.T("Spam filter (%s): %s"), d.Source, strings.Join(d.Hits, "; "))
	}
	m.banner.SetTitle(text)
	if d.Spam && d.Source != "user" {
		m.banner.SetButtonLabel(i18n.T("Not Spam"))
		m.bannerFn = func() {
			m.a.markNotSpam(from, msg.Meta.ID)
			m.banner.SetRevealed(false)
		}
	} else {
		m.banner.SetButtonLabel("")
		m.bannerFn = nil
	}
	m.banner.SetRevealed(true)
}

// summarize summarises the whole conversation (all its messages).
func (m *mainView) summarize() {
	if m.thread == nil {
		return
	}
	if !m.a.ai.HasAssistant() {
		m.a.toastWithAction(i18n.T("The AI assistant is not set up"), i18n.T("Set Up"), m.a.openPreferences)
		return
	}
	th := m.thread
	m.summaryBtn.SetSensitive(false)
	m.summaryLabel.SetText(i18n.T("Preparing the summary…"))
	m.summaryCard.SetVisible(true)
	metas := th.metas
	have := map[string]*protonmail.Message{}
	for k, v := range th.full {
		have[k] = v
	}
	acc := m.a.acc
	go func() {
		out, err := m.a.summarizeThread(m.a.ctx, acc, metas, have)
		ui(func() {
			if m.thread != th {
				return
			}
			m.summaryBtn.SetSensitive(true)
			if err != nil {
				m.summaryLabel.SetText(i18n.T("The summary failed: ") + ai.Explain(m.a.cfg.AssistantProvider, err).Text)
				return
			}
			m.summaryLabel.SetText(out)
		})
	}()
}
