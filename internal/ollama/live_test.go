//go:build live

package ollama

import (
	"context"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
)

// go test -tags live -run TestLive -v ./internal/ollama
// Installs the latest Ollama into Klient's data dir, pulls a model and runs
// a structured chat. No mail data involved.
func TestLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	r := New(config.DataDir())
	rel, err := Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest %s (%d MB), installed %q", rel.Version, rel.Size>>20, r.Installed())
	if Newer(rel.Version, r.Installed()) {
		last := time.Now()
		if err := r.Install(ctx, rel, func(done, total int64) {
			if time.Since(last) > 20*time.Second {
				t.Logf("download %d/%d MB", done>>20, total>>20)
				last = time.Now()
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	prog, _ := r.DiskUsage()
	t.Logf("installed %s, program %d MB", r.Installed(), prog>>20)
	release, err := r.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	defer r.Stop()
	c := NewClient(r.Addr())
	v, _ := c.Version(ctx)
	t.Logf("server %s at %s", v, r.Addr())
	model := "qwen3.5:4b"
	last := time.Now()
	if err := c.Pull(ctx, model, func(st string, done, total int64) {
		if time.Since(last) > 20*time.Second {
			t.Logf("pull %s %d/%d MB", st, done>>20, total>>20)
			last = time.Now()
		}
	}); err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spam_probability": map[string]any{"type": "number"},
			"category":         map[string]any{"type": "string", "enum": []string{"ham", "spam", "phishing"}},
			"reason":           map[string]any{"type": "string"},
		},
		"required": []string{"spam_probability", "category", "reason"},
	}
	for _, body := range []string{
		"Dobrý den, posílám fakturu za září v příloze. S pozdravem Jana, účetní",
		"Vaše schránka bude zablokována! Ověřte heslo do 24 hodin na http://proton-verify.example.ru/login",
	} {
		out, st, err := c.Chat(ctx, ChatRequest{Model: model, System: "Jsi spamový filtr. Odpověz JSON česky.", User: body, Schema: schema, NumCtx: 4096})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%.1fs prompt %d out %d: %s", st.Duration.Seconds(), st.PromptTokens, st.OutputTokens, out)
	}
}
