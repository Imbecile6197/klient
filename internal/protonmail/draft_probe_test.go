//go:build probe

package protonmail

import (
	"context"
	"crypto/rand"
	"net/mail"
	"testing"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/libormacak/klient/internal/config"
	"github.com/libormacak/klient/internal/secrets"
)

// Reproduces the draft update error: creates a draft to self with a small
// attachment, updates it, prints the API error in full, deletes the draft.
func TestDraftProbe(t *testing.T) {
	cfg, _ := config.Load()
	s, _ := secrets.LoadProtonSession(probeUser())
	if s == nil {
		t.Skip("no session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	acc, err := Resume(ctx, NewManager(cfg), s)
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	from := acc.SendAddresses()[0]
	d := &Draft{
		FromAddressID: from.ID, To: []*mail.Address{{Address: from.Email}},
		Subject: "Klient test konceptu (smaže se)", Body: "",
		Attachments:  []*Outgoing{{Name: "IMG_test.jpeg", MIMEType: "image/jpeg", Data: jpegish, Size: int64(len(jpegish))}},
		SignExternal: true,
	}
	if err := acc.SaveDraft(ctx, d); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		t.Logf("smazání konceptu: %v", acc.DeleteDraft(context.Background(), d.ID))
	}()
	t.Logf("koncept vytvořen, příloha nahrána")
	d.Body = "" // attachment-only message
	err = acc.SaveDraft(ctx, d)
	t.Logf("aktualizace s přílohou: %v", err)
	var apiErr *proton.APIError
	if errorsAs(err, &apiErr) {
		t.Logf("kód %d, zpráva %q, detaily %s", apiErr.Code, apiErr.Message, string(apiErr.Details))
	}
}

var jpegish = func() []byte {
	b := make([]byte, 100*1024)
	rand.Read(b)
	b[0], b[1], b[2] = 0xFF, 0xD8, 0xFF
	return b
}()

func errorsAs(err error, target **proton.APIError) bool {
	for err != nil {
		if e, ok := err.(*proton.APIError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
