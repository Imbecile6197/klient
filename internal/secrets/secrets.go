// Package secrets keeps credentials in the desktop keyring (Secret Service /
// GNOME Keyring) instead of on disk.
package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

const service = "klient"

const keyProtonSession = "proton-session"

// apiKeyName maps an AI provider ID to its keyring entry. The Claude entry
// keeps its original name so existing keys keep working.
// UseMemory keeps every secret in memory for this process instead of the
// system keyring (the demo mode must not touch the user's keyring).
func UseMemory() { keyring.MockInit() }

func apiKeyName(provider string) string {
	if provider == "claude" {
		return "anthropic-api-key"
	}
	return provider + "-api-key"
}

// ProtonSession is what we need to resume a session without asking for the
// password again. KeyPass is the salted key passphrase, not the password.
type ProtonSession struct {
	Username     string `json:"username"`
	UserID       string `json:"user_id,omitempty"` // needed to find the offline cache
	UID          string `json:"uid"`
	RefreshToken string `json:"refresh_token"`
	KeyPass      string `json:"key_pass"` // base64
}

func (s ProtonSession) KeyPassBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(s.KeyPass)
}

// sessionName is the keyring entry of one account's session. Sessions saved
// before multi-account support live under the bare keyProtonSession.
func sessionName(username string) string {
	return keyProtonSession + ":" + strings.ToLower(strings.TrimSpace(username))
}

func SaveProtonSession(s ProtonSession) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return keyring.Set(service, sessionName(s.Username), string(b))
}

func loadSession(name string) (*ProtonSession, error) {
	v, err := keyring.Get(service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s ProtonSession
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadProtonSession returns the session of an account, (nil, nil) when none
// is stored.
func LoadProtonSession(username string) (*ProtonSession, error) {
	return loadSession(sessionName(username))
}

// MigrateLegacySession moves a session stored by an older version (single
// account) to its per-account entry and returns its username ("" if none).
func MigrateLegacySession() (string, error) {
	s, err := loadSession(keyProtonSession)
	if err != nil || s == nil {
		return "", err
	}
	if err := SaveProtonSession(*s); err != nil {
		return "", err
	}
	_ = keyring.Delete(service, keyProtonSession)
	return s.Username, nil
}

func DeleteProtonSession(username string) error {
	err := keyring.Delete(service, sessionName(username))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// SaveMailPassword stores the password of an IMAP/SMTP account (by its
// config ID); "" deletes it.
func SaveMailPassword(id, password string) error {
	if password == "" {
		err := keyring.Delete(service, "mail-password:"+id)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	return keyring.Set(service, "mail-password:"+id, password)
}

func LoadMailPassword(id string) (string, error) {
	v, err := keyring.Get(service, "mail-password:"+id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

// SaveAPIKey stores the API key of an AI provider; "" deletes it.
func SaveAPIKey(provider, key string) error {
	if key == "" {
		err := keyring.Delete(service, apiKeyName(provider))
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	return keyring.Set(service, apiKeyName(provider), key)
}

// LoadAPIKey returns "" when no key is stored.
func LoadAPIKey(provider string) (string, error) {
	v, err := keyring.Get(service, apiKeyName(provider))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

// CacheKey returns the key sealing the offline cache of a user, creating a
// random one on first use. It never touches the disk.
func CacheKey(userID string) ([]byte, error) {
	name := "cache-key-" + userID
	v, err := keyring.Get(service, name)
	if err == nil {
		return base64.StdEncoding.DecodeString(v)
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, keyring.Set(service, name, base64.StdEncoding.EncodeToString(key))
}

func DeleteCacheKey(userID string) {
	_ = keyring.Delete(service, "cache-key-"+userID)
}

func EncodeKeyPass(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
