//go:build live

package ai

import (
	"context"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/ollama"
)

// go test -tags live -run TestLocalSpam -v ./internal/ai  (synthetic mail only)
func TestLocalSpam(t *testing.T) {
	ctx := context.Background()
	rt := ollama.New(config.DataDir())
	defer rt.Stop()
	c := New(ctx, Settings{SpamProvider: ProviderOllama, SpamModel: "qwen3.5:4b",
		AssistantProvider: ProviderOllama, AssistantModel: "qwen3.5:4b", Local: rt, LocalOnly: true}, func(string) string { return "" })
	for _, in := range []SpamInput{
		{From: "Jana Nováková <jana.novakova@ucetni-firma.cz>", To: "jsem@libormacak.eu", Subject: "Faktura za září",
			Body:           "Dobrý den,\n\nv příloze posílám fakturu za vedení účetnictví za září 2026, splatnost 14 dní.\n\nS pozdravem\nJana Nováková\nÚčetní firma s.r.o.",
			Authentication: "SPF pass, DKIM pass, DMARC pass", KnownContact: true},
		{From: "Proton Support <security@proton-verify.ru>", To: "jsem@libormacak.eu", Subject: "Vaše schránka bude zablokována",
			Body:           "Vaše schránka bude do 24 hodin zablokována. Ověřte své heslo zde: http://proton-verify.ru/login",
			Authentication: "SPF fail, DKIM none, DMARC fail", BlocklistHits: []string{"doména proton-verify.ru v OpenPhish"}},
		{From: "Alza.cz <newsletter@alza.cz>", To: "jsem@libormacak.eu", Subject: "Víkendové slevy až 40 %",
			Body:           "Tento víkend slevy na notebooky a příslušenství. Odhlásit odběr: https://alza.cz/unsubscribe",
			Authentication: "SPF pass, DKIM pass, DMARC pass"},
	} {
		t0 := time.Now()
		v, err := c.ClassifySpam(ctx, in)
		t.Logf("%-35s %.0fs %v %+v", in.Subject, time.Since(t0).Seconds(), err, v)
	}
	t0 := time.Now()
	s, err := c.Summarize(ctx, "Jana Nováková", "Faktura za září", "Dobrý den, v příloze posílám fakturu za vedení účetnictví za září 2026, částka 4 500 Kč, splatnost do 14. 10. S pozdravem Jana")
	t.Logf("summary %.0fs %v: %s", time.Since(t0).Seconds(), err, s)
}
