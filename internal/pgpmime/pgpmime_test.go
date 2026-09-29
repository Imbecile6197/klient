package pgpmime

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
)

func keyring(t *testing.T, email string) *crypto.KeyRing {
	k, err := crypto.GenerateKey("Test", email, "x25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	kr, _ := crypto.NewKeyRing(k)
	return kr
}

func pub(t *testing.T, kr *crypto.KeyRing) *crypto.KeyRing {
	k, _ := kr.GetKey(0)
	p, _ := k.ToPublic()
	out, _ := crypto.NewKeyRing(p)
	return out
}

const entity = "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nP=C5=99=C3=ADli=C5=A1 =C5=BElu=C5=A5ou=C4=8Dk=C3=BD k=C5=AF=C5=88\r\n"

func message(ct string, body []byte) []byte {
	return append([]byte("From: a@x.cz\r\nTo: b@y.cz\r\nSubject: t\r\nMIME-Version: 1.0\r\nContent-Type: "+ct+"\r\n\r\n"), body...)
}

func TestSignVerify(t *testing.T) {
	alice := keyring(t, "a@x.cz")
	ct, body, err := Sign([]byte(entity), alice)
	if err != nil {
		t.Fatal(err)
	}
	raw := message(ct, body)
	if Detect(raw) != Signed {
		t.Fatal("not detected as signed")
	}
	inner, err := Verify(raw, pub(t, alice))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !bytes.Contains(inner, []byte("P=C5=99=C3=ADli=C5=A1")) {
		t.Fatalf("inner %q", inner)
	}
	// Tampering breaks the signature.
	bad := bytes.Replace(raw, []byte("P=C5=99"), []byte("X=C5=99"), 1)
	if _, err := Verify(bad, pub(t, alice)); err == nil {
		t.Fatal("tampered message verified")
	}
	// Unknown sender key: not valid, but the content is still there.
	if in, err := Verify(raw, nil); err == nil || len(in) == 0 {
		t.Fatal("no key must not verify")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	alice, bob := keyring(t, "a@x.cz"), keyring(t, "b@y.cz")
	recips := pub(t, bob)
	ak, _ := alice.GetKey(0)
	apub, _ := ak.ToPublic()
	_ = recips.AddKey(apub) // encrypt to self too
	ct, body, err := Encrypt([]byte(entity), recips, alice)
	if err != nil {
		t.Fatal(err)
	}
	raw := message(ct, body)
	if Detect(raw) != Encrypted || strings.Contains(string(raw), "C5=99") {
		t.Fatal("not encrypted")
	}
	inner, verr, err := Decrypt(raw, bob, pub(t, alice))
	if err != nil || verr != nil {
		t.Fatalf("decrypt %v verify %v", err, verr)
	}
	if !bytes.Contains(inner, []byte("Content-Type: text/plain")) {
		t.Fatalf("inner %q", inner)
	}
	// The sender can read their own copy.
	if _, _, err := Decrypt(raw, alice, nil); err != nil {
		t.Fatalf("self decrypt: %v", err)
	}
	// Someone else cannot.
	if _, _, err := Decrypt(raw, keyring(t, "c@z.cz"), nil); err != ErrNoKey {
		t.Fatalf("stranger: %v", err)
	}
}

func TestAutocrypt(t *testing.T) {
	alice := keyring(t, "a@x.cz")
	k, _ := alice.GetKey(0)
	h, err := AutocryptHeader("a@x.cz", k)
	if err != nil {
		t.Fatal(err)
	}
	armored, ok := ParseAutocrypt(h, "A@x.cz")
	if !ok || !strings.Contains(armored, "PUBLIC KEY") {
		t.Fatal("autocrypt round trip")
	}
	if _, ok := ParseAutocrypt(h, "evil@x.cz"); ok {
		t.Fatal("key accepted for another sender")
	}
}

func TestWKD(t *testing.T) {
	// Example from draft-koch-openpgp-webkey-service.
	u := WKDURLs("Joe.Doe@Example.ORG")
	if len(u) != 2 || !strings.Contains(u[0], "iy9q119eutrkn8s1mk4r39qejnbu3n5q") ||
		!strings.HasPrefix(u[0], "https://openpgpkey.example.org/.well-known/openpgpkey/example.org/hu/") ||
		!strings.HasSuffix(u[0], "?l=Joe.Doe") {
		t.Fatalf("%v", u)
	}
}
