package ai

import (
	"context"
	"errors"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/power"
)

// LocalBackend is the managed Ollama server (started on demand).
type LocalBackend interface {
	Acquire(ctx context.Context) (release func(), err error)
	Addr() string
}

type ollamaProvider struct{ backend LocalBackend }

func newOllama(b LocalBackend) (Provider, error) {
	if b == nil {
		return nil, errors.New(i18n.T("local AI (Ollama) is not available"))
	}
	return &ollamaProvider{backend: b}, nil
}

func (p *ollamaProvider) Complete(ctx context.Context, req Request) (string, error) {
	release, err := p.backend.Acquire(ctx)
	defer release()
	if err != nil {
		return "", err
	}
	// Context: the prompt (roughly 3 characters per token for Czech) plus
	// room for the answer; small contexts keep CPU inference fast.
	numCtx := (len([]rune(req.System))+len([]rune(req.User)))/3 + 1024
	numCtx = max(4096, min(numCtx, 16384))
	// On the charger the model stays loaded longer: reloading it costs
	// seconds and the memory is not needed for anything else.
	keep := "5m"
	if power.OnAC() {
		keep = "30m"
	}
	out, _, err := ollama.NewClient(p.backend.Addr()).Chat(ctx, ollama.ChatRequest{
		Model: req.Model, System: req.System, User: req.User, Schema: req.Schema, NumCtx: numCtx, KeepAlive: keep,
	})
	return out, err
}

// IsLocal reports providers that run on this computer.
func IsLocal(provider string) bool { return provider == ProviderOllama }
