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
	vs, err := ef.Versions(ctx)
	if err != nil || len(vs) == 0 {
		t.Fatalf("versions: %d, %v", len(vs), err)
	}
	for _, v := range vs {
		if _, err := syncstate.Decode(v.Data); err != nil {
			t.Fatalf("stored version unreadable: %v", err)
		}
	}
}
