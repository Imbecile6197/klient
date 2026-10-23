package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrokCompleteAndList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"object":"list","data":[{"id":"grok-4.7","object":"model"},{"id":"grok-imagine-image","object":"model"},{"id":"grok-3","object":"model"}]}`)
		case "/v1/chat/completions":
			b, _ := json.Marshal(`{"ok":true}`)
			io.WriteString(w, `{"id":"x","object":"chat.completion","model":"grok-4.7","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":`+string(b)+`}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	old := grokBaseURL
	grokBaseURL = srv.URL + "/v1/"
	defer func() { grokBaseURL = old }()

	ctx := context.Background()
	p, err := NewProvider(ctx, ProviderGrok, "key")
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
		},
		"required":             []string{"ok"},
		"additionalProperties": false,
	}
	out, err := p.Complete(ctx, Request{Model: "grok-4.7", System: "s", User: "u", Schema: schema, MaxTokens: 100})
	if err != nil || out == "" {
		t.Fatalf("complete: %q %v", out, err)
	}
	models, err := ListModels(ctx, ProviderGrok, "key")
	if err != nil || len(models) != 2 {
		t.Fatalf("list: %v %v", models, err)
	}
	if _, ok := ProviderByID(ProviderGrok); !ok || IsLocal(ProviderGrok) {
		t.Fatal("provider meta")
	}
}
