package pgp

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/zalando/go-keyring"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// Own keys of non-Proton accounts. The private key is stored on disk locked
// with a random passphrase that lives only in the system keyring.

func ownDir() string { return filepath.Join(keyDir(), "own") }

func ownFile(email string) string {
	return filepath.Join(ownDir(), strings.ToLower(strings.TrimSpace(email))+".asc")
}

func passName(email string) string { return "pgp-own:" + strings.ToLower(strings.TrimSpace(email)) }

// HasOwnKey reports whether a private key is set up for the address.
func HasOwnKey(email string) bool {
	_, err := os.Stat(ownFile(email))
	return err == nil
}

func storeOwn(email string, key *crypto.Key) error {
	pass := make([]byte, 32)
	if _, err := rand.Read(pass); err != nil {
		return err
	}
	passStr := base64.StdEncoding.EncodeToString(pass)
	locked, err := key.Lock([]byte(passStr))
	if err != nil {
		return err
	}
	armored, err := locked.Armor()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ownDir(), 0o700); err != nil {
		return err
	}
	if err := keyring.Set("klient", passName(email), passStr); err != nil {
		return fmt.Errorf(i18n.T("cannot save the key passphrase to the keyring: %w"), err)
	}
	return os.WriteFile(ownFile(email), []byte(armored), 0o600)
}

// GenerateOwnKey creates a new key (Curve25519) for the address.
func GenerateOwnKey(name, email string) error {
	key, err := crypto.GenerateKey(name, email, "x25519", 0)
	if err != nil {
		return err
	}
	return storeOwn(email, key)
}

// ImportOwnKey imports an armored private key (unlocked with passphrase if
// it is protected). The key must contain the address.
func ImportOwnKey(armored, passphrase, email string) error {
	key, err := crypto.NewKeyFromArmored(armored)
	if err != nil {
		return fmt.Errorf(i18n.T("invalid key: %w"), err)
	}
	if !key.IsPrivate() {
		return errors.New(i18n.T("this is a public key – signing and decryption need the private key"))
	}
	if locked, _ := key.IsLocked(); locked {
		if key, err = key.Unlock([]byte(passphrase)); err != nil {
			return errors.New(i18n.T("wrong key passphrase"))
		}
	}
	found := false
	for _, id := range key.GetEntity().Identities {
		if strings.EqualFold(id.UserId.Email, email) {
			found = true
		}
	}
	if !found {
		return fmt.Errorf(i18n.T("the key does not belong to %s"), email)
	}
	return storeOwn(email, key)
}

// OwnKeyRing returns the unlocked private key of an address.
func OwnKeyRing(email string) (*crypto.KeyRing, error) {
	b, err := os.ReadFile(ownFile(email))
	if err != nil {
		return nil, errors.New(i18n.T("you have no PGP key of your own for this address"))
	}
	pass, err := keyring.Get("klient", passName(email))
	if err != nil {
		return nil, fmt.Errorf(i18n.T("the key passphrase is missing from the keyring: %w"), err)
	}
	key, err := crypto.NewKeyFromArmored(string(b))
	if err != nil {
		return nil, err
	}
	if key, err = key.Unlock([]byte(pass)); err != nil {
		return nil, err
	}
	return crypto.NewKeyRing(key)
}

// OwnPublicKey returns the armored public key of an address.
func OwnPublicKey(email string) (string, *crypto.Key, error) {
	kr, err := OwnKeyRing(email)
	if err != nil {
		return "", nil, err
	}
	k, err := kr.GetKey(0)
	if err != nil {
		return "", nil, err
	}
	pub, err := k.GetArmoredPublicKey()
	return pub, k, err
}

// ExportOwnPrivate returns the private key armored and protected with the
// given passphrase (for a backup).
func ExportOwnPrivate(email, passphrase string) (string, error) {
	kr, err := OwnKeyRing(email)
	if err != nil {
		return "", err
	}
	k, err := kr.GetKey(0)
	if err != nil {
		return "", err
	}
	k, err = k.Copy()
	if err != nil {
		return "", err
	}
	locked, err := k.Lock([]byte(passphrase))
	if err != nil {
		return "", err
	}
	return locked.Armor()
}

func DeleteOwnKey(email string) error {
	_ = keyring.Delete("klient", passName(email))
	err := os.Remove(ownFile(email))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
