package macbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/mailparse"
)

func (e *Engine) remoteAllowed(sender string) bool {
	sender = strings.ToLower(strings.TrimSpace(sender))
	if sender == "" {
		return false
	}
	domain := ""
	if i := strings.LastIndexByte(sender, '@'); i >= 0 {
		domain = sender[i:]
	}
	e.mu.Lock()
	list := append([]string(nil), e.cfg.RemoteContentSenders...)
	e.mu.Unlock()
	for _, s := range list {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == sender || (domain != "" && s == domain) {
			return true
		}
	}
	return false
}

func senderEmail(a *mail.Address) string {
	if a == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(a.Address))
}

func (e *Engine) mailAllowRemote(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		ID      string `json:"id"`
		Sender  string `json:"sender"` // email or "@domain"
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	sender := strings.ToLower(strings.TrimSpace(p.Sender))
	if sender == "" {
		return Response{OK: false, Error: "sender required"}
	}
	if !e.remoteAllowed(sender) {
		e.mu.Lock()
		e.cfg.RemoteContentSenders = append(e.cfg.RemoteContentSenders, sender)
		cfg := e.cfg
		e.mu.Unlock()
		if err := config.Save(cfg); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		e.emit(Event{Type: "configChanged"})
	}
	if p.Account == "" || p.ID == "" {
		return Response{OK: true, Result: map[string]any{"allowed": true}}
	}
	return e.mailGetFull(mustRaw(map[string]any{
		"account":     p.Account,
		"id":          p.ID,
		"allowRemote": true,
	}))
}

func (e *Engine) mailUnsubscribe(params json.RawMessage) Response {
	var p struct {
		HTTP     string `json:"http"`
		OneClick bool   `json:"oneClick"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if !p.OneClick || p.HTTP == "" {
		return Response{OK: false, Error: "one-click HTTP target required"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.HTTP, strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	res.Body.Close()
	if res.StatusCode >= 400 {
		return Response{OK: false, Error: fmt.Sprintf("server responded with %s", res.Status)}
	}
	return Response{OK: true}
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// prepareMessageHTML builds CSP-safe HTML for the reader (Linux mailparse.PrepareHTML).
func prepareMessageHTML(html, text string, inline map[string]string, allowRemote bool) (body string, hasRemote bool) {
	src := html
	if src == "" {
		src = mailparse.PlainToHTML(text)
	}
	hasRemote = mailparse.HasRemoteContent(src)
	return mailparse.PrepareHTML(src, inline, allowRemote), hasRemote
}
