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

func TestPGPProtectedInlineAndKeyChange(t *testing.T) {
	set, u, smtpBe := startServers(t)
	ctx := context.Background()
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
	janPub, _, _ := pgp.OwnPublicKey("jan@example.cz")
	janKey, _ := crypto.NewKeyFromArmored(janPub)
	janKR, _ := crypto.NewKeyRing(janKey)

	// Protected subject: "..." outside, the real one inside.
	SetProtectSubject(true)
	defer SetProtectSubject(false)
	d := &protonmail.Draft{To: []*mail.Address{{Address: "petr@example.org"}}, Subject: "Tajná schůzka", Body: "v 10"}
	if err := acc.Send(ctx, d); err != nil {
		t.Fatal(err)
	}
	enc := smtpBe.mail[len(smtpBe.mail)-1]
	if !strings.Contains(enc, "Subject: ...") || strings.Contains(enc, "schůzka") || strings.Contains(enc, "sch=C5") {
		t.Fatalf("the subject must be hidden outside:\n%.600s", enc)
	}
	inner, _, err := pgpmime.Decrypt([]byte(enc), petrKR, janKR)
	if err != nil || pgpmime.ProtectedSubject(inner) != "Tajná schůzka" {
		t.Fatalf("protected subject: %v %q\n%.400s", err, pgpmime.ProtectedSubject(inner), inner)
	}
	sent, _ := acc.List(ctx, protonmail.SentID, 0, 10)
	if len(sent) == 0 || sent[0].Subject != "Tajná schůzka" {
		t.Fatalf("the copy in Sent should keep its subject: %+v", sent)
	}

	// Incoming mail with a protected subject shows the real one.
	entity := pgpmime.ProtectHeaders([]byte("Content-Type: text/plain; charset=utf-8\r\n\r\nAhoj\r\n"), [][2]string{{"Subject", "=?utf-8?q?Skryt=C3=BD_p=C5=99edm=C4=9Bt?="}})
	recips, _ := crypto.NewKeyRing(janKey)
	ct, body, _ := pgpmime.Encrypt(entity, recips, petrKR)
	appendMsg(t, u, "INBOX", "From: Petr <petr@example.org>\r\nTo: jan@example.cz\r\nSubject: ...\r\nMessage-ID: <p1@example.org>\r\nMIME-Version: 1.0\r\nContent-Type: "+ct+"\r\n\r\n"+string(body))

	// Inline PGP from Mailvelope.
	pm, _ := recips.Encrypt(crypto.NewPlainMessageFromString("Inline tajemství"), petrKR)
	arm, _ := pm.GetArmored()
	appendMsg(t, u, "INBOX", "From: Petr <petr@example.org>\r\nTo: jan@example.cz\r\nSubject: inline\r\nMessage-ID: <p2@example.org>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"+arm+"\r\n")

	in, _ := acc.List(ctx, protonmail.InboxID, 0, 10)
	found := 0
	for _, s := range in {
		m, err := acc.Get(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(m.Text, "Ahoj"):
			found++
			if m.Meta.Subject != "Skrytý předmět" {
				t.Errorf("protected incoming subject = %q", m.Meta.Subject)
			}
		case s.Subject == "inline":
			found++
			if !strings.Contains(m.Text, "Inline tajemství") || strings.Contains(m.Text, "BEGIN PGP") || m.Signature != pgp.SigValid || !strings.HasPrefix(m.Encryption, "End-to-end") {
				t.Errorf("inline: text=%q sig=%v enc=%q", m.Text, m.Signature, m.Encryption)
			}
		}
	}
	if found != 2 {
		t.Fatalf("found %d of 2 test messages", found)
	}

	// Petr's imported key is not swapped by an Autocrypt header; the new
	// key waits for the user.
	newKey, _ := crypto.GenerateKey("Petr", "petr@example.org", "x25519", 0)
	ac, _ := pgpmime.AutocryptHeader("petr@example.org", newKey)
	appendMsg(t, u, "INBOX", "From: petr@example.org\r\nTo: jan@example.cz\r\nSubject: novy klic\r\nAutocrypt: "+ac+"\r\nContent-Type: text/plain\r\n\r\nahoj\r\n")
	in, _ = acc.List(ctx, protonmail.InboxID, 0, 10)
	for _, s := range in {
		if s.Subject == "novy klic" {
			if _, err := acc.Get(ctx, s.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if pgp.PendingKey("petr@example.org") == nil || pgp.Fingerprint(pgp.LocalKey("petr@example.org")) != strings.ToUpper(petrKey.GetFingerprint()) {
		t.Fatal("the new key should wait and the old one stay in use")
	}
}
