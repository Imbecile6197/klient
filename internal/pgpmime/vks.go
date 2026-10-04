package pgpmime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// vksURL is keys.openpgp.org's Verifying Key Server API (a variable for
// tests).
var vksURL = "https://keys.openpgp.org/vks/v1"

// Publish uploads a public key to keys.openpgp.org and asks it to send a
// confirmation e-mail to each address; the key becomes findable by an
// address only after its owner clicks the link. It returns the state of
// each address ("unpublished", "pending", "published", "revoked").
func Publish(ctx context.Context, armored string, emails []string, locale string) (map[string]string, error) {
	var up struct {
		Token  string            `json:"token"`
		Status map[string]string `json:"status"`
	}
	if err := vksPost(ctx, "/upload", map[string]any{"keytext": armored}, &up); err != nil {
		return nil, err
	}
	var ask []string
	for _, e := range emails {
		if st, ok := up.Status[e]; ok && st == "unpublished" {
			ask = append(ask, e)
		}
	}
	if len(ask) == 0 {
		return up.Status, nil
	}
	var ver struct {
		Status map[string]string `json:"status"`
	}
	if err := vksPost(ctx, "/request-verify", map[string]any{"token": up.Token, "addresses": ask, "locale": []string{locale}}, &ver); err != nil {
		return nil, err
	}
	for k, v := range ver.Status {
		up.Status[k] = v
	}
	return up.Status, nil
}

func vksPost(ctx context.Context, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vksURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error != "" {
			return fmt.Errorf(i18n.T("keys.openpgp.org: %s"), e.Error)
		}
		return fmt.Errorf(i18n.T("keys.openpgp.org: HTTP %d"), res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
