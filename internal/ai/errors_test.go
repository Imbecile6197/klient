package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestExplain(t *testing.T) {
	gem := errors.New(`Error 403, Message: Gemini API has not been used in project 161238864110 before or it is disabled. Enable it by visiting https://console.developers.google.com/apis/api/generativelanguage.googleapis.com/overview?project=161238864110 then retry., Status: PERMISSION_DENIED, Details: [reason:SERVICE_DISABLED]`)
	ex := Explain(ProviderGemini, gem)
	if !strings.HasSuffix(ex.URL, "project=161238864110") || !strings.Contains(ex.Text, "Gemini API") {
		t.Errorf("gemini: %+v", ex)
	}
	oai := errors.New(`POST "https://api.openai.com/v1/responses": 429 Too Many Requests {"type": "insufficient_quota", "code": "credit_balance_exhausted"}`)
	ex = Explain(ProviderOpenAI, oai)
	if !strings.Contains(ex.URL, "billing") {
		t.Errorf("openai: %+v", ex)
	}
	if ex := Explain(ProviderClaude, errors.New("boom")); ex.Text != "boom" || ex.URL != "" {
		t.Errorf("fallback: %+v", ex)
	}
}
