package macbridge

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
)

const unifiedID = "unified"

func (e *Engine) accountsEnableUnified(params json.RawMessage) Response {
	var p struct {
		Enable bool `json:"enable"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !p.Enable {
		if s, ok := e.sessions[unifiedID]; ok {
			s.cancel()
			delete(e.sessions, unifiedID)
		}
		return e.listAccountsLocked()
	}
	if _, ok := e.sessions[unifiedID]; ok {
		return e.listAccountsLocked()
	}
	u := mailbox.NewUnified(func() []mailbox.Account {
		e.mu.Lock()
		defer e.mu.Unlock()
		out := make([]mailbox.Account, 0, len(e.sessions))
		for id, s := range e.sessions {
			if id == unifiedID {
				continue
			}
			out = append(out, s.acc)
		}
		return out
	})
	// No event watch on unified; underlying accounts already watch.
	ctx, cancel := context.WithCancel(e.ctx)
	e.sessions[unifiedID] = &session{
		acc: u, ctx: ctx, cancel: cancel,
		checked: map[string]time.Time{},
		guard:   map[string]bool{},
	}
	return e.listAccountsLocked()
}
