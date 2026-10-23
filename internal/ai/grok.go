package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// grokBaseURL is xAI's OpenAI-compatible API.
var grokBaseURL = "https://api.x.ai/v1/"

// grokProvider uses Chat Completions (OpenAI-compatible).
type grokProvider struct{ api openai.Client }

func grokClient(apiKey string) openai.Client {
	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(grokBaseURL))
}

func newGrok(apiKey string) *grokProvider {
	return &grokProvider{api: grokClient(apiKey)}
}

func (g *grokProvider) Complete(ctx context.Context, req Request) (string, error) {
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(req.Model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(req.System),
			openai.UserMessage(req.User),
		},
		MaxTokens: openai.Int(req.MaxTokens),
	}
	if req.Schema != nil {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "result",
					Schema: req.Schema,
					Strict: openai.Bool(true),
				},
			},
		}
	}
	var last error
	for range 2 {
		resp, err := g.api.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
		}
		if req.usage != nil {
			req.usage.In += resp.Usage.PromptTokens
			req.usage.Out += resp.Usage.CompletionTokens
		}
		if len(resp.Choices) == 0 {
			last = errors.New(i18n.T("the model returned an empty answer"))
			continue
		}
		c := resp.Choices[0]
		out := strings.TrimSpace(c.Message.Content)
		if c.FinishReason == "length" {
			last = errors.New(i18n.T("the model's answer is incomplete (max_output_tokens)"))
			continue
		}
		if out == "" {
			last = errors.New(i18n.T("the model returned an empty answer"))
			continue
		}
		if req.Schema != nil && !json.Valid([]byte(out)) {
			last = errors.New(i18n.T("the model did not return valid JSON"))
			continue
		}
		return out, nil
	}
	return "", last
}

// isGrokChatModel keeps text chat models and drops image/voice/imagine IDs.
func isGrokChatModel(id string) bool {
	low := strings.ToLower(id)
	for _, skip := range []string{"image", "imagine", "voice", "tts", "audio", "embedding", "vision"} {
		if strings.Contains(low, skip) {
			return false
		}
	}
	return strings.HasPrefix(low, "grok")
}
