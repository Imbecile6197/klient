package protonmail

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/go-proton-api/server"
	"github.com/zalando/go-keyring"

	"github.com/Imbecile6197/klient/internal/syncstate"
)

// login signs in to the test server like a computer running Klient.
func login(t *testing.T, s *server.Server) *Account {
	t.Helper()
	m := proton.New(proton.WithHostURL(s.GetHostURL()), proton.WithTransport(proton.InsecureTransport()))
	t.Cleanup(m.Close)
	p, err := Login(context.Background(), m, "jan", []byte("tajne"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.Finish(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Two computers share state through the Proton test server: what one
// stores, the other reads, and the stored mail is readable after Proton's
// encryption.
func TestIsSyncMessage(t *testing.T) {
	for _, c := range []struct {
		m    Summary
		want bool
	}{
		{Summary{Subject: syncSubject}, true},
		{Summary{ExternalID: "abc@klient.local"}, true},
		{Summary{Subject: "Klient – data sdílená mezi vašimi počítači (neměňte ani nemažte)"}, true}, // 0.12.0
		{Summary{Subject: "=?utf-8?q?Klient_=E2=80=93_data_sd=C3=ADlen=C3=A1_mezi_va=C5=A1imi_po?= =?utf-8?q?=C4=8D=C3=ADta=C4=8Di_(nem=C4=9B=C5=88te_ani_nema=C5=BEte)?="}, true},
		{Summary{Subject: "Re: Klient sync"}, false},
		{Summary{Subject: "Faktura", ExternalID: "x@klient.cz"}, false},
	} {
		if got := IsSyncMessage(c.m); got != c.want {
			t.Errorf("%q %q: %v", c.m.Subject, c.m.ExternalID, got)
		}
	}
}

func TestProtonSync(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := server.New()
	defer s.Close()
	if _, _, err := s.CreateUser("jan", []byte("tajne")); err != nil {
		t.Fatal(err)
	}
	fedora, ubuntu := login(t, s), login(t, s)
	dir := t.TempDir()
	ef := syncstate.NewEngine(fedora.SyncBackend(), filepath.Join(dir, "fedora.json"), "fedora")
	eu := syncstate.NewEngine(ubuntu.SyncBackend(), filepath.Join(dir, "ubuntu.json"), "ubuntu")
	ctx := context.Background()

	// Before anything is stored there is nothing to read.
	if _, err := eu.Sync(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	_ = ef.Change(func(st *syncstate.State, s syncstate.Stamp) { _ = st.Map("deny").Set("spam@example.org", true, s) })
	if _, err := ef.Sync(ctx); err != nil {
		t.Fatalf("fedora sync: %v", err)
	}
	changed, err := eu.Sync(ctx)
	if err != nil {
		t.Fatalf("ubuntu sync: %v", err)
	}
	var ok bool
	eu.Read(func(st *syncstate.State) { ok = st.Map("deny").Get("spam@example.org", new(bool)) })
	if !changed || !ok {
		t.Fatal("the sender blocked on Fedora did not reach Ubuntu")
	}

	// And back, with a removal.
	_ = eu.Change(func(st *syncstate.State, s syncstate.Stamp) { st.Map("deny").Delete("spam@example.org", s) })
	if _, err := eu.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ef.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	ef.Read(func(st *syncstate.State) { ok = st.Map("deny").Get("spam@example.org", new(bool)) })
	if ok {
		t.Fatal("the removal made on Ubuntu did not reach Fedora")
	}

	// The folder exists once and holds Klient's versions only.
	labels, err := fedora.client.GetLabels(ctx, proton.LabelTypeFolder)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range labels {
		if l.Name == SyncFolderName {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d sync folders", n)
	}
	// The stored versions are Klient's data, not mail: in no list.
	all, err := fedora.List(ctx, AllMailID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("%d sync messages shown as mail", len(all))
	}
	vs, err := ef.Versions(ctx)
	if err != nil || len(vs) == 0 {
		t.Fatalf("versions: %d, %v", len(vs), err)
	}
	for _, v := range vs {
		if _, err := syncstate.Decode(v.Data); err != nil {
			t.Fatalf("stored version unreadable: %v", err)
		}
	}
	// And they are recognised from their metadata (no notification).
	id, _ := fedora.syncFolder(ctx, false)
	metas, err := fedora.client.GetMessageMetadataPage(ctx, 0, 50, proton.MessageFilter{LabelID: id})
	if err != nil || len(metas) == 0 {
		t.Fatalf("sync folder: %d, %v", len(metas), err)
	}
	for _, m := range metas {
		if !IsSyncMessage(m) {
			t.Fatalf("sync message not recognised: %q", m.ExternalID)
		}
	}
}
