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

// mistralBaseURL is Mistral's OpenAI-compatible API (servers in the EU).
var mistralBaseURL = "https://api.mistral.ai/v1/"

// mistralProvider uses Chat Completions: Mistral has no Responses API.
type mistralProvider struct{ api openai.Client }

func mistralClient(apiKey string) openai.Client {
	return openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(mistralBaseURL))
}

func newMistral(apiKey string) *mistralProvider {
	return &mistralProvider{api: mistralClient(apiKey)}
}

func (m *mistralProvider) Complete(ctx context.Context, req Request) (string, error) {
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
	// A wrong-shaped or cut-off answer is retried once: the caller parses it.
	var last error
	for range 2 {
		resp, err := m.api.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
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

// isMistralChatModel keeps text chat models and drops embeddings, OCR,
// audio, moderation and code-only models from the model list.
func isMistralChatModel(id string) bool {
	for _, skip := range []string{"embed", "ocr", "voxtral", "moderation", "codestral", "devstral", "shieldstral", "transcribe", "pixtral"} {
		if strings.Contains(id, skip) {
			return false
		}
	}
	return strings.Contains(id, "mistral") || strings.Contains(id, "ministral") || strings.Contains(id, "magistral")
}
