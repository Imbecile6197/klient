package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LabelChoice is the AI's pick of a label for a message.
type LabelChoice struct {
	Label      string  `json:"label"` // exact label name, or "" for none
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

const noLabel = "(žádný)"

// SuggestLabel picks the best fitting label for a message from the user's
// labels (by name). An empty Label means none fits.
func (c *Client) SuggestLabel(ctx context.Context, labels []string, from, subject, body string) (LabelChoice, error) {
	if !c.HasAssistant() {
		return LabelChoice{}, ErrNoAPIKey
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":      map[string]any{"type": "string", "enum": append([]string{noLabel}, labels...), "description": "Přesný název štítku, nebo „(žádný)“, pokud se žádný nehodí."},
			"confidence": map[string]any{"type": "number", "description": "0.0–1.0"},
			"reason":     map[string]any{"type": "string", "description": "Krátké zdůvodnění česky, jedna věta."},
		},
		"required":             []string{"label", "confidence", "reason"},
		"additionalProperties": false,
	}
	prompt := fmt.Sprintf("Roztřiď e-mail do jednoho ze štítků uživatele. Vyber štítek jen tehdy, když zpráva do něj zjevně patří; jinak vrať label „(žádný)“.\n\nŠtítky: %s\n\n<email>\nOd: %s\nPředmět: %s\n\n%s\n</email>\n\nOdpověz výhradně JSON objektem.",
		strings.Join(quoteAll(labels), ", "), from, subject, truncate(body, c.limit(4000, 2000)))
	out, err := c.assistant.Complete(ctx, Request{
		Model: c.assistantModel, System: assistantSystem, User: prompt,
		Schema: schema, Effort: EffortLow, MaxTokens: 4000,
	})
	if err != nil {
		return LabelChoice{}, err
	}
	var ch LabelChoice
	if err := json.Unmarshal([]byte(stripFence(out)), &ch); err != nil {
		return LabelChoice{}, fmt.Errorf("neplatná odpověď AI: %w", err)
	}
	// Models occasionally invent a label despite the enum.
	for _, l := range labels {
		if strings.EqualFold(l, ch.Label) {
			ch.Label = l
			return ch, nil
		}
	}
	ch.Label = ""
	return ch, nil
}

// limit picks the input size for cloud or local assistant models.
func (c *Client) limit(cloud, local int) int {
	if c.assistantLocal {
		return local
	}
	return cloud
}

// MaxDocs is how many messages AnswerFromMail and Digest should get.
func (c *Client) MaxDocs(cloud, local int) int { return c.limit(cloud, local) }

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = "„" + s + "“"
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// SearchPlan turns a question about the user's mail into search terms for
// the mailbox search (which matches subject, sender and recipients).
func (c *Client) SearchPlan(ctx context.Context, question, today string) ([]string, error) {
	if !c.HasAssistant() {
		return nil, ErrNoAPIKey
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "1–4 krátké vyhledávací dotazy (1–2 slova), od nejslibnějšího",
			},
		},
		"required":             []string{"queries"},
		"additionalProperties": false,
	}
	prompt := fmt.Sprintf(`Uživatel se ptá na svou e-mailovou schránku. Vyhledávání umí hledat jen slova v předmětu, jménu a adrese odesílatele a příjemců (ne v textu zprávy), všechna slova dotazu musí být nalezena. Navrhni 1–4 krátké dotazy (typicky jméno firmy/osoby, doména, nebo klíčové slovo předmětu, bez diakritiky i s ní podle toho, jak se to obvykle píše), které nejspíš najdou relevantní zprávy. Dnes je %s.

Otázka: %s

Odpověz výhradně JSON objektem.`, today, question)
	out, err := c.assistant.Complete(ctx, Request{
		Model: c.assistantModel, System: assistantSystem, User: prompt,
		Schema: schema, Effort: EffortLow, MaxTokens: 4000,
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(stripFence(out)), &res); err != nil {
		return nil, fmt.Errorf("neplatná odpověď AI: %w", err)
	}
	var q []string
	for _, s := range res.Queries {
		if s = strings.TrimSpace(s); s != "" && len(q) < 4 {
			q = append(q, s)
		}
	}
	return q, nil
}

// MailDoc is one message given to AnswerFromMail.
type MailDoc struct {
	From, Date, Subject, Body string
}

// AnswerFromMail answers a question using only the given messages, citing
// them as [1], [2], …
func (c *Client) AnswerFromMail(ctx context.Context, question, today string, docs []MailDoc) (string, error) {
	var sb strings.Builder
	for i, d := range docs {
		fmt.Fprintf(&sb, "<email id=\"%d\">\nOd: %s\nDatum: %s\nPředmět: %s\n\n%s\n</email>\n\n", i+1, d.From, d.Date, d.Subject, truncate(d.Body, c.limit(2500, 1000)))
	}
	return c.assist(ctx, fmt.Sprintf(`Odpověz na otázku uživatele o jeho poště, a to pouze na základě níže uvedených e-mailů. Uváděj zdroje jako [1], [2] podle id e-mailu. Pokud odpověď v e-mailech není, řekni to na rovinu a navrhni, co hledat jinak. Buď stručný. Dnes je %s.

Otázka: %s

%s`, today, question, sb.String()))
}

// Digest summarises the unread mail for a morning overview.
func (c *Client) Digest(ctx context.Context, today string, docs []MailDoc) (string, error) {
	var sb strings.Builder
	for i, d := range docs {
		fmt.Fprintf(&sb, "<email id=\"%d\">\nOd: %s\nDatum: %s\nPředmět: %s\n\n%s\n</email>\n\n", i+1, d.From, d.Date, d.Subject, truncate(d.Body, c.limit(800, 350)))
	}
	return c.assist(ctx, fmt.Sprintf(`Připrav ranní přehled nepřečtené pošty (dnes je %s). Nejdřív jednou větou celkový stav, pak odrážky seřazené podle důležitosti: co vyžaduje reakci nebo má termín (uveď ho), potom ostatní ve zkratce. Newslettery a reklamu shrň jedním řádkem. Piš stručně, bez úvodních frází.

%s`, today, sb.String()))
}
