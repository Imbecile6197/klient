// Package shared maps what Klient keeps on one computer (spam verdicts,
// sender lists, settings, AI statistics) to the state shared with the
// user's other computers (package syncstate), and back.
//
// Local changes are sent as changes only: settings and lists are compared
// with what was last applied or sent, so a computer that was switched off
// for a while does not send its old values over newer ones.
package shared

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/syncstate"
)

// Names of the shared maps.
const (
	mVerdicts  = "verdicts"
	mAllow     = "allow"
	mDeny      = "deny"
	mContacts  = "contacts"
	mSettings  = "settings"
	mUsage     = "usage"
	mComputers = "computers"
	mRoles     = "roles"
	mDigest    = "digest"
)

// Settings shared between the computers (config.json keys). API keys and
// passwords are not settings and never leave the computer.
var Settings = []string{
	"rules",
	"signature", "signature_html", "signature_use_html",
	"spam_filter_enabled", "spam_provider", "spam_model", "spam_threshold", "spam_body_chars", "hold_until_checked",
	"assistant_provider", "assistant_model", "role_models",
	"auto_label", "digest", "digest_hour", "precompute_summaries", "ai_prices",
}

// Verdict is the spam filter's verdict on a message, by its Message-ID.
type Verdict struct {
	Spam     bool    `json:"spam"`
	Prob     float64 `json:"p,omitempty"`
	Source   string  `json:"src,omitempty"`
	Category string  `json:"cat,omitempty"`
	By       string  `json:"by,omitempty"` // computer
}

// Computer is one of the user's computers running Klient.
type Computer struct {
	ID       string   `json:"-"`
	Name     string   `json:"name"`
	Seen     int64    `json:"seen"`               // unix seconds of its last sign of life
	Accounts []string `json:"accounts,omitempty"` // accounts open there (lower case)
}

// Lists are the spam filter's sender lists.
type Lists struct {
	Allow, Deny, Contacts map[string]bool
}

// How often a computer tells the others it runs, and after how long without
// that it counts as off.
const (
	HeartbeatEvery = 15 * time.Minute
	AliveFor       = 40 * time.Minute
)

// Hub is this computer's view of the shared state.
type Hub struct {
	E    *syncstate.Engine
	Node string

	mu           sync.Mutex
	seenSettings map[string]string // key -> JSON last applied or sent
	seenLists    map[string]map[string]bool
	now          func() time.Time
}

func NewHub(e *syncstate.Engine, node string) *Hub {
	return &Hub{E: e, Node: node, seenSettings: map[string]string{}, seenLists: map[string]map[string]bool{}, now: time.Now}
}

// NormalizeID makes a Message-ID comparable ("<A@x>" -> "a@x").
func NormalizeID(id string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(id), "<>"))
}

// ---- verdicts ------------------------------------------------------------

func (h *Hub) PublishVerdict(messageID string, v Verdict) {
	id := NormalizeID(messageID)
	if id == "" {
		return
	}
	v.By = h.Node
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) { _ = s.Map(mVerdicts).Set(id, v, st) })
}

func (h *Hub) Verdict(messageID string) (v Verdict, ok bool) {
	id := NormalizeID(messageID)
	if id == "" {
		return v, false
	}
	h.E.Read(func(s *syncstate.State) { ok = s.Map(mVerdicts).Get(id, &v) })
	return v, ok
}

// ---- settings ------------------------------------------------------------

func settingsOf(cfg config.Config) map[string]string {
	b, _ := json.Marshal(cfg)
	var all map[string]json.RawMessage
	_ = json.Unmarshal(b, &all)
	out := map[string]string{}
	for _, k := range Settings {
		if v, ok := all[k]; ok {
			out[k] = string(v)
		} else {
			out[k] = "null" // omitted empty value
		}
	}
	return out
}

// SeedSettings shares the settings the shared state does not have yet
// (the first computer to switch sync on). Others are left alone.
func (h *Hub) SeedSettings(cfg config.Config) {
	local := settingsOf(cfg)
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		m := s.Map(mSettings)
		for k, v := range local {
			if _, known := m[k]; !known {
				m[k] = syncstate.Item{V: json.RawMessage(v), S: st}
			}
		}
	})
}

// PushSettings sends the settings the user changed on this computer.
func (h *Hub) PushSettings(cfg config.Config) {
	local := settingsOf(cfg)
	h.mu.Lock()
	changed := map[string]string{}
	for k, v := range local {
		if h.seenSettings[k] != v {
			changed[k] = v
			h.seenSettings[k] = v
		}
	}
	h.mu.Unlock()
	if len(changed) == 0 {
		return
	}
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		m := s.Map(mSettings)
		for k, v := range changed {
			m[k] = syncstate.Item{V: json.RawMessage(v), S: st}
		}
	})
}

// ApplySettings puts the shared settings into cfg; it reports whether cfg
// changed.
func (h *Hub) ApplySettings(cfg *config.Config) bool {
	shared := map[string]json.RawMessage{}
	h.E.Read(func(s *syncstate.State) {
		for k, it := range s.Map(mSettings) {
			if !it.Del {
				shared[k] = it.V
			}
		}
	})
	local := settingsOf(*cfg)
	b, _ := json.Marshal(*cfg)
	var all map[string]json.RawMessage
	_ = json.Unmarshal(b, &all)
	changed := false
	h.mu.Lock()
	for _, k := range Settings {
		v, ok := shared[k]
		if !ok {
			// Not shared yet: what this computer has counts as seen.
			if _, seen := h.seenSettings[k]; !seen {
				h.seenSettings[k] = local[k]
			}
			continue
		}
		h.seenSettings[k] = string(v)
		if local[k] != string(v) {
			all[k] = v
			changed = true
		}
	}
	h.mu.Unlock()
	if !changed {
		return false
	}
	nb, err := json.Marshal(all)
	if err != nil {
		return false
	}
	var out config.Config
	if json.Unmarshal(nb, &out) != nil {
		return false
	}
	*cfg = out
	return true
}

// ---- sender lists --------------------------------------------------------

func listMaps(l Lists) map[string]map[string]bool {
	return map[string]map[string]bool{mAllow: l.Allow, mDeny: l.Deny, mContacts: l.Contacts}
}

// SeedLists adds this computer's entries the shared lists do not know
// (also not as removed): a computer joining brings its own senders.
func (h *Hub) SeedLists(l Lists) {
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		for name, local := range listMaps(l) {
			m := s.Map(name)
			for k := range local {
				k = strings.ToLower(k)
				if _, known := m[k]; !known {
					_ = m.Set(k, true, st)
				}
			}
		}
	})
}

// PushLists sends what the user added or removed on this computer.
func (h *Hub) PushLists(l Lists) {
	type change struct {
		name, key string
		del       bool
	}
	var changes []change
	h.mu.Lock()
	for name, local := range listMaps(l) {
		seen := h.seenLists[name]
		for k := range local {
			if !seen[k] {
				changes = append(changes, change{name, k, false})
			}
		}
		for k := range seen {
			if !local[k] {
				changes = append(changes, change{name, k, true})
			}
		}
		h.seenLists[name] = copySet(local)
	}
	h.mu.Unlock()
	if len(changes) == 0 {
		return
	}
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		for _, c := range changes {
			if c.del {
				s.Map(c.name).Delete(c.key, st)
			} else {
				_ = s.Map(c.name).Set(c.key, true, st)
			}
		}
	})
}

// SharedLists are the lists as shared; they also become the seen ones.
func (h *Hub) SharedLists() Lists {
	out := Lists{Allow: map[string]bool{}, Deny: map[string]bool{}, Contacts: map[string]bool{}}
	h.E.Read(func(s *syncstate.State) {
		for name, dst := range listMaps(out) {
			for _, k := range s.Map(name).Keys() {
				dst[k] = true
			}
		}
	})
	h.mu.Lock()
	for name, m := range listMaps(out) {
		h.seenLists[name] = copySet(m)
	}
	h.mu.Unlock()
	return out
}

func copySet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		if v {
			out[k] = true
		}
	}
	return out
}

// ---- AI statistics -------------------------------------------------------

// Counts mirror usage.Counts (kept here to avoid an import cycle).
type Counts struct {
	Requests int64 `json:"requests"`
	In       int64 `json:"in"`
	Cached   int64 `json:"cached,omitempty"`
	Out      int64 `json:"out"`
}

// PushUsage shares this computer's days (only those that changed).
func (h *Hub) PushUsage(days map[string]map[string]Counts) {
	type upd struct {
		key  string
		data map[string]Counts
	}
	var updates []upd
	h.E.Read(func(s *syncstate.State) {
		m := s.Map(mUsage)
		for day, c := range days {
			key := h.Node + "|" + day
			var cur map[string]Counts
			if m.Get(key, &cur) && sameCounts(cur, c) {
				continue
			}
			updates = append(updates, upd{key, c})
		}
	})
	if len(updates) == 0 {
		return
	}
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		for _, u := range updates {
			_ = s.Map(mUsage).Set(u.key, u.data, st)
		}
	})
}

func sameCounts(a, b map[string]Counts) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// OthersUsage are the other computers' days: computer -> day -> key.
func (h *Hub) OthersUsage() map[string]map[string]map[string]Counts {
	out := map[string]map[string]map[string]Counts{}
	h.E.Read(func(s *syncstate.State) {
		m := s.Map(mUsage)
		for _, key := range m.Keys() {
			node, day, ok := strings.Cut(key, "|")
			if !ok || node == h.Node {
				continue
			}
			var c map[string]Counts
			if !m.Get(key, &c) {
				continue
			}
			if out[node] == nil {
				out[node] = map[string]map[string]Counts{}
			}
			out[node][day] = c
		}
	})
	return out
}

// ---- computers and the main one -------------------------------------------

// Heartbeat tells the others this computer runs and which accounts it has
// open (at most every HeartbeatEvery, or when something changed).
func (h *Hub) Heartbeat(name string, accounts []string) {
	now := h.now()
	accs := make([]string, 0, len(accounts))
	for _, a := range accounts {
		accs = append(accs, strings.ToLower(a))
	}
	sort.Strings(accs)
	var cur Computer
	var known bool
	h.E.Read(func(s *syncstate.State) { known = s.Map(mComputers).Get(h.Node, &cur) })
	if known && cur.Name == name && strings.Join(cur.Accounts, "\n") == strings.Join(accs, "\n") &&
		now.Sub(time.Unix(cur.Seen, 0)) < HeartbeatEvery {
		return
	}
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		_ = s.Map(mComputers).Set(h.Node, Computer{Name: name, Seen: now.Unix(), Accounts: accs}, st)
	})
}

// MainHas reports whether the main computer has the account open (only
// then is it worth waiting for its verdict).
func (h *Hub) MainHas(account string) bool {
	m := h.Main()
	var c Computer
	var ok bool
	h.E.Read(func(s *syncstate.State) { ok = s.Map(mComputers).Get(m, &c) })
	if !ok {
		return false
	}
	account = strings.ToLower(account)
	for _, a := range c.Accounts {
		if a == account {
			return true
		}
	}
	return false
}

// Computers lists the known computers, by name.
func (h *Hub) Computers() []Computer {
	var out []Computer
	h.E.Read(func(s *syncstate.State) {
		m := s.Map(mComputers)
		for _, id := range m.Keys() {
			var c Computer
			if m.Get(id, &c) {
				c.ID = id
				out = append(out, c)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Forget removes a computer that no longer runs Klient.
func (h *Hub) Forget(id string) {
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) { s.Map(mComputers).Delete(id, st) })
}

// Main is the computer doing the AI work ("" = none chosen).
func (h *Hub) Main() string {
	var id string
	h.E.Read(func(s *syncstate.State) { s.Map(mRoles).Get("main", &id) })
	return id
}

func (h *Hub) SetMain(id string) {
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) { _ = s.Map(mRoles).Set("main", id, st) })
}

// SeedMain makes this computer the main one when none is chosen yet.
func (h *Hub) SeedMain() {
	if h.Main() == "" {
		h.SetMain(h.Node)
	}
}

// IsMain reports whether this computer does the AI work.
func (h *Hub) IsMain() bool {
	m := h.Main()
	return m == "" || m == h.Node
}

// MainAlive reports whether another computer is the main one and runs.
func (h *Hub) MainAlive() bool {
	m := h.Main()
	if m == "" || m == h.Node {
		return false
	}
	var c Computer
	var ok bool
	h.E.Read(func(s *syncstate.State) { ok = s.Map(mComputers).Get(m, &c) })
	return ok && h.now().Sub(time.Unix(c.Seen, 0)) < AliveFor
}

// ---- once-a-day work -----------------------------------------------------

// Digest is the morning overview made by one computer for all of them.
type Digest struct {
	By   string `json:"by"`
	Text string `json:"text,omitempty"`
	N    int    `json:"n"` // unread messages summarised
}

// DigestOf returns the overview of day, if a computer made it.
func (h *Hub) DigestOf(day string) (d Digest, ok bool) {
	h.E.Read(func(s *syncstate.State) { ok = s.Map(mDigest).Get(day, &d) })
	return d, ok
}

// DigestDone reports whether a computer already made the overview of day.
func (h *Hub) DigestDone(day string) bool {
	_, ok := h.DigestOf(day)
	return ok
}

// MarkDigest shares the overview of day.
func (h *Hub) MarkDigest(day, text string, n int) {
	_ = h.E.Change(func(s *syncstate.State, st syncstate.Stamp) {
		_ = s.Map(mDigest).Set(day, Digest{By: h.Node, Text: text, N: n}, st)
	})
}
