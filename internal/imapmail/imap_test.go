package imapmail

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/mail"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/zalando/go-keyring"

	"github.com/Imbecile6197/klient/internal/cache"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- test servers ---------------------------------------------------------------

func selfSigned(t *testing.T) (*tls.Config, *x509.CertPool) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, pool
}

type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return l.Reader.Size() }

func appendMsg(t *testing.T, u *imapmemserver.User, mbox, raw string) {
	t.Helper()
	if _, err := u.Append(mbox, literal{bytes.NewReader([]byte(raw))}, &imap.AppendOptions{Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

type smtpBackend struct {
	mu   sync.Mutex
	mail []string
	rcpt []string
}

type smtpSession struct{ be *smtpBackend }

func (b *smtpBackend) NewSession(*smtp.Conn) (smtp.Session, error) { return &smtpSession{b}, nil }
func (s *smtpSession) AuthMechanisms() []string                    { return []string{sasl.Plain} }
func (s *smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != "jan@example.cz" || pass != "tajne" {
			return errors.New("bad credentials")
		}
		return nil
	}), nil
}
func (s *smtpSession) Mail(string, *smtp.MailOptions) error { return nil }
func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.be.mu.Lock()
	s.be.rcpt = append(s.be.rcpt, to)
	s.be.mu.Unlock()
	return nil
}
func (s *smtpSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.be.mu.Lock()
	s.be.mail = append(s.be.mail, string(b))
	s.be.mu.Unlock()
	return nil
}
func (s *smtpSession) Reset()        {}
func (s *smtpSession) Logout() error { return nil }

func startServers(t *testing.T) (config.MailServer, *imapmemserver.User, *smtpBackend) {
	tc, pool := selfSigned(t)
	testRootCAs, noCache = pool, true
	// No network key lookups and no access to the real key store.
	lookup = func(context.Context, string, bool) (*crypto.KeyRing, string) { return nil, "" }
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	keyring.MockInit()

	mem := imapmemserver.New()
	u := imapmemserver.NewUser("jan@example.cz", "tajne")
	for _, mb := range []string{"INBOX", "Sent", "Drafts", "Trash", "Spam", "Faktury"} {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(u)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:      imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		TLSConfig: tc,
		Logger:    nopLogger{},
	})
	iln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(iln)
	t.Cleanup(func() { srv.Close() })

	be := &smtpBackend{}
	ss := smtp.NewServer(be)
	ss.Domain = "localhost"
	sln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
	if err != nil {
		t.Fatal(err)
	}
	go ss.Serve(sln)
	t.Cleanup(func() { ss.Close() })

	port := func(l net.Listener) int { return l.Addr().(*net.TCPAddr).Port }
	return config.MailServer{
		Kind: "imap", Email: "jan@example.cz", Name: "Jan Novák", Username: "jan@example.cz",
		IMAPHost: "127.0.0.1", IMAPPort: port(iln), IMAPSecurity: "ssl",
		SMTPHost: "127.0.0.1", SMTPPort: port(sln), SMTPSecurity: "ssl",
	}, u, be
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...any) {}

const hello = "From: Petr <petr@example.org>\r\nTo: jan@example.cz\r\nSubject: =?UTF-8?Q?Ahoj_=C4=8De=C5=A1tino?=\r\n" +
	"Message-ID: <abc@example.org>\r\nDate: Mon, 28 Sep 2026 10:00:00 +0200\r\nList-Unsubscribe: <mailto:off@example.org>\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n\r\nDobrý den, posílám fakturu.\r\n"

// ---- tests ------------------------------------------------------------------------

func TestAccountAgainstServer(t *testing.T) {
	set, u, smtpBe := startServers(t)
	appendMsg(t, u, "INBOX", hello)
	ctx := context.Background()

	if err := Test(ctx, set, "spatne"); !errors.Is(err, ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	for _, f := range []string{protonmail.SentID, protonmail.DraftsID, protonmail.TrashID, protonmail.SpamID} {
		if !acc.HasFolder(f) {
			t.Errorf("folder %s not mapped (roles %v)", f, acc.roles)
		}
	}
	if acc.HasFolder(protonmail.ArchiveID) {
		t.Error("archive should not exist")
	}
	labels, _ := acc.UserLabels(ctx)
	if len(labels) != 1 || labels[0].Name != "Faktury" {
		t.Fatalf("user folders %+v", labels)
	}

	list, err := acc.List(ctx, protonmail.InboxID, 0, 50)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	s := list[0]
	if s.Subject != "Ahoj češtino" || s.Sender.Address != "petr@example.org" || !bool(s.Unread) || s.IsDraft() {
		t.Fatalf("summary %+v", s)
	}
	msg, err := acc.Get(ctx, s.ID)
	if err != nil || !strings.Contains(msg.Text, "posílám fakturu") {
		t.Fatalf("get %v %v", msg, err)
	}
	if msg.Headers["List-Unsubscribe"] == nil {
		t.Errorf("headers %v", msg.Headers)
	}
	if err := acc.MarkRead(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := acc.SetLabel(ctx, protonmail.StarredID, true, s.ID); err != nil {
		t.Fatal(err)
	}
	starred, _ := acc.List(ctx, protonmail.StarredID, 0, 50)
	if len(starred) != 1 || bool(starred[0].Unread) {
		t.Fatalf("starred %+v", starred)
	}
	found, err := acc.Search(ctx, protonmail.AllMailID, "fakturu", 50)
	if err != nil || len(found) != 1 {
		t.Fatalf("full-text search %v %v", found, err)
	}
	counts, _ := acc.UnreadCounts(ctx)
	if counts[protonmail.InboxID] != 0 {
		t.Errorf("unread %v", counts)
	}

	// Moving to Trash.
	if err := acc.Move(ctx, protonmail.TrashID, s.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := acc.List(ctx, protonmail.InboxID, 0, 50); len(l) != 0 {
		t.Fatalf("inbox after move %d", len(l))
	}
	if l, _ := acc.List(ctx, protonmail.TrashID, 0, 50); len(l) != 1 {
		t.Fatalf("trash after move %d", len(l))
	}

	// Drafts and sending.
	d := &protonmail.Draft{
		To: []*mail.Address{{Name: "Petr", Address: "petr@example.org"}}, BCC: []*mail.Address{{Address: "tajny@example.org"}},
		Subject: "Odpověď", Body: "Díky, Jan", HTML: "<p><b>Díky</b>, Jan</p>",
		Attachments: []*protonmail.Outgoing{{Name: "a.txt", MIMEType: "text/plain", Data: []byte("příloha"), Size: 8}},
	}
	if err := acc.SaveDraft(ctx, d); err != nil || d.ID == "" {
		t.Fatalf("save draft %q %v", d.ID, err)
	}
	firstDraft := d.ID
	if err := acc.SaveDraft(ctx, d); err != nil || d.ID == firstDraft {
		t.Fatalf("resave draft %q %v", d.ID, err)
	}
	if l, _ := acc.List(ctx, protonmail.DraftsID, 0, 50); len(l) != 1 || !l[0].IsDraft() {
		t.Fatalf("drafts %+v", l)
	}
	od, _, err := acc.OpenDraft(ctx, d.ID)
	if err != nil || len(od.Attachments) != 1 || string(od.Attachments[0].Data) != "příloha" {
		t.Fatalf("open draft %+v %v", od, err)
	}
	if err := acc.Send(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(smtpBe.mail) != 1 || len(smtpBe.rcpt) != 2 {
		t.Fatalf("smtp got %d mails, rcpt %v", len(smtpBe.mail), smtpBe.rcpt)
	}
	if strings.Contains(smtpBe.mail[0], "tajny@example.org") {
		t.Error("Bcc leaked into the message")
	}
	if l, _ := acc.List(ctx, protonmail.DraftsID, 0, 50); len(l) != 0 {
		t.Errorf("draft not removed after send: %d", len(l))
	}
	sent, _ := acc.List(ctx, protonmail.SentID, 0, 50)
	if len(sent) != 1 || sent[0].IsDraft() {
		t.Fatalf("sent %+v", sent)
	}
	sm, err := acc.Get(ctx, sent[0].ID)
	if err != nil || sm.HTML == "" || len(sm.Attachments) != 1 || !strings.Contains(sm.Text, "Díky, Jan") {
		t.Fatalf("sent message %+v %v", sm, err)
	}
}

// New mail: onNew runs for each new unread message before onChange.
func TestEventsOrder(t *testing.T) {
	set, u, _ := startServers(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	var mu sync.Mutex
	var order []string
	done := make(chan struct{}, 1)
	if err := acc.Events(ctx, func(s protonmail.Summary) {
		mu.Lock()
		order = append(order, "new:"+s.Subject)
		mu.Unlock()
	}, func() {
		mu.Lock()
		order = append(order, "change")
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // watcher connected and idling
	appendMsg(t, u, "INBOX", hello)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no event for new mail")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 || order[0] != "new:Ahoj češtino" || order[1] != "change" {
		t.Fatalf("order %v", order)
	}
}

func TestIDs(t *testing.T) {
	id := makeID("INBOX/Faktury, 2026", 42, 7)
	r, err := parseID(id)
	if err != nil || r.mailbox != "INBOX/Faktury, 2026" || r.uidValidity != 42 || r.uid != 7 {
		t.Fatalf("%v %+v", err, r)
	}
	if strings.ContainsAny(id, ",\x1f") {
		t.Fatalf("id %q must be safe for comma lists", id)
	}
}

func TestAutoconfigParse(t *testing.T) {
	var cc clientConfig
	cc.Provider.Incoming = []server{{Type: "pop3", Hostname: "pop.x.cz", Port: 995, SocketType: "SSL"}, {Type: "imap", Hostname: "imap.x.cz", Port: 143, SocketType: "STARTTLS", Username: "%EMAILLOCALPART%"}}
	cc.Provider.Outgoing = []server{{Type: "smtp", Hostname: "smtp.x.cz", Port: 587, SocketType: "STARTTLS"}}
	s, ok := parseAutoconfig(cc, config.MailServer{Email: "jan@x.cz"})
	if !ok || s.IMAPHost != "imap.x.cz" || s.IMAPSecurity != "starttls" || s.Username != "jan" || s.SMTPPort != 587 {
		t.Fatalf("%+v", s)
	}
	if KindForEmail("Jan@Email.cz") != "seznam" || KindForEmail("x@gmail.com") != "gmail" || KindForEmail("a@b.cz") != "imap" {
		t.Fatal("kinds")
	}
	if s, err := Discover(context.Background(), "jan@seznam.cz"); err != nil || s.IMAPHost != "imap.seznam.cz" || s.SMTPPort != 465 {
		t.Fatalf("seznam preset %+v %v", s, err)
	}
}

// Threads, local snooze and local scheduled sending.
func TestThreadsSnoozeSchedule(t *testing.T) {
	set, u, smtpBe := startServers(t)
	appendMsg(t, u, "INBOX", hello)
	ctx := context.Background()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	c, err := cache.Open(filepath.Join(t.TempDir(), "c.db"), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	acc.cache = c

	in, _ := acc.List(ctx, protonmail.InboxID, 0, 10)
	orig := in[0]
	if orig.ConversationID == "" {
		t.Fatal("no conversation ID")
	}
	// A reply lands in Sent with References to the original.
	msg, _ := acc.Get(ctx, orig.ID)
	d := &protonmail.Draft{To: []*mail.Address{orig.Sender}, Subject: "Re: " + orig.Subject, Body: "odpověď",
		ParentID: orig.ID, Action: protonmail.ActionReply}
	_ = msg
	if err := acc.Send(ctx, d); err != nil {
		t.Fatal(err)
	}
	thread, err := acc.ThreadMessages(ctx, orig.ConversationID, false)
	if err != nil || len(thread) != 2 {
		t.Fatalf("thread %d %v", len(thread), err)
	}
	if !strings.Contains(smtpBe.mail[0], "In-Reply-To: <abc@example.org>") {
		t.Errorf("reply headers:\n%s", smtpBe.mail[0][:300])
	}

	// Snooze moves the conversation out of the inbox and back when due.
	if err := acc.Snooze(ctx, []string{orig.ConversationID}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if l, _ := acc.List(ctx, protonmail.InboxID, 0, 10); len(l) != 0 {
		t.Fatalf("inbox after snooze %d", len(l))
	}
	if !acc.HasFolder(protonmail.SnoozedID) {
		t.Fatal("snoozed folder not created")
	}
	if l, _ := acc.List(ctx, protonmail.SnoozedID, 0, 10); len(l) != 1 {
		t.Fatalf("snoozed %d", len(l))
	}
	if acc.SnoozedUntil(orig.ConversationID).IsZero() {
		t.Error("snooze time not stored")
	}
	st := acc.loadLocal()
	st.Snoozed[orig.ConversationID] = time.Now().Add(-time.Minute).Unix()
	_ = acc.saveLocal(st)
	acc.runDue(ctx, nil)
	back, _ := acc.List(ctx, protonmail.InboxID, 0, 10)
	if len(back) != 1 || !bool(back[0].Unread) {
		t.Fatalf("after snooze ended: %+v", back)
	}

	// Scheduled mail waits in "Naplánované" and leaves when due.
	when := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	sd := &protonmail.Draft{To: []*mail.Address{{Address: "petr@example.org"}}, Subject: "Později", Body: "text", DeliveryTime: when}
	if err := acc.Send(ctx, sd); err != nil {
		t.Fatal(err)
	}
	sched, _ := acc.List(ctx, protonmail.ScheduledID, 0, 10)
	if len(sched) != 1 || sched[0].Time != when.Unix() || sched[0].IsDraft() || !protonmail.IsScheduled(sched[0]) {
		t.Fatalf("scheduled %+v", sched)
	}
	if len(smtpBe.mail) != 1 {
		t.Fatal("scheduled mail sent too early")
	}
	st = acc.loadLocal()
	for id := range st.Scheduled {
		st.Scheduled[id] = time.Now().Add(-time.Second).Unix()
	}
	_ = acc.saveLocal(st)
	acc.runDue(ctx, nil)
	if len(smtpBe.mail) != 2 {
		t.Fatalf("scheduled mail not sent (%d)", len(smtpBe.mail))
	}
	if l, _ := acc.List(ctx, protonmail.ScheduledID, 0, 10); len(l) != 0 {
		t.Fatalf("scheduled folder not emptied: %d", len(l))
	}
	if len(acc.loadLocal().Scheduled) != 0 {
		t.Error("queue not emptied")
	}
}

// Gmail: labels are mailboxes; adding copies, removing deletes the copy,
// archiving moves to All Mail.
func TestGmailLabels(t *testing.T) {
	set, u, _ := startServers(t)
	for _, mb := range []string{"[Gmail]/All Mail", "[Gmail]/Starred", "Práce"} {
		if err := u.Create(mb, nil); err != nil {
			t.Fatal(err)
		}
	}
	set.Kind = "gmail"
	appendMsg(t, u, "INBOX", hello)
	ctx := context.Background()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()

	labels, _ := acc.UserLabels(ctx)
	var work protonmail.UserLabel
	for _, l := range labels {
		if l.Name == "Práce" {
			work = l
		}
	}
	if work.ID == "" || work.Folder || work.Color == "" {
		t.Fatalf("Gmail label should be a coloured tag: %+v", labels)
	}
	if acc.HasFolder(protonmail.ArchiveID) || !acc.HasFolder(protonmail.AllMailID) {
		t.Fatal("Gmail archive/all mail mapping")
	}
	in, _ := acc.List(ctx, protonmail.InboxID, 0, 10)
	if err := acc.SetLabel(ctx, work.ID, true, in[0].ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := acc.List(ctx, protonmail.InboxID, 0, 10); len(l) != 1 {
		t.Fatal("labelling must not remove from the inbox")
	}
	if l, _ := acc.List(ctx, work.ID, 0, 10); len(l) != 1 {
		t.Fatal("label not added")
	}
	if err := acc.SetLabel(ctx, work.ID, false, in[0].ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := acc.List(ctx, work.ID, 0, 10); len(l) != 0 {
		t.Fatal("label not removed")
	}
	if err := acc.Move(ctx, protonmail.ArchiveID, in[0].ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := acc.List(ctx, protonmail.AllMailID, 0, 10); len(l) != 1 {
		t.Fatal("archive should land in All Mail")
	}
	if l, _ := acc.List(ctx, protonmail.InboxID, 0, 10); len(l) != 0 {
		t.Fatal("archived message still in inbox")
	}
	if !acc.HasFolderRole(protonmail.StarredID) {
		t.Fatal("Gmail Starred folder not mapped")
	}
}

// Gmail keeps every message also in All Mail (its archive): a thread must
// show each message once.
func TestGmailThreadNoDuplicates(t *testing.T) {
	set, u, _ := startServers(t)
	if err := u.Create("[Gmail]/All Mail", nil); err != nil {
		t.Fatal(err)
	}
	set.Kind = "gmail"
	appendMsg(t, u, "INBOX", hello)
	appendMsg(t, u, "[Gmail]/All Mail", hello)
	ctx := context.Background()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	in, _ := acc.List(ctx, protonmail.InboxID, 0, 10)
	if len(in) != 1 {
		t.Fatalf("inbox %d", len(in))
	}
	thread, err := acc.ThreadMessages(ctx, in[0].ConversationID, false)
	if err != nil || len(thread) != 1 {
		t.Fatalf("thread has %d messages (%v), want 1", len(thread), err)
	}
	if thread[0].ID != in[0].ID {
		t.Error("the copy from the inbox should be shown")
	}
}
