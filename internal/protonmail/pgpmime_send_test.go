package protonmail

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ProtonMail/gluon/rfc822"
	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/mimebuild"
)

// A PGP/MIME package keeps the HTML and the attachments, and the recipient
// can open it with their own key; a signed clear one carries the body key.
func TestPGPMIMEPackage(t *testing.T) {
	sender, _ := crypto.GenerateKey("Me", "me@proton.me", "x25519", 0)
	senderKR, _ := crypto.NewKeyRing(sender)
	rcpt, _ := crypto.GenerateKey("Them", "them@example.org", "x25519", 0)
	rcptPub, _ := rcpt.ToPublic()
	rcptKR, _ := crypto.NewKeyRing(rcptPub)

	entity, err := mimebuild.Content("Ahoj", "<p><b>Ahoj</b></p>", []mimebuild.Attachment{{Name: "a.txt", MIMEType: "text/plain", Data: []byte("příloha")}})
	if err != nil {
		t.Fatal(err)
	}
	var req proton.SendDraftReq
	err = req.AddMIMEPackage(senderKR, string(entity), map[string]proton.SendPreferences{
		"them@example.org": {Encrypt: true, PubKey: rcptKR, SignatureType: proton.DetachedSignature,
			EncryptionScheme: proton.PGPMIMEScheme, MIMEType: rfc822.MultipartMixed},
		"clear@example.org": {SignatureType: proton.DetachedSignature,
			EncryptionScheme: proton.ClearMIMEScheme, MIMEType: rfc822.MultipartMixed},
	})
	if err != nil {
		t.Fatal(err)
	}
	pkg := req.Packages[0]
	if pkg.BodyKey == nil {
		t.Error("a clear MIME recipient needs the body key")
	}
	kp, _ := base64.StdEncoding.DecodeString(pkg.Addresses["them@example.org"].BodyKeyPacket)
	privKR, _ := crypto.NewKeyRing(rcpt)
	sk, err := privKR.DecryptSessionKey(kp)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := base64.StdEncoding.DecodeString(pkg.Body)
	plain, err := sk.Decrypt(data)
	if err != nil {
		t.Fatal(err)
	}
	body := plain.GetString()
	for _, want := range []string{"text/html", "<b>Ahoj</b>", "a.txt"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q missing in the decrypted body:\n%s", want, body)
		}
	}
}
