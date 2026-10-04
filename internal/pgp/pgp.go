// Package pgp holds the local store of contacts' public keys (for recipients
// outside Proton that do not publish keys via WKD) and signature helpers.
package pgp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/constants"
	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/ProtonMail/gopenpgp/v2/helper"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
)

func keyDir() string { return filepath.Join(config.DataDir(), "keys") }

func keyFile(email string) string {
	return filepath.Join(keyDir(), strings.ToLower(strings.TrimSpace(email))+".asc")
}

// ImportKey stores an armored public key for every e-mail address in its
// user IDs and returns those addresses.
func ImportKey(armored string) ([]string, error) {
	key, err := crypto.NewKeyFromArmored(armored)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("invalid PGP key: %w"), err)
	}
	if key.IsPrivate() {
		pub, err := key.ToPublic()
		if err != nil {
			return nil, err
		}
		key = pub
	}
	if key.IsExpired() || key.IsRevoked() {
		return nil, errors.New(i18n.T("the key has expired or been revoked"))
	}
	pubArm, err := key.GetArmoredPublicKey()
	if err != nil {
		return nil, err
	}
	var emails []string
	for _, id := range key.GetEntity().Identities {
		if e := strings.ToLower(id.UserId.Email); e != "" {
			emails = append(emails, e)
		}
	}
	if len(emails) == 0 {
		return nil, errors.New(i18n.T("the key contains no email address"))
	}
	if err := os.MkdirAll(keyDir(), 0o700); err != nil {
		return nil, err
	}
	for _, e := range emails {
		if err := os.WriteFile(keyFile(e), []byte(pubArm), 0o600); err != nil {
			return nil, err
		}
		_ = os.Remove(pendingFile(e))
		_ = updateMeta(e, func(m *KeyMeta) { *m = KeyMeta{Source: SourceImport, Added: time.Now()} })
	}
	return emails, nil
}

// LocalKey returns the stored public keyring for email, or nil.
func LocalKey(email string) *crypto.KeyRing {
	b, err := os.ReadFile(keyFile(email))
	if err != nil {
		return nil
	}
	key, err := crypto.NewKeyFromArmored(string(b))
	if err != nil || key.IsExpired() || key.IsRevoked() {
		return nil
	}
	kr, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil
	}
	return kr
}

// LocalKeys lists e-mail addresses with a stored key.
func LocalKeys() []string {
	entries, _ := os.ReadDir(keyDir())
	var out []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".asc"); ok && !strings.HasSuffix(name, ".pending") {
			out = append(out, name)
		}
	}
	return out
}

func DeleteKey(email string) error {
	_ = os.Remove(pendingFile(email))
	_ = os.Remove(metaFile(email))
	return os.Remove(keyFile(email))
}

// Fingerprint returns the primary key fingerprint of a keyring for display.
func Fingerprint(kr *crypto.KeyRing) string {
	if kr == nil {
		return ""
	}
	k, err := kr.GetKey(0)
	if err != nil {
		return ""
	}
	return strings.ToUpper(k.GetFingerprint())
}

// SignatureStatus describes the result of a signature check for the UI.
type SignatureStatus int

const (
	SigUnknown SignatureStatus = iota // no key to verify against
	SigNone                           // message is not signed
	SigValid                          // signature verified
	SigInvalid                        // signature present but does not verify
	SigOwn                            // the user's own message: nothing to verify
)

func (s SignatureStatus) String() string {
	switch s {
	case SigValid:
		return i18n.T("Signature verified")
	case SigInvalid:
		return i18n.T("Signature INVALID")
	case SigNone:
		return i18n.T("Not signed")
	case SigOwn:
		return i18n.T("Sent from your account")
	default:
		return i18n.T("The signature cannot be verified (the sender's key is missing)")
	}
}

// StatusFromError maps a gopenpgp verification error to a SignatureStatus.
func StatusFromError(err error) SignatureStatus {
	if err == nil {
		return SigValid
	}
	var sigErr crypto.SignatureVerificationError
	if errors.As(err, &sigErr) {
		switch sigErr.Status {
		case constants.SIGNATURE_NOT_SIGNED:
			return SigNone
		case constants.SIGNATURE_NO_VERIFIER:
			return SigUnknown
		}
	}
	var sigErrPtr *crypto.SignatureVerificationError
	if errors.As(err, &sigErrPtr) {
		switch sigErrPtr.Status {
		case constants.SIGNATURE_NOT_SIGNED:
			return SigNone
		case constants.SIGNATURE_NO_VERIFIER:
			return SigUnknown
		}
	}
	return SigInvalid
}

const clearSignedHeader = "-----BEGIN PGP SIGNED MESSAGE-----"

// HasClearSigned reports whether text contains an inline cleartext signature.
func HasClearSigned(text string) bool { return strings.Contains(text, clearSignedHeader) }

// VerifyClearSigned verifies an inline cleartext-signed block and returns the
// signed text without armor.
func VerifyClearSigned(text string, kr *crypto.KeyRing) (string, SignatureStatus) {
	start := strings.Index(text, clearSignedHeader)
	end := strings.Index(text, "-----END PGP SIGNATURE-----")
	if start < 0 || end < 0 {
		return text, SigNone
	}
	block := text[start : end+len("-----END PGP SIGNATURE-----")]
	msg, err := crypto.NewClearTextMessageFromArmored(block)
	if err != nil {
		return text, SigInvalid
	}
	if kr == nil {
		return msg.GetString(), SigUnknown
	}
	plain, err := helper.VerifyCleartextMessage(kr, block, crypto.GetUnixTime())
	if err != nil {
		return msg.GetString(), StatusFromError(err)
	}
	return plain, SigValid
}

const (
	encryptedHeader = "-----BEGIN PGP MESSAGE-----"
	encryptedFooter = "-----END PGP MESSAGE-----"
)

// HasInlineEncrypted reports whether text contains an inline PGP message
// (Mailvelope, Enigmail in inline mode, copy-paste from GnuPG).
func HasInlineEncrypted(text string) bool { return strings.Contains(text, encryptedHeader) }

// DecryptInline replaces every inline PGP message in text that own can open
// by its plain text. verify (may be nil) checks the signatures; the result
// is the weakest status of the blocks. ok is false when nothing could be
// decrypted.
func DecryptInline(text string, own, verify *crypto.KeyRing) (out string, status SignatureStatus, ok bool) {
	if own == nil {
		return text, SigNone, false
	}
	status = SigValid
	var sb strings.Builder
	rest := text
	for {
		start := strings.Index(rest, encryptedHeader)
		if start < 0 {
			break
		}
		end := strings.Index(rest[start:], encryptedFooter)
		if end < 0 {
			break
		}
		end += start + len(encryptedFooter)
		block := normalizeArmor(rest[start:end])
		msg, err := crypto.NewPGPMessageFromArmored(block)
		if err != nil {
			sb.WriteString(rest[:end])
			rest = rest[end:]
			continue
		}
		plain, verr := own.Decrypt(msg, verify, crypto.GetUnixTime())
		if plain == nil && verr != nil {
			// The signature check may have failed: decrypt without it. A
			// block not meant for our key stays as it is.
			plain, _ = own.Decrypt(msg, nil, 0)
			if plain == nil {
				sb.WriteString(rest[:end])
				rest = rest[end:]
				continue
			}
		}
		ok = true
		st := SigValid
		switch {
		case verify == nil:
			st = SigUnknown
		case verr != nil:
			st = StatusFromError(verr)
		}
		status = weaker(status, st)
		sb.WriteString(rest[:start])
		sb.WriteString(plain.GetString())
		rest = rest[end:]
	}
	sb.WriteString(rest)
	if !ok {
		return text, SigNone, false
	}
	return sb.String(), status, true
}

// normalizeArmor undoes what mail bodies do to armor: quoted lines ("> "),
// non-breaking spaces and CRLF line ends.
func normalizeArmor(block string) string {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	block = strings.ReplaceAll(block, "\u00a0", " ")
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(strings.TrimLeft(l, "> "))
	}
	return strings.Join(lines, "\n")
}

// weaker returns the less trustworthy of two signature results.
func weaker(a, b SignatureStatus) SignatureStatus {
	rank := map[SignatureStatus]int{SigInvalid: 0, SigNone: 1, SigUnknown: 2, SigValid: 3, SigOwn: 4}
	if rank[b] < rank[a] {
		return b
	}
	return a
}
