package macbridge

import (
	"context"
	"encoding/json"
	"time"
)

func (e *Engine) foldersCreate(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Name    string `json:"name"`
		Label   bool   `json:"label"`
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
	if err := s.acc.CreateFolder(ctx, p.Name, p.Label); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) foldersRename(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		ID      string `json:"id"`
		Name    string `json:"name"`
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
	if err := s.acc.RenameFolder(ctx, p.ID, p.Name); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) foldersDelete(params json.RawMessage) Response {
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
	if err := s.acc.DeleteFolder(ctx, p.ID); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) foldersEmpty(params json.RawMessage) Response {
	var p struct {
		Account   string `json:"account"`
		Folder    string `json:"folder"`
		OlderThan string `json:"olderThan"` // RFC3339; empty = all
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	older, _ := parseTime(p.OlderThan)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Minute)
	defer cancel()
	n, err := s.acc.EmptyFolder(ctx, p.Folder, older, nil)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]int{"deleted": n}}
}
