package pgp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// Where a contact's key came from.
const (
	SourceImport    = "import"    // imported by the user
	SourceAutocrypt = "autocrypt" // the Autocrypt header of their mail
	SourceWKD       = "wkd"       // their domain's Web Key Directory
)

// KeyMeta is what Klient remembers about a contact's key.
type KeyMeta struct {
	Source   string    `json:"source"`
	Added    time.Time `json:"added"`
	Verified bool      `json:"verified,omitempty"` // fingerprint checked with the contact
	// The previous key, when it was replaced by a newer one automatically.
	ReplacedFP string    `json:"replaced_fp,omitempty"`
	ReplacedAt time.Time `json:"replaced_at,omitempty"`
	// A different key seen for the contact, waiting for the user's decision
	// (stored next to the key as <email>.pending.asc).
	PendingSource string    `json:"pending_source,omitempty"`
	PendingSeen   time.Time `json:"pending_seen,omitempty"`
	// Last time the Web Key Directory was asked for a newer key.
	Checked time.Time `json:"checked,omitempty"`
}

var metaMu sync.Mutex

func metaFile(email string) string {
	return filepath.Join(keyDir(), strings.ToLower(strings.TrimSpace(email))+".json")
}

func pendingFile(email string) string {
	return filepath.Join(keyDir(), strings.ToLower(strings.TrimSpace(email))+".pending.asc")
}

// Meta returns what is known about a contact's key (zero without a key).
func Meta(email string) KeyMeta {
	var m KeyMeta
	if b, err := os.ReadFile(metaFile(email)); err == nil {
		_ = json.Unmarshal(b, &m)
	} else if _, err := os.Stat(keyFile(email)); err == nil {
		m.Source = SourceImport // stored before Klient kept this
	}
	return m
}

func saveMeta(email string, m KeyMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(keyDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(metaFile(email), b, 0o600)
}

func updateMeta(email string, f func(m *KeyMeta)) error {
	metaMu.Lock()
	defer metaMu.Unlock()
	m := Meta(email)
	f(&m)
	return saveMeta(email, m)
}

// Learned tells what LearnKey did with a key.
type Learned int

const (
	KeySame     Learned = iota // the known key, or nothing usable
	KeyNew                     // first key for the contact, stored
	KeyReplaced                // a newer key replaced an unverified one
	KeyPending                 // a different key waits for the user
)

// LearnKey takes a key seen for email (Autocrypt, WKD). A first key is
// stored. A different key replaces the stored one only when that one was
// learned the same automatic way, is not verified and is older; otherwise
// it is kept aside until the user accepts or rejects it, so a forged
// message cannot swap a key the user relies on.
func LearnKey(email, armored, source string) Learned {
	email = strings.ToLower(strings.TrimSpace(email))
	key, err := publicKey(armored)
	if err != nil || key.IsExpired() || key.IsRevoked() || !hasEmail(key, email) {
		return KeySame
	}
	pubArm, err := key.GetArmoredPublicKey()
	if err != nil {
		return KeySame
	}
	if err := os.MkdirAll(keyDir(), 0o700); err != nil {
		return KeySame
	}
	old := storedKey(keyFile(email))
	if old == nil {
		if os.WriteFile(keyFile(email), []byte(pubArm), 0o600) != nil {
			return KeySame
		}
		_ = updateMeta(email, func(m *KeyMeta) {
			*m = KeyMeta{Source: source, Added: time.Now()}
		})
		return KeyNew
	}
	if old.GetFingerprint() == key.GetFingerprint() {
		return KeySame
	}
	m := Meta(email)
	automatic := m.Source == SourceAutocrypt || (m.Source == SourceWKD && source == SourceWKD)
	if !m.Verified && automatic && created(key).After(created(old)) {
		if os.WriteFile(keyFile(email), []byte(pubArm), 0o600) != nil {
			return KeySame
		}
		oldFP := strings.ToUpper(old.GetFingerprint())
		_ = updateMeta(email, func(m *KeyMeta) {
			*m = KeyMeta{Source: source, Added: time.Now(), ReplacedFP: oldFP, ReplacedAt: time.Now(), Checked: m.Checked}
		})
		_ = os.Remove(pendingFile(email))
		return KeyReplaced
	}
	if p := storedKey(pendingFile(email)); p != nil && p.GetFingerprint() == key.GetFingerprint() {
		return KeyPending
	}
	if os.WriteFile(pendingFile(email), []byte(pubArm), 0o600) != nil {
		return KeySame
	}
	_ = updateMeta(email, func(m *KeyMeta) {
		m.PendingSource, m.PendingSeen = source, time.Now()
	})
	return KeyPending
}

// PendingKey returns the key waiting for the user's decision, or nil.
func PendingKey(email string) *crypto.Key { return storedKey(pendingFile(email)) }

// AcceptPending makes the waiting key the contact's key.
func AcceptPending(email string) error {
	p := PendingKey(email)
	if p == nil {
		return errors.New(i18n.T("no new key is waiting"))
	}
	arm, err := p.GetArmoredPublicKey()
	if err != nil {
		return err
	}
	oldFP := ""
	if old := storedKey(keyFile(email)); old != nil {
		oldFP = strings.ToUpper(old.GetFingerprint())
	}
	if err := os.WriteFile(keyFile(email), []byte(arm), 0o600); err != nil {
		return err
	}
	_ = os.Remove(pendingFile(email))
	return updateMeta(email, func(m *KeyMeta) {
		*m = KeyMeta{Source: m.PendingSource, Added: time.Now(), ReplacedFP: oldFP, ReplacedAt: time.Now()}
	})
}

// RejectPending drops the waiting key.
func RejectPending(email string) error {
	_ = os.Remove(pendingFile(email))
	return updateMeta(email, func(m *KeyMeta) { m.PendingSource, m.PendingSeen = "", time.Time{} })
}

// SetVerified marks the contact's key as checked with them (or not).
func SetVerified(email string, on bool) error {
	return updateMeta(email, func(m *KeyMeta) { m.Verified = on })
}

// MarkChecked records a look into the Web Key Directory.
func MarkChecked(email string) { _ = updateMeta(email, func(m *KeyMeta) { m.Checked = time.Now() }) }

// StoredKey returns the contact's stored key (also expired or revoked), or nil.
func StoredKey(email string) *crypto.Key { return storedKey(keyFile(email)) }

func storedKey(path string) *crypto.Key {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	k, err := crypto.NewKeyFromArmored(string(b))
	if err != nil {
		return nil
	}
	return k
}

func publicKey(armored string) (*crypto.Key, error) {
	key, err := crypto.NewKeyFromArmored(armored)
	if err != nil {
		return nil, err
	}
	if key.IsPrivate() {
		return key.ToPublic()
	}
	return key, nil
}

func hasEmail(key *crypto.Key, email string) bool {
	for _, id := range key.GetEntity().Identities {
		if strings.EqualFold(id.UserId.Email, email) {
			return true
		}
	}
	return false
}

func created(key *crypto.Key) time.Time { return key.GetEntity().PrimaryKey.CreationTime }

// KeyDetails describes a key for the user.
type KeyDetails struct {
	Fingerprint string
	UserIDs     []string
	Created     time.Time
	Expires     time.Time // zero: never
	Expired     bool
	Revoked     bool
	Algorithm   string
}

// Details reads the facts about a key worth showing.
func Details(key *crypto.Key) KeyDetails {
	if key == nil {
		return KeyDetails{}
	}
	e := key.GetEntity()
	d := KeyDetails{
		Fingerprint: strings.ToUpper(key.GetFingerprint()),
		Created:     e.PrimaryKey.CreationTime,
		Expired:     key.IsExpired(),
		Revoked:     key.IsRevoked(),
	}
	for name := range e.Identities {
		d.UserIDs = append(d.UserIDs, name)
	}
	if id := e.PrimaryIdentity(); id != nil && id.SelfSignature != nil && id.SelfSignature.KeyLifetimeSecs != nil && *id.SelfSignature.KeyLifetimeSecs > 0 {
		d.Expires = d.Created.Add(time.Duration(*id.SelfSignature.KeyLifetimeSecs) * time.Second)
	}
	switch {
	case e.PrimaryKey.PubKeyAlgo == 22 || e.PrimaryKey.PubKeyAlgo == 27:
		d.Algorithm = "Ed25519"
	case e.PrimaryKey.PubKeyAlgo == 28:
		d.Algorithm = "Ed448"
	case e.PrimaryKey.PubKeyAlgo <= 3:
		if bits, err := e.PrimaryKey.BitLength(); err == nil {
			d.Algorithm = "RSA " + strconv.Itoa(int(bits))
		} else {
			d.Algorithm = "RSA"
		}
	case e.PrimaryKey.PubKeyAlgo == 19:
		d.Algorithm = "ECDSA"
	default:
		d.Algorithm = "OpenPGP"
	}
	return d
}

// GroupFingerprint writes a fingerprint in groups of four for reading aloud.
func GroupFingerprint(fp string) string {
	var parts []string
	for len(fp) > 4 {
		parts = append(parts, fp[:4])
		fp = fp[4:]
	}
	if fp != "" {
		parts = append(parts, fp)
	}
	return strings.Join(parts, " ")
}
