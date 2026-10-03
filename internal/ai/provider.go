package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// Provider IDs used in the config.
const (
	ProviderClaude  = "claude"
	ProviderOpenAI  = "openai"
	ProviderGemini  = "gemini"
	ProviderMistral = "mistral"
	ProviderOllama  = "ollama"
)

// ProviderInfo describes a provider for the preferences UI.
type ProviderInfo struct {
	ID     string
	Name   string
	KeyURL string
	// Default models: a capable one for the assistant, a cheap one for spam.
	AssistantModel string
	SpamModel      string
}

var Providers = []ProviderInfo{
	{ProviderClaude, "Claude (Anthropic)", "https://platform.claude.com", "claude-opus-5", "claude-opus-5"},
	{ProviderOpenAI, "ChatGPT (OpenAI)", "https://platform.openai.com/api-keys", "gpt-5.5", "gpt-5.4-mini"},
	{ProviderGemini, "Gemini (Google)", "https://aistudio.google.com/apikey", "gemini-3.1-pro-preview", "gemini-3-flash-preview"},
	{ProviderMistral, i18n.T("Mistral (EU servers)"), "https://console.mistral.ai/api-keys", "mistral-medium-latest", "mistral-small-latest"},
	{ProviderOllama, i18n.T("Local AI (Ollama, on this computer)"), "", "qwen3.5:4b", "qwen3.5:4b"},
}

func ProviderByID(id string) (ProviderInfo, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// Effort is a provider-neutral reasoning effort hint.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
)

// Request is one single-turn completion.
type Request struct {
	Model     string
	System    string
	User      string
	Schema    map[string]any // non-nil: the answer must be JSON matching it
	Effort    Effort
	MaxTokens int64
}

// Provider is implemented by every LLM backend.
type Provider interface {
	Complete(ctx context.Context, req Request) (string, error)
}

// NewProvider builds a provider client for id with the given API key.
func NewProvider(ctx context.Context, id, apiKey string) (Provider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf(i18n.T("%w for %s"), ErrNoAPIKey, id)
	}
	var p Provider
	var err error
	switch id {
	case ProviderClaude:
		p = newClaude(apiKey)
	case ProviderOpenAI:
		p = newOpenAI(apiKey)
	case ProviderGemini:
		p, err = newGemini(ctx, apiKey)
	case ProviderMistral:
		p = newMistral(apiKey)
	default:
		return nil, fmt.Errorf(i18n.T("unknown AI provider %q"), id)
	}
	if err != nil {
		return nil, err
	}
	return logged{p, id}, nil
}

// logged records every request in the log (provider, model, size, time,
// result), so it can be checked which model is really used and what fails.
// The content itself is never logged.
type logged struct {
	Provider
	id string
}

func (l logged) Complete(ctx context.Context, req Request) (string, error) {
	start := time.Now()
	out, err := l.Provider.Complete(ctx, req)
	res := "ok"
	if err != nil {
		res = "error: " + err.Error()
	}
	log.Printf("AI request: %s, model %s, %d characters in, %d out, %.1f s – %s",
		l.id, req.Model, len(req.System)+len(req.User), len(out), time.Since(start).Seconds(), res)
	return out, err
}

// Test sends a tiny request to check that the key and model work.
func Test(ctx context.Context, id, apiKey, model string) error {
	p, err := NewProvider(ctx, id, apiKey)
	if err != nil {
		return err
	}
	out, err := p.Complete(ctx, Request{
		Model: model, System: "You are a connection test.", User: "Reply with just the word OK.",
		Effort: EffortLow, MaxTokens: 2000,
	})
	if err != nil {
		return err
	}
	if out == "" {
		return errors.New(i18n.T("empty answer"))
	}
	return nil
}
