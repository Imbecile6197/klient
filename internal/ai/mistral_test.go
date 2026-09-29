package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMistral(t *testing.T) {
	var calls int
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"object":"list","data":[{"id":"mistral-small-latest","object":"model"},{"id":"mistral-embed","object":"model"},{"id":"ministral-8b-latest","object":"model"},{"id":"codestral-latest","object":"model"}]}`)
		case "/v1/chat/completions":
			calls++
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			content := `{"spam":true}`
			if calls == 1 {
				content = `{"spam":tr` // broken: must be retried
			}
			b, _ := json.Marshal(content)
			io.WriteString(w, `{"id":"x","object":"chat.completion","model":"mistral-small-latest","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":`+string(b)+`}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := mistralBaseURL
	mistralBaseURL = srv.URL + "/v1/"
	defer func() { mistralBaseURL = old }()

	ctx := context.Background()
	p, err := NewProvider(ctx, ProviderMistral, "key")
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{"spam": map[string]any{"type": "boolean"}}, "required": []string{"spam"}, "additionalProperties": false}
	out, err := p.Complete(ctx, Request{Model: "mistral-small-latest", System: "s", User: "u", Schema: schema, MaxTokens: 100})
	if err != nil || out != `{"spam":true}` || calls != 2 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
	rf, _ := body["response_format"].(map[string]any)
	js, _ := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["strict"] != true || body["max_tokens"] != float64(100) {
		t.Fatalf("request body %v", body)
	}
	models, err := ListModels(ctx, ProviderMistral, "key")
	if err != nil || len(models) != 2 || models[0] != "ministral-8b-latest" && models[0] != "mistral-small-latest" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if _, ok := ProviderByID(ProviderMistral); !ok || IsLocal(ProviderMistral) {
		t.Fatal("provider registration")
	}
}
