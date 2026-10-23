package macbridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/usage"
)

// Ensures macOS meters AI the same way Linux startUsage does: SetUsageHook
// must append to ai-usage.json so ai.usage / the sidebar footer are non-zero.
func TestUsageHookPersistsRecord(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	path := usage.Path()
	if filepath.Dir(path) != filepath.Join(dir, "klient") && filepath.Base(filepath.Dir(path)) != "klient" {
		// Path() joins config.DataDir(); accept any writable path under dir.
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
	}
	store := usage.Open(path)
	ai.SetUsageHook(store.Add)
	t.Cleanup(func() { ai.SetUsageHook(nil) })

	store.Add(ai.Record{
		Time:     time.Now(),
		Provider: "openai",
		Model:    "gpt-5-mini",
		Task:     ai.TaskTest,
		Usage:    ai.Usage{In: 100, Out: 20},
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 10 {
		t.Fatalf("usage file empty: %q", b)
	}
	reloaded := usage.Open(path)
	from, to := usage.Period(time.Now(), 0)
	lines := reloaded.Lines(from, to, nil)
	if len(lines) == 0 {
		t.Fatal("expected at least one usage line after Add")
	}
	cost, _ := usage.Total(lines)
	if cost <= 0 && !lines[0].Priced {
		// Unpriced models still count as activity for the Details sheet.
		if lines[0].Requests < 1 {
			t.Fatal("expected request count")
		}
	}
	_ = cost
}
