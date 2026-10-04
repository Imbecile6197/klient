package pgp

import (
	"strings"
	"testing"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
)

func genKey(t *testing.T, email string, ageSecs int64) string {
	t.Helper()
	crypto.SetKeyGenerationOffset(-ageSecs)
	defer crypto.SetKeyGenerationOffset(0)
	k, err := crypto.GenerateKey("Contact", email, "x25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	arm, err := k.GetArmoredPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return arm
}

func fp(arm string) string {
	k, _ := crypto.NewKeyFromArmored(arm)
	return GroupFingerprint(Details(k).Fingerprint)
}

func TestLearnKey(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	const who = "jan@example.org"
	old, newer, other := genKey(t, who, 3600), genKey(t, who, 60), genKey(t, "else@example.org", 0)

	if LearnKey(who, other, SourceAutocrypt) != KeySame || StoredKey(who) != nil {
		t.Fatal("a key for another address must be ignored")
	}
	if LearnKey(who, old, SourceAutocrypt) != KeyNew || Meta(who).Source != SourceAutocrypt {
		t.Fatal("first key not stored")
	}
	if LearnKey(who, old, SourceAutocrypt) != KeySame {
		t.Fatal("the same key again is no change")
	}
	// An older key never replaces a newer one silently.
	if LearnKey(who, newer, SourceAutocrypt) != KeyReplaced {
		t.Fatal("a newer automatic key should replace an unverified automatic one")
	}
	if m := Meta(who); m.ReplacedFP == "" || fp(newer) != GroupFingerprint(Details(StoredKey(who)).Fingerprint) {
		t.Fatalf("replacement not recorded: %+v", m)
	}
	if LearnKey(who, old, SourceAutocrypt) != KeyPending || PendingKey(who) == nil {
		t.Fatal("an older key must wait for the user")
	}
	if err := RejectPending(who); err != nil || PendingKey(who) != nil {
		t.Fatal("reject failed")
	}

	// A verified key is never replaced; the new one waits.
	if err := SetVerified(who, true); err != nil {
		t.Fatal(err)
	}
	newest := genKey(t, who, 0)
	if LearnKey(who, newest, SourceAutocrypt) != KeyPending {
		t.Fatal("a verified key must not be replaced automatically")
	}
	if err := AcceptPending(who); err != nil {
		t.Fatal(err)
	}
	if m := Meta(who); m.Verified || m.PendingSource != "" || fp(newest) != GroupFingerprint(Details(StoredKey(who)).Fingerprint) {
		t.Fatalf("accept: %+v", m)
	}

	// An imported key is never replaced by Autocrypt either.
	if _, err := ImportKey(old); err != nil {
		t.Fatal(err)
	}
	if LearnKey(who, newest, SourceAutocrypt) != KeyPending {
		t.Fatal("an imported key must not be replaced automatically")
	}
	if got := LocalKeys(); len(got) != 1 || got[0] != who {
		t.Fatalf("LocalKeys = %v (pending keys must not be listed)", got)
	}
	if err := DeleteKey(who); err != nil || PendingKey(who) != nil || Meta(who).Source != "" {
		t.Fatal("delete must remove the pending key and the record")
	}
}

func TestGroupFingerprint(t *testing.T) {
	if got := GroupFingerprint("ABCDEF0123456789"); got != "ABCD EF01 2345 6789" {
		t.Error(got)
	}
}

func TestDecryptInline(t *testing.T) {
	me, _ := crypto.GenerateKey("Me", "me@example.org", "x25519", 0)
	meKR, _ := crypto.NewKeyRing(me)
	mePub, _ := me.ToPublic()
	mePubKR, _ := crypto.NewKeyRing(mePub)
	them, _ := crypto.GenerateKey("Them", "them@example.org", "x25519", 0)
	themKR, _ := crypto.NewKeyRing(them)
	themPub, _ := them.ToPublic()
	themPubKR, _ := crypto.NewKeyRing(themPub)
	other, _ := crypto.GenerateKey("Other", "other@example.org", "x25519", 0)
	otherPub, _ := other.ToPublic()
	otherPubKR, _ := crypto.NewKeyRing(otherPub)

	enc, _ := mePubKR.Encrypt(crypto.NewPlainMessageFromString("tajná zpráva"), themKR)
	arm, _ := enc.GetArmored()
	// Mail clients quote and wrap: a reply with the block quoted.
	quoted := "> " + strings.ReplaceAll(arm, "\n", "\n> ")
	for _, text := range []string{"Ahoj,\n\n" + arm + "\n\nJan", "Ahoj,\n\n" + quoted + "\n"} {
		out, st, ok := DecryptInline(text, meKR, themPubKR)
		if !ok || !strings.Contains(out, "tajná zpráva") || strings.Contains(out, "BEGIN PGP") || st != SigValid {
			t.Fatalf("ok=%v st=%v out=%q", ok, st, out)
		}
	}
	if _, st, ok := DecryptInline(arm, meKR, otherPubKR); !ok || st == SigValid {
		t.Errorf("wrong signer: ok=%v st=%v", ok, st)
	}
	if _, st, ok := DecryptInline(arm, meKR, nil); !ok || st != SigUnknown {
		t.Errorf("no sender key: ok=%v st=%v", ok, st)
	}
	otherKR, _ := crypto.NewKeyRing(other)
	if out, _, ok := DecryptInline(arm, otherKR, nil); ok || out != arm {
		t.Error("a block for someone else must stay as it is")
	}
}
