// Package rules implements simple user filters for incoming mail:
// "if <field> contains <text> then <action>".
package rules

import (
	"net/mail"
	"strings"

	"github.com/Imbecile6197/klient/internal/i18n"
)

type Field string

const (
	FieldFrom    Field = "from"
	FieldTo      Field = "to"
	FieldSubject Field = "subject"
)

type Action string

const (
	ActionMove  Action = "move"  // Target = folder ID
	ActionLabel Action = "label" // Target = label ID
	ActionRead  Action = "read"
	ActionStar  Action = "star"
)

type Rule struct {
	Name     string `json:"name"`
	Field    Field  `json:"field"`
	Contains string `json:"contains"`
	Action   Action `json:"action"`
	Target   string `json:"target,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// Message is what a rule looks at.
type Message struct {
	From    *mail.Address
	To      []*mail.Address
	Subject string
}

func addr(a *mail.Address) string {
	if a == nil {
		return ""
	}
	return strings.ToLower(a.Name + " " + a.Address)
}

// Matches reports whether r applies to m (case-insensitive substring).
func (r Rule) Matches(m Message) bool {
	needle := strings.ToLower(strings.TrimSpace(r.Contains))
	if !r.Enabled || needle == "" {
		return false
	}
	switch r.Field {
	case FieldFrom:
		return strings.Contains(addr(m.From), needle)
	case FieldTo:
		for _, a := range m.To {
			if strings.Contains(addr(a), needle) {
				return true
			}
		}
		return false
	case FieldSubject:
		return strings.Contains(strings.ToLower(m.Subject), needle)
	}
	return false
}

// Matching returns the rules that apply, in order. At most one move is
// returned (the first), since a message can only be in one folder.
func Matching(rs []Rule, m Message) []Rule {
	var out []Rule
	moved := false
	for _, r := range rs {
		if !r.Matches(m) {
			continue
		}
		if r.Action == ActionMove {
			if moved {
				continue
			}
			moved = true
		}
		out = append(out, r)
	}
	return out
}

func FieldName(f Field) string {
	switch f {
	case FieldFrom:
		return i18n.T("Sender")
	case FieldTo:
		return i18n.T("Recipient")
	case FieldSubject:
		return i18n.T("Subject")
	}
	return string(f)
}

func ActionName(a Action) string {
	switch a {
	case ActionMove:
		return i18n.T("Move to")
	case ActionLabel:
		return i18n.T("Add label")
	case ActionRead:
		return i18n.T("Mark as read")
	case ActionStar:
		return i18n.T("Add star")
	}
	return string(a)
}
