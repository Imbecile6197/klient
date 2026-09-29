//go:build probe

package protonmail

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/secrets"
)

// Counts how many recent messages carry List-Unsubscribe in the raw header,
// in ParsedHeaders from the API, in the cached copy and after Get(). Counts only.
func TestUnsubscribeProbe(t *testing.T) {
	cfg, _ := config.Load()
	s, _ := secrets.LoadProtonSession(probeUser())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	acc, err := Resume(ctx, NewManager(cfg), s)
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	msgs, err := acc.List(ctx, InboxID, 0, 40)
	if err != nil {
		t.Fatal(err)
	}
	var inRaw, inParsed, inCache, inGet, parsedOK int
	keys := map[string]int{}
	for _, m := range msgs {
		raw, err := acc.client.GetMessage(ctx, m.ID)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(raw.Header), "list-unsubscribe:") {
			inRaw++
		}
		for k := range raw.ParsedHeaders.Values {
			if strings.EqualFold(k, "List-Unsubscribe") {
				inParsed++
				keys[k]++
			}
		}
		if c, ok := acc.cache.Message(m.ID); ok {
			for k := range c.ParsedHeaders.Values {
				if strings.EqualFold(k, "List-Unsubscribe") {
					inCache++
				}
			}
		}
		full, err := acc.Get(ctx, m.ID)
		if err == nil {
			if _, ok := headerLookup(full.Headers, "List-Unsubscribe"); ok {
				inGet++
			}
			if mailparse.ParseUnsubscribe(full.Headers).Any() {
				parsedOK++
			}
		}
	}
	t.Logf("zpráv: %d | v surové hlavičce: %d | v ParsedHeaders: %d %v | v cache: %d | po Get(): %d | ParseUnsubscribe OK: %d",
		len(msgs), inRaw, inParsed, keys, inCache, inGet, parsedOK)
}

func headerLookup(h map[string][]string, name string) ([]string, bool) {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return nil, false
}
