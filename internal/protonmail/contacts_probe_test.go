//go:build probe

package protonmail

import (
	"context"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/secrets"
)

// Live check against the logged-in account; prints counts only.
// go test -tags probe -run TestContactsProbe -v ./internal/protonmail/
func TestContactsProbe(t *testing.T) {
	cfg, _ := config.Load()
	s, err := secrets.LoadProtonSession(probeUser())
	if err != nil || s == nil {
		t.Skip("no saved session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	acc, err := Resume(ctx, NewManager(cfg), s)
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	all, err := acc.client.GetAllContactEmails(ctx, "")
	t.Logf("kontaktní adresy ze serveru: %d (chyba: %v)", len(all), err)
	c, err := acc.Contacts(ctx)
	t.Logf("Contacts(): %d (chyba: %v)", len(c), err)
	t.Logf("nedávné adresy z cache: %d", len(acc.RecentAddresses(200)))
}
