// Package demo is a made-up mail account for screenshots and trying Klient
// out (KLIENT_DEMO=1). Every person and address in it is fictional; the
// domains are the reserved example.com/.org/.net.
package demo

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"time"

	proton "github.com/ProtonMail/go-proton-api"

	"github.com/Imbecile6197/klient/internal/cache"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// Account implements mailbox.Account over the made-up messages.
type Account struct {
	mu     sync.Mutex
	me     *mail.Address
	msgs   []*protonmail.Message
	labels []protonmail.UserLabel
	user   string // "demo", or "demo-2" for the second account
	kind   mailbox.Kind
}

// Offline makes sending fail as if there were no connection (to try the
// Outbox: KLIENT_DEMO_OFFLINE=1).
var Offline bool

type offlineError struct{}

func (offlineError) Error() string   { return "dial tcp: connect: network is unreachable" }
func (offlineError) Timeout() bool   { return false }
func (offlineError) Temporary() bool { return true }

var _ mailbox.Account = (*Account)(nil)

// Label IDs of the demo account.
const (
	LabelWork   = "demo-work"
	LabelFamily = "demo-family"
	FolderBills = "demo-bills"
)

func addr(name, email string) *mail.Address { return &mail.Address{Name: name, Address: email} }

// New builds the account in the interface language.
func New() *Account {
	cs := i18n.Lang() == "cs"
	pick := func(en, czech string) string {
		if cs {
			return czech
		}
		return en
	}
	a := &Account{me: addr(pick("Alex Carter", "Petra Svobodová"), pick("alex@example.com", "petra@example.com")), user: "demo", kind: mailbox.KindProton}
	a.labels = []protonmail.UserLabel{
		{ID: LabelWork, Name: pick("Work", "Práce"), Color: "#1c71d8"},
		{ID: LabelFamily, Name: pick("Family", "Rodina"), Color: "#2ec27e"},
		{ID: FolderBills, Name: pick("Bills", "Faktury"), Color: "#e5a50a", Folder: true},
	}
	emma := addr(pick("Emma Walker", "Jana Nováková"), pick("emma.walker@example.org", "jana.novakova@example.org"))
	tom := addr(pick("Tom Harris", "Tomáš Dvořák"), pick("tom@example.org", "tomas@example.org"))
	mia := addr(pick("Mia Carter", "Eva Svobodová"), pick("mia@example.net", "eva@example.net"))
	shop := addr(pick("Northwind Energy", "Energie Sever"), "billing@example.net")
	news := addr("GNOME Weekly", "news@example.org")
	spammer := addr(pick("Prize Center", "Centrum výher"), "winner@example.net")

	now := time.Now()
	day := func(d, h, m int) int64 {
		y, mo, dd := now.AddDate(0, 0, -d).Date()
		return time.Date(y, mo, dd, h, m, 0, 0, time.Local).Unix()
	}
	n := 0
	add := func(conv string, from *mail.Address, to []*mail.Address, subject, text string, t int64, unread bool, labels []string, atts ...protonmail.Attachment) {
		n++
		meta := protonmail.Summary{
			ID: fmt.Sprintf("demo-%d", n), ConversationID: conv, AddressID: "demo-address",
			Subject: subject, Sender: from, ToList: to, Time: t,
			Unread: proton.Bool(unread), LabelIDs: append([]string{protonmail.AllMailID}, labels...),
			NumAttachments: len(atts), Size: len(text) + 2048,
		}
		if from.Address == a.me.Address {
			meta.Flags = proton.MessageFlagSent
		} else {
			meta.Flags = proton.MessageFlagReceived
		}
		a.msgs = append(a.msgs, &protonmail.Message{
			Meta: meta, Text: text, Attachments: atts,
			Encryption: i18n.T("End-to-end encrypted (Proton)"),
			Signature:  pgp.SigValid,
		})
	}
	me := []*mail.Address{a.me}
	inbox := []string{protonmail.InboxID}

	// A thread with the latest message unread.
	add("c-kickoff", emma, me, pick("Project kickoff – agenda", "Zahájení projektu – program"),
		pick("Hi Alex,\n\nthanks for joining the kickoff on Monday. Here is the agenda for next week:\n\n1. Goals for the first release\n2. Timeline and milestones\n3. Who does what\n\nCould you prepare a short overview of the design so far? Ten minutes is plenty.\n\nBest,\nEmma",
			"Ahoj Petro,\n\ndíky, že ses v pondělí připojila k zahájení. Posílám program na příští týden:\n\n1. Cíle prvního vydání\n2. Harmonogram a milníky\n3. Kdo co dělá\n\nMohla bys připravit krátký přehled dosavadního návrhu? Deset minut bohatě stačí.\n\nDíky,\nJana"),
		day(4, 9, 12), false, append(inbox, LabelWork))
	add("c-kickoff", a.me, []*mail.Address{emma}, pick("Re: Project kickoff – agenda", "Re: Zahájení projektu – program"),
		pick("Sure, I will bring the overview. Should I also cover the open questions?\n\nAlex", "Jasně, přehled připravím. Mám zmínit i otevřené otázky?\n\nPetra"),
		day(3, 14, 40), false, []string{protonmail.SentID, LabelWork})
	add("c-kickoff", emma, me, pick("Re: Project kickoff – agenda", "Re: Zahájení projektu – program"),
		pick("Yes please – the open questions are exactly what we should agree on first. I have attached the draft plan so you can check the dates.\n\nSee you Monday!\nEmma",
			"Ano, prosím – právě na otevřených otázkách bychom se měli shodnout nejdřív. Přikládám návrh plánu, ať si můžeš zkontrolovat termíny.\n\nUvidíme se v pondělí!\nJana"),
		day(0, 8, 47), true, append(inbox, LabelWork),
		protonmail.InlineAttachment(pick("project-plan.pdf", "plan-projektu.pdf"), "application/pdf", "", make([]byte, 184320)))

	// A calendar invitation.
	start := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local).AddDate(0, 0, 3)
	add("c-review", tom, me, pick("Invitation: Design review", "Pozvánka: Kontrola návrhu"),
		pick("Tom Harris invites you to “Design review”.", "Tomáš Dvořák vás zve na „Kontrola návrhu“."),
		day(0, 7, 55), true, inbox,
		protonmail.InlineAttachment("invite.ics", "text/calendar", "", invite(start, pick("Design review", "Kontrola návrhu"), pick("Meeting room 2", "Zasedačka 2"), tom, a.me)))

	add("c-photos", mia, me, pick("Photos from the weekend", "Fotky z víkendu"),
		pick("Here are the photos from Saturday – the one by the lake came out great!\n\nMia", "Posílám fotky ze soboty – ta u jezera se moc povedla!\n\nEva"),
		day(1, 19, 20), false, append(inbox, LabelFamily, protonmail.StarredID),
		protonmail.InlineAttachment("lake.jpg", "image/jpeg", "", make([]byte, 2_300_000)),
		protonmail.InlineAttachment("picnic.jpg", "image/jpeg", "", make([]byte, 1_900_000)))

	add("c-invoice", shop, me, pick("Your invoice for September", "Vaše faktura za září"),
		pick("Hello,\n\nyour invoice for September is ready. The amount of $42.10 will be paid by direct debit on the 15th.\n\nNorthwind Energy", "Dobrý den,\n\nvaše faktura za září je připravená. Částka 1 042 Kč bude stržena inkasem 15. dne v měsíci.\n\nEnergie Sever"),
		day(2, 6, 30), false, inbox,
		protonmail.InlineAttachment(pick("invoice-2026-09.pdf", "faktura-2026-09.pdf"), "application/pdf", "", make([]byte, 96000)))

	add("c-lunch", tom, me, pick("Lunch on Friday?", "Oběd v pátek?"),
		pick("Shall we try the new place around the corner on Friday at noon?\n\nTom", "Nezkusíme v pátek v poledne to nové bistro za rohem?\n\nTomáš"),
		day(2, 11, 5), false, inbox)

	add("c-news", news, me, pick("This week: five apps worth trying", "Tento týden: pět aplikací, které stojí za to"),
		pick("A short tour of new and updated apps from the GNOME ecosystem, plus tips for keeping your inbox tidy.", "Krátká prohlídka nových a aktualizovaných aplikací pro GNOME a tipy, jak udržet pořádek v poště."),
		day(3, 7, 0), false, inbox)

	add("c-spam", spammer, me, pick("Congratulations!!! You have won a prize", "Gratulujeme!!! Vyhráli jste cenu"),
		pick("Click here within 24 hours to claim your reward. Enter your card details to pay the small handling fee.", "Klikněte do 24 hodin a vyzvedněte si výhru. Zadejte údaje karty kvůli malému manipulačnímu poplatku."),
		day(0, 3, 14), true, []string{protonmail.SpamID})

	for _, m := range a.msgs {
		if m.Meta.Sender.Address != a.me.Address {
			m.Meta.Flags |= proton.MessageFlagDMARCPass
		}
	}
	return a
}

// NewSecond is a second, smaller account (KLIENT_DEMO=2), to show all
// accounts together.
func NewSecond() *Account {
	cs := i18n.Lang() == "cs"
	pick := func(en, czech string) string {
		if cs {
			return czech
		}
		return en
	}
	a := &Account{me: addr(pick("Alex Carter", "Petra Svobodová"), pick("alex.carter@example.net", "petra.svobodova@example.net")), user: "demo-2", kind: mailbox.KindSeznam}
	now := time.Now()
	at := func(d, h, m int) int64 {
		y, mo, dd := now.AddDate(0, 0, -d).Date()
		return time.Date(y, mo, dd, h, m, 0, 0, time.Local).Unix()
	}
	add := func(id string, from *mail.Address, subject, text string, t int64, unread bool) {
		a.msgs = append(a.msgs, &protonmail.Message{
			Meta: protonmail.Summary{
				ID: id, ConversationID: "c-" + id, AddressID: "demo2-address", Subject: subject, Sender: from,
				ToList: []*mail.Address{a.me}, Time: t, Unread: proton.Bool(unread), Size: len(text) + 1024,
				LabelIDs: []string{protonmail.AllMailID, protonmail.InboxID}, Flags: proton.MessageFlagReceived | proton.MessageFlagDMARCPass,
			},
			Text: text, Encryption: i18n.T("Not end-to-end encrypted – the provider stores the message readably (TLS protects it in transit)"),
		})
	}
	add("demo2-1", addr(pick("City Library", "Městská knihovna"), "library@example.org"), pick("Your book is ready for pickup", "Kniha je připravená k vyzvednutí"),
		pick("The book you reserved is waiting for you at the front desk until Friday.", "Rezervovaná kniha na vás čeká u pultu do pátku."), at(0, 10, 30), true)
	add("demo2-2", addr(pick("Running Club", "Běžecký klub"), "club@example.org"), pick("Saturday run: new route", "Sobotní běh: nová trasa"),
		pick("This Saturday we start at the park gate at 8:00 and run along the river.", "V sobotu startujeme v 8:00 u brány parku a poběžíme podél řeky."), at(1, 18, 5), false)
	add("demo2-3", addr(pick("Mia Carter", "Eva Svobodová"), pick("mia@example.net", "eva@example.net")), pick("Dinner on Sunday?", "Večeře v neděli?"),
		pick("Would you like to come over for dinner on Sunday? Bring nothing but a good mood.", "Nepřijdeš v neděli na večeři? Stačí dobrá nálada."), at(2, 20, 15), false)
	return a
}

// invite is a small iCalendar invitation.
func invite(start time.Time, summary, location string, org, me *mail.Address) []byte {
	f := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	return []byte(strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Klient//Demo//EN", "METHOD:REQUEST",
		"BEGIN:VEVENT", "UID:demo-review@example.org",
		"DTSTAMP:" + f(time.Now()), "DTSTART:" + f(start), "DTEND:" + f(start.Add(time.Hour)),
		"SUMMARY:" + summary, "LOCATION:" + location,
		fmt.Sprintf("ORGANIZER;CN=%s:mailto:%s", org.Name, org.Address),
		fmt.Sprintf("ATTENDEE;CN=%s;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:%s", me.Name, me.Address),
		"END:VEVENT", "END:VCALENDAR", "",
	}, "\r\n"))
}

// SpamID is the made-up spam message, for the demo filter decision.
func (a *Account) SpamID() string {
	for _, m := range a.msgs {
		if slices.Contains(m.Meta.LabelIDs, protonmail.SpamID) {
			return m.Meta.ID
		}
	}
	return ""
}

func (a *Account) Kind() mailbox.Kind { return a.kind }
func (a *Account) Caps() mailbox.Caps {
	return mailbox.Caps{Labels: true, ServerSnooze: true, ServerSchedule: true, AutoReply: true, E2E: true, Contacts: true}
}
func (a *Account) HasFolder(string) bool { return true }
func (a *Account) Username() string      { return a.user }
func (a *Account) UserID() string        { return a.user }
func (a *Account) Email() string         { return a.me.Address }
func (a *Account) DisplayName() string   { return a.me.Name }
func (a *Account) SendAddresses() []proton.Address {
	return []proton.Address{{ID: a.user + "-address", Email: a.me.Address, DisplayName: a.me.Name, Send: true, Receive: true}}
}
func (a *Account) IsOwnAddress(s string) bool { return strings.EqualFold(s, a.me.Address) }

func (a *Account) OnDeauth(func())                   {}
func (a *Account) StartedOffline() bool              { return false }
func (a *Account) Logout(context.Context) error      { return nil }
func (a *Account) Close()                            {}
func (a *Account) Cache() *cache.Cache               { return nil }
func (a *Account) RefreshUser(context.Context) error { return nil }

func (a *Account) inFolder(folderID string) []protonmail.Summary {
	var out []protonmail.Summary
	for _, m := range a.msgs {
		if slices.Contains(m.Meta.LabelIDs, folderID) {
			out = append(out, m.Meta)
		}
	}
	slices.SortFunc(out, func(x, y protonmail.Summary) int { return int(y.Time - x.Time) })
	return out
}

func (a *Account) List(_ context.Context, folderID string, page, pageSize int) ([]protonmail.Summary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if page > 0 {
		return nil, nil
	}
	return a.inFolder(folderID), nil
}

func (a *Account) ThreadMessages(_ context.Context, conv string, _ bool) ([]protonmail.Summary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []protonmail.Summary
	for _, m := range a.msgs {
		if m.Meta.ConversationID == conv {
			out = append(out, m.Meta)
		}
	}
	slices.SortFunc(out, func(x, y protonmail.Summary) int { return int(y.Time - x.Time) })
	return out, nil
}

func (a *Account) find(id string) *protonmail.Message {
	for _, m := range a.msgs {
		if m.Meta.ID == id {
			return m
		}
	}
	return nil
}

func (a *Account) Get(_ context.Context, id string) (*protonmail.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.find(id)
	if m == nil {
		return nil, fmt.Errorf("demo: no message %s", id)
	}
	c := *m
	return &c, nil
}

func (a *Account) AttachmentData(_ context.Context, att protonmail.Attachment) ([]byte, error) {
	return att.InlineData(), nil
}
func (a *Account) InlineImages(context.Context, *protonmail.Message) map[string]string { return nil }
func (a *Account) Search(_ context.Context, folderID, q string, _ int) ([]protonmail.Summary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []protonmail.Summary
	for _, s := range a.inFolder(folderID) {
		if strings.Contains(strings.ToLower(s.Subject), strings.ToLower(q)) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (a *Account) ExportEML(context.Context, string) ([]byte, error) {
	return nil, mailbox.ErrUnsupported
}
func (a *Account) Events(ctx context.Context, _ func(protonmail.Summary), _ func()) error {
	<-ctx.Done()
	return nil
}
func (a *Account) SyncOffline(context.Context, int, func(int, int)) error { return nil }

func (a *Account) UserLabels(context.Context) ([]protonmail.UserLabel, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]protonmail.UserLabel{}, a.labels...), nil
}

// Folders of the demo account change only in memory.
func (a *Account) CreateFolder(_ context.Context, name string, label bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	l := protonmail.UserLabel{ID: fmt.Sprintf("demo-new-%d", len(a.labels)), Name: name, Folder: !label}
	if label {
		l.Color = "#1DA583"
	}
	a.labels = append(a.labels, l)
	return nil
}

func (a *Account) RenameFolder(_ context.Context, id, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.labels {
		if a.labels[i].ID == id {
			a.labels[i].Name = name
		}
	}
	return nil
}

func (a *Account) DeleteFolder(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.labels = slices.DeleteFunc(a.labels, func(l protonmail.UserLabel) bool { return l.ID == id })
	return nil
}

func (a *Account) EmptyFolder(_ context.Context, folderID string, olderThan time.Time) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	a.msgs = slices.DeleteFunc(a.msgs, func(m *protonmail.Message) bool {
		del := slices.Contains(m.Meta.LabelIDs, folderID) && (olderThan.IsZero() || time.Unix(m.Meta.Time, 0).Before(olderThan))
		if del {
			n++
		}
		return del
	})
	return n, nil
}

func (a *Account) setUnread(on bool, ids []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		if m := a.find(id); m != nil {
			m.Meta.Unread = proton.Bool(on)
		}
	}
}
func (a *Account) MarkRead(_ context.Context, ids ...string) error {
	a.setUnread(false, ids)
	return nil
}
func (a *Account) MarkUnread(_ context.Context, ids ...string) error {
	a.setUnread(true, ids)
	return nil
}
func (a *Account) Move(_ context.Context, folderID string, ids ...string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	system := []string{protonmail.InboxID, protonmail.ArchiveID, protonmail.SpamID, protonmail.TrashID, FolderBills}
	for _, id := range ids {
		if m := a.find(id); m != nil {
			m.Meta.LabelIDs = slices.DeleteFunc(m.Meta.LabelIDs, func(l string) bool { return slices.Contains(system, l) })
			m.Meta.LabelIDs = append(m.Meta.LabelIDs, folderID)
		}
	}
	return nil
}
func (a *Account) SetLabel(_ context.Context, labelID string, on bool, ids ...string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		if m := a.find(id); m != nil {
			m.Meta.LabelIDs = slices.DeleteFunc(m.Meta.LabelIDs, func(l string) bool { return l == labelID })
			if on {
				m.Meta.LabelIDs = append(m.Meta.LabelIDs, labelID)
			}
		}
	}
	return nil
}
func (a *Account) UnreadCounts(context.Context) (map[string]int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]int{}
	for _, m := range a.msgs {
		if bool(m.Meta.Unread) {
			for _, l := range m.Meta.LabelIDs {
				out[l]++
			}
		}
	}
	return out, nil
}
func (a *Account) Storage() protonmail.StorageInfo {
	return protonmail.StorageInfo{Used: 1_288_490_188, Max: 16_106_127_360, Mail: 1_073_741_824, Drive: 214_748_364, Updated: time.Now()}
}

func (a *Account) SaveDraft(context.Context, *protonmail.Draft) error { return nil }
func (a *Account) Send(context.Context, *protonmail.Draft) error {
	if Offline {
		return &net.OpError{Op: "dial", Net: "tcp", Err: offlineError{}}
	}
	return nil
}
func (a *Account) DeleteDraft(context.Context, string) error { return nil }
func (a *Account) OpenDraft(context.Context, string) (*protonmail.Draft, *protonmail.Message, error) {
	return nil, nil, mailbox.ErrUnsupported
}
func (a *Account) ForwardAttachments(context.Context, *protonmail.Message) ([]*protonmail.Outgoing, error) {
	return nil, nil
}
func (a *Account) PlanEncryption(_ context.Context, addrs []*mail.Address) []protonmail.RecipientMode {
	var out []protonmail.RecipientMode
	for _, ad := range addrs {
		out = append(out, protonmail.RecipientMode{Address: ad.Address, Scheme: "proton"})
	}
	return out
}
func (a *Account) OwnPublicKey(string) (string, error) { return "", mailbox.ErrUnsupported }
func (a *Account) PublicKeyAttachment(string) (string, string, error) {
	return "", "", mailbox.ErrUnsupported
}

func (a *Account) Contacts(context.Context) ([]protonmail.Contact, error) {
	var out []protonmail.Contact
	seen := map[string]bool{}
	for _, m := range a.msgs {
		if s := m.Meta.Sender; s != nil && !seen[s.Address] && s.Address != a.me.Address {
			seen[s.Address] = true
			out = append(out, protonmail.Contact{Name: s.Name, Email: s.Address})
		}
	}
	return out, nil
}
func (a *Account) InvalidateContacts()                              {}
func (a *Account) RecentAddresses(int) []protonmail.Contact         { return nil }
func (a *Account) AddContact(context.Context, string, string) error { return nil }
func (a *Account) DeleteContact(context.Context, string) error      { return nil }

func (a *Account) CancelScheduled(context.Context, string) error     { return nil }
func (a *Account) Snooze(context.Context, []string, time.Time) error { return nil }
func (a *Account) Unsnooze(context.Context, []string) error          { return nil }
func (a *Account) SnoozedUntil(string) time.Time                     { return time.Time{} }
func (a *Account) AutoReply(context.Context) (protonmail.AutoReply, error) {
	return protonmail.AutoReply{}, nil
}
func (a *Account) SetAutoReply(context.Context, protonmail.AutoReply) error { return nil }

// Verdict is the made-up spam filter decision about a message.
type Verdict struct {
	ID, From, Subject string
	Spam              bool
	Probability       float64
	Category, Reason  string
}

// Verdicts are the AI decisions shown in the demo (reasons in the
// interface language, as the real filter writes them).
func (a *Account) Verdicts() []Verdict {
	cs := i18n.Lang() == "cs"
	var out []Verdict
	for _, m := range a.msgs {
		s := m.Meta
		if s.Sender == nil || s.Sender.Address == a.me.Address {
			continue
		}
		v := Verdict{ID: s.ID, From: s.Sender.Address, Subject: s.Subject, Probability: 0.03, Category: "ham",
			Reason: "A known contact in an ongoing conversation; DMARC passed."}
		if cs {
			v.Reason = "Známý kontakt v probíhající konverzaci; DMARC v pořádku."
		}
		if slices.Contains(s.LabelIDs, protonmail.SpamID) {
			v.Spam, v.Probability, v.Category = true, 0.97, "scam"
			v.Reason = "Promises a prize and asks for card details; the sender is unknown and pushes for a quick reaction."
			if cs {
				v.Reason = "Slibuje výhru a žádá údaje o kartě; odesílatel je neznámý a tlačí na rychlou reakci."
			}
		}
		out = append(out, v)
	}
	return out
}
