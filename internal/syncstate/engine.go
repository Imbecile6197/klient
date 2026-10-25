package syncstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Version is one stored copy of the shared state.
type Version struct {
	ID   string
	Data []byte
	Time time.Time // when it was stored (server time)
}

// Backend stores the shared state, as several versions: every computer adds
// a new one and old ones are removed only later, so a write that is cut off
// never loses anything.
type Backend interface {
	Load(ctx context.Context) ([]Version, error)
	Save(ctx context.Context, data []byte) error
	Delete(ctx context.Context, ids []string) error
}

// How many versions are kept, and how long a version is kept at least (so
// that one written by another computer at the same moment is merged before
// it can be removed).
const (
	keepVersions = 5
	keepMinAge   = 10 * time.Minute
)

// How long values are kept: verdicts are only needed while the mail is new;
// removal marks must outlive the longest time a computer is expected to be
// switched off.
var (
	MaxAges = map[string]time.Duration{
		"verdicts": 60 * 24 * time.Hour,
		"usage":    400 * 24 * time.Hour,
		"digest":   30 * 24 * time.Hour,
		"claims":   7 * 24 * time.Hour,
	}
	TombstoneAge = 180 * 24 * time.Hour
)

// Engine keeps the local copy of the shared state and syncs it with the
// backend. Changes made while offline wait in the local copy.
type Engine struct {
	backend Backend
	path    string // local copy
	Clock   *Clock

	mu       sync.Mutex
	st       *State
	pending  bool // local changes not stored yet
	readOnly bool // a newer Klient wrote the shared state
	lastSync time.Time
	lastErr  error
	now      func() time.Time
}

type localFile struct {
	State   *State `json:"state"`
	Pending bool   `json:"pending"`
}

// NewEngine opens the local copy at path (made when missing).
func NewEngine(backend Backend, path, node string) *Engine {
	e := &Engine{backend: backend, path: path, Clock: NewClock(node), st: New(), now: time.Now}
	if b, err := os.ReadFile(path); err == nil {
		var lf localFile
		if decodeJSON(b, &lf) == nil && lf.State != nil {
			e.st, e.pending = lf.State, lf.Pending
			if e.st.Maps == nil {
				e.st.Maps = map[string]Map{}
			}
		}
	}
	e.Clock.Observe(e.st.Newest())
	return e
}

// Read runs f with the current state (not to be changed).
func (e *Engine) Read(f func(s *State)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e.st)
}

// Change runs f on the state with a fresh stamp and keeps the change for
// the next sync.
func (e *Engine) Change(f func(s *State, stamp Stamp)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e.st, e.Clock.Next())
	e.pending = true
	return e.saveLocal()
}

// Status is shown in the preferences.
type Status struct {
	LastSync time.Time
	Err      error
	Pending  bool
	ReadOnly bool
}

func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Status{LastSync: e.lastSync, Err: e.lastErr, Pending: e.pending, ReadOnly: e.readOnly}
}

// Sync merges all stored versions into the local copy and stores the
// result when it holds anything they do not. It returns whether the local
// state changed (values came from other computers).
func (e *Engine) Sync(ctx context.Context) (changed bool, err error) {
	defer func() {
		e.mu.Lock()
		e.lastErr = err
		if err == nil {
			e.lastSync = e.now()
		}
		e.mu.Unlock()
	}()
	versions, err := e.backend.Load(ctx)
	if err != nil {
		return false, err
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Time.After(versions[j].Time) })

	remote := New()
	readOnly := false
	var usable []Version // decoded fine, newest first
	for _, v := range versions {
		s, err := Decode(v.Data)
		if errors.Is(err, ErrNewer) {
			readOnly = true
		} else if err != nil {
			continue // damaged: skipped, removed later with the old ones
		}
		remote.Merge(s)
		usable = append(usable, v)
	}

	e.mu.Lock()
	before := e.st.Clone()
	merged := e.st.Clone()
	merged.Merge(remote)
	merged.Prune(e.now(), MaxAges, TombstoneAge)
	if merged.Format < Format {
		merged.Format = Format
	}
	e.Clock.Observe(merged.Newest())
	e.st = merged
	e.readOnly = readOnly
	changed = !Equal(before, merged)
	snapshot := merged.Clone() // e.st may change while it is being stored
	needSave := e.pending || !Equal(remote, snapshot) || len(usable) == 0
	e.mu.Unlock()

	if readOnly {
		// Use what the newer Klient shared, never overwrite it.
		e.mu.Lock()
		err := e.saveLocal()
		e.mu.Unlock()
		return changed, err
	}
	saved := false
	if needSave {
		data, err := Encode(snapshot)
		if err != nil {
			return changed, err
		}
		if err := e.backend.Save(ctx, data); err != nil {
			e.mu.Lock()
			_ = e.saveLocal()
			e.mu.Unlock()
			return changed, err
		}
		saved = true
		e.mu.Lock()
		// Changes made during the save are newer than what was saved.
		e.pending = !Equal(e.st, snapshot)
		_ = e.saveLocal()
		e.mu.Unlock()
	} else {
		e.mu.Lock()
		e.pending = false
		_ = e.saveLocal()
		e.mu.Unlock()
	}

	// Remove old versions only now that a newer one holds their content
	// (the one just stored counts as one of those kept).
	keep := keepVersions
	if saved {
		keep--
	}
	var old []string
	for i, v := range versions {
		if i >= keep && e.now().Sub(v.Time) > keepMinAge {
			old = append(old, v.ID)
		}
	}
	if len(old) > 0 {
		_ = e.backend.Delete(ctx, old)
	}
	return changed, nil
}

// Versions lists the stored versions (newest first) for restoring one.
func (e *Engine) Versions(ctx context.Context) ([]Version, error) {
	vs, err := e.backend.Load(ctx)
	sort.Slice(vs, func(i, j int) bool { return vs[i].Time.After(vs[j].Time) })
	return vs, err
}

// Restore makes the content of an older version current again: its values
// are stored with fresh stamps, so they win over everything newer.
func (e *Engine) Restore(ctx context.Context, v Version) error {
	old, err := Decode(v.Data)
	if err != nil {
		return err
	}
	e.mu.Lock()
	stamp := e.Clock.Next()
	restored := New()
	for name, m := range e.st.Maps {
		rm := restored.Map(name)
		for k := range m {
			rm[k] = Item{Del: true, S: stamp}
		}
	}
	for name, m := range old.Maps {
		rm := restored.Map(name)
		for k, it := range m {
			if it.Del {
				continue
			}
			rm[k] = Item{V: it.V, S: stamp}
		}
	}
	e.st = restored
	e.pending = true
	err = e.saveLocal()
	e.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = e.Sync(ctx)
	return err
}

// Reset forgets the local copy (turning sync off or starting anew).
func (e *Engine) Reset() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.st, e.pending = New(), false
	err := os.Remove(e.path)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return err
}

// saveLocal writes the local copy atomically. Callers hold e.mu.
func (e *Engine) saveLocal() error {
	b, err := encodeJSON(localFile{State: e.st, Pending: e.pending})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		return err
	}
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}

func encodeJSON(v any) ([]byte, error) { return json.Marshal(v) }
func decodeJSON(b []byte, v any) error { return json.Unmarshal(b, v) }
