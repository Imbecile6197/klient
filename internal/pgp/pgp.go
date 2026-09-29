// Package pgp holds the local store of contacts' public keys (for recipients
// outside Proton that do not publish keys via WKD) and signature helpers.
package pgp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/gopenpgp/v2/constants"
	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/ProtonMail/gopenpgp/v2/helper"

	"github.com/Imbecile6197/klient/internal/config"
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
		return nil, fmt.Errorf("neplatný PGP klíč: %w", err)
	}
	if key.IsPrivate() {
		pub, err := key.ToPublic()
		if err != nil {
			return nil, err
		}
		key = pub
	}
	if key.IsExpired() || key.IsRevoked() {
		return nil, errors.New("klíč je expirovaný nebo revokovaný")
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
		return nil, errors.New("klíč neobsahuje žádnou e-mailovou adresu")
	}
	if err := os.MkdirAll(keyDir(), 0o700); err != nil {
		return nil, err
	}
	for _, e := range emails {
		if err := os.WriteFile(keyFile(e), []byte(pubArm), 0o600); err != nil {
			return nil, err
		}
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
		if name, ok := strings.CutSuffix(e.Name(), ".asc"); ok {
			out = append(out, name)
		}
	}
	return out
}

func DeleteKey(email string) error { return os.Remove(keyFile(email)) }

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
		return "Podpis ověřen"
	case SigInvalid:
		return "Podpis NEPLATNÝ"
	case SigNone:
		return "Nepodepsáno"
	case SigOwn:
		return "Odesláno z vašeho účtu"
	default:
		return "Podpis nelze ověřit (chybí klíč odesílatele)"
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
