package ai

import (
	"context"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

type claudeProvider struct{ api anthropic.Client }

func newClaude(apiKey string) *claudeProvider {
	return &claudeProvider{api: anthropic.NewClient(option.WithAPIKey(apiKey))}
}

// Complete sends one request. Refusals are re-served server-side by
// Anthropic's default fallback model.
func (c *claudeProvider) Complete(ctx context.Context, req Request) (string, error) {
	effort := anthropic.BetaOutputConfigEffortMedium
	if req.Effort == EffortLow {
		effort = anthropic.BetaOutputConfigEffortLow
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: req.MaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: req.System}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.User)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: effort},
		Betas:        []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks:    anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
	}
	if strings.HasPrefix(req.Model, "claude-haiku") {
		// Haiku has no effort parameter and no server-side fallbacks.
		params.OutputConfig.Effort = ""
		params.Betas = nil
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{}
	}
	if req.Schema != nil {
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: req.Schema}
	}
	resp, err := c.api.Beta.Messages.New(ctx, params)
	if err != nil {
		return "", err
	}
	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return "", errors.New("model odmítl požadavek zpracovat")
	case anthropic.BetaStopReasonMaxTokens:
		return "", errors.New("odpověď modelu byla oříznuta (max_tokens)")
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String(), nil
}
