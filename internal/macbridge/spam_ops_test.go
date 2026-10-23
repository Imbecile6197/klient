package macbridge

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/spam"
)

func TestDecisionResultJSONShape(t *testing.T) {
	d := spam.Decision{
		MessageID: "m1",
		From:      "a@b.c",
		Subject:   "hi",
		Spam:      false,
		Source:    "ai",
		Hits:      []string{"x"},
		Time:      time.Unix(1, 0).UTC(),
		Verdict: ai.Verdict{
			SpamProbability: 0.12,
			Category:        "ham",
			Reason:          "looks fine",
		},
	}
	raw, err := json.Marshal(decisionResult(d))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["source"] != "ai" || m["spam"] != false {
		t.Fatalf("top fields: %v", m)
	}
	v, ok := m["verdict"].(map[string]any)
	if !ok {
		t.Fatalf("verdict missing: %v", m)
	}
	if v["category"] != "ham" {
		t.Fatalf("category: %v", v["category"])
	}
	prob, ok := v["spam_probability"].(float64)
	if !ok || prob < 0.11 || prob > 0.13 {
		t.Fatalf("spam_probability: %v", v["spam_probability"])
	}
}

func TestOwnMessageIDPassthrough(t *testing.T) {
	e := &Engine{sessions: map[string]*session{}}
	if got := e.ownMessageID("plain-id"); got != "plain-id" {
		t.Fatalf("got %q", got)
	}
	if got := e.ownMessageID(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
