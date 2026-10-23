package macbridge

import (
	"context"
	"encoding/json"
	"time"
)

func (e *Engine) contactsList(params json.RawMessage) Response {
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
	list, err := s.acc.Contacts(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	out := make([]map[string]string, 0, len(list))
	for _, c := range list {
		out = append(out, map[string]string{
			"id": c.ContactID, "name": c.Name, "email": c.Email,
		})
	}
	return Response{OK: true, Result: out}
}

func (e *Engine) contactsRecent(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Max     int    `json:"max"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.Max <= 0 {
		p.Max = 20
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	list := s.acc.RecentAddresses(p.Max)
	out := make([]map[string]string, 0, len(list))
	for _, c := range list {
		out = append(out, map[string]string{"name": c.Name, "email": c.Email})
	}
	return Response{OK: true, Result: out}
}

func (e *Engine) contactsAdd(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Name    string `json:"name"`
		Email   string `json:"email"`
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
	if err := s.acc.AddContact(ctx, p.Name, p.Email); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) contactsDelete(params json.RawMessage) Response {
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
	if err := s.acc.DeleteContact(ctx, p.ID); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}
