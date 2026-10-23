package macbridge

import (
	"encoding/json"
	"net/mail"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/spam"
)

// ownMessageID returns the account-local message ID used by the spam filter
// and rules. Unified inbox IDs are "N\x1emsg"; ProcessNew stores the bare id.
func (e *Engine) ownMessageID(id string) string {
	if id == "" {
		return id
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.sessions[unifiedID]; ok {
		if u, ok := s.acc.(*mailbox.Unified); ok {
			if _, real, ok := u.Resolve(id); ok {
				return real
			}
		}
	}
	return id
}

// decisionResult is the stable JSON shape for spam.decision (Swift banner).
func decisionResult(d spam.Decision) map[string]any {
	return map[string]any{
		"message_id": d.MessageID,
		"from":       d.From,
		"subject":    d.Subject,
		"spam":       d.Spam,
		"source":     d.Source,
		"hits":       d.Hits,
		"verdict": map[string]any{
			"spam_probability": d.Verdict.SpamProbability,
			"category":         d.Verdict.Category,
			"reason":           d.Verdict.Reason,
		},
	}
}

func parseParams[T any](raw json.RawMessage, dst *T) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func parseAddressList(s string) []*mail.Address {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []*mail.Address
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, err := mail.ParseAddress(part)
		if err != nil {
			// bare address
			out = append(out, &mail.Address{Address: part})
			continue
		}
		out = append(out, a)
	}
	return out
}

func formatAddrs(list []*mail.Address) string {
	parts := make([]string, 0, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		parts = append(parts, a.String())
	}
	return strings.Join(parts, ", ")
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04", s)
}
