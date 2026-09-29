//go:build probe

package protonmail

import (
	"context"
	"testing"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/libormacak/klient/internal/config"
	"github.com/libormacak/klient/internal/secrets"
)

// Re-saves the user's failed test draft unchanged to get the exact API error.
func TestExistingDraftProbe(t *testing.T) {
	cfg, _ := config.Load()
	s, _ := secrets.LoadProtonSession(probeUser())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	acc, err := Resume(ctx, NewManager(cfg), s)
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	drafts, err := acc.List(ctx, DraftsID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range drafts {
		if m.Subject != "Posílám zkušební přílohu" {
			continue
		}
		t.Logf("koncept: příloh %d, flags %b, labels %v, draft=%v", m.NumAttachments, m.Flags, m.LabelIDs, m.IsDraft())
		raw, err := acc.client.GetMessage(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MIMEType %s, příloh na serveru %d", raw.MIMEType, len(raw.Attachments))
		d, _, err := acc.OpenDraft(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		err = acc.SaveDraft(ctx, d)
		t.Logf("uložení beze změny: %v", err)
		var apiErr *proton.APIError
		if errorsAs(err, &apiErr) {
			t.Logf("kód %d, zpráva %q, detaily %s", apiErr.Code, apiErr.Message, string(apiErr.Details))
		}
	}
}
