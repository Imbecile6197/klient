// Package syncstate keeps what several computers running Klient share: spam
// verdicts, sender lists, settings and the like. Every value carries a
// stamp; merging two states keeps the newer value of each key. Merging is
// commutative, associative and idempotent, so the computers end up with the
// same state whatever order the changes reach them in and however often.
package syncstate

import (
	"bytes"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Format is the version of the stored state this Klient understands. A
// state of a newer format is read but never written over.
const Format = 1

// Stamp orders changes (a hybrid logical clock): wall time in milliseconds,
// a counter for changes within the same millisecond, and the computer that
// made the change, which settles exact ties.
type Stamp struct {
	Wall int64  `json:"w"`
	N    uint32 `json:"n,omitempty"`
	Node string `json:"d"`
}

// Less reports whether a is older than b.
func (a Stamp) Less(b Stamp) bool {
	if a.Wall != b.Wall {
		return a.Wall < b.Wall
	}
	if a.N != b.N {
		return a.N < b.N
	}
	return a.Node < b.Node
}

// Time is the wall time of the stamp.
func (a Stamp) Time() time.Time { return time.UnixMilli(a.Wall) }

// Clock makes stamps that are always newer than any stamp it has made or
// seen, even when the computer's clock goes back or another computer's
// clock is ahead.
type Clock struct {
	mu   sync.Mutex
	node string
	last Stamp
	now  func() time.Time
}

func NewClock(node string) *Clock { return &Clock{node: node, now: time.Now} }

// Next returns a new stamp.
func (c *Clock) Next() Stamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := c.now().UnixMilli()
	s := Stamp{Wall: wall, Node: c.node}
	if wall <= c.last.Wall {
		s.Wall, s.N = c.last.Wall, c.last.N+1
	}
	c.last = s
	return s
}

// Observe moves the clock past a stamp made elsewhere.
func (c *Clock) Observe(s Stamp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last.Less(s) {
		c.last = Stamp{Wall: s.Wall, N: s.N, Node: c.node}
	}
}

// Item is one value; Del marks a removed one (kept so that the removal
// reaches the other computers and the value does not come back).
type Item struct {
	V   json.RawMessage `json:"v,omitempty"`
	Del bool            `json:"x,omitempty"`
	S   Stamp           `json:"s"`
}

// newer reports whether a wins over b.
func (a Item) newer(b Item) bool {
	if a.S != b.S {
		return b.S.Less(a.S)
	}
	// The same stamp with different content cannot come from a correct
	// computer; still decide it the same way everywhere.
	if a.Del != b.Del {
		return a.Del
	}
	return bytes.Compare(a.V, b.V) > 0
}

// Map is a set of keyed values.
type Map map[string]Item

// Merge takes over every value of o that is newer.
func (m Map) Merge(o Map) {
	for k, it := range o {
		if cur, ok := m[k]; !ok || it.newer(cur) {
			m[k] = it
		}
	}
}

// Set stores v under key.
func (m Map) Set(key string, v any, s Stamp) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m[key] = Item{V: b, S: s}
	return nil
}

// Delete removes key.
func (m Map) Delete(key string, s Stamp) { m[key] = Item{Del: true, S: s} }

// Get reads the value under key into v; false when there is none.
func (m Map) Get(key string, v any) bool {
	it, ok := m[key]
	if !ok || it.Del {
		return false
	}
	return json.Unmarshal(it.V, v) == nil
}

// Keys are the keys with a value, sorted.
func (m Map) Keys() []string {
	var out []string
	for k, it := range m {
		if !it.Del {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// State is everything shared, as named maps.
type State struct {
	Format int            `json:"format"`
	Maps   map[string]Map `json:"maps"`
}

func New() *State { return &State{Format: Format, Maps: map[string]Map{}} }

// Map returns the named map, made when missing.
func (s *State) Map(name string) Map {
	if s.Maps == nil {
		s.Maps = map[string]Map{}
	}
	m := s.Maps[name]
	if m == nil {
		m = Map{}
		s.Maps[name] = m
	}
	return m
}

// Merge takes over the newer values of o.
func (s *State) Merge(o *State) {
	if o == nil {
		return
	}
	if o.Format > s.Format {
		s.Format = o.Format
	}
	for name, m := range o.Maps {
		s.Map(name).Merge(m)
	}
}

// Clone is a deep copy.
func (s *State) Clone() *State {
	c := New()
	c.Format = s.Format
	for name, m := range s.Maps {
		cm := c.Map(name)
		for k, it := range m {
			cm[k] = it
		}
	}
	return c
}

// Newest is the newest stamp in the state (for the clock).
func (s *State) Newest() Stamp {
	var n Stamp
	for _, m := range s.Maps {
		for _, it := range m {
			if n.Less(it.S) {
				n = it.S
			}
		}
	}
	return n
}

// Prune forgets values older than the age given for their map (maps not
// listed are kept) and removal marks older than tombstones.
func (s *State) Prune(now time.Time, ages map[string]time.Duration, tombstones time.Duration) {
	for name, m := range s.Maps {
		age, limited := ages[name]
		for k, it := range m {
			old := now.Sub(it.S.Time())
			if (limited && old > age) || (it.Del && old > tombstones) {
				delete(m, k)
			}
		}
	}
}

// Equal reports whether two states hold the same values.
func Equal(a, b *State) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}
