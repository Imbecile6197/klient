// Package mailbox is the interface between the user interface and a mail
// service. Proton (internal/protonmail) is one implementation; IMAP/SMTP
// services (Seznam, Gmail, others) are another.
//
// The message types are still those of the Proton implementation (a message
// summary is proton.MessageMetadata); other services fill them in.
package mailbox

import (
	"context"
	"errors"
	"net/mail"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/libormacak/klient/internal/cache"
	"github.com/libormacak/klient/internal/protonmail"
)

// CanSnooze and CanSchedule: offered by the server or by Klient itself.
func (c Caps) CanSnooze() bool   { return c.ServerSnooze || c.LocalSnooze }
func (c Caps) CanSchedule() bool { return c.ServerSchedule || c.LocalSchedule }

// ErrUnsupported is returned for features a service does not have.
var ErrUnsupported = errors.New("tato služba to nepodporuje")

// Kind identifies the service (for logos and service specific texts).
type Kind string

const (
	KindProton Kind = "proton"
	KindGmail  Kind = "gmail"
	KindSeznam Kind = "seznam"
	KindIMAP   Kind = "imap"
)

// Caps says which optional features a service offers; the UI hides the rest.
type Caps struct {
	Labels         bool // several labels per message (Proton, Gmail)
	ServerSnooze   bool // the server returns snoozed mail by itself
	ServerSchedule bool // the server sends scheduled mail by itself
	LocalSnooze    bool // Klient returns snoozed mail (while it runs)
	LocalSchedule  bool // Klient sends scheduled mail (while it runs)
	AutoReply      bool
	E2E            bool // end-to-end encryption and key handling
	Contacts       bool // server address book
	FullTextSearch bool // search in message bodies on the server
}

// Account is one logged-in mail account.
type Account interface {
	Kind() Kind
	Caps() Caps
	// HasFolder reports whether a system folder (protonmail.InboxID, …)
	// exists on this account.
	HasFolder(id string) bool

	// Identity
	Username() string
	UserID() string // stable ID of the account (same account logged in twice)
	Email() string
	DisplayName() string
	SendAddresses() []proton.Address
	IsOwnAddress(addr string) bool

	// Lifecycle
	OnDeauth(f func())
	StartedOffline() bool
	Logout(ctx context.Context) error
	Close()
	Cache() *cache.Cache // may be nil

	// Reading
	List(ctx context.Context, folderID string, page, pageSize int) ([]protonmail.Summary, error)
	ThreadMessages(ctx context.Context, conversationID string, includeHidden bool) ([]protonmail.Summary, error)
	Get(ctx context.Context, id string) (*protonmail.Message, error)
	AttachmentData(ctx context.Context, att protonmail.Attachment) ([]byte, error)
	InlineImages(ctx context.Context, msg *protonmail.Message) map[string]string
	Search(ctx context.Context, folderID, query string, maxMessages int) ([]protonmail.Summary, error)
	ExportEML(ctx context.Context, id string) ([]byte, error)
	Events(ctx context.Context, onNew func(protonmail.Summary), onChange func()) error
	SyncOffline(ctx context.Context, n int, progress func(done, total int)) error

	// Organising
	UserLabels(ctx context.Context) ([]protonmail.UserLabel, error)
	MarkRead(ctx context.Context, ids ...string) error
	MarkUnread(ctx context.Context, ids ...string) error
	Move(ctx context.Context, folderID string, ids ...string) error
	SetLabel(ctx context.Context, labelID string, on bool, ids ...string) error
	UnreadCounts(ctx context.Context) (map[string]int, error)
	Storage() protonmail.StorageInfo
	RefreshUser(ctx context.Context) error

	// Writing
	SaveDraft(ctx context.Context, d *protonmail.Draft) error
	Send(ctx context.Context, d *protonmail.Draft) error
	DeleteDraft(ctx context.Context, id string) error
	OpenDraft(ctx context.Context, id string) (*protonmail.Draft, *protonmail.Message, error)
	ForwardAttachments(ctx context.Context, msg *protonmail.Message) ([]*protonmail.Outgoing, error)
	PlanEncryption(ctx context.Context, addrs []*mail.Address) []protonmail.RecipientMode
	OwnPublicKey(addressID string) (string, error)
	PublicKeyAttachment(addressID string) (string, string, error)

	// Contacts
	Contacts(ctx context.Context) ([]protonmail.Contact, error)
	InvalidateContacts()
	RecentAddresses(max int) []protonmail.Contact
	AddContact(ctx context.Context, name, email string) error
	DeleteContact(ctx context.Context, contactID string) error

	// Optional features (see Caps)
	CancelScheduled(ctx context.Context, id string) error
	Snooze(ctx context.Context, conversationIDs []string, t time.Time) error
	Unsnooze(ctx context.Context, conversationIDs []string) error
	SnoozedUntil(conversationID string) time.Time
	AutoReply(ctx context.Context) (protonmail.AutoReply, error)
	SetAutoReply(ctx context.Context, r protonmail.AutoReply) error
}

// The Proton implementation.
var _ Account = (*protonAccount)(nil)

type protonAccount struct{ *protonmail.Account }

func (protonAccount) Kind() Kind { return KindProton }

func (protonAccount) HasFolder(string) bool { return true }

func (protonAccount) Caps() Caps {
	return Caps{Labels: true, ServerSnooze: true, ServerSchedule: true, AutoReply: true, E2E: true, Contacts: true}
}

// Proton wraps a Proton account.
func Proton(a *protonmail.Account) Account { return protonAccount{a} }

// AsProton returns the Proton account behind acc (nil for other services).
func AsProton(acc Account) *protonmail.Account {
	if p, ok := acc.(protonAccount); ok {
		return p.Account
	}
	return nil
}
