package ai

import (
	"context"
	"errors"

	"google.golang.org/genai"
)

type geminiProvider struct{ api *genai.Client }

func newGemini(ctx context.Context, apiKey string) (*geminiProvider, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, err
	}
	return &geminiProvider{api: c}, nil
}

func (g *geminiProvider) Complete(ctx context.Context, req Request) (string, error) {
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: req.System}}},
		MaxOutputTokens:   int32(req.MaxTokens),
	}
	if req.Schema != nil {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseJsonSchema = req.Schema
	}
	res, err := g.api.Models.GenerateContent(ctx, req.Model, genai.Text(req.User), cfg)
	if err != nil {
		return "", err
	}
	if res.PromptFeedback != nil && res.PromptFeedback.BlockReason != "" {
		return "", errors.New("Gemini požadavek zablokoval: " + string(res.PromptFeedback.BlockReason))
	}
	text := res.Text()
	if text == "" {
		return "", errors.New("Gemini vrátil prázdnou odpověď")
	}
	return text, nil
}
