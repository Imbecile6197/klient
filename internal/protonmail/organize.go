package protonmail

import (
	"context"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/emersion/go-vcard"
	"github.com/google/uuid"
)

// UserLabel is a custom folder or label created by the user in Proton.
type UserLabel struct {
	ID     string
	Name   string // full path, e.g. "Práce/Projekty"
	Color  string
	Folder bool // true = folder (exclusive), false = label (tag)
}

// UserLabels returns the user's folders and labels sorted by name.
func (a *Account) UserLabels(ctx context.Context) ([]UserLabel, error) {
	labels, err := a.client.GetLabels(ctx, proton.LabelTypeFolder, proton.LabelTypeLabel)
	if err != nil {
		if IsOffline(err) && a.cache != nil && a.cache.Get("labels", &labels) {
			err = nil
		} else {
			return nil, err
		}
	} else if a.cache != nil {
		_ = a.cache.Set("labels", labels)
	}
	var out []UserLabel
	for _, l := range labels {
		name := l.Name
		if len(l.Path) > 0 {
			name = strings.Join(l.Path, "/")
		}
		out = append(out, UserLabel{ID: l.ID, Name: name, Color: l.Color, Folder: l.Type == proton.LabelTypeFolder})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// SetLabel adds or removes a label (or star) on messages.
func (a *Account) SetLabel(ctx context.Context, labelID string, on bool, ids ...string) error {
	if on {
		return a.client.LabelMessages(ctx, ids, labelID)
	}
	return a.client.UnlabelMessages(ctx, ids, labelID)
}

func (a *Account) MarkUnread(ctx context.Context, ids ...string) error {
	return a.client.MarkMessagesUnread(ctx, ids...)
}

// Contact is one address from the Proton address book (or, with Recent set,
// an address the user corresponded with).
type Contact struct {
	Name      string
	Email     string
	ContactID string
	Recent    bool
}

var contactsMu sync.Mutex

// InvalidateContacts forces the next Contacts call to fetch from the server.
func (a *Account) InvalidateContacts() {
	contactsMu.Lock()
	a.contacts = nil
	contactsMu.Unlock()
}

// Contacts returns all contact e-mail addresses (cached after the first
// successful non-empty fetch).
func (a *Account) Contacts(ctx context.Context) ([]Contact, error) {
	contactsMu.Lock()
	defer contactsMu.Unlock()
	if len(a.contacts) > 0 {
		return a.contacts, nil
	}
	emails, err := a.client.GetAllContactEmails(ctx, "")
	if err != nil {
		if IsOffline(err) && a.cache != nil && a.cache.Get("contacts", &emails) {
			err = nil
		} else {
			return nil, err
		}
	} else if a.cache != nil {
		_ = a.cache.Set("contacts", emails)
	}
	out := make([]Contact, 0, len(emails))
	for _, e := range emails {
		out = append(out, Contact{Name: e.Name, Email: e.Email, ContactID: e.ContactID})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(orEmail(out[i])) < strings.ToLower(orEmail(out[j]))
	})
	a.contacts = out
	return out, nil
}

func orEmail(c Contact) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Email
}

// RecentAddresses returns people from the newest cached messages (senders of
// received mail, recipients of sent mail), newest first, for suggestions.
func (a *Account) RecentAddresses(max int) []Contact {
	if a.cache == nil {
		return nil
	}
	msgs, err := a.cache.List(AllMailID, 0, 500)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Contact
	add := func(addr *mail.Address) {
		if addr == nil || addr.Address == "" || a.IsOwnAddress(addr.Address) {
			return
		}
		k := strings.ToLower(addr.Address)
		if seen[k] || strings.Contains(k, "noreply") || strings.Contains(k, "no-reply") {
			return
		}
		seen[k] = true
		out = append(out, Contact{Name: addr.Name, Email: addr.Address, Recent: true})
	}
	for _, m := range msgs {
		if m.Flags.Has(proton.MessageFlagSent) {
			for _, r := range append(append([]*mail.Address{}, m.ToList...), m.CCList...) {
				add(r)
			}
		} else {
			add(m.Sender)
		}
		if len(out) >= max {
			break
		}
	}
	return out
}

// AddContact creates a contact with one address in the Proton address book
// (a signed vCard, as the Proton apps store it).
func (a *Account) AddContact(ctx context.Context, name, email string) error {
	a.mu.RLock()
	kr := a.userKR
	a.mu.RUnlock()
	card, err := proton.NewCard(kr, proton.CardTypeSigned)
	if err != nil {
		return err
	}
	if name == "" {
		name = email
	}
	if err := card.Set(kr, vcard.FieldUID, &vcard.Field{Value: "proton-klient-" + uuid.NewString()}); err != nil {
		return err
	}
	if err := card.Set(kr, vcard.FieldFormattedName, &vcard.Field{Value: name}); err != nil {
		return err
	}
	if err := card.Set(kr, vcard.FieldEmail, &vcard.Field{Value: email, Group: "item1"}); err != nil {
		return err
	}
	res, err := a.client.CreateContacts(ctx, proton.CreateContactsReq{
		Contacts: []proton.ContactCards{{Cards: proton.Cards{card}}},
	})
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Response.Code != 0 && r.Response.Code != 1000 {
			return fmt.Errorf("kontakt nebyl uložen: %s", r.Response.Message)
		}
	}
	a.InvalidateContacts()
	return nil
}

// DeleteContact removes a contact from the address book.
func (a *Account) DeleteContact(ctx context.Context, contactID string) error {
	err := a.client.DeleteContacts(ctx, proton.DeleteContactsReq{IDs: []string{contactID}})
	if err == nil {
		a.InvalidateContacts()
	}
	return err
}

// Search looks through message metadata (subject, sender, recipients) of a
// folder. Bodies are end-to-end encrypted on the server, so full-text search
// would need a local index; this covers the common "who/what" searches.
// It scans at most maxMessages newest messages.
func (a *Account) Search(ctx context.Context, labelID, query string, maxMessages int) ([]Summary, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, nil
	}
	const page = 150
	var out []Summary
	for p := 0; p*page < maxMessages; p++ {
		msgs, err := a.client.GetMessageMetadataPage(ctx, p, page, proton.MessageFilter{LabelID: labelID, Desc: true})
		if err != nil {
			if IsOffline(err) && a.cache != nil {
				return a.cache.Search(labelID, func(m Summary) bool { return matches(m, words) }, maxMessages)
			}
			return out, err
		}
		for _, m := range msgs {
			if matches(m, words) {
				out = append(out, m)
			}
		}
		if len(msgs) < page || ctx.Err() != nil {
			break
		}
	}
	return out, nil
}

func matches(m Summary, words []string) bool {
	var sb strings.Builder
	sb.WriteString(strings.ToLower(m.Subject))
	if m.Sender != nil {
		sb.WriteString(" " + strings.ToLower(m.Sender.Name+" "+m.Sender.Address))
	}
	for _, list := range [][]*mail.Address{m.ToList, m.CCList} {
		for _, r := range list {
			sb.WriteString(" " + strings.ToLower(r.Name+" "+r.Address))
		}
	}
	hay := sb.String()
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// UnreadCounts returns unread messages per folder/label ID.
func (a *Account) UnreadCounts(ctx context.Context) (map[string]int, error) {
	counts, err := a.client.GetGroupedMessageCount(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, c := range counts {
		out[c.LabelID] = c.Unread
	}
	return out, nil
}

// StorageInfo is the space shown for mail, like on Proton's web: on plans
// with split storage the mail (base) quota, otherwise the shared quota.
type StorageInfo struct {
	Used, Max   uint64
	Mail, Drive uint64 // used by mail and Drive
	Split       bool   // Drive has its own quota
	DriveMax    uint64
	Updated     time.Time
}

func (a *Account) Storage() StorageInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	u := a.user
	s := StorageInfo{Mail: u.ProductUsedSpace.Mail, Drive: u.ProductUsedSpace.Drive, Updated: a.userAt}
	if u.MaxBaseSpace != nil {
		s.Split = true
		s.Max = *u.MaxBaseSpace
		if u.UsedBaseSpace != nil {
			s.Used = *u.UsedBaseSpace
		}
		if u.MaxDriveSpace != nil {
			s.DriveMax = *u.MaxDriveSpace
		}
		return s
	}
	s.Used, s.Max = u.UsedSpace, u.MaxSpace
	return s
}

// RefreshUser reloads the account (storage, display name) from the server.
func (a *Account) RefreshUser(ctx context.Context) error {
	u, err := a.client.GetUser(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.user, a.userAt = u, time.Now()
	a.mu.Unlock()
	if a.cache != nil {
		_ = a.cache.Set("user", u)
	}
	return nil
}

// DisplayName is the user's display name, or the e-mail address.
func (a *Account) DisplayName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.user.DisplayName != "" {
		return a.user.DisplayName
	}
	return a.user.Email
}
