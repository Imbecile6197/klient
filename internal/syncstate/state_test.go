package syncstate

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---- merge ---------------------------------------------------------------

// randomState makes a state from random changes of a few computers.
func randomState(r *rand.Rand, nodes []string) *State {
	s := New()
	for range r.Intn(30) {
		m := s.Map([]string{"a", "b", "c"}[r.Intn(3)])
		key := fmt.Sprint("k", r.Intn(8))
		st := Stamp{Wall: int64(r.Intn(20)), N: uint32(r.Intn(2)), Node: nodes[r.Intn(len(nodes))]}
		if r.Intn(4) == 0 {
			m.Delete(key, st)
		} else {
			_ = m.Set(key, r.Intn(5), st)
		}
	}
	return s
}

func merged(states ...*State) *State {
	out := New()
	for _, s := range states {
		out.Merge(s.Clone())
	}
	return out
}

// Merging must not depend on order, grouping or repetition: then all
// computers end with the same state.
func TestMergeLaws(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	nodes := []string{"fedora", "ubuntu", "mac"}
	for i := range 3000 {
		a, b, c := randomState(r, nodes), randomState(r, nodes), randomState(r, nodes)
		if !Equal(merged(a, b), merged(b, a)) {
			t.Fatalf("case %d: not commutative", i)
		}
		if !Equal(merged(merged(a, b), c), merged(a, merged(b, c))) {
			t.Fatalf("case %d: not associative", i)
		}
		if !Equal(merged(a, a), merged(a)) || !Equal(merged(a, b, a, b), merged(a, b)) {
			t.Fatalf("case %d: not idempotent", i)
		}
	}
}

// The newest change of a key wins, removals included.
func TestNewestWins(t *testing.T) {
	a, b := New(), New()
	_ = a.Map("deny").Set("spam@example.org", true, Stamp{Wall: 10, Node: "fedora"})
	b.Map("deny").Delete("spam@example.org", Stamp{Wall: 20, Node: "ubuntu"})
	for _, m := range []*State{merged(a, b), merged(b, a)} {
		if m.Map("deny").Get("spam@example.org", new(bool)) {
			t.Fatal("removed sender came back")
		}
	}
	_ = a.Map("deny").Set("spam@example.org", true, Stamp{Wall: 30, Node: "fedora"})
	if !merged(a, b).Map("deny").Get("spam@example.org", new(bool)) {
		t.Fatal("sender added again after the removal is missing")
	}
}

// ---- clock ---------------------------------------------------------------

func TestClock(t *testing.T) {
	now := time.UnixMilli(1000)
	c := NewClock("fedora")
	c.now = func() time.Time { return now }
	a := c.Next()
	now = time.UnixMilli(500) // the computer's clock went back
	b := c.Next()
	if !a.Less(b) {
		t.Fatalf("%v not before %v", a, b)
	}
	c.Observe(Stamp{Wall: 9000, Node: "ubuntu"}) // another computer is ahead
	d := c.Next()
	if d.Wall < 9000 || !b.Less(d) {
		t.Fatalf("stamp %v not after the observed one", d)
	}
}

// ---- stored form ---------------------------------------------------------

func TestEncodeDecode(t *testing.T) {
	s := New()
	for i := range 500 {
		_ = s.Map("verdicts").Set(fmt.Sprintf("<%d@example.org>", i), map[string]any{"spam": i%3 == 0}, Stamp{Wall: int64(i), Node: "fedora"})
	}
	data, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	// Inside a mail body with text around it.
	body := append([]byte("Klient keeps its shared data here.\r\n\r\n"), data...)
	body = append(body, []byte("\r\n-- \r\nKlient\r\n")...)
	got, err := Decode(body)
	if err != nil || !Equal(got, s) {
		t.Fatalf("round trip: %v", err)
	}

	broken := append([]byte{}, data...)
	broken[len(broken)/2] ^= 0x01
	if _, err := Decode(broken); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("damaged copy: err = %v", err)
	}
	if _, err := Decode(data[:len(data)/2]); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("cut copy: err = %v", err)
	}
	if _, err := Decode([]byte("hello")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("foreign text: err = %v", err)
	}
}

// ---- engine --------------------------------------------------------------

// memBackend is the shared storage, with faults to switch on.
type memBackend struct {
	mu       sync.Mutex
	versions []Version
	n        int
	now      func() time.Time

	failLoad, failSave, cutSave bool
	saves, deletes              int
}

func (m *memBackend) Load(context.Context) ([]Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failLoad {
		return nil, errors.New("offline")
	}
	return append([]Version{}, m.versions...), nil
}

func (m *memBackend) Save(_ context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSave {
		return errors.New("offline")
	}
	m.n++
	m.saves++
	if m.cutSave { // the connection dropped in the middle of the write
		data = data[:len(data)/3]
	}
	m.versions = append(m.versions, Version{ID: fmt.Sprint(m.n), Data: append([]byte{}, data...), Time: m.now().Add(time.Duration(m.n) * time.Millisecond)})
	if m.cutSave {
		return errors.New("connection dropped")
	}
	return nil
}

func (m *memBackend) Delete(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	var keep []Version
	for _, v := range m.versions {
		if !drop[v.ID] {
			keep = append(keep, v)
		}
	}
	m.deletes += len(m.versions) - len(keep)
	m.versions = keep
	return nil
}

func newEngines(t *testing.T, b *memBackend, names ...string) []*Engine {
	var out []*Engine
	dir := t.TempDir()
	for _, n := range names {
		out = append(out, NewEngine(b, filepath.Join(dir, n+".json"), n))
	}
	return out
}

func set(e *Engine, m, k string, v any) {
	_ = e.Change(func(s *State, st Stamp) { _ = s.Map(m).Set(k, v, st) })
}

func del(e *Engine, m, k string) {
	_ = e.Change(func(s *State, st Stamp) { s.Map(m).Delete(k, st) })
}

func value(e *Engine, m, k string) (v int, ok bool) {
	e.Read(func(s *State) { ok = s.Map(m).Get(k, &v) })
	return
}

func sameStates(t *testing.T, engines []*Engine) {
	t.Helper()
	var first *State
	for _, e := range engines {
		e.Read(func(s *State) {
			if first == nil {
				first = s.Clone()
			} else if !Equal(first, s) {
				t.Fatal("computers ended with different states")
			}
		})
	}
}

// Random work on three computers with faults on the way: the computers end
// with the same state, and every key holds its newest value.
func TestEnginesConverge(t *testing.T) {
	for seed := range int64(40) {
		r := rand.New(rand.NewSource(seed))
		clock := time.Unix(1_800_000_000, 0)
		b := &memBackend{now: func() time.Time { return clock }}
		es := newEngines(t, b, "fedora", "ubuntu", "mac")
		for _, e := range es {
			e.now = func() time.Time { return clock }
			e.Clock.now = func() time.Time { return clock }
		}
		type last struct {
			v   int
			del bool
			st  Stamp
		}
		want := map[string]last{}
		for range 300 {
			clock = clock.Add(time.Duration(r.Intn(3000)) * time.Millisecond)
			e := es[r.Intn(len(es))]
			b.mu.Lock()
			b.failLoad, b.failSave, b.cutSave = r.Intn(10) == 0, r.Intn(10) == 0, r.Intn(15) == 0
			b.mu.Unlock()
			key := fmt.Sprint("k", r.Intn(10))
			switch r.Intn(4) {
			case 0, 1:
				v := r.Intn(100)
				_ = e.Change(func(s *State, st Stamp) {
					_ = s.Map("m").Set(key, v, st)
					if w, ok := want[key]; !ok || w.st.Less(st) {
						want[key] = last{v: v, st: st}
					}
				})
			case 2:
				_ = e.Change(func(s *State, st Stamp) {
					s.Map("m").Delete(key, st)
					if w, ok := want[key]; !ok || w.st.Less(st) {
						want[key] = last{del: true, st: st}
					}
				})
			default:
				_, _ = e.Sync(context.Background())
			}
		}
		b.mu.Lock()
		b.failLoad, b.failSave, b.cutSave = false, false, false
		b.mu.Unlock()
		for range 2 {
			for _, e := range es {
				if _, err := e.Sync(context.Background()); err != nil {
					t.Fatalf("seed %d: final sync: %v", seed, err)
				}
			}
		}
		sameStates(t, es)
		for k, w := range want {
			v, ok := value(es[0], "m", k)
			if w.del && ok || !w.del && (!ok || v != w.v) {
				t.Fatalf("seed %d: key %s = %d,%v; want %d deleted=%v", seed, k, v, ok, w.v, w.del)
			}
		}
	}
}

// Two computers syncing at the same moment, many times: nothing is lost.
func TestEnginesConcurrent(t *testing.T) {
	b := &memBackend{now: time.Now}
	es := newEngines(t, b, "fedora", "ubuntu")
	var wg sync.WaitGroup
	for i, e := range es {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 100 {
				set(e, "m", fmt.Sprintf("%d-%d", i, j), j)
				_, _ = e.Sync(context.Background())
			}
		}()
	}
	wg.Wait()
	for range 2 {
		for _, e := range es {
			if _, err := e.Sync(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	sameStates(t, es)
	es[0].Read(func(s *State) {
		if n := len(s.Map("m").Keys()); n != 200 {
			t.Fatalf("%d of 200 values", n)
		}
	})
}

// A change made without a connection waits, survives a restart and is
// stored once the connection is back.
func TestOfflineChangeWaits(t *testing.T) {
	b := &memBackend{now: time.Now, failLoad: true}
	dir := t.TempDir()
	e := NewEngine(b, filepath.Join(dir, "fedora.json"), "fedora")
	set(e, "deny", "spam@example.org", true)
	if _, err := e.Sync(context.Background()); err == nil {
		t.Fatal("sync without a connection reported no error")
	}
	if !e.Status().Pending {
		t.Fatal("the change is not marked as waiting")
	}
	again := NewEngine(b, filepath.Join(dir, "fedora.json"), "fedora") // Klient restarted
	b.failLoad = false
	if _, err := again.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	other := newEngines(t, b, "ubuntu")[0]
	if _, err := other.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	var ok bool
	other.Read(func(s *State) { ok = s.Map("deny").Get("spam@example.org", new(bool)) })
	if !ok {
		t.Fatal("the change made offline never arrived")
	}
}

// A damaged version (a write cut off) is skipped; the others are used.
func TestDamagedVersionSkipped(t *testing.T) {
	b := &memBackend{now: time.Now}
	es := newEngines(t, b, "fedora", "ubuntu")
	set(es[0], "m", "a", 1)
	if _, err := es[0].Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.cutSave = true
	set(es[0], "m", "b", 2)
	if _, err := es[0].Sync(context.Background()); err == nil {
		t.Fatal("cut write reported no error")
	}
	b.cutSave = false
	if _, err := es[1].Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, ok := value(es[1], "m", "a"); !ok || v != 1 {
		t.Fatal("good version not used next to a damaged one")
	}
	if _, err := es[0].Sync(context.Background()); err != nil { // retries the cut write
		t.Fatal(err)
	}
	if _, err := es[1].Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, ok := value(es[1], "m", "b"); !ok || v != 2 {
		t.Fatal("the change whose write was cut off never arrived")
	}
}

// Data of a newer Klient is used but never written over.
func TestNewerFormatNotOverwritten(t *testing.T) {
	b := &memBackend{now: time.Now}
	newer := New()
	newer.Format = Format + 1
	_ = newer.Map("m").Set("a", 1, Stamp{Wall: 1, Node: "new"})
	data, _ := Encode(newer)
	b.versions = []Version{{ID: "x", Data: data, Time: time.Now()}}
	e := newEngines(t, b, "fedora")[0]
	set(e, "m", "b", 2)
	if _, err := e.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.saves != 0 {
		t.Fatal("wrote over the data of a newer Klient")
	}
	if !e.Status().ReadOnly {
		t.Fatal("not marked read-only")
	}
	if v, ok := value(e, "m", "a"); !ok || v != 1 {
		t.Fatal("did not use the newer data")
	}
}

// Old versions are removed only when newer ones exist and they are old
// enough; nothing is removed when the save failed.
func TestOldVersionsRemoved(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	b := &memBackend{now: func() time.Time { return clock }}
	e := newEngines(t, b, "fedora")[0]
	e.now = func() time.Time { return clock }
	for i := range 20 {
		set(e, "m", fmt.Sprint(i), i)
		if _, err := e.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Minute)
	}
	if n := len(b.versions); n > keepVersions+11 {
		t.Fatalf("%d versions kept", n)
	}
	clock = clock.Add(time.Hour)
	set(e, "m", "last", 1)
	if _, err := e.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(b.versions); n != keepVersions {
		t.Fatalf("%d versions kept, want %d", n, keepVersions)
	}
	// The newest version holds everything.
	s, err := Decode(b.versions[len(b.versions)-1].Data)
	if err != nil || len(s.Map("m").Keys()) != 21 {
		t.Fatal("newest version incomplete")
	}
}

// Nothing changed: no new version is written (no needless traffic).
func TestQuietSyncWritesNothing(t *testing.T) {
	b := &memBackend{now: time.Now}
	e := newEngines(t, b, "fedora")[0]
	set(e, "m", "a", 1)
	if _, err := e.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	saves := b.saves
	for range 5 {
		if changed, err := e.Sync(context.Background()); err != nil || changed {
			t.Fatalf("quiet sync: changed=%v err=%v", changed, err)
		}
	}
	if b.saves != saves {
		t.Fatalf("%d needless writes", b.saves-saves)
	}
}

// Restoring an older version brings its values back on every computer.
func TestRestore(t *testing.T) {
	b := &memBackend{now: time.Now}
	es := newEngines(t, b, "fedora", "ubuntu")
	set(es[0], "deny", "a@example.org", 1)
	_, _ = es[0].Sync(context.Background())
	vs, _ := es[0].Versions(context.Background())
	good := vs[0]
	del(es[0], "deny", "a@example.org")
	set(es[0], "deny", "b@example.org", 1)
	_, _ = es[0].Sync(context.Background())
	if err := es[0].Restore(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	_, _ = es[1].Sync(context.Background())
	if _, ok := value(es[1], "deny", "a@example.org"); !ok {
		t.Fatal("restored value missing")
	}
	if _, ok := value(es[1], "deny", "b@example.org"); ok {
		t.Fatal("value newer than the restored version still there")
	}
}
