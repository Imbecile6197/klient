package ai

import (
	"regexp"
	"strings"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// Explanation is a user-facing description of a provider error.
type Explanation struct {
	Text string // short Czech explanation
	URL  string // page where the user can fix it ("" if none)
}

var activationURL = regexp.MustCompile(`https://console\.developers\.google\.com/apis/api/generativelanguage\.googleapis\.com/overview\?project=\d+`)

// Explain turns common provider errors (billing, disabled API, bad key, bad
// model) into a short explanation with a link to fix them.
func Explain(provider string, err error) Explanation {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case provider == ProviderOllama && strings.Contains(low, strings.ToLower(i18n.T("Ollama is not installed"))):
		return Explanation{i18n.T("Local AI is not ready yet. In Preferences → AI, click “Install” next to Ollama and download a model."), ""}
	case provider == ProviderOllama && strings.Contains(low, "not found") && strings.Contains(low, "model"):
		return Explanation{i18n.T("The selected local model is not downloaded. Download it in Preferences → AI (Local AI)."), ""}
	case provider == ProviderOllama && (strings.Contains(low, "deadline") || strings.Contains(low, "timeout")):
		return Explanation{i18n.T("The local model did not answer in time. It is slow on a processor without a graphics card – try a shorter text or a smaller model."), ""}
	case provider == ProviderOllama && strings.Contains(low, "memory"):
		return Explanation{i18n.T("There is not enough memory left for the local model. Close demanding applications or choose a smaller model."), ""}
	case provider == ProviderOpenAI && (strings.Contains(low, "insufficient_quota") || strings.Contains(low, "credit_balance")):
		return Explanation{i18n.T("The OpenAI account has no credit. The API is billed separately from a ChatGPT subscription – add credit in the billing settings."),
			"https://platform.openai.com/settings/organization/billing"}
	case provider == ProviderMistral && (strings.Contains(low, "402") || strings.Contains(low, "payment") || strings.Contains(low, "billing")):
		return Explanation{i18n.T("The Mistral account has no active plan or credit. Choose a plan (the free “Experiment” plan works too) in the Mistral console."),
			"https://console.mistral.ai/billing"}
	case provider == ProviderClaude && strings.Contains(low, "credit balance"):
		return Explanation{i18n.T("The Anthropic account has no credit. Add some in the Billing section."), "https://platform.claude.com"}
	case provider == ProviderGemini && strings.Contains(low, "service_disabled"):
		u := activationURL.FindString(msg)
		if u == "" {
			u = "https://aistudio.google.com/apikey"
		}
		return Explanation{i18n.T("The Gemini API is not enabled in the Google Cloud project the key belongs to. Enable it (the Enable button) and try again in a few minutes – or create a new key in Google AI Studio, which enables the API automatically."), u}
	case strings.Contains(low, "401") || strings.Contains(low, "invalid_api_key") || strings.Contains(low, "api key not valid") || strings.Contains(low, "authentication_error"):
		info, _ := ProviderByID(provider)
		return Explanation{i18n.T("The key is invalid or has been revoked. Create a new one and paste it again."), info.KeyURL}
	case strings.Contains(low, "model") && (strings.Contains(low, "no longer available") || strings.Contains(low, "not_found") || strings.Contains(low, "not found") || strings.Contains(low, "does not exist") || strings.Contains(low, "404")):
		return Explanation{i18n.T("The selected model no longer exists or your account has no access to it. Choose another one in Preferences → AI with the model list button."), ""}
	case strings.Contains(low, "429") || strings.Contains(low, "rate limit") || strings.Contains(low, "resource_exhausted"):
		return Explanation{i18n.T("The request limit or quota has been exceeded. Try again later, or check the limits of your account."), ""}
	}
	return Explanation{Text: msg}
}
