package macbridge_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/macbridge"
)

func TestAboutReleaseNotesRespectsLangParam(t *testing.T) {
	macbridge.Init(func(string) {})
	defer i18n.Set(i18n.Lang())
	// Force English on the Go side; lang param must still win.
	i18n.Apply("en")

	raw := macbridge.Call(`{"method":"about.releaseNotes","id":1,"params":{"lang":"cs","currentOnly":true}}`)
	if !strings.Contains(raw, "Účty") {
		t.Fatalf("lang=cs should return Czech notes, got: %s", trunc(raw, 280))
	}
	rawEN := macbridge.Call(`{"method":"about.releaseNotes","id":2,"params":{"lang":"en","currentOnly":true}}`)
	if !strings.Contains(rawEN, "IMAP accounts") {
		t.Fatalf("lang=en should return English notes, got: %s", trunc(rawEN, 280))
	}
}

func TestConfigSaveAppliesLanguage(t *testing.T) {
	macbridge.Init(func(string) {})
	defer i18n.Set(i18n.Lang())

	cfgRaw := macbridge.Call(`{"method":"config.get","id":1}`)
	var wrap struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(cfgRaw), &wrap); err != nil || !wrap.OK {
		t.Fatalf("config.get: %v %s", err, trunc(cfgRaw, 200))
	}
	prev, _ := wrap.Result["language"].(string)
	t.Cleanup(func() {
		wrap.Result["language"] = prev
		b, _ := json.Marshal(map[string]any{"method": "config.save", "id": 99, "params": wrap.Result})
		_ = macbridge.Call(string(b))
	})

	wrap.Result["language"] = "cs"
	b, _ := json.Marshal(map[string]any{"method": "config.save", "id": 2, "params": wrap.Result})
	if res := macbridge.Call(string(b)); !strings.Contains(res, `"ok":true`) {
		t.Fatalf("config.save: %s", res)
	}
	if i18n.Lang() != "cs" {
		t.Fatalf("i18n after save: %q", i18n.Lang())
	}
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
