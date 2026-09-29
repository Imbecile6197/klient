package pgpmime

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
)

type cached struct {
	kr  *crypto.KeyRing
	src string
	at  time.Time
}

var (
	cacheMu sync.Mutex
	found   = map[string]cached{}
)

// Lookup finds a recipient's public key on the web: the Web Key Directory
// of their own domain, and – if allowed – keys.openpgp.org (which only
// publishes keys whose owner confirmed the address). Results are kept for a
// day, misses for an hour.
func Lookup(ctx context.Context, email string, keyServer bool) (*crypto.KeyRing, string) {
	email = strings.ToLower(strings.TrimSpace(email))
	cacheMu.Lock()
	if c, ok := found[email]; ok {
		ttl := 24 * time.Hour
		if c.kr == nil {
			ttl = time.Hour
		}
		if time.Since(c.at) < ttl && (c.kr != nil || !keyServer || c.src == "vks") {
			cacheMu.Unlock()
			return c.kr, c.src
		}
	}
	cacheMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var kr *crypto.KeyRing
	src := ""
	for _, u := range WKDURLs(email) {
		if kr = fetchKey(ctx, u, email); kr != nil {
			src = "wkd"
			break
		}
	}
	if kr == nil && keyServer {
		if kr = fetchKey(ctx, "https://keys.openpgp.org/vks/v1/by-email/"+url.PathEscape(email), email); kr != nil {
			src = "vks"
		} else {
			src = "vks" // remember that the key server was asked too
		}
	}
	cacheMu.Lock()
	found[email] = cached{kr, src, time.Now()}
	cacheMu.Unlock()
	if kr == nil {
		return nil, ""
	}
	return kr, src
}

func fetchKey(ctx context.Context, u, email string) *crypto.KeyRing {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || len(b) == 0 {
		return nil
	}
	var key *crypto.Key
	if strings.Contains(string(b), "BEGIN PGP PUBLIC KEY BLOCK") {
		key, err = crypto.NewKeyFromArmored(string(b))
	} else {
		key, err = crypto.NewKey(b)
	}
	if err != nil || key.IsExpired() || key.IsRevoked() {
		return nil
	}
	ok := false
	for _, id := range key.GetEntity().Identities {
		if strings.EqualFold(id.UserId.Email, email) {
			ok = true
		}
	}
	if !ok {
		return nil
	}
	kr, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil
	}
	return kr
}
