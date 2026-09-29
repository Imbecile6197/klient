package imapmail

import (
	"context"
	"net/mail"
	"strings"
	"sync/atomic"

	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/pgpmime"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

var keyServer atomic.Bool

// lookup finds keys on the web (replaced in tests).
var lookup = pgpmime.Lookup

// SetKeyServer allows looking up recipients' keys on keys.openpgp.org (it
// learns whom you write to, so it is optional).
func SetKeyServer(on bool) { keyServer.Store(on) }

// keyFor finds a public key: locally stored (imported, Autocrypt), the
// recipient's Web Key Directory, or keys.openpgp.org if allowed.
func (a *Account) keyFor(ctx context.Context, email string) *crypto.KeyRing {
	if kr := pgp.LocalKey(email); kr != nil {
		return kr
	}
	kr, _ := lookup(ctx, email, keyServer.Load())
	return kr
}

func (a *Account) ownKey() *crypto.KeyRing {
	kr, err := pgp.OwnKeyRing(a.set.Email)
	if err != nil {
		return nil
	}
	return kr
}

// publicOf returns a public-only copy of a key ring (for encrypting to self).
func publicOf(kr *crypto.KeyRing) *crypto.Key {
	k, err := kr.GetKey(0)
	if err != nil {
		return nil
	}
	p, err := k.ToPublic()
	if err != nil {
		return nil
	}
	return p
}

// forSelf encrypts a message to the sender's own key only (drafts and
// scheduled mail stay unreadable for the provider). Without a key the
// message is returned unchanged.
func (a *Account) forSelf(d *protonmail.Draft, parent parentRef) ([]byte, error) {
	own := a.ownKey()
	if own == nil {
		return a.build(d, parent)
	}
	h, err := a.headers(d, parent)
	if err != nil {
		return nil, err
	}
	entity, err := content(d)
	if err != nil {
		return nil, err
	}
	self, err := crypto.NewKeyRing(publicOf(own))
	if err != nil {
		return nil, err
	}
	ct, body, err := pgpmime.Encrypt(entity, self, own)
	if err != nil {
		return nil, err
	}
	return assemble(h, ct, body)
}

// outgoing builds what recipients receive: an encrypted message for those
// with a key (and the sender's own copy), a plain – signed if asked – one
// for the rest. Both carry the same headers and Message-ID.
type outgoing struct {
	plain, encrypted []byte
	keyed, clear     []string
	sentCopy         []byte
}

func (a *Account) prepare(ctx context.Context, d *protonmail.Draft, parent parentRef) (*outgoing, error) {
	h, err := a.headers(d, parent)
	if err != nil {
		return nil, err
	}
	entity, err := content(d)
	if err != nil {
		return nil, err
	}
	own := a.ownKey()
	out := &outgoing{}
	recipients, _ := crypto.NewKeyRing(nil)
	for _, l := range [][]*mail.Address{d.To, d.CC, d.BCC} {
		for _, r := range l {
			if kr := a.keyFor(ctx, r.Address); kr != nil {
				for _, k := range kr.GetKeys() {
					_ = recipients.AddKey(k)
				}
				out.keyed = append(out.keyed, r.Address)
			} else {
				out.clear = append(out.clear, r.Address)
			}
		}
	}
	if len(out.clear) > 0 || len(out.keyed) == 0 {
		if own != nil && d.SignExternal {
			ct, body, err := pgpmime.Sign(entity, own)
			if err != nil {
				return nil, err
			}
			out.plain, err = assemble(h, ct, body)
			if err != nil {
				return nil, err
			}
		} else {
			// assemble mutates h (Content-Type) only when given one.
			if out.plain, err = assemble(h.Copy(), "", entity); err != nil {
				return nil, err
			}
		}
	}
	if len(out.keyed) > 0 {
		if own != nil {
			_ = recipients.AddKey(publicOf(own))
		}
		ct, body, err := pgpmime.Encrypt(entity, recipients, own)
		if err != nil {
			return nil, err
		}
		hc := h.Copy()
		if out.encrypted, err = assemble(hc, ct, body); err != nil {
			return nil, err
		}
	}
	out.sentCopy = out.plain
	if out.encrypted != nil && (own != nil || out.plain == nil) {
		out.sentCopy = out.encrypted
	}
	return out, nil
}

// PlanEncryption shows who gets the message encrypted.
func (a *Account) PlanEncryption(ctx context.Context, addrs []*mail.Address) []protonmail.RecipientMode {
	out := make([]protonmail.RecipientMode, 0, len(addrs))
	for _, ad := range addrs {
		m := protonmail.RecipientMode{Address: ad.Address, Scheme: "clear"}
		if kr := a.keyFor(ctx, ad.Address); kr != nil {
			m.Scheme, m.KeyFP = "pgp", pgp.Fingerprint(kr)
		}
		out = append(out, m)
	}
	return out
}

func (a *Account) OwnPublicKey(string) (string, error) {
	pub, _, err := pgp.OwnPublicKey(a.set.Email)
	return pub, err
}

func (a *Account) PublicKeyAttachment(string) (string, string, error) {
	pub, k, err := pgp.OwnPublicKey(a.set.Email)
	if err != nil {
		return "", "", err
	}
	fp := strings.ToUpper(k.GetFingerprint())
	if len(fp) > 8 {
		fp = fp[:8]
	}
	return pub, "publickey - " + a.set.Email + " - 0x" + fp + ".asc", nil
}

// open decrypts or verifies a PGP/MIME message. It returns the entity to
// display, a description of the protection and the signature status.
func (a *Account) open(ctx context.Context, raw []byte, sender string) ([]byte, string, pgp.SignatureStatus) {
	switch pgpmime.Detect(raw) {
	case pgpmime.Encrypted:
		var senderKR *crypto.KeyRing
		if sender != "" && !a.IsOwnAddress(sender) {
			senderKR = a.keyFor(ctx, sender)
		} else if own := a.ownKey(); own != nil {
			if p := publicOf(own); p != nil {
				senderKR, _ = crypto.NewKeyRing(p)
			}
		}
		entity, verr, err := pgpmime.Decrypt(raw, a.ownKey(), senderKR)
		if err != nil {
			return nil, err.Error(), pgp.SigUnknown
		}
		return entity, i18n.T("End-to-end encrypted (PGP)"), sigStatus(senderKR, verr)
	case pgpmime.Signed:
		var senderKR *crypto.KeyRing
		if sender != "" {
			if a.IsOwnAddress(sender) {
				if own := a.ownKey(); own != nil {
					senderKR, _ = crypto.NewKeyRing(publicOf(own))
				}
			} else {
				senderKR = a.keyFor(ctx, sender)
			}
		}
		entity, verr := pgpmime.Verify(raw, senderKR)
		return entity, "", sigStatus(senderKR, verr)
	}
	return nil, "", pgp.SigNone
}

func sigStatus(kr *crypto.KeyRing, verr error) pgp.SignatureStatus {
	switch {
	case kr == nil:
		return pgp.SigUnknown
	case verr == nil:
		return pgp.SigValid
	}
	return pgp.SigInvalid
}
