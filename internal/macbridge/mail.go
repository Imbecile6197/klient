package macbridge

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

func (e *Engine) mailList(params json.RawMessage) Response {
	var p struct {
		Account  string `json:"account"`
		Folder   string `json:"folder"`
		Page     int    `json:"page"`
		PageSize int    `json:"pageSize"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.Folder == "" {
		p.Folder = protonmail.InboxID
	}
	if p.PageSize <= 0 {
		p.PageSize = 50
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	list, err := s.acc.List(ctx, p.Folder, p.Page, p.PageSize)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	held := s.held()
	out := make([]summaryDTO, 0, len(list))
	for _, m := range list {
		if held[m.ID] {
			continue // spam filter still judging
		}
		out = append(out, e.summaryToDTO(m))
	}
	return Response{OK: true, Result: out}
}

func (e *Engine) mailGet(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	msg, err := s.acc.Get(ctx, p.ID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	_ = s.acc.MarkRead(ctx, p.ID)
	return Response{OK: true, Result: messageToDTO(msg)}
}

func (e *Engine) mailMarkRead(params json.RawMessage) Response {
	var p struct {
		Account string   `json:"account"`
		IDs     []string `json:"ids"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := s.acc.MarkRead(ctx, p.IDs...); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) mailMove(params json.RawMessage) Response {
	var p struct {
		Account string   `json:"account"`
		Folder  string   `json:"folder"`
		IDs     []string `json:"ids"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := s.acc.Move(ctx, p.Folder, p.IDs...); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) foldersList(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	type folderDTO struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
		Folder bool   `json:"folder"` // true = exclusive folder; false = Proton label (tag)
		Color  string `json:"color,omitempty"`
	}
	out := make([]folderDTO, 0, len(protonmail.Folders))
	for _, f := range protonmail.Folders {
		if !s.acc.HasFolder(f.ID) {
			continue
		}
		out = append(out, folderDTO{ID: f.ID, Name: f.Name, Custom: false, Folder: true})
	}
	if labels, err := s.acc.UserLabels(e.ctx); err == nil {
		for _, l := range labels {
			out = append(out, folderDTO{
				ID: l.ID, Name: l.Name, Custom: true,
				Folder: l.Folder, Color: l.Color,
			})
		}
	}
	return Response{OK: true, Result: out}
}

func summaryToDTO(m protonmail.Summary) summaryDTO {
	sender := ""
	email := ""
	if m.Sender != nil {
		sender = mailparse.DisplayAddress(m.Sender)
		email = senderEmail(m.Sender)
	}
	starred := false
	for _, l := range m.LabelIDs {
		if l == protonmail.StarredID {
			starred = true
			break
		}
	}
	return summaryDTO{
		ID: m.ID, Subject: m.Subject, Sender: sender, SenderEmail: email, Time: m.Time,
		Unread: bool(m.Unread), Starred: starred, HasAtt: m.NumAttachments > 0,
		ConversationID: m.ConversationID, Labels: append([]string(nil), m.LabelIDs...),
	}
}

// summaryToDTO attaches spam-filter verdict flags for list badges (Linux parity).
func (e *Engine) summaryToDTO(m protonmail.Summary) summaryDTO {
	dto := summaryToDTO(m)
	// Unified list IDs are wrapped; decisions are keyed by the account-own id.
	if d, ok := e.filter.Decision(e.ownMessageID(m.ID)); ok && d.Spam && d.Source != "none" {
		dto.IsSpam = true
		dto.SpamCategory = d.Verdict.Category
	}
	return dto
}

func messageToDTO(msg *protonmail.Message) messageDTO {
	sender := ""
	if msg.Meta.Sender != nil {
		sender = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	var toParts []string
	for _, a := range msg.Meta.ToList {
		toParts = append(toParts, mailparse.DisplayAddress(a))
	}
	return messageDTO{
		ID: msg.Meta.ID, Subject: msg.Meta.Subject, Sender: sender,
		To: strings.Join(toParts, ", "), Time: msg.Meta.Time,
		Unread: bool(msg.Meta.Unread), Text: msg.Text, HTML: msg.HTML,
	}
}

func hasInbox(meta protonmail.Summary) bool {
	for _, l := range meta.LabelIDs {
		if l == protonmail.InboxID {
			return true
		}
	}
	return false
}

// startEvents starts the mailbox event stream once the API is reachable
// (Linux parity). Safe to call again after a failed start or StatusUp.
func (e *Engine) startEvents(s *session) {
	if s == nil || !e.sessionAlive(s) {
		return
	}
	if s.acc.Kind() == mailbox.KindUnified {
		return
	}
	s.mu.Lock()
	if s.events {
		s.mu.Unlock()
		return
	}
	s.events = true
	startLoops := !s.loopsStarted
	s.loopsStarted = true
	s.mu.Unlock()

	if startLoops {
		go e.catchUp(s, true)
		go e.sweepLoop(s)
	} else {
		// Reconnect after a failed start: light catch-up only.
		go e.catchUp(s, false)
	}
	go e.watchEvents(s)
}

func (e *Engine) watchEvents(s *session) {
	clearEvents := func() {
		s.mu.Lock()
		s.events = false
		s.mu.Unlock()
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("macbridge events panic %s: %v", s.acc.Username(), r)
			clearEvents()
		}
	}()
	acc := s.acc
	ctx := s.ctx
	if ctx == nil {
		ctx = e.ctx
	}
	err := acc.Events(ctx,
		func(meta protonmail.Summary) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("macbridge onNew panic %s: %v", acc.Username(), r)
				}
			}()
			e.onNewMessage(s, meta)
		},
		func() {
			e.emitMailChanged(acc.Username())
		},
	)
	if err != nil {
		clearEvents()
		if ctx.Err() != nil || !e.sessionAlive(s) {
			return
		}
		log.Printf("macbridge events %s: %v", acc.Username(), err)
		if !protonmail.IsOffline(err) {
			e.emit(Event{Type: "eventsError", Account: acc.Username(), Payload: err.Error()})
		}
		// StatusUp (Proton) / later startEvents retries after a failed start.
		return
	}
	// Stream is running (Proton/IMAP return nil after starting). Wait until
	// the session is replaced or the engine stops so startEvents can run again.
	select {
	case <-ctx.Done():
	case <-e.ctx.Done():
	}
	clearEvents()
}

func (e *Engine) holdNew(s *session, meta protonmail.Summary) bool {
	e.mu.Lock()
	holdUntil := e.cfg.HoldUntilChecked
	e.mu.Unlock()
	held := holdUntil && hasInbox(meta)
	if held {
		if _, known := e.filter.Decision(meta.ID); known {
			held = false
		}
	}
	if held {
		s.hold(meta.ID)
		e.emitMailChanged(s.acc.Username())
	}
	return held
}

func (e *Engine) onNewMessage(s *session, meta protonmail.Summary) {
	held := e.holdNew(s, meta)
	go e.checkNew(s, meta, held, true)
}

func (e *Engine) checkNew(s *session, meta protonmail.Summary, held, notify bool) {
	if !s.begin(meta.ID) {
		return
	}
	defer s.end(meta.ID)

	if _, known := e.filter.Decision(meta.ID); known {
		notify = false
	}

	d, moved, err := e.filter.ProcessNew(e.ctx, s.acc, meta)
	if held {
		s.release(meta.ID)
		e.emitMailChanged(s.acc.Username())
	}
	if err != nil {
		log.Printf("macbridge spam: %v", err)
	}
	if moved {
		e.emit(Event{
			Type:    "spamMoved",
			Account: s.acc.Username(),
			Payload: map[string]any{"id": meta.ID, "subject": meta.Subject, "from": d.From},
		})
		return
	}
	var applied []string
	if !d.Spam {
		applied = e.applyRules(e.ctx, s.acc, meta)
		e.mu.Lock()
		autoLabel := e.cfg.AutoLabel
		e.mu.Unlock()
		if autoLabel && len(applied) == 0 {
			if l := e.autoLabelMessage(s.acc, meta); l != "" {
				applied = append(applied, l)
			}
		}
		if len(applied) > 0 {
			e.emit(Event{
				Type:    "rulesApplied",
				Account: s.acc.Username(),
				Payload: map[string]any{"id": meta.ID, "rules": applied, "subject": meta.Subject},
			})
		}
	}
	if d.Spam || !notify {
		return
	}
	e.emit(Event{
		Type:    "newMail",
		Account: s.acc.Username(),
		Payload: e.summaryToDTO(meta),
	})
}
