package ai

import (
	"context"
	"errors"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Imbecile6197/klient/internal/i18n"
)

type openaiProvider struct{ api openai.Client }

func newOpenAI(apiKey string) *openaiProvider {
	return &openaiProvider{api: openai.NewClient(option.WithAPIKey(apiKey))}
}

// Complete uses the Responses API.
func (o *openaiProvider) Complete(ctx context.Context, req Request) (string, error) {
	effort := shared.ReasoningEffortMedium
	if req.Effort == EffortLow {
		effort = shared.ReasoningEffortLow
	}
	params := responses.ResponseNewParams{
		Model:           shared.ResponsesModel(req.Model),
		Instructions:    openai.String(req.System),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(req.User)},
		MaxOutputTokens: openai.Int(req.MaxTokens),
		Reasoning:       shared.ReasoningParam{Effort: effort},
		// Do not keep e-mail content stored on OpenAI's side for later retrieval.
		Store: openai.Bool(false),
	}
	if req.Schema != nil {
		params.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   "result",
					Schema: req.Schema,
					Strict: openai.Bool(true),
				},
			},
		}
	}
	resp, err := o.api.Responses.New(ctx, params)
	if err != nil {
		return "", err
	}
	if resp.Status == responses.ResponseStatusIncomplete {
		return "", errors.New(i18n.T("the model's answer is incomplete (") + string(resp.IncompleteDetails.Reason) + ")")
	}
	return resp.OutputText(), nil
}
