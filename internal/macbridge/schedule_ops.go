package macbridge

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Imbecile6197/klient/internal/protonmail"
)

func (e *Engine) mailSnooze(params json.RawMessage) Response {
	var p struct {
		Account string   `json:"account"`
		IDs     []string `json:"ids"` // conversation IDs
		Until   string   `json:"until"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	until, err := parseTime(p.Until)
	if err != nil || until.IsZero() {
		return Response{OK: false, Error: "invalid until time"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := s.acc.Snooze(ctx, p.IDs, until); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) mailUnsnooze(params json.RawMessage) Response {
	var p struct {
		Account string   `json:"account"`
		IDs     []string `json:"ids"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := s.acc.Unsnooze(ctx, p.IDs); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) mailCancelScheduled(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		ID      string `json:"id"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := s.acc.CancelScheduled(ctx, p.ID); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) autoReplyGet(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	r, err := s.acc.AutoReply(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: r}
}

func (e *Engine) autoReplySet(params json.RawMessage) Response {
	var p struct {
		Account string                `json:"account"`
		Reply   protonmail.AutoReply `json:"reply"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := s.acc.SetAutoReply(ctx, p.Reply); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}
