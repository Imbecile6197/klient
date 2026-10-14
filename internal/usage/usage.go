// Package usage keeps statistics of the AI requests: per day, provider,
// model and task, how many requests and tokens – never any content. The
// costs are computed from the price list when shown.
package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
)

// Counts add up requests and tokens.
type Counts struct {
	Requests int64 `json:"requests"`
	In       int64 `json:"in"`
	Cached   int64 `json:"cached,omitempty"`
	Out      int64 `json:"out"`
}

func (c *Counts) add(o Counts) {
	c.Requests += o.Requests
	c.In += o.In
	c.Cached += o.Cached
	c.Out += o.Out
}

// Store is the statistics file.
type Store struct {
	mu   sync.Mutex
	path string
	// Day ("2006-01-02") -> "provider\x1fmodel\x1ftask" -> counts.
	Days map[string]map[string]*Counts `json:"days"`
	// OnChange is called after each new record (not on the UI thread).
	OnChange func() `json:"-"`
}

// Path is where Klient keeps the statistics.
func Path() string { return filepath.Join(config.DataDir(), "ai-usage.json") }

// Open loads the statistics (an empty store when the file is missing).
func Open(path string) *Store {
	s := &Store{path: path, Days: map[string]map[string]*Counts{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, s)
		if s.Days == nil {
			s.Days = map[string]map[string]*Counts{}
		}
	}
	return s
}

func key(provider, model, task string) string {
	return provider + "\x1f" + model + "\x1f" + task
}

// Add records one request and saves the file.
func (s *Store) Add(r ai.Record) {
	s.mu.Lock()
	day := r.Time.Local().Format("2006-01-02")
	m := s.Days[day]
	if m == nil {
		m = map[string]*Counts{}
		s.Days[day] = m
	}
	k := key(r.Provider, r.Model, r.Task)
	if m[k] == nil {
		m[k] = &Counts{}
	}
	m[k].add(Counts{Requests: 1, In: r.In, Cached: r.Cached, Out: r.Out})
	b, err := json.Marshal(s)
	s.mu.Unlock()
	if err == nil {
		if os.MkdirAll(filepath.Dir(s.path), 0o700) == nil {
			tmp := s.path + ".part"
			if os.WriteFile(tmp, b, 0o600) == nil {
				_ = os.Rename(tmp, s.path)
			}
		}
	}
	if s.OnChange != nil {
		s.OnChange()
	}
}

// Line is one provider, model and task in a period, with its cost.
type Line struct {
	Provider, Model, Task string
	Counts
	Cost   float64 // USD
	Priced bool    // false: the price of the model is not known
}

// Lines sums the days from..to (inclusive, "2006-01-02"), the most
// expensive first.
func (s *Store) Lines(from, to string, prices map[string]config.ModelPrice) []Line {
	s.mu.Lock()
	sum := map[string]*Counts{}
	for day, m := range s.Days {
		if day < from || day > to {
			continue
		}
		for k, c := range m {
			if sum[k] == nil {
				sum[k] = &Counts{}
			}
			sum[k].add(*c)
		}
	}
	s.mu.Unlock()
	var out []Line
	for k, c := range sum {
		parts := strings.SplitN(k, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		l := Line{Provider: parts[0], Model: parts[1], Task: parts[2], Counts: *c}
		if p, ok := ai.PriceFor(l.Provider, l.Model, prices); ok {
			l.Priced = true
			l.Cost = ai.Cost(ai.Usage{In: c.In, Cached: c.Cached, Out: c.Out}, p)
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Requests > out[j].Requests
	})
	return out
}

// Total sums lines: cost of the priced ones, and whether some are not.
func Total(lines []Line) (cost float64, unpriced bool) {
	for _, l := range lines {
		cost += l.Cost
		if !l.Priced {
			unpriced = true
		}
	}
	return cost, unpriced
}

// Period returns the first and last day of a month offset from now (0 =
// this month, -1 = the last one).
func Period(now time.Time, months int) (string, string) {
	first := time.Date(now.Year(), now.Month()+time.Month(months), 1, 0, 0, 0, 0, now.Location())
	last := first.AddDate(0, 1, -1)
	return first.Format("2006-01-02"), last.Format("2006-01-02")
}

// Today is the day key of now.
func Today(now time.Time) string { return now.Format("2006-01-02") }
