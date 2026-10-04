// Package protonmail talks to Proton Mail directly over the Proton API:
// SRP login, 2FA, key unlocking, message decryption and end-to-end
// encrypted sending. No Proton Bridge is involved.
package protonmail

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/cache"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/secrets"
)

func NewManager(cfg config.Config) *proton.Manager {
	return proton.New(
		proton.WithHostURL(cfg.ProtonHostURL),
		proton.WithAppVersion(cfg.ProtonAppVersion),
	)
}

// PendingLogin is a login that has passed SRP but may still need a TOTP code
// and/or the mailbox password (two-password mode).
type PendingLogin struct {
	mgr      *proton.Manager
	client   *proton.Client
	auth     proton.Auth
	username string
	password []byte
}

func (p *PendingLogin) NeedsTOTP() bool { return p.auth.TwoFA.Enabled&proton.HasTOTP != 0 }

func (p *PendingLogin) NeedsMailboxPassword() bool {
	return p.auth.PasswordMode == proton.TwoPasswordMode
}

// HVRequiredError means Proton wants human verification before login. The
// user solves it in a browser at URL(); then the login is retried with HV.
type HVRequiredError struct {
	HV *proton.APIHVDetails
}

func (e *HVRequiredError) Error() string { return i18n.T("Proton requires human verification") }

// URL is Proton's verification page for this token.
func (e *HVRequiredError) URL() string {
	q := url.Values{}
	q.Set("methods", strings.Join(e.HV.Methods, ","))
	q.Set("token", e.HV.Token)
	return "https://verify.proton.me/?" + q.Encode()
}

// Login performs SRP authentication. hv is nil on the first attempt; after an
// HVRequiredError pass its HV once the user has completed verification.
func Login(ctx context.Context, mgr *proton.Manager, username string, password []byte, hv *proton.APIHVDetails) (*PendingLogin, error) {
	c, auth, err := mgr.NewClientWithLoginWithHVToken(ctx, username, password, hv)
	if err != nil {
		var apiErr *proton.APIError
		if errors.As(err, &apiErr) && apiErr.IsHVError() {
			details, derr := apiErr.GetHVDetails()
			if derr == nil && details.Token != "" {
				return nil, &HVRequiredError{HV: details}
			}
		}
		if errors.As(err, &apiErr) {
			switch apiErr.Code {
			case 8002:
				return nil, errors.New(i18n.T("Incorrect username or password. Check the keyboard layout and Caps Lock."))
			case 2028:
				return nil, errors.New(i18n.T("Too many login attempts. Try again later."))
			case 5003:
				return nil, errors.New(i18n.T("Proton rejected this client version (x-pm-appversion). Change proton_app_version in ~/.config/klient/config.json."))
			}
		}
		return nil, err
	}
	return &PendingLogin{mgr: mgr, client: c, auth: auth, username: username, password: password}, nil
}

func (p *PendingLogin) SubmitTOTP(ctx context.Context, code string) error {
	return p.client.Auth2FA(ctx, proton.Auth2FAReq{TwoFactorCode: code})
}

// Finish unlocks the keys and persists the session to the keyring.
// mailboxPassword is only used in two-password mode.
func (p *PendingLogin) Finish(ctx context.Context, mailboxPassword []byte) (*Account, error) {
	pass := p.password
	if p.NeedsMailboxPassword() {
		pass = mailboxPassword
	}
	salts, err := p.client.GetSalts(ctx)
	if err != nil {
		return nil, err
	}
	user, err := p.client.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	keyPass, err := salts.SaltForKey(pass, user.Keys.Primary().ID)
	if err != nil {
		return nil, err
	}
	acc := &Account{client: p.client, username: p.username, keyPass: keyPass, mgr: p.mgr}
	if err := acc.unlock(ctx, user); err != nil {
		return nil, err
	}
	acc.persist(p.auth.UID, p.auth.RefreshToken)
	p.client.AddAuthHandler(func(a proton.Auth) { acc.persist(a.UID, a.RefreshToken) })
	return acc, nil
}

// Resume restores a session saved by a previous Finish. Without network it
// starts offline from the encrypted cache (if it was synced before) and the
// client reconnects by itself once the API is reachable.
func Resume(ctx context.Context, mgr *proton.Manager, s *secrets.ProtonSession) (*Account, error) {
	keyPass, err := s.KeyPassBytes()
	if err != nil {
		return nil, err
	}
	c, auth, err := mgr.NewClientWithRefresh(ctx, s.UID, s.RefreshToken)
	if err != nil {
		if !IsOffline(err) || s.UserID == "" {
			return nil, err
		}
		return resumeOffline(mgr, s, keyPass)
	}
	acc := &Account{client: c, username: s.Username, keyPass: keyPass, mgr: mgr}
	user, err := c.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := acc.unlock(ctx, user); err != nil {
		return nil, err
	}
	acc.persist(auth.UID, auth.RefreshToken)
	c.AddAuthHandler(func(a proton.Auth) { acc.persist(a.UID, a.RefreshToken) })
	return acc, nil
}

type Account struct {
	client   *proton.Client
	mgr      *proton.Manager
	username string
	keyPass  []byte
	userID   string
	cache    *cache.Cache // nil if the cache could not be opened
	offline  bool         // started without network
	refresh  string       // current refresh token (rotates on every refresh)

	mu     sync.RWMutex
	user   proton.User
	userAt time.Time // when user was fetched
	addrs  []proton.Address
	userKR *crypto.KeyRing
	// Encryption settings from the Proton contacts, by address.
	contactCache map[string]cachedContact
	addrKRs      map[string]*crypto.KeyRing // address ID -> unlocked keyring
	contacts     []Contact
}

func (a *Account) unlock(ctx context.Context, user proton.User) error {
	addrs, err := a.client.GetAddresses(ctx)
	if err != nil {
		return err
	}
	if err := a.unlockKeys(user, addrs); err != nil {
		return err
	}
	a.openCache(user.ID)
	if a.cache != nil {
		// Keys stay passphrase-protected; the passphrase is only in the keyring.
		_ = a.cache.Set("user", user)
		_ = a.cache.Set("addresses", addrs)
	}
	return nil
}

func (a *Account) unlockKeys(user proton.User, addrs []proton.Address) error {
	userKR, err := user.Keys.Unlock(a.keyPass, nil)
	if err != nil {
		return fmt.Errorf(i18n.T("could not unlock the keys (wrong mailbox password?): %w"), err)
	}
	addrKRs := map[string]*crypto.KeyRing{}
	for _, addr := range addrs {
		if kr, err := addr.Keys.Unlock(a.keyPass, userKR); err == nil {
			addrKRs[addr.ID] = kr
		}
	}
	if len(addrKRs) == 0 {
		return errors.New(i18n.T("could not unlock any address key"))
	}
	a.mu.Lock()
	a.user, a.addrs, a.userKR, a.addrKRs = user, addrs, userKR, addrKRs
	a.userAt = time.Now()
	a.mu.Unlock()
	return nil
}

// RefreshToken is the refresh token this account currently uses.
func (a *Account) RefreshToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.refresh
}

// OnDeauth registers f to run when the server revokes the session (e.g. the
// refresh token was rotated by another process or the session was logged out
// on the web).
func (a *Account) OnDeauth(f func()) { a.client.AddDeauthHandler(f) }

func (a *Account) persist(uid, refresh string) {
	a.mu.Lock()
	a.refresh = refresh
	a.mu.Unlock()
	_ = secrets.SaveProtonSession(secrets.ProtonSession{
		Username:     a.username,
		UserID:       a.userID,
		UID:          uid,
		RefreshToken: refresh,
		KeyPass:      secrets.EncodeKeyPass(a.keyPass),
	})
}

// Logout revokes the session on the server and forgets it locally.
func (a *Account) Logout(ctx context.Context) error {
	err := a.client.AuthDelete(ctx)
	a.client.Close()
	a.mu.Lock()
	if a.userKR != nil {
		a.userKR.ClearPrivateParams()
	}
	for _, kr := range a.addrKRs {
		kr.ClearPrivateParams()
	}
	a.mu.Unlock()
	if e := secrets.DeleteProtonSession(a.username); e != nil && err == nil {
		err = e
	}
	// Leave nothing behind: the cache file and its key.
	if a.cache != nil {
		a.cache.Close()
		_ = os.Remove(cachePath(a.userID))
		_ = os.Remove(cachePath(a.userID) + "-wal")
		_ = os.Remove(cachePath(a.userID) + "-shm")
		secrets.DeleteCacheKey(a.userID)
	}
	return err
}

// Username is the login name the session is stored under.
func (a *Account) Username() string { return a.username }

// UserID is the Proton user ID (the same account logged in twice has one ID).
func (a *Account) UserID() string { return a.userID }

func (a *Account) Close() {
	a.client.Close()
	if a.cache != nil {
		a.cache.Close()
	}
}

func (a *Account) Email() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.addrs) > 0 {
		return a.addrs[0].Email
	}
	return a.user.Email
}

// SendAddresses lists addresses that may send, in the user's order.
func (a *Account) SendAddresses() []proton.Address {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []proton.Address
	for _, addr := range a.addrs {
		if bool(addr.Send) && addr.Status == proton.AddressStatusEnabled && a.addrKRs[addr.ID] != nil {
			out = append(out, addr)
		}
	}
	return out
}

func (a *Account) addrKR(addressID string) (*crypto.KeyRing, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	kr := a.addrKRs[addressID]
	if kr == nil {
		return nil, fmt.Errorf(i18n.T("no key for address %s"), addressID)
	}
	return kr, nil
}

// OwnPublicKey returns the armored public key of the given address, for
// sharing with external PGP users.
func (a *Account) OwnPublicKey(addressID string) (string, error) {
	kr, err := a.addrKR(addressID)
	if err != nil {
		return "", err
	}
	k, err := kr.GetKey(0)
	if err != nil {
		return "", err
	}
	return k.GetArmoredPublicKey()
}

// readableErr wraps an API error with a short text (the server's own message,
// without the request URL) while keeping the original for errors.As.
type readableErr struct {
	msg string
	err error
}

func (e *readableErr) Error() string { return e.msg }
func (e *readableErr) Unwrap() error { return e.err }

// apiErr formats err for people: "prefix: <server message>".
func apiErr(prefix string, err error) error {
	var ae *proton.APIError
	if errors.As(err, &ae) && ae.Message != "" {
		msg := ae.Message
		if len(ae.Details) > 0 && string(ae.Details) != "null" {
			msg += " " + string(ae.Details)
		}
		return &readableErr{msg: prefix + ": " + msg, err: err}
	}
	if IsOffline(err) {
		return &readableErr{msg: prefix + i18n.T(": the Proton server is unavailable"), err: err}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// IsOffline reports whether err means the Proton API was unreachable.
func IsOffline(err error) bool {
	var ne *proton.NetError
	return errors.As(err, &ne)
}

func cachePath(userID string) string {
	return filepath.Join(config.DataDir(), "cache", userID+".db")
}

func (a *Account) openCache(userID string) {
	a.userID = userID
	if a.cache != nil || userID == "" {
		return
	}
	key, err := secrets.CacheKey(userID)
	if err != nil {
		return
	}
	if c, err := cache.Open(cachePath(userID), key); err == nil {
		a.cache = c
	}
}

func resumeOffline(mgr *proton.Manager, s *secrets.ProtonSession, keyPass []byte) (*Account, error) {
	acc := &Account{
		// No access token yet: the first request after reconnecting gets a 401
		// and the client refreshes it with the refresh token.
		client: mgr.NewClient(s.UID, "", s.RefreshToken), mgr: mgr,
		username: s.Username, keyPass: keyPass,
	}
	acc.openCache(s.UserID)
	if acc.cache == nil {
		return nil, errors.New(i18n.T("offline and without a saved cache"))
	}
	var user proton.User
	var addrs []proton.Address
	if !acc.cache.Get("user", &user) || !acc.cache.Get("addresses", &addrs) {
		return nil, errors.New(i18n.T("offline, and the cache does not contain the account keys yet"))
	}
	if err := acc.unlockKeys(user, addrs); err != nil {
		return nil, err
	}
	acc.offline = true
	acc.refresh = s.RefreshToken
	acc.client.AddAuthHandler(func(au proton.Auth) { acc.persist(au.UID, au.RefreshToken) })
	return acc, nil
}

// StartedOffline reports whether the session was restored without network.
func (a *Account) StartedOffline() bool { return a.offline }

// Cache exposes the offline cache (may be nil).
func (a *Account) Cache() *cache.Cache { return a.cache }
