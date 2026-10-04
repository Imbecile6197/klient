package mailbox

import (
	"context"
	"errors"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/Imbecile6197/klient/internal/cache"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// KindUnified is the combined view of all accounts.
const KindUnified Kind = "unified"

// UnifiedFolders are the folders the combined view offers: the ones every
// service has and where merging makes sense.
var UnifiedFolders = []string{
	protonmail.InboxID, protonmail.StarredID, protonmail.SentID,
	protonmail.ArchiveID, protonmail.SpamID, protonmail.TrashID,
}

// idSep separates the account number from the account's own ID. It is not
// a comma (IDs travel in comma lists) and not in any service's IDs.
const idSep = "\x1e"

// Unified shows several accounts as one. Messages of all accounts are listed
// together; every message and conversation ID gets the number of its
// account in front, so that opening, moving or deleting it reaches the
// account it belongs to. Writing and account settings stay with the real
// accounts (Resolve, Owner).
type Unified struct {
	accounts func() []Account

	mu    sync.Mutex
	nums  map[string]int // UserID -> number
	byNum map[int]Account
}

// NewUnified combines the accounts returned by accounts (asked each time,
// so accounts added or removed later are included).
func NewUnified(accounts func() []Account) *Unified {
	return &Unified{accounts: accounts, nums: map[string]int{}, byNum: map[int]Account{}}
}

var _ Account = (*Unified)(nil)

func (u *Unified) list() []Account {
	accs := u.accounts()
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, a := range accs {
		n, ok := u.nums[a.UserID()]
		if !ok {
			n = len(u.nums) + 1
			u.nums[a.UserID()] = n
		}
		u.byNum[n] = a // a newer session of the same account replaces the old
	}
	return accs
}

func (u *Unified) num(acc Account) int {
	u.list()
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.nums[acc.UserID()]
}

// WrapID turns an account's own ID into an ID of the combined view.
func (u *Unified) WrapID(acc Account, id string) string {
	if id == "" {
		return ""
	}
	return strconv.Itoa(u.num(acc)) + idSep + id
}

// Resolve returns the account an ID of the combined view belongs to and the
// account's own ID.
func (u *Unified) Resolve(id string) (Account, string, bool) {
	n, rest, ok := strings.Cut(id, idSep)
	if !ok {
		return nil, id, false
	}
	k, err := strconv.Atoi(n)
	if err != nil {
		return nil, id, false
	}
	u.list()
	u.mu.Lock()
	acc := u.byNum[k]
	u.mu.Unlock()
	if acc == nil {
		return nil, rest, false
	}
	// Only accounts that are still open.
	for _, a := range u.accounts() {
		if a == acc {
			return acc, rest, true
		}
	}
	return nil, rest, false
}

// Owner returns the account an ID belongs to (nil if unknown).
func (u *Unified) Owner(id string) Account {
	acc, _, _ := u.Resolve(id)
	return acc
}

var errGone = errors.New(i18n.T("the account of this message is no longer open"))

func (u *Unified) resolve(id string) (Account, string, error) {
	acc, real, ok := u.Resolve(id)
	if !ok {
		return nil, "", errGone
	}
	return acc, real, nil
}

// group splits combined IDs by account.
func (u *Unified) group(ids []string) (map[Account][]string, []Account, error) {
	out := map[Account][]string{}
	var order []Account
	for _, id := range ids {
		acc, real, err := u.resolve(id)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := out[acc]; !ok {
			order = append(order, acc)
		}
		out[acc] = append(out[acc], real)
	}
	return out, order, nil
}

// WrapSummary turns an account's message into a message of the combined view.
func (u *Unified) WrapSummary(acc Account, s protonmail.Summary) protonmail.Summary {
	return u.wrapSummary(acc, s)
}

func (u *Unified) wrapSummary(acc Account, s protonmail.Summary) protonmail.Summary {
	s.ID = u.WrapID(acc, s.ID)
	s.ConversationID = u.WrapID(acc, s.ConversationID)
	return s
}

func (u *Unified) wrapAll(acc Account, list []protonmail.Summary) []protonmail.Summary {
	out := make([]protonmail.Summary, len(list))
	for i, s := range list {
		out[i] = u.wrapSummary(acc, s)
	}
	return out
}

func (u *Unified) wrapMessage(acc Account, msg *protonmail.Message) *protonmail.Message {
	if msg == nil {
		return nil
	}
	c := *msg
	c.Meta = u.wrapSummary(acc, msg.Meta)
	c.Attachments = make([]protonmail.Attachment, len(msg.Attachments))
	for i, a := range msg.Attachments {
		a.ID = u.WrapID(acc, a.ID)
		c.Attachments[i] = a
	}
	return &c
}

// UnwrapSummary returns the account of a message of the combined view and
// the message with the account's own IDs.
func (u *Unified) UnwrapSummary(s protonmail.Summary) (Account, protonmail.Summary, bool) {
	acc, id, ok := u.Resolve(s.ID)
	if !ok {
		return nil, s, false
	}
	s.ID = id
	if _, conv, ok := u.Resolve(s.ConversationID); ok {
		s.ConversationID = conv
	}
	return acc, s, true
}

// UnwrapMessage returns the account of a message of the combined view and
// the message with the account's own IDs.
func (u *Unified) UnwrapMessage(msg *protonmail.Message) (Account, *protonmail.Message, error) {
	acc, id, err := u.resolve(msg.Meta.ID)
	if err != nil {
		return nil, nil, err
	}
	c := *msg
	c.Meta.ID = id
	if _, conv, ok := u.Resolve(msg.Meta.ConversationID); ok {
		c.Meta.ConversationID = conv
	}
	c.Attachments = make([]protonmail.Attachment, len(msg.Attachments))
	for i, a := range msg.Attachments {
		if _, real, ok := u.Resolve(a.ID); ok {
			a.ID = real
		}
		c.Attachments[i] = a
	}
	return acc, &c, nil
}

// merge runs f for every account in parallel and returns the results
// newest first. Accounts that fail are skipped unless all fail.
func (u *Unified) merge(f func(Account) ([]protonmail.Summary, error)) ([]protonmail.Summary, error) {
	accs := u.list()
	type res struct {
		acc  Account
		list []protonmail.Summary
		err  error
	}
	ch := make(chan res, len(accs))
	for _, acc := range accs {
		go func(acc Account) {
			l, err := f(acc)
			ch <- res{acc, l, err}
		}(acc)
	}
	var out []protonmail.Summary
	var firstErr error
	ok := 0
	for range accs {
		r := <-ch
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		ok++
		out = append(out, u.wrapAll(r.acc, r.list)...)
	}
	if ok == 0 && firstErr != nil {
		return nil, firstErr
	}
	SortNewestFirst(out)
	return out, nil
}

// SortNewestFirst orders a merged list by time, newest first.
func SortNewestFirst(list []protonmail.Summary) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Time > list[j].Time })
}

// ---- Account ----------------------------------------------------------------

func (u *Unified) Kind() Kind { return KindUnified }

// Caps: only reading and organising; snoozing, labels and contacts belong
// to the individual accounts.
func (u *Unified) Caps() Caps { return Caps{} }

func (u *Unified) HasFolder(id string) bool {
	for _, f := range UnifiedFolders {
		if f == id {
			return true
		}
	}
	return false
}

func (u *Unified) Username() string    { return "unified" }
func (u *Unified) UserID() string      { return "unified" }
func (u *Unified) Email() string       { return i18n.T("All Accounts") }
func (u *Unified) DisplayName() string { return i18n.T("All Accounts") }

func (u *Unified) SendAddresses() []proton.Address {
	var out []proton.Address
	for _, a := range u.list() {
		out = append(out, a.SendAddresses()...)
	}
	return out
}

func (u *Unified) IsOwnAddress(addr string) bool {
	for _, a := range u.list() {
		if a.IsOwnAddress(addr) {
			return true
		}
	}
	return false
}

func (u *Unified) OnDeauth(func())                   {}
func (u *Unified) StartedOffline() bool              { return false }
func (u *Unified) Logout(context.Context) error      { return ErrUnsupported }
func (u *Unified) Close()                            {}
func (u *Unified) Cache() *cache.Cache               { return nil }
func (u *Unified) RefreshUser(context.Context) error { return nil }

func (u *Unified) List(ctx context.Context, folderID string, page, pageSize int) ([]protonmail.Summary, error) {
	return u.merge(func(a Account) ([]protonmail.Summary, error) {
		if !a.HasFolder(folderID) {
			return nil, nil
		}
		return a.List(ctx, folderID, page, pageSize)
	})
}

func (u *Unified) Search(ctx context.Context, folderID, query string, maxMessages int) ([]protonmail.Summary, error) {
	return u.merge(func(a Account) ([]protonmail.Summary, error) {
		if folderID != "" && !a.HasFolder(folderID) {
			return nil, nil
		}
		return a.Search(ctx, folderID, query, maxMessages)
	})
}

func (u *Unified) ThreadMessages(ctx context.Context, conversationID string, includeHidden bool) ([]protonmail.Summary, error) {
	acc, id, err := u.resolve(conversationID)
	if err != nil {
		return nil, err
	}
	list, err := acc.ThreadMessages(ctx, id, includeHidden)
	return u.wrapAll(acc, list), err
}

func (u *Unified) Get(ctx context.Context, id string) (*protonmail.Message, error) {
	acc, real, err := u.resolve(id)
	if err != nil {
		return nil, err
	}
	msg, err := acc.Get(ctx, real)
	if err != nil {
		return nil, err
	}
	return u.wrapMessage(acc, msg), nil
}

func (u *Unified) AttachmentData(ctx context.Context, att protonmail.Attachment) ([]byte, error) {
	acc, real, err := u.resolve(att.ID)
	if err != nil {
		return nil, err
	}
	att.ID = real
	return acc.AttachmentData(ctx, att)
}

func (u *Unified) InlineImages(ctx context.Context, msg *protonmail.Message) map[string]string {
	acc, real, err := u.UnwrapMessage(msg)
	if err != nil {
		return nil
	}
	return acc.InlineImages(ctx, real)
}

func (u *Unified) ExportEML(ctx context.Context, id string) ([]byte, error) {
	acc, real, err := u.resolve(id)
	if err != nil {
		return nil, err
	}
	return acc.ExportEML(ctx, real)
}

// Events: the real accounts deliver their own events.
func (u *Unified) Events(ctx context.Context, _ func(protonmail.Summary), _ func()) error {
	<-ctx.Done()
	return ctx.Err()
}

func (u *Unified) SyncOffline(context.Context, int, func(int, int)) error { return nil }

func (u *Unified) UserLabels(context.Context) ([]protonmail.UserLabel, error) { return nil, nil }

func (u *Unified) CreateFolder(context.Context, string, bool) error   { return ErrUnsupported }
func (u *Unified) RenameFolder(context.Context, string, string) error { return ErrUnsupported }
func (u *Unified) DeleteFolder(context.Context, string) error         { return ErrUnsupported }

// EmptyFolder empties the folder in every account.
func (u *Unified) EmptyFolder(ctx context.Context, folderID string, olderThan time.Time) (int, error) {
	total := 0
	var first error
	for _, a := range u.list() {
		n, err := a.EmptyFolder(ctx, folderID, olderThan)
		total += n
		if err != nil && first == nil {
			first = err
		}
	}
	return total, first
}

// each applies f to the IDs of every account they belong to.
func (u *Unified) each(ids []string, f func(Account, []string) error) error {
	groups, order, err := u.group(ids)
	if err != nil {
		return err
	}
	var first error
	for _, acc := range order {
		if err := f(acc, groups[acc]); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (u *Unified) MarkRead(ctx context.Context, ids ...string) error {
	return u.each(ids, func(a Account, ids []string) error { return a.MarkRead(ctx, ids...) })
}

func (u *Unified) MarkUnread(ctx context.Context, ids ...string) error {
	return u.each(ids, func(a Account, ids []string) error { return a.MarkUnread(ctx, ids...) })
}

func (u *Unified) Move(ctx context.Context, folderID string, ids ...string) error {
	return u.each(ids, func(a Account, ids []string) error { return a.Move(ctx, folderID, ids...) })
}

func (u *Unified) SetLabel(ctx context.Context, labelID string, on bool, ids ...string) error {
	return u.each(ids, func(a Account, ids []string) error { return a.SetLabel(ctx, labelID, on, ids...) })
}

func (u *Unified) UnreadCounts(ctx context.Context) (map[string]int, error) {
	total := map[string]int{}
	var first error
	ok := 0
	for _, a := range u.list() {
		c, err := a.UnreadCounts(ctx)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		ok++
		for k, v := range c {
			total[k] += v
		}
	}
	if ok == 0 && first != nil {
		return nil, first
	}
	return total, nil
}

func (u *Unified) Storage() protonmail.StorageInfo { return protonmail.StorageInfo{} }

// ---- Writing: done by the real accounts ----------------------------------------

// OwnerOfAddress returns the account that sends from an address ID.
func (u *Unified) OwnerOfAddress(addressID string) Account {
	for _, a := range u.list() {
		for _, ad := range a.SendAddresses() {
			if ad.ID == addressID {
				return a
			}
		}
	}
	return nil
}

func (u *Unified) SaveDraft(context.Context, *protonmail.Draft) error { return ErrUnsupported }
func (u *Unified) Send(context.Context, *protonmail.Draft) error      { return ErrUnsupported }

func (u *Unified) DeleteDraft(ctx context.Context, id string) error {
	acc, real, err := u.resolve(id)
	if err != nil {
		return err
	}
	return acc.DeleteDraft(ctx, real)
}

func (u *Unified) OpenDraft(context.Context, string) (*protonmail.Draft, *protonmail.Message, error) {
	return nil, nil, ErrUnsupported
}

func (u *Unified) ForwardAttachments(ctx context.Context, msg *protonmail.Message) ([]*protonmail.Outgoing, error) {
	acc, real, err := u.UnwrapMessage(msg)
	if err != nil {
		return nil, err
	}
	return acc.ForwardAttachments(ctx, real)
}

func (u *Unified) PlanEncryption(context.Context, []*mail.Address) []protonmail.RecipientMode {
	return nil
}

func (u *Unified) OwnPublicKey(addressID string) (string, error) {
	if a := u.OwnerOfAddress(addressID); a != nil {
		return a.OwnPublicKey(addressID)
	}
	return "", ErrUnsupported
}

func (u *Unified) PublicKeyAttachment(addressID string) (string, string, error) {
	if a := u.OwnerOfAddress(addressID); a != nil {
		return a.PublicKeyAttachment(addressID)
	}
	return "", "", ErrUnsupported
}

func (u *Unified) Contacts(ctx context.Context) ([]protonmail.Contact, error) {
	var out []protonmail.Contact
	for _, a := range u.list() {
		c, err := a.Contacts(ctx)
		if err == nil {
			out = append(out, c...)
		}
	}
	return out, nil
}

func (u *Unified) InvalidateContacts() {
	for _, a := range u.list() {
		a.InvalidateContacts()
	}
}

func (u *Unified) RecentAddresses(max int) []protonmail.Contact {
	var out []protonmail.Contact
	for _, a := range u.list() {
		out = append(out, a.RecentAddresses(max)...)
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func (u *Unified) AddContact(context.Context, string, string) error { return ErrUnsupported }
func (u *Unified) DeleteContact(context.Context, string) error      { return ErrUnsupported }

func (u *Unified) CancelScheduled(context.Context, string) error     { return ErrUnsupported }
func (u *Unified) Snooze(context.Context, []string, time.Time) error { return ErrUnsupported }
func (u *Unified) Unsnooze(context.Context, []string) error          { return ErrUnsupported }
func (u *Unified) SnoozedUntil(string) time.Time                     { return time.Time{} }
func (u *Unified) AutoReply(context.Context) (protonmail.AutoReply, error) {
	return protonmail.AutoReply{}, ErrUnsupported
}
func (u *Unified) SetAutoReply(context.Context, protonmail.AutoReply) error { return ErrUnsupported }
