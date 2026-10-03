package ai

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
)

// recorder answers every request and remembers the model it was asked for.
type recorder struct {
	mu     sync.Mutex
	models []string
	answer string
}

func (r *recorder) Complete(_ context.Context, req Request) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, req.Model)
	return r.answer, nil
}

// Every task must use exactly the model configured for its role, and the
// request log must name it.
func TestTasksUseConfiguredModels(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	asst := &recorder{answer: `{"label":"(none)","confidence":0.1,"reason":"x"}`}
	spam := &recorder{answer: `{"spam_probability":0.1,"category":"ham","reason":"x"}`}
	c := &Client{
		assistant: logged{asst, ProviderOpenAI}, assistantModel: "gpt-6-luna",
		spam: logged{spam, ProviderGemini}, spamModel: "gemini-2.5-flash-lite",
	}
	ctx := context.Background()
	docs := []MailDoc{{From: "a@example.org", Subject: "s", Body: "b"}}
	c.Summarize(ctx, "a@example.org", "s", "b")
	c.DraftReply(ctx, "a@example.org", "s", "b", "")
	c.Improve(ctx, "draft", "shorter")
	c.SuggestLabel(ctx, []string{"Work"}, "a@example.org", "s", "b")
	c.SearchPlan(ctx, "q", "today")
	c.AnswerFromMail(ctx, "q", "today", docs)
	c.Digest(ctx, "today", docs)
	if _, err := c.ClassifySpam(ctx, SpamInput{From: "a@example.org", Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}

	if len(asst.models) != 7 {
		t.Errorf("assistant got %d requests, want 7", len(asst.models))
	}
	for _, m := range asst.models {
		if m != "gpt-6-luna" {
			t.Errorf("assistant task used model %q", m)
		}
	}
	if len(spam.models) != 1 || spam.models[0] != "gemini-2.5-flash-lite" {
		t.Errorf("spam filter used %v", spam.models)
	}
	logText := buf.String()
	if strings.Count(logText, "AI request: openai, model gpt-6-luna") != 7 ||
		!strings.Contains(logText, "AI request: gemini, model gemini-2.5-flash-lite") {
		t.Errorf("request log:\n%s", logText)
	}
	if strings.Contains(logText, "a@example.org") {
		t.Error("the log must not contain message content")
	}
}
