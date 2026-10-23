package macbridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/internal/i18n"
)

func TestAITestMissingKeyCzech(t *testing.T) {
	Init(nil)
	i18n.Set("cs")
	raw := Call(`{"method":"ai.test","id":1,"params":{"provider":"grok","model":"grok-4.7"}}`)
	var res Response
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected failure without key")
	}
	if !strings.Contains(res.Error, "klíč") {
		t.Fatalf("want Czech key hint, got %q", res.Error)
	}
}

func TestAITestOllamaMissingModel(t *testing.T) {
	Init(nil)
	i18n.Set("cs")
	raw := Call(`{"method":"ai.test","id":2,"params":{"provider":"ollama","model":"qwen3.5:4b"}}`)
	var res Response
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected failure for missing model")
	}
	if res.Error == "" {
		t.Fatal("empty error")
	}
}
