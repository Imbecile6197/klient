package usage

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
)

func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "u.json")
	s := Open(path)
	day := time.Date(2026, 10, 14, 10, 0, 0, 0, time.Local)
	add := func(t time.Time, provider, model, task string, in, cached, out int64) {
		s.Add(ai.Record{Time: t, Provider: provider, Model: model, Task: task, Usage: ai.Usage{In: in, Cached: cached, Out: out}})
	}
	add(day, "openai", "gpt-6-luna-2026-09-01", ai.TaskDigest, 1_000_000, 0, 100_000)
	add(day, "openai", "gpt-6-luna-2026-09-01", ai.TaskDigest, 1_000_000, 1_000_000, 100_000)
	add(day, "gemini", "gemini-3.1-flash-lite", ai.TaskSpam, 2_000_000, 0, 0)
	add(day, "mistral", "mistral-medium-latest", ai.TaskSummary, 10, 0, 10)
	add(day, "ollama", "qwen3.5:4b", ai.TaskSpam, 5000, 0, 100)
	add(day.AddDate(0, -1, 0), "openai", "gpt-6-luna", ai.TaskReply, 1_000_000, 0, 0)

	// Reopened from disk.
	s = Open(path)
	from, to := Period(day, 0)
	lines := s.Lines(from, to, nil)
	if len(lines) != 4 {
		t.Fatalf("%d lines: %+v", len(lines), lines)
	}
	// The most expensive first: Gemini 2M in × $0.25 = $0.50.
	if lines[0].Model != "gemini-3.1-flash-lite" || math.Abs(lines[0].Cost-0.50) > 1e-9 {
		t.Errorf("first line %+v", lines[0])
	}
	// gpt-6-luna (a dated name): 2M in × $0.10 + 1M cached × $0.01 + 0.2M out × $0.50 = $0.31
	if l := lines[1]; l.Model != "gpt-6-luna-2026-09-01" || l.Requests != 2 || math.Abs(l.Cost-0.31) > 1e-9 {
		t.Errorf("second line %+v", l)
	}
	cost, unpriced := Total(lines)
	if math.Abs(cost-0.81) > 1e-9 || !unpriced {
		t.Errorf("total %v unpriced %v", cost, unpriced)
	}
	// A price set by the user makes the Mistral line priced.
	own := map[string]config.ModelPrice{"mistral-medium-latest": {In: 1, Out: 1}}
	if _, unpriced := Total(s.Lines(from, to, own)); unpriced {
		t.Error("own price not used")
	}
	// The month before holds only its own request.
	from, to = Period(day, -1)
	if l := s.Lines(from, to, nil); len(l) != 1 || l[0].Task != ai.TaskReply || from != "2026-09-01" || to != "2026-09-30" {
		t.Errorf("last month %v..%v %+v", from, to, l)
	}
}
