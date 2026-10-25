package shared

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/syncstate"
)

type mem struct {
	mu sync.Mutex
	vs []syncstate.Version
	n  int
}

func (m *mem) Load(context.Context) ([]syncstate.Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]syncstate.Version{}, m.vs...), nil
}
func (m *mem) Save(_ context.Context, d []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	m.vs = append(m.vs, syncstate.Version{ID: fmt.Sprint(m.n), Data: d, Time: time.Now().Add(time.Duration(m.n) * time.Millisecond)})
	return nil
}
func (m *mem) Delete(context.Context, []string) error { return nil }

func hub(t *testing.T, b *mem, dir, node string) *Hub {
	e := syncstate.NewEngine(b, filepath.Join(dir, node+".json"), node)
	return NewHub(e, node)
}

func sync2(t *testing.T, hs ...*Hub) {
	t.Helper()
	for range 2 {
		for _, h := range hs {
			if _, err := h.E.Sync(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestVerdicts(t *testing.T) {
	b, dir := &mem{}, t.TempDir()
	f, u := hub(t, b, dir, "fedora"), hub(t, b, dir, "ubuntu")
	f.PublishVerdict("<ABC@example.org>", Verdict{Spam: false, Prob: 0.05, Source: "ai"})
	sync2(t, f, u)
	v, ok := u.Verdict("abc@example.org")
	if !ok || v.Spam || v.By != "fedora" {
		t.Fatalf("verdict on ubuntu: %+v %v", v, ok)
	}
	if _, ok := u.Verdict(""); ok {
		t.Fatal("empty Message-ID found")
	}
}

// The first computer shares its settings; a computer joining takes them
// over instead of sending its own.
func TestSettingsJoin(t *testing.T) {
	b, dir := &mem{}, t.TempDir()
	f, u := hub(t, b, dir, "fedora"), hub(t, b, dir, "ubuntu")
	cf, cu := config.Default(), config.Default()
	cf.SpamModel, cf.SpamThreshold, cf.Signature = "gemini-3.1-flash", 0.6, "Libor"
	cu.SpamModel, cu.Signature = "something-old", "Old"

	f.ApplySettings(&cf)
	f.SeedSettings(cf)
	sync2(t, f)

	u.ApplySettings(&cu) // nothing shared yet in its local copy
	sync2(t, u)
	u.SeedSettings(cu) // keys exist already: nothing sent
	if !u.ApplySettings(&cu) {
		t.Fatal("joining computer did not take over the settings")
	}
	if cu.SpamModel != "gemini-3.1-flash" || cu.SpamThreshold != 0.6 || cu.Signature != "Libor" {
		t.Fatalf("joined settings: %q %v %q", cu.SpamModel, cu.SpamThreshold, cu.Signature)
	}
	sync2(t, f, u)
	f.ApplySettings(&cf)
	if cf.SpamModel != "gemini-3.1-flash" || cf.Signature != "Libor" {
		t.Fatal("the joining computer overwrote the first one's settings")
	}

	// A change made on Ubuntu reaches Fedora.
	cu.SpamThreshold = 0.8
	u.PushSettings(cu)
	sync2(t, u, f)
	if !f.ApplySettings(&cf) || cf.SpamThreshold != 0.8 {
		t.Fatal("changed setting did not arrive")
	}
	// Fields not shared are not touched.
	if cf.SyncNode != "" || cf.Language != config.Default().Language {
		t.Fatal("a local setting was changed")
	}
}

// A computer that was off and restarts does not send its old settings over
// newer ones: only what the user changes is sent.
func TestSettingsNoStaleOverwrite(t *testing.T) {
	b, dir := &mem{}, t.TempDir()
	f, u := hub(t, b, dir, "fedora"), hub(t, b, dir, "ubuntu")
	cf, cu := config.Default(), config.Default()
	f.ApplySettings(&cf)
	f.SeedSettings(cf)
	sync2(t, f, u)
	u.ApplySettings(&cu)

	// Ubuntu is off; meanwhile Fedora changes the model.
	cf.SpamModel = "new-model"
	f.PushSettings(cf)
	sync2(t, f)

	// Ubuntu starts again (new hub on the same local copy), still with the
	// old value in its config, and the user changes something else.
	u2 := NewHub(syncstate.NewEngine(b, filepath.Join(dir, "ubuntu.json"), "ubuntu"), "ubuntu")
	u2.ApplySettings(&cu) // from its local copy: marks what it had seen
	cu.Signature = "Nový podpis"
	u2.PushSettings(cu)
	sync2(t, u2, f)
	u2.ApplySettings(&cu)
	f.ApplySettings(&cf)
	if cf.SpamModel != "new-model" || cu.SpamModel != "new-model" {
		t.Fatalf("old setting came back: fedora %q, ubuntu %q", cf.SpamModel, cu.SpamModel)
	}
	if cf.Signature != "Nový podpis" {
		t.Fatal("the real change was lost")
	}
}

func lists(allow, deny []string) Lists {
	l := Lists{Allow: map[string]bool{}, Deny: map[string]bool{}, Contacts: map[string]bool{}}
	for _, a := range allow {
		l.Allow[a] = true
	}
	for _, d := range deny {
		l.Deny[d] = true
	}
	return l
}

// Lists merge; a sender removed on one computer stays removed even if the
// other still had it.
func TestLists(t *testing.T) {
	b, dir := &mem{}, t.TempDir()
	f, u := hub(t, b, dir, "fedora"), hub(t, b, dir, "ubuntu")
	f.SharedLists()
	f.SeedLists(lists([]string{"boss@work.cz"}, []string{"spam@x.org", "old@y.org"}))
	sync2(t, f)
	u.SharedLists()
	sync2(t, u)
	u.SeedLists(lists(nil, []string{"other@z.org", "old@y.org"}))
	got := u.SharedLists()
	if !got.Allow["boss@work.cz"] || !got.Deny["spam@x.org"] || !got.Deny["other@z.org"] {
		t.Fatalf("joined lists: %+v", got)
	}
	sync2(t, u, f)

	// Fedora removes old@y.org; Ubuntu (not synced yet) still has it.
	fl := f.SharedLists()
	delete(fl.Deny, "old@y.org")
	f.PushLists(fl)
	sync2(t, f)
	ul := u.SharedLists() // Ubuntu's view before the sync
	ul.Deny["new@q.org"] = true
	u.PushLists(ul) // sends only the addition
	sync2(t, u, f)
	final := f.SharedLists()
	if final.Deny["old@y.org"] {
		t.Fatal("removed sender came back")
	}
	if !final.Deny["new@q.org"] {
		t.Fatal("added sender lost")
	}
	// A restart seeds again: the removed sender, still in Ubuntu's local
	// list, is not sent again because the removal is known.
	u.SeedLists(lists(nil, []string{"old@y.org"}))
	sync2(t, u, f)
	if f.SharedLists().Deny["old@y.org"] {
		t.Fatal("seeding brought a removed sender back")
	}
}

func TestMainComputer(t *testing.T) {
	b, dir := &mem{}, t.TempDir()
	f, u := hub(t, b, dir, "fedora"), hub(t, b, dir, "ubuntu")
	now := time.Unix(1_800_000_000, 0)
	f.now = func() time.Time { return now }
	u.now = func() time.Time { return now }
	if !u.IsMain() {
		t.Fatal("without a main computer every computer must do its own work")
	}
	f.SeedMain()
	f.Heartbeat("Fedora", []string{"Jan@Proton.me"})
	sync2(t, f, u)
	u.SeedMain() // a main exists already
	u.Heartbeat("Ubuntu", []string{"jan@proton.me", "imap:info@firma.cz"})
	sync2(t, u, f)
	if !f.IsMain() || u.IsMain() {
		t.Fatal("wrong main computer")
	}
	if !u.MainAlive() {
		t.Fatal("main computer counts as off")
	}
	// Ubuntu waits for Fedora's verdict only for accounts Fedora has open.
	if !u.MainHas("jan@proton.me") || u.MainHas("imap:info@firma.cz") {
		t.Fatal("wrong accounts of the main computer")
	}
	// A heartbeat right after the last one changes nothing.
	f.Heartbeat("Fedora", []string{"jan@proton.me"})
	if f.E.Status().Pending {
		t.Fatal("needless heartbeat")
	}
	now = now.Add(AliveFor + time.Minute) // Fedora is switched off
	if u.MainAlive() {
		t.Fatal("switched-off main computer counts as running")
	}
	u.SetMain("ubuntu")
	sync2(t, u, f)
	if f.IsMain() || !u.IsMain() {
		t.Fatal("changing the main computer did not reach the other")
	}
	if n := len(f.Computers()); n != 2 {
		t.Fatalf("%d computers", n)
	}
}

func TestUsageAndDigest(t *testing.T) {
	b, dir := &mem{}, t.TempDir()
	f, u := hub(t, b, dir, "fedora"), hub(t, b, dir, "ubuntu")
	f.PushUsage(map[string]map[string]Counts{"2026-10-25": {"gemini\x1fm\x1fspam": {Requests: 3, In: 100, Out: 10}}})
	f.MarkDigest("2026-10-25", "3 nepřečtené zprávy …", 3)
	sync2(t, f, u)
	if d, ok := u.DigestOf("2026-10-25"); !ok || d.N != 3 || d.By != "fedora" || d.Text == "" {
		t.Fatalf("digest on ubuntu: %+v %v", d, ok)
	}
	o := u.OthersUsage()
	if o["fedora"]["2026-10-25"]["gemini\x1fm\x1fspam"].Requests != 3 {
		t.Fatalf("usage: %+v", o)
	}
	if len(f.OthersUsage()) != 0 {
		t.Fatal("own usage counted as another computer's")
	}
	if !u.DigestDone("2026-10-25") || u.DigestDone("2026-10-26") {
		t.Fatal("digest marker wrong")
	}
	// Same numbers again: no change is made (no needless write).
	if f.E.Status().Pending {
		t.Fatal("pending after sync")
	}
	f.PushUsage(map[string]map[string]Counts{"2026-10-25": {"gemini\x1fm\x1fspam": {Requests: 3, In: 100, Out: 10}}})
	if f.E.Status().Pending {
		t.Fatal("unchanged usage made a change")
	}
}
