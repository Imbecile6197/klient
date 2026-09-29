package ai

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// ListModels returns the text models the account can use right now, so the
// user can pick one instead of guessing names that providers retire.
func ListModels(ctx context.Context, provider, apiKey string) ([]string, error) {
	if apiKey == "" {
		return nil, ErrNoAPIKey
	}
	var out []string
	switch provider {
	case ProviderClaude:
		c := anthropic.NewClient(anthropicopt.WithAPIKey(apiKey))
		it := c.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
		for it.Next() {
			out = append(out, it.Current().ID)
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
	case ProviderOpenAI:
		c := openai.NewClient(openaiopt.WithAPIKey(apiKey))
		it := c.Models.ListAutoPaging(ctx)
		for it.Next() {
			id := it.Current().ID
			if isOpenAIChatModel(id) {
				out = append(out, id)
			}
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
	case ProviderGemini:
		c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
		if err != nil {
			return nil, err
		}
		for m, err := range c.Models.All(ctx) {
			if err != nil {
				return nil, err
			}
			name := strings.TrimPrefix(m.Name, "models/")
			if slices.Contains(m.SupportedActions, "generateContent") && isGeminiTextModel(name) {
				out = append(out, name)
			}
		}
	case ProviderMistral:
		c := mistralClient(apiKey)
		it := c.Models.ListAutoPaging(ctx)
		for it.Next() {
			if id := it.Current().ID; isMistralChatModel(id) {
				out = append(out, id)
			}
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		out = slices.Compact(slices.Sorted(slices.Values(out)))
	default:
		return nil, fmt.Errorf(i18n.T("unknown provider %q"), provider)
	}
	slices.Sort(out)
	slices.Reverse(out) // newest versions first for the usual naming schemes
	return out, nil
}

func isOpenAIChatModel(id string) bool {
	if !strings.HasPrefix(id, "gpt-") && !strings.HasPrefix(id, "o") {
		return false
	}
	for _, skip := range []string{"audio", "realtime", "transcribe", "tts", "image", "search", "instruct", "embedding", "moderation"} {
		if strings.Contains(id, skip) {
			return false
		}
	}
	return true
}

func isGeminiTextModel(name string) bool {
	if !strings.HasPrefix(name, "gemini") {
		return false
	}
	for _, skip := range []string{"image", "tts", "audio", "live", "embedding", "robotics", "computer-use"} {
		if strings.Contains(name, skip) {
			return false
		}
	}
	return true
}
