package imapmail

import (
	"context"
	"net/mail"
	"strings"
	"testing"

	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/pgpmime"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

func TestPGP(t *testing.T) {
	set, u, smtpBe := startServers(t)
	ctx := context.Background()

	// Own key for the account, and Petr's key in the local store.
	if err := pgp.GenerateOwnKey("Jan Novák", "jan@example.cz"); err != nil {
		t.Fatal(err)
	}
	petrKey, _ := crypto.GenerateKey("Petr", "petr@example.org", "x25519", 0)
	petrKR, _ := crypto.NewKeyRing(petrKey)
	petrPub, _ := petrKey.GetArmoredPublicKey()
	if _, err := pgp.ImportKey(petrPub); err != nil {
		t.Fatal(err)
	}

	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	if !acc.Caps().E2E {
		t.Fatal("E2E should be on with an own key")
	}
	modes := acc.PlanEncryption(ctx, []*mail.Address{{Address: "petr@example.org"}, {Address: "eva@example.net"}})
	if modes[0].Scheme != "pgp" || modes[1].Scheme != "clear" {
		t.Fatalf("plan %+v", modes)
	}

	// Mixed recipients: Petr gets it encrypted, Eva plain but signed.
	d := &protonmail.Draft{
		To:      []*mail.Address{{Address: "petr@example.org"}, {Address: "eva@example.net"}},
		Subject: "Tajné", Body: "Heslo k wifi je koloběžka", SignExternal: true,
	}
	if err := acc.Send(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(smtpBe.mail) != 2 {
		t.Fatalf("expected 2 SMTP transactions, got %d", len(smtpBe.mail))
	}
	enc, plain := smtpBe.mail[0], smtpBe.mail[1]
	if !strings.Contains(enc, "multipart/encrypted") || strings.Contains(enc, "koloběžka") {
		t.Fatal("Petr's copy is not encrypted")
	}
	if !strings.Contains(plain, "multipart/signed") || !strings.Contains(plain, "Autocrypt: addr=jan@example.cz") {
		t.Fatalf("Eva's copy should be signed with Autocrypt:\n%.400s", plain)
	}
	// Petr can decrypt and verify it.
	janPub, _, _ := pgp.OwnPublicKey("jan@example.cz")
	janKey, _ := crypto.NewKeyFromArmored(janPub)
	janKR, _ := crypto.NewKeyRing(janKey)
	inner, verr, err := pgpmime.Decrypt([]byte(enc), petrKR, janKR)
	if err != nil || verr != nil || !strings.Contains(string(inner), "kolob") {
		t.Fatalf("petr decrypt %v %v %.200s", err, verr, inner)
	}
	// Eva's signed copy verifies.
	if _, err := pgpmime.Verify([]byte(plain), janKR); err != nil {
		t.Fatalf("signature: %v", err)
	}
	// The copy in Sent is encrypted, and readable in Klient.
	sent, _ := acc.List(ctx, protonmail.SentID, 0, 10)
	raw, _, _ := acc.raw(ctx, sent[0].ID)
	if strings.Contains(string(raw), "koloběžka") {
		t.Fatal("sent copy stored in plain text")
	}
	sm, err := acc.Get(ctx, sent[0].ID)
	if err != nil || !strings.Contains(sm.Text, "koloběžka") || !strings.HasPrefix(sm.Encryption, "End-to-end") {
		t.Fatalf("sent copy: %v %q %q", err, sm.Text, sm.Encryption)
	}

	// Incoming mail encrypted by Petr and signed.
	entity := "Content-Type: text/plain; charset=utf-8\r\n\r\nAhoj Jene, schůzka v 10.\r\n"
	recips, _ := crypto.NewKeyRing(janKey)
	ct, body, err := pgpmime.Encrypt([]byte(entity), recips, petrKR)
	if err != nil {
		t.Fatal(err)
	}
	appendMsg(t, u, "INBOX", "From: Petr <petr@example.org>\r\nTo: jan@example.cz\r\nSubject: ...\r\nMessage-ID: <e1@example.org>\r\nMIME-Version: 1.0\r\nContent-Type: "+ct+"\r\n\r\n"+string(body))
	in, _ := acc.List(ctx, protonmail.InboxID, 0, 10)
	m, err := acc.Get(ctx, in[0].ID)
	if err != nil || !strings.Contains(m.Text, "schůzka v 10") || m.Signature != pgp.SigValid || !strings.HasPrefix(m.Encryption, "End-to-end") {
		t.Fatalf("incoming: %v text=%q sig=%v enc=%q", err, m.Text, m.Signature, m.Encryption)
	}

	// Drafts are encrypted to self on the server and still open.
	dr := &protonmail.Draft{To: []*mail.Address{{Address: "eva@example.net"}}, Subject: "Koncept", Body: "rozepsaný tajný text"}
	if err := acc.SaveDraft(ctx, dr); err != nil {
		t.Fatal(err)
	}
	draw, _, _ := acc.raw(ctx, dr.ID)
	if strings.Contains(string(draw), "tajný text") {
		t.Fatal("draft stored in plain text")
	}
	od, _, err := acc.OpenDraft(ctx, dr.ID)
	if err != nil || !strings.Contains(od.Body, "rozepsaný tajný text") {
		t.Fatalf("open draft %v %+v", err, od)
	}

	// Autocrypt: a new sender's key is learned from the header.
	evaKey, _ := crypto.GenerateKey("Eva", "eva@example.net", "x25519", 0)
	ac, _ := pgpmime.AutocryptHeader("eva@example.net", evaKey)
	appendMsg(t, u, "INBOX", "From: eva@example.net\r\nTo: jan@example.cz\r\nSubject: ahoj\r\nAutocrypt: "+ac+"\r\nContent-Type: text/plain\r\n\r\nahoj\r\n")
	in, _ = acc.List(ctx, protonmail.InboxID, 0, 10)
	for _, s := range in {
		if s.Subject == "ahoj" {
			if _, err := acc.Get(ctx, s.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if pgp.LocalKey("eva@example.net") == nil {
		t.Fatal("Autocrypt key not learned")
	}
}
