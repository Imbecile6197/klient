package pgp

import (
	"testing"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/zalando/go-keyring"
)

func TestOwnKeyExpiryAndRevocation(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	const me = "jan@example.cz"
	if err := GenerateOwnKeyFor("Jan", me, 2); err != nil {
		t.Fatal(err)
	}
	expires := func() time.Time {
		_, k, err := OwnPublicKey(me)
		if err != nil {
			t.Fatal(err)
		}
		return Details(k).Expires
	}
	near := func(got, want time.Time) bool { d := got.Sub(want); return d > -48*time.Hour && d < 48*time.Hour }
	if e := expires(); !near(e, time.Now().AddDate(2, 0, 0)) {
		t.Fatalf("new key expires %v", e)
	}
	if err := SetOwnExpiry(me, 5); err != nil {
		t.Fatal(err)
	}
	if e := expires(); !near(e, time.Now().AddDate(5, 0, 0)) {
		t.Fatalf("extended key expires %v", e)
	}
	if err := SetOwnExpiry(me, 0); err != nil {
		t.Fatal(err)
	}
	if e := expires(); !e.IsZero() {
		t.Fatalf("key should not expire, got %v", e)
	}
	// The re-signed key still encrypts and decrypts.
	kr, _ := OwnKeyRing(me)
	pubArm, _, _ := OwnPublicKey(me)
	pub, _ := crypto.NewKeyFromArmored(pubArm)
	pubKR, _ := crypto.NewKeyRing(pub)
	enc, err := pubKR.Encrypt(crypto.NewPlainMessageFromString("ahoj"), kr)
	if err != nil {
		t.Fatal(err)
	}
	if dec, err := kr.Decrypt(enc, pubKR, crypto.GetUnixTime()); err != nil || dec.GetString() != "ahoj" {
		t.Fatalf("after extension: %v", err)
	}

	cert, err := RevocationCertificate(me)
	if err != nil {
		t.Fatal(err)
	}
	rk, err := crypto.NewKeyFromArmored(cert)
	if err != nil || !rk.IsRevoked() || rk.IsPrivate() {
		t.Fatalf("revocation certificate: err=%v revoked=%v", err, rk != nil && rk.IsRevoked())
	}
	if _, k, _ := OwnPublicKey(me); k.IsRevoked() {
		t.Fatal("making the certificate must not revoke the key in use")
	}
}
