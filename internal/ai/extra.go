package ai

import (
	"strconv"

	"context"
	"encoding/json"
	"fmt"
	"github.com/Imbecile6197/klient/internal/i18n"
	"strings"
)

// LabelChoice is the AI's pick of a label for a message.
type LabelChoice struct {
	Label      string  `json:"label"` // exact label name, or "" for none
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

const noLabel = "(none)"

// SuggestLabel picks the best fitting label for a message from the user's
// labels (by name). An empty Label means none fits.
func (c *Client) SuggestLabel(ctx context.Context, labels []string, from, subject, body string) (LabelChoice, error) {
	if !c.HasAssistant() {
		return LabelChoice{}, ErrNoAPIKey
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":      map[string]any{"type": "string", "enum": append([]string{noLabel}, labels...), "description": "The exact label name, or \"(none)\" if none fits."},
			"confidence": map[string]any{"type": "number", "description": "0.0–1.0"},
			"reason":     map[string]any{"type": "string", "description": "A short reason in " + i18n.LanguageName() + ", one sentence."},
		},
		"required":             []string{"label", "confidence", "reason"},
		"additionalProperties": false,
	}
	prompt := fmt.Sprintf("Sort the e-mail into one of the user's labels. Pick a label only if the message clearly belongs to it; otherwise return the label \"(none)\".\n\nLabels: %s\n\n<email>\nFrom: %s\nSubject: %s\n\n%s\n</email>\n\nAnswer only with a JSON object.",
		strings.Join(quoteAll(labels), ", "), from, subject, truncate(body, c.limit(4000, 2000)))
	out, err := c.assistant.Complete(ctx, Request{
		Model: c.assistantModel, System: assistantSystem(), User: prompt,
		Schema: schema, Effort: EffortLow, MaxTokens: 4000,
	})
	if err != nil {
		return LabelChoice{}, err
	}
	var ch LabelChoice
	if err := json.Unmarshal([]byte(stripFence(out)), &ch); err != nil {
		return LabelChoice{}, fmt.Errorf(i18n.T("invalid answer from the AI: %w"), err)
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
		out[i] = strconv.Quote(s)
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
				"description": "1–4 short search queries (1–2 words each), most promising first",
			},
		},
		"required":             []string{"queries"},
		"additionalProperties": false,
	}
	prompt := fmt.Sprintf(`The user asks about their mailbox. The search can only match words in the subject and in the names and addresses of the sender and recipients (not in the message text), and every word of a query must match. Suggest 1–4 short queries (typically the name of a company or person, a domain, or a keyword from the subject, with or without diacritics depending on how it is usually written) that are most likely to find the relevant messages. Today is %s.

Question: %s

Answer only with a JSON object.`, today, question)
	out, err := c.assistant.Complete(ctx, Request{
		Model: c.assistantModel, System: assistantSystem(), User: prompt,
		Schema: schema, Effort: EffortLow, MaxTokens: 4000,
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(stripFence(out)), &res); err != nil {
		return nil, fmt.Errorf(i18n.T("invalid answer from the AI: %w"), err)
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
		fmt.Fprintf(&sb, "<email id=\"%d\">\nFrom: %s\nDate: %s\nSubject: %s\n\n%s\n</email>\n\n", i+1, d.From, d.Date, d.Subject, truncate(d.Body, c.limit(2500, 1000)))
	}
	return c.assist(ctx, fmt.Sprintf(`Answer the user's question about their mail using only the e-mails below. Cite the sources as [1], [2] by the e-mail id. If the answer is not in the e-mails, say so plainly and suggest what to search for instead. Be brief. Today is %s.

Question: %s

%s`, today, question, sb.String())+outputLanguage())
}

// Digest summarises the unread mail for a morning overview.
func (c *Client) Digest(ctx context.Context, today string, docs []MailDoc) (string, error) {
	var sb strings.Builder
	for i, d := range docs {
		fmt.Fprintf(&sb, "<email id=\"%d\">\nFrom: %s\nDate: %s\nSubject: %s\n\n%s\n</email>\n\n", i+1, d.From, d.Date, d.Subject, truncate(d.Body, c.limit(800, 350)))
	}
	return c.assist(ctx, fmt.Sprintf(`Prepare a morning overview of the unread mail (today is %s). Start with one sentence on the overall state, then bullet points ordered by importance: first what needs a response or has a deadline (give the deadline), then everything else in brief. Sum up newsletters and advertising in one line. Keep it short, without introductory phrases.

%s`, today, sb.String())+outputLanguage())
}
