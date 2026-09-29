// Package imapmail is the IMAP/SMTP implementation of mailbox.Account, used
// for Seznam, Gmail (app password) and any other standard mail service.
package imapmail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/libormacak/klient/internal/cache"
	"github.com/libormacak/klient/internal/config"
	"github.com/libormacak/klient/internal/mailbox"
	"github.com/libormacak/klient/internal/pgp"
	"github.com/libormacak/klient/internal/protonmail"
	"github.com/libormacak/klient/internal/secrets"
)

var _ mailbox.Account = (*Account)(nil)

// ErrAuth means the server rejected the login (wrong or revoked password).
var ErrAuth = errors.New("server odmítl přihlášení – zkontrolujte heslo")

// Account is one IMAP/SMTP account.
type Account struct {
	set      config.MailServer
	password string

	mu sync.Mutex // guards the command connection (network operations)
	c  *imapclient.Client

	// Folder list and quota: read by the UI thread, so under their own lock
	// that is never held during network operations.
	meta      sync.RWMutex
	roles     map[string]string // system folder ID -> mailbox name
	userBoxes []protonmail.UserLabel
	delim     rune
	quota     protonmail.StorageInfo

	cache   *cache.Cache
	offline bool
	deauth  func()
}

var wordDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

// Test hooks: trusted CAs of a test server, and no keyring/disk cache.
var (
	testRootCAs *x509.CertPool
	noCache     bool
)

func tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: testRootCAs}
}

func (a *Account) dial() (*imapclient.Client, error) { return a.dialWith(nil) }

// dialWith connects and logs in; handler receives unsolicited server data
// (used by the IDLE connection).
func (a *Account) dialWith(handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	addr := net.JoinHostPort(a.set.IMAPHost, strconv.Itoa(a.set.IMAPPort))
	opts := &imapclient.Options{
		TLSConfig:             tlsConfig(a.set.IMAPHost),
		WordDecoder:           wordDecoder,
		Dialer:                &net.Dialer{Timeout: 30 * time.Second},
		UnilateralDataHandler: handler,
	}
	var c *imapclient.Client
	var err error
	if a.set.IMAPSecurity == "starttls" {
		c, err = imapclient.DialStartTLS(addr, opts)
	} else {
		c, err = imapclient.DialTLS(addr, opts)
	}
	if err != nil {
		return nil, &netErr{err}
	}
	if err := c.Login(a.set.Username, a.password).Wait(); err != nil {
		c.Close()
		var ie *imap.Error
		if errors.As(err, &ie) && (ie.Code == imap.ResponseCodeAuthenticationFailed || ie.Code == imap.ResponseCodeAuthorizationFailed || ie.Type == imap.StatusResponseTypeNo) {
			return nil, fmt.Errorf("%w (%s)", ErrAuth, ie.Text)
		}
		return nil, &netErr{err}
	}
	return c, nil
}

// netErr marks connection problems (offline) as opposed to server answers.
type netErr struct{ err error }

func (e *netErr) Error() string { return "server " + "není dostupný: " + e.err.Error() }
func (e *netErr) Unwrap() error { return e.err }

// IsOffline reports whether err means the server was unreachable.
func IsOffline(err error) bool {
	var ne *netErr
	return errors.As(err, &ne)
}

// Open logs in, reads the folder list and opens the offline cache. Without
// network it starts from the cache if possible.
func Open(ctx context.Context, set config.MailServer, password string) (*Account, error) {
	a := &Account{set: set, password: password}
	a.openCache()
	c, err := a.dial()
	if err != nil {
		if IsOffline(err) && a.cache != nil {
			a.offline = true
			a.roles = map[string]string{protonmail.InboxID: "INBOX"}
			var saved map[string]string
			if a.cache.Get("imap-roles", &saved) {
				a.roles = saved
			}
			a.cache.Get("imap-folders", &a.userBoxes)
			return a, nil
		}
		return nil, err
	}
	a.c = c
	if err := a.loadFolders(); err != nil {
		c.Close()
		return nil, err
	}
	a.refreshQuota()
	return a, nil
}

// Test checks the IMAP login (for the account wizard).
func Test(ctx context.Context, set config.MailServer, password string) error {
	a := &Account{set: set, password: password}
	c, err := a.dial()
	if err != nil {
		return err
	}
	c.Logout().Wait()
	return nil
}

func (a *Account) openCache() {
	if noCache {
		return
	}
	key, err := secrets.CacheKey(a.UserID())
	if err != nil {
		return
	}
	name := strings.NewReplacer("@", "_at_", ":", "_", "/", "_").Replace(a.UserID())
	if c, err := cache.Open(filepath.Join(config.DataDir(), "cache", name+".db"), key); err == nil {
		a.cache = c
	}
}

func (a *Account) cachePath() string {
	name := strings.NewReplacer("@", "_at_", ":", "_", "/", "_").Replace(a.UserID())
	return filepath.Join(config.DataDir(), "cache", name+".db")
}

// conn returns the command connection, reconnecting if needed. Callers hold a.mu.
func (a *Account) conn() (*imapclient.Client, error) {
	if a.c != nil {
		select {
		case <-a.c.Closed():
			a.c = nil
		default:
			return a.c, nil
		}
	}
	c, err := a.dial()
	if err != nil {
		if errors.Is(err, ErrAuth) && a.deauth != nil {
			go a.deauth()
		}
		return nil, err
	}
	a.c = c
	if a.offline {
		a.offline = false
		_ = a.loadFolders()
	}
	return c, nil
}

// with runs f on the command connection, retrying once after a dropped
// connection.
func (a *Account) with(f func(c *imapclient.Client) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for try := 0; ; try++ {
		c, err := a.conn()
		if err != nil {
			return err
		}
		err = f(c)
		if err == nil || try > 0 {
			return err
		}
		select {
		case <-c.Closed():
			a.c = nil // dropped: reconnect and retry
		default:
			return err
		}
	}
}

// ---- Folders -------------------------------------------------------------------

var roleAttrs = map[imap.MailboxAttr]string{
	imap.MailboxAttrSent:    protonmail.SentID,
	imap.MailboxAttrDrafts:  protonmail.DraftsID,
	imap.MailboxAttrTrash:   protonmail.TrashID,
	imap.MailboxAttrJunk:    protonmail.SpamID,
	imap.MailboxAttrArchive: protonmail.ArchiveID,
	imap.MailboxAttrAll:     protonmail.AllMailID,
	imap.MailboxAttrFlagged: protonmail.StarredID, // Gmail's "Starred"
}

// Names used by servers without SPECIAL-USE (lower case, last path part).
var roleNames = map[string]string{
	"sent": protonmail.SentID, "sent items": protonmail.SentID, "sent messages": protonmail.SentID,
	"sent mail": protonmail.SentID, "odeslané": protonmail.SentID, "odeslana posta": protonmail.SentID, "odeslaná pošta": protonmail.SentID,
	"drafts": protonmail.DraftsID, "koncepty": protonmail.DraftsID, "rozepsané": protonmail.DraftsID,
	"trash": protonmail.TrashID, "deleted": protonmail.TrashID, "deleted items": protonmail.TrashID,
	"deleted messages": protonmail.TrashID, "koš": protonmail.TrashID, "kos": protonmail.TrashID,
	"spam": protonmail.SpamID, "junk": protonmail.SpamID, "junk e-mail": protonmail.SpamID, "nevyžádaná pošta": protonmail.SpamID,
	"archive": protonmail.ArchiveID, "archiv": protonmail.ArchiveID,
	"all mail": protonmail.AllMailID, "všechny zprávy": protonmail.AllMailID,
	"starred": protonmail.StarredID, "s hvězdičkou": protonmail.StarredID,
	// Folders Klient creates for local snooze and scheduled sending.
	"odložené": protonmail.SnoozedID, "snoozed": protonmail.SnoozedID,
	"naplánované": protonmail.ScheduledID, "scheduled": protonmail.ScheduledID,
}

// loadFolders maps mailboxes to Klient's system folders. Callers hold a.mu
// (or it runs during Open).
func (a *Account) loadFolders() error {
	list, err := a.c.List("", "*", &imap.ListOptions{ReturnSpecialUse: a.c.Caps().Has(imap.CapSpecialUse)}).Collect()
	if err != nil {
		return err
	}
	roles := map[string]string{protonmail.InboxID: "INBOX"}
	byName := map[string]string{}
	var users []protonmail.UserLabel
	for _, l := range list {
		if l.Delim != 0 {
			a.meta.Lock()
			a.delim = l.Delim
			a.meta.Unlock()
		}
		noSelect := false
		role := ""
		for _, attr := range l.Attrs {
			if attr == imap.MailboxAttrNoSelect || attr == imap.MailboxAttrNonExistent {
				noSelect = true
			}
			if attr == imap.MailboxAttrImportant && a.gmail() {
				noSelect = true // Gmail's "Important" duplicates the inbox
			}
			if r, ok := roleAttrs[attr]; ok {
				role = r
			}
		}
		if noSelect {
			continue
		}
		if strings.EqualFold(l.Mailbox, "INBOX") {
			continue
		}
		if role != "" {
			if _, taken := roles[role]; !taken {
				roles[role] = l.Mailbox
				continue
			}
		}
		last := l.Mailbox
		if l.Delim != 0 {
			if i := strings.LastIndexByte(last, byte(l.Delim)); i >= 0 {
				last = last[i+1:]
			}
		}
		if r, ok := roleNames[strings.ToLower(last)]; ok {
			byName[r] = l.Mailbox
			continue
		}
		name := l.Mailbox
		if l.Delim != 0 {
			name = strings.ReplaceAll(name, string(l.Delim), "/")
		}
		name = strings.TrimPrefix(name, "INBOX/")
		users = append(users, a.userBox(l.Mailbox, name))
	}
	for r, name := range byName {
		if _, ok := roles[r]; !ok {
			roles[r] = name
		} else {
			users = append(users, a.userBox(name, name))
		}
	}
	sort.Slice(users, func(i, j int) bool { return strings.ToLower(users[i].Name) < strings.ToLower(users[j].Name) })
	a.meta.Lock()
	a.roles, a.userBoxes = roles, users
	a.meta.Unlock()
	if a.cache != nil {
		_ = a.cache.Set("imap-roles", roles)
		_ = a.cache.Set("imap-folders", users)
	}
	return nil
}

func (a *Account) gmail() bool { return a.set.Kind == "gmail" }

// Gmail label colours (Gmail does not tell them over IMAP).
var labelPalette = []string{"#e66100", "#3584e4", "#2ec27e", "#9141ac", "#c64600", "#1c71d8", "#26a269", "#a51d2d", "#986a44", "#613583"}

// userBox describes a user mailbox: a folder, or on Gmail a label.
func (a *Account) userBox(mailbox, name string) protonmail.UserLabel {
	if !a.gmail() {
		return protonmail.UserLabel{ID: folderID(mailbox), Name: name, Folder: true}
	}
	h := 0
	for _, r := range mailbox {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return protonmail.UserLabel{ID: folderID(mailbox), Name: name, Color: labelPalette[h%len(labelPalette)]}
}

func folderID(mailbox string) string {
	return "f." + base64.RawURLEncoding.EncodeToString([]byte(mailbox))
}

// mailboxOf returns the IMAP mailbox of a folder ID.
func (a *Account) mailboxOf(id string) (string, error) {
	if strings.HasPrefix(id, "f.") {
		b, err := base64.RawURLEncoding.DecodeString(id[2:])
		return string(b), err
	}
	a.meta.RLock()
	defer a.meta.RUnlock()
	if name, ok := a.roles[id]; ok {
		return name, nil
	}
	switch {
	case id == protonmail.StarredID:
		return "INBOX", nil // flagged messages of the inbox
	case id == protonmail.ArchiveID && a.gmail() && a.roles[protonmail.AllMailID] != "":
		// Gmail archives by removing the Inbox label: moving to All Mail.
		return a.roles[protonmail.AllMailID], nil
	}
	return "", fmt.Errorf("složka %s na serveru neexistuje", protonmail.FolderByID(id).Name)
}

// folderOf is the folder ID of a mailbox.
func (a *Account) folderOf(mailbox string) string {
	a.meta.RLock()
	defer a.meta.RUnlock()
	for id, name := range a.roles {
		if name == mailbox {
			return id
		}
	}
	return folderID(mailbox)
}

func (a *Account) HasFolder(id string) bool {
	a.meta.RLock()
	defer a.meta.RUnlock()
	switch id {
	case protonmail.InboxID, protonmail.StarredID:
		return true
	}
	_, ok := a.roles[id]
	return ok
}

// HasFolderRole reports a system folder backed by its own mailbox.
func (a *Account) HasFolderRole(id string) bool {
	a.meta.RLock()
	defer a.meta.RUnlock()
	_, ok := a.roles[id]
	return ok
}

func (a *Account) UserLabels(ctx context.Context) ([]protonmail.UserLabel, error) {
	a.meta.RLock()
	defer a.meta.RUnlock()
	return append([]protonmail.UserLabel{}, a.userBoxes...), nil
}

// ---- Message IDs -------------------------------------------------------------

// A message ID is "<mailbox base64>.<uidvalidity>.<uid>": stable while the
// message stays in its mailbox (moving it gives it a new UID).
func makeID(mailbox string, uidValidity uint32, uid imap.UID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(mailbox)) + "." +
		strconv.FormatUint(uint64(uidValidity), 10) + "." + strconv.FormatUint(uint64(uid), 10)
}

type msgRef struct {
	mailbox     string
	uidValidity uint32
	uid         imap.UID
}

func parseID(id string) (msgRef, error) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 {
		return msgRef{}, fmt.Errorf("neplatné ID zprávy")
	}
	mb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return msgRef{}, err
	}
	v, err1 := strconv.ParseUint(parts[1], 10, 32)
	u, err2 := strconv.ParseUint(parts[2], 10, 32)
	if err1 != nil || err2 != nil {
		return msgRef{}, fmt.Errorf("neplatné ID zprávy")
	}
	return msgRef{string(mb), uint32(v), imap.UID(u)}, nil
}

// groupIDs sorts message IDs by mailbox.
func groupIDs(ids []string) (map[string][]imap.UID, error) {
	out := map[string][]imap.UID{}
	for _, id := range ids {
		r, err := parseID(id)
		if err != nil {
			return nil, err
		}
		out[r.mailbox] = append(out[r.mailbox], r.uid)
	}
	return out, nil
}

// ---- Identity and lifecycle ----------------------------------------------------

func (a *Account) Kind() mailbox.Kind {
	switch a.set.Kind {
	case "seznam":
		return mailbox.KindSeznam
	case "gmail":
		return mailbox.KindGmail
	}
	return mailbox.KindIMAP
}

func (a *Account) Caps() mailbox.Caps {
	// E2E (PGP) once the user has a key for this address.
	return mailbox.Caps{FullTextSearch: true, LocalSnooze: true, LocalSchedule: true, E2E: pgp.HasOwnKey(a.set.Email), Labels: a.gmail()}
}

func (a *Account) Settings() config.MailServer { return a.set }
func (a *Account) Username() string            { return a.set.ID() }
func (a *Account) UserID() string              { return a.set.ID() }
func (a *Account) Email() string               { return a.set.Email }

func (a *Account) DisplayName() string {
	if a.set.Name != "" {
		return a.set.Name
	}
	return a.set.Email
}

func (a *Account) SendAddresses() []proton.Address {
	return []proton.Address{{
		ID: "0", Email: a.set.Email, DisplayName: a.set.Name,
		Send: true, Receive: true, Status: proton.AddressStatusEnabled,
	}}
}

func (a *Account) IsOwnAddress(addr string) bool {
	return strings.EqualFold(strings.TrimSpace(addr), a.set.Email)
}

func (a *Account) OnDeauth(f func())    { a.deauth = f }
func (a *Account) StartedOffline() bool { return a.offline }
func (a *Account) Cache() *cache.Cache  { return a.cache }

func (a *Account) Close() {
	a.mu.Lock()
	if a.c != nil {
		a.c.Logout().Wait()
		a.c.Close()
		a.c = nil
	}
	a.mu.Unlock()
	if a.cache != nil {
		a.cache.Close()
	}
}

// Logout forgets the account on this computer: password, cache and its key.
func (a *Account) Logout(ctx context.Context) error {
	a.Close()
	err := secrets.SaveMailPassword(a.set.ID(), "")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(a.cachePath() + suffix)
	}
	secrets.DeleteCacheKey(a.UserID())
	return err
}

// ---- Listing ---------------------------------------------------------------------

var fetchMeta = &imap.FetchOptions{
	Envelope: true, Flags: true, InternalDate: true, RFC822Size: true, UID: true,
	BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	// References is not part of the envelope; it is needed for threads.
	BodySection: []*imap.FetchItemBodySection{{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}, Peek: true}},
}

func addrList(in []imap.Address) []*mail.Address {
	out := []*mail.Address{}
	for _, ad := range in {
		if e := ad.Addr(); e != "" {
			out = append(out, &mail.Address{Name: ad.Name, Address: e})
		}
	}
	return out
}

func countAttachments(bs imap.BodyStructure) int {
	n := 0
	mp, ok := bs.(*imap.BodyStructureMultiPart)
	if !ok {
		return 0
	}
	mp.Walk(func(path []int, part imap.BodyStructure) bool {
		sp, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if d := sp.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			n++
		} else if sp.Filename() != "" && !strings.EqualFold(sp.Type, "text") {
			n++
		}
		return true
	})
	return n
}

// summary converts FETCH data of a message in mailbox to Klient's summary.
func (a *Account) summary(mailbox string, uidValidity uint32, m *imapclient.FetchMessageBuffer) protonmail.Summary {
	folder := a.folderOf(mailbox)
	s := protonmail.Summary{
		ID: makeID(mailbox, uidValidity, m.UID), AddressID: "0",
		LabelIDs: []string{folder}, Size: int(m.RFC822Size),
		NumAttachments: countAttachments(m.BodyStructure),
		Unread:         true,
	}
	if env := m.Envelope; env != nil {
		s.Subject = env.Subject
		s.ExternalID = strings.Trim(env.MessageID, "<>")
		if from := addrList(env.From); len(from) > 0 {
			s.Sender = from[0]
		}
		s.ToList, s.CCList, s.BCCList = addrList(env.To), addrList(env.Cc), addrList(env.Bcc)
		s.ReplyTos = addrList(env.ReplyTo)
		if !env.Date.IsZero() {
			s.Time = env.Date.Unix()
		}
	}
	if s.Time == 0 {
		s.Time = m.InternalDate.Unix()
	}
	var refs string
	for _, sec := range m.BodySection {
		refs += headerValue(headerBlock(sec.Bytes), "References")
	}
	var inReplyTo []string
	if m.Envelope != nil {
		inReplyTo = m.Envelope.InReplyTo
	}
	s.ConversationID = conversationID(refs, inReplyTo, s.ExternalID)
	draft := false
	for _, f := range m.Flags {
		switch f {
		case imap.FlagSeen:
			s.Unread = false
		case imap.FlagFlagged:
			s.LabelIDs = append(s.LabelIDs, protonmail.StarredID)
		case imap.FlagDraft:
			draft = true
		}
	}
	switch {
	case folder == protonmail.ScheduledID:
		s.Flags = proton.MessageFlagSent // waits for its time, not a draft
	case folder == protonmail.DraftsID || draft:
		s.Flags = 0 // IsDraft
	case folder == protonmail.SentID || (s.Sender != nil && a.IsOwnAddress(s.Sender.Address)):
		s.Flags = proton.MessageFlagSent
	default:
		s.Flags = proton.MessageFlagReceived
	}
	return s
}

// fetchUIDs fetches summaries of the given UIDs of the selected mailbox.
func (a *Account) fetchUIDs(c *imapclient.Client, mailbox string, uidValidity uint32, uids []imap.UID) ([]protonmail.Summary, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), fetchMeta).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]protonmail.Summary, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, a.summary(mailbox, uidValidity, m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out, nil
}

func (a *Account) List(ctx context.Context, folderID string, page, pageSize int) ([]protonmail.Summary, error) {
	var out []protonmail.Summary
	err := a.with(func(c *imapclient.Client) error {
		mb, err := a.mailboxOf(folderID)
		if err != nil {
			return err
		}
		sel, err := c.Select(mb, nil).Wait()
		if err != nil {
			return err
		}
		if folderID == protonmail.StarredID && !a.HasFolderRole(protonmail.StarredID) {
			res, err := c.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagFlagged}}, nil).Wait()
			if err != nil {
				return err
			}
			uids := pageOf(res.AllUIDs(), page, pageSize)
			out, err = a.fetchUIDs(c, mb, sel.UIDValidity, uids)
			return err
		}
		n := int(sel.NumMessages)
		hi := n - page*pageSize
		if hi < 1 {
			return nil
		}
		lo := max(1, hi-pageSize+1)
		var set imap.SeqSet
		set.AddRange(uint32(lo), uint32(hi))
		msgs, err := c.Fetch(set, fetchMeta).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			out = append(out, a.summary(mb, sel.UIDValidity, m))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
		return nil
	})
	if err != nil {
		if IsOffline(err) && a.cache != nil {
			return a.cache.List(folderID, page, pageSize)
		}
		return nil, err
	}
	if a.cache != nil {
		_ = a.cache.PutMetadata(out...)
	}
	return out, nil
}

// pageOf returns one page of UIDs, newest (highest) first.
func pageOf(uids []imap.UID, page, size int) []imap.UID {
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	start := page * size
	if start >= len(uids) {
		return nil
	}
	return uids[start:min(len(uids), start+size)]
}

// Search looks for all words in the whole message (the server searches
// bodies too). For "all mail" without a server-side All folder it searches
// the inbox, archive and sent folders.
func (a *Account) Search(ctx context.Context, folderID, query string, maxMessages int) ([]protonmail.Summary, error) {
	words := strings.Fields(query)
	if len(words) == 0 {
		return nil, nil
	}
	folders := []string{folderID}
	if folderID == protonmail.AllMailID && !a.HasFolder(protonmail.AllMailID) {
		folders = []string{protonmail.InboxID, protonmail.ArchiveID, protonmail.SentID}
	}
	var out []protonmail.Summary
	err := a.with(func(c *imapclient.Client) error {
		for _, f := range folders {
			mb, err := a.mailboxOf(f)
			if err != nil {
				continue
			}
			sel, err := c.Select(mb, nil).Wait()
			if err != nil {
				return err
			}
			res, err := c.UIDSearch(&imap.SearchCriteria{Text: words}, &imap.SearchOptions{}).Wait()
			if err != nil {
				return err
			}
			found, err := a.fetchUIDs(c, mb, sel.UIDValidity, pageOf(res.AllUIDs(), 0, maxMessages))
			if err != nil {
				return err
			}
			out = append(out, found...)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	if len(out) > maxMessages {
		out = out[:maxMessages]
	}
	return out, err
}

// ---- Flags and moving ------------------------------------------------------------

func (a *Account) store(ids []string, op imap.StoreFlagsOp, flag imap.Flag) error {
	groups, err := groupIDs(ids)
	if err != nil {
		return err
	}
	return a.with(func(c *imapclient.Client) error {
		for mb, uids := range groups {
			if _, err := c.Select(mb, nil).Wait(); err != nil {
				return err
			}
			if err := c.Store(imap.UIDSetNum(uids...), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Close(); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *Account) MarkRead(ctx context.Context, ids ...string) error {
	return a.store(ids, imap.StoreFlagsAdd, imap.FlagSeen)
}

func (a *Account) MarkUnread(ctx context.Context, ids ...string) error {
	return a.store(ids, imap.StoreFlagsDel, imap.FlagSeen)
}

func (a *Account) SetLabel(ctx context.Context, labelID string, on bool, ids ...string) error {
	if labelID != protonmail.StarredID {
		if !strings.HasPrefix(labelID, "f.") {
			return mailbox.ErrUnsupported
		}
		if a.gmail() {
			return a.gmailLabel(ctx, labelID, on, ids)
		}
		if on {
			return a.Move(ctx, labelID, ids...)
		}
		return mailbox.ErrUnsupported
	}
	op := imap.StoreFlagsDel
	if on {
		op = imap.StoreFlagsAdd
	}
	return a.store(ids, op, imap.FlagFlagged)
}

// Move moves messages to a folder (MOVE, or COPY + delete on old servers).
func (a *Account) Move(ctx context.Context, folderID string, ids ...string) error {
	groups, err := groupIDs(ids)
	if err != nil {
		return err
	}
	err = a.with(func(c *imapclient.Client) error {
		dest, err := a.mailboxOf(folderID)
		if err != nil {
			return err
		}
		for mb, uids := range groups {
			if mb == dest {
				continue
			}
			if _, err := c.Select(mb, nil).Wait(); err != nil {
				return err
			}
			set := imap.UIDSetNum(uids...)
			if c.Caps().Has(imap.CapMove) {
				if _, err := c.Move(set, dest).Wait(); err != nil {
					return err
				}
				continue
			}
			if _, err := c.Copy(set, dest).Wait(); err != nil {
				return err
			}
			if err := a.expungeUIDs(c, set); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && a.cache != nil {
		_ = a.cache.Delete(ids...)
	}
	return err
}

// expungeUIDs deletes messages of the selected mailbox.
func (a *Account) expungeUIDs(c *imapclient.Client, set imap.UIDSet) error {
	if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return err
	}
	if c.Caps().Has(imap.CapUIDPlus) {
		return c.UIDExpunge(set).Close()
	}
	return c.Expunge().Close()
}

// deleteIDs removes messages permanently (old drafts).
func (a *Account) deleteIDs(ids ...string) error {
	groups, err := groupIDs(ids)
	if err != nil {
		return err
	}
	return a.with(func(c *imapclient.Client) error {
		for mb, uids := range groups {
			if _, err := c.Select(mb, nil).Wait(); err != nil {
				return err
			}
			if err := a.expungeUIDs(c, imap.UIDSetNum(uids...)); err != nil {
				return err
			}
		}
		return nil
	})
}

// UnreadCounts returns unread messages of the inbox, system and user folders.
func (a *Account) UnreadCounts(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	err := a.with(func(c *imapclient.Client) error {
		boxes := map[string]string{}
		a.meta.RLock()
		for id, mb := range a.roles {
			boxes[id] = mb
		}
		users := append([]protonmail.UserLabel{}, a.userBoxes...)
		a.meta.RUnlock()
		for i, u := range users {
			if i >= 40 {
				break
			}
			if mb, err := a.mailboxOf(u.ID); err == nil {
				boxes[u.ID] = mb
			}
		}
		for id, mb := range boxes {
			st, err := c.Status(mb, &imap.StatusOptions{NumUnseen: true}).Wait()
			if err != nil {
				continue
			}
			if st.NumUnseen != nil {
				out[id] = int(*st.NumUnseen)
			}
		}
		return nil
	})
	return out, err
}

// ---- Storage ---------------------------------------------------------------------

// refreshQuota reads the mailbox quota (QUOTA extension). Callers hold a.mu or
// run during Open.
func (a *Account) refreshQuota() {
	if a.c == nil || !a.c.Caps().Has(imap.CapQuota) {
		return
	}
	qs, err := a.c.GetQuotaRoot("INBOX").Wait()
	if err != nil {
		return
	}
	for _, q := range qs {
		if r, ok := q.Resources[imap.QuotaResourceStorage]; ok {
			a.meta.Lock()
			defer a.meta.Unlock()
			a.quota = protonmail.StorageInfo{
				Used: uint64(r.Usage) * 1024, Max: uint64(r.Limit) * 1024,
				Mail: uint64(r.Usage) * 1024, Updated: time.Now(),
			}
		}
	}
}

func (a *Account) Storage() protonmail.StorageInfo {
	a.meta.RLock()
	defer a.meta.RUnlock()
	return a.quota
}

func (a *Account) RefreshUser(ctx context.Context) error {
	return a.with(func(c *imapclient.Client) error {
		a.refreshQuota()
		a.meta.RLock()
		known := a.quota.Max > 0
		a.meta.RUnlock()
		if !known {
			a.measureSize(c) // servers without QUOTA (e.g. Seznam)
		}
		return nil
	})
}

// measureSize adds up the size of all folders when the server does not
// report a quota: the UI then shows the used space without a limit.
func (a *Account) measureSize(c *imapclient.Client) {
	a.meta.RLock()
	var boxes []string
	for _, mb := range a.roles {
		boxes = append(boxes, mb)
	}
	for _, u := range a.userBoxes {
		if mb, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.ID, "f.")); err == nil {
			boxes = append(boxes, string(mb))
		}
	}
	a.meta.RUnlock()
	var total int64
	for _, mb := range boxes {
		if c.Caps().Has(imap.CapStatusSize) {
			if st, err := c.Status(mb, &imap.StatusOptions{Size: true}).Wait(); err == nil && st.Size != nil {
				total += *st.Size
				continue
			}
		}
		sel, err := c.Select(mb, nil).Wait()
		if err != nil || sel.NumMessages == 0 {
			continue
		}
		var set imap.SeqSet
		set.AddRange(1, sel.NumMessages)
		msgs, err := c.Fetch(set, &imap.FetchOptions{RFC822Size: true}).Collect()
		if err != nil {
			continue
		}
		for _, m := range msgs {
			total += m.RFC822Size
		}
	}
	a.meta.Lock()
	a.quota = protonmail.StorageInfo{Used: uint64(total), Mail: uint64(total), Updated: time.Now()}
	a.meta.Unlock()
}
