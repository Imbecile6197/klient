package macbridge

import (
	"context"
	"encoding/json"
	"net/mail"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/rules"
)

func (e *Engine) systemLabels(_ json.RawMessage) Response {
	return Response{OK: true, Result: map[string]string{
		"inbox":     protonmail.InboxID,
		"spam":      protonmail.SpamID,
		"trash":     protonmail.TrashID,
		"archive":   protonmail.ArchiveID,
		"starred":   protonmail.StarredID,
		"drafts":    protonmail.DraftsID,
		"sent":      protonmail.SentID,
		"allMail":   protonmail.AllMailID,
		"scheduled": protonmail.ScheduledID,
		"snoozed":   protonmail.SnoozedID,
	}}
}

func ruleMessage(meta protonmail.Summary) rules.Message {
	return rules.Message{
		From:    meta.Sender,
		To:      append(append([]*mail.Address{}, meta.ToList...), meta.CCList...),
		Subject: meta.Subject,
	}
}

func (e *Engine) applyRules(ctx context.Context, acc mailbox.Account, meta protonmail.Summary) []string {
	e.mu.Lock()
	rs := append([]rules.Rule(nil), e.cfg.Rules...)
	e.mu.Unlock()
	var done []string
	for _, r := range rules.Matching(rs, ruleMessage(meta)) {
		var err error
		switch r.Action {
		case rules.ActionMove:
			err = acc.Move(ctx, r.Target, meta.ID)
		case rules.ActionLabel:
			err = acc.SetLabel(ctx, r.Target, true, meta.ID)
		case rules.ActionRead:
			err = acc.MarkRead(ctx, meta.ID)
		case rules.ActionStar:
			err = acc.SetLabel(ctx, protonmail.StarredID, true, meta.ID)
		}
		if err == nil {
			name := r.Name
			if name == "" {
				name = rules.ActionName(r.Action)
			}
			done = append(done, name)
		}
	}
	return done
}

func (e *Engine) rulesApplyInbox(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"` // empty = all sessions
	}
	_ = parseParams(params, &p)
	e.mu.Lock()
	if len(e.cfg.Rules) == 0 {
		e.mu.Unlock()
		return Response{OK: false, Error: "no rules configured"}
	}
	var accs []mailbox.Account
	if p.Account != "" {
		if s, ok := e.sessions[p.Account]; ok {
			accs = append(accs, s.acc)
		}
	} else {
		for _, s := range e.sessions {
			if s.acc.UserID() == "unified" {
				continue
			}
			accs = append(accs, s.acc)
		}
	}
	e.mu.Unlock()

	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
	defer cancel()
	n := 0
	for _, acc := range accs {
		msgs, err := acc.List(ctx, protonmail.InboxID, 0, 150)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		for _, s := range msgs {
			if len(e.applyRules(ctx, acc, s)) > 0 {
				n++
			}
		}
	}
	return Response{OK: true, Result: map[string]int{"applied": n}}
}
