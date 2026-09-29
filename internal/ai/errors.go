package ai

import (
	"regexp"
	"strings"
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
	case provider == ProviderOllama && strings.Contains(low, "není nainstalovaná"):
		return Explanation{"Lokální AI ještě není připravená. V Předvolbách → AI klikněte na „Nainstalovat Ollamu“ a stáhněte model.", ""}
	case provider == ProviderOllama && strings.Contains(low, "not found") && strings.Contains(low, "model"):
		return Explanation{"Zvolený lokální model není stažený. Stáhněte ho v Předvolbách → AI (Lokální AI).", ""}
	case provider == ProviderOllama && (strings.Contains(low, "deadline") || strings.Contains(low, "timeout")):
		return Explanation{"Lokální model nestihl odpovědět. Na procesoru bez grafické karty je pomalý – zkuste kratší text nebo menší model.", ""}
	case provider == ProviderOllama && strings.Contains(low, "memory"):
		return Explanation{"Na lokální model nezbývá dost paměti. Zavřete náročné aplikace nebo zvolte menší model.", ""}
	case provider == ProviderOpenAI && (strings.Contains(low, "insufficient_quota") || strings.Contains(low, "credit_balance")):
		return Explanation{"Na účtu OpenAI není kredit. API se platí zvlášť od předplatného ChatGPT – nahrajte kredit v nastavení fakturace.",
			"https://platform.openai.com/settings/organization/billing"}
	case provider == ProviderMistral && (strings.Contains(low, "402") || strings.Contains(low, "payment") || strings.Contains(low, "billing")):
		return Explanation{"Účet Mistral nemá aktivní platební tarif nebo kredit. Zvolte tarif (i bezplatný „Experiment“) v konzoli Mistral.",
			"https://console.mistral.ai/billing"}
	case provider == ProviderClaude && strings.Contains(low, "credit balance"):
		return Explanation{"Na účtu Anthropic není kredit. Nahrajte ho v sekci Billing.", "https://platform.claude.com"}
	case provider == ProviderGemini && strings.Contains(low, "service_disabled"):
		u := activationURL.FindString(msg)
		if u == "" {
			u = "https://aistudio.google.com/apikey"
		}
		return Explanation{"V projektu Google Cloud, ke kterému klíč patří, není zapnuté Gemini API. Zapněte ho (tlačítko Povolit) a za pár minut to zkuste znovu – nebo si vytvořte nový klíč v Google AI Studiu, kde se API zapne samo.", u}
	case strings.Contains(low, "401") || strings.Contains(low, "invalid_api_key") || strings.Contains(low, "api key not valid") || strings.Contains(low, "authentication_error"):
		info, _ := ProviderByID(provider)
		return Explanation{"Klíč je neplatný nebo byl zrušen. Vytvořte nový a vložte ho znovu.", info.KeyURL}
	case strings.Contains(low, "model") && (strings.Contains(low, "no longer available") || strings.Contains(low, "not_found") || strings.Contains(low, "not found") || strings.Contains(low, "does not exist") || strings.Contains(low, "404")):
		return Explanation{"Zvolený model už neexistuje nebo k němu váš účet nemá přístup. Vyberte jiný v Předvolbách → AI tlačítkem se seznamem modelů.", ""}
	case strings.Contains(low, "429") || strings.Contains(low, "rate limit") || strings.Contains(low, "resource_exhausted"):
		return Explanation{"Překročen limit požadavků nebo kvóta. Zkuste to později, případně zkontrolujte limity účtu.", ""}
	}
	return Explanation{Text: msg}
}
