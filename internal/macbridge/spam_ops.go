package macbridge

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

func (e *Engine) spamFeedback(params json.RawMessage) Response {
	var p struct {
		MessageID string `json:"messageId"`
		From      string `json:"from"`
		IsSpam    bool   `json:"isSpam"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.filter.Feedback(e.ownMessageID(p.MessageID), p.From, p.IsSpam)
	return Response{OK: true}
}

func (e *Engine) spamDecision(params json.RawMessage) Response {
	var p struct {
		MessageID string `json:"messageId"`
		Account   string `json:"account"`
		Evaluate  bool   `json:"evaluate"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ownID := e.ownMessageID(p.MessageID)
	if d, ok := e.filter.Decision(ownID); ok {
		return Response{OK: true, Result: map[string]any{"known": true, "decision": decisionResult(d)}}
	}
	if !p.Evaluate || p.Account == "" || p.MessageID == "" {
		return Response{OK: true, Result: map[string]any{"known": false}}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
	defer cancel()
	msg, err := s.acc.Get(ctx, p.MessageID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	// Evaluate must store under the account-own ID (same as ProcessNew).
	if u, ok := s.acc.(*mailbox.Unified); ok {
		if _, real, err := u.UnwrapMessage(msg); err == nil {
			msg = real
		}
	} else if msg.Meta.ID != ownID && ownID != "" {
		msg.Meta.ID = ownID
	}
	d := e.filter.Evaluate(ctx, msg)
	return Response{OK: true, Result: map[string]any{
		"known": true, "evaluated": true, "decision": decisionResult(d),
	}}
}

func (e *Engine) spamScanInbox(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Max     int    `json:"max"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.Max <= 0 {
		p.Max = 30
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Minute)
	defer cancel()
	list, err := s.acc.List(ctx, protonmail.InboxID, 0, p.Max)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	// Unified list IDs are wrapped; ProcessNew / decisions use account-own IDs
	// (same as catch-up and Linux scanInbox over each real account).
	u, unified := s.acc.(*mailbox.Unified)
	moved := 0
	for _, m := range list {
		acc := s.acc
		meta := m
		if unified {
			real, unwrapped, ok := u.UnwrapSummary(m)
			if !ok {
				continue
			}
			acc, meta = real, unwrapped
		}
		_, did, err := e.filter.ProcessNew(ctx, acc, meta)
		if err == nil && did {
			moved++
		}
	}
	e.emit(Event{Type: "mailChanged", Account: p.Account})
	return Response{OK: true, Result: map[string]int{"checked": len(list), "moved": moved}}
}
