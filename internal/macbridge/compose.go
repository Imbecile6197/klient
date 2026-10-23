package macbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/richtext"
)

type draftIn struct {
	Account         string `json:"account"`
	ID              string `json:"id"`
	FromAddressID   string `json:"fromAddressId"`
	To              string `json:"to"`
	CC              string `json:"cc"`
	BCC             string `json:"bcc"`
	Subject         string `json:"subject"`
	Body            string `json:"body"`
	HTML            string `json:"html"`
	ParentID        string `json:"parentId"`
	Action          int    `json:"action"` // 0 new, 1 reply, 2 replyAll, 3 forward
	SignExternal    bool   `json:"signExternal"`
	AttachPublicKey bool   `json:"attachPublicKey"`
	DeliveryTime    string `json:"deliveryTime"` // RFC3339
	Attachments     []struct {
		Name     string `json:"name"`
		MIMEType string `json:"mimeType"`
		Data     string `json:"data"` // base64
	} `json:"attachments"`
}

func (e *Engine) draftFrom(p draftIn) (*protonmail.Draft, error) {
	d := &protonmail.Draft{
		ID:            p.ID,
		FromAddressID: p.FromAddressID,
		To:            parseAddressList(p.To),
		CC:            parseAddressList(p.CC),
		BCC:           parseAddressList(p.BCC),
		Subject:       p.Subject,
		Body:          p.Body,
		HTML:          p.HTML,
		ParentID:      p.ParentID,
		Action:        protonmail.ComposeAction(p.Action),
		SignExternal:  p.SignExternal,
	}
	if t, err := parseTime(p.DeliveryTime); err == nil {
		d.DeliveryTime = t
	}
	for _, a := range p.Attachments {
		raw, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil {
			return nil, err
		}
		d.Attachments = append(d.Attachments, &protonmail.Outgoing{
			Name: a.Name, MIMEType: a.MIMEType, Data: raw, Size: int64(len(raw)),
		})
	}
	return d, nil
}

func (e *Engine) mailSaveDraft(params json.RawMessage) Response {
	var p draftIn
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.FromAddressID == "" {
		addrs := s.acc.SendAddresses()
		if len(addrs) == 0 {
			return Response{OK: false, Error: "no send address"}
		}
		p.FromAddressID = addrs[0].ID
	}
	d, err := e.draftFrom(p)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.appendSignature(d)
	e.syncKeyAttachment(s.acc, d, p.AttachPublicKey)
	ctx, cancel := context.WithTimeout(e.ctx, 120*time.Second)
	defer cancel()
	if err := s.acc.SaveDraft(ctx, d); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"id": d.ID}}
}

func (e *Engine) mailSend(params json.RawMessage) Response {
	var p draftIn
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.FromAddressID == "" {
		addrs := s.acc.SendAddresses()
		if len(addrs) == 0 {
			return Response{OK: false, Error: "no send address"}
		}
		p.FromAddressID = addrs[0].ID
	}
	d, err := e.draftFrom(p)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.appendSignature(d)
	e.syncKeyAttachment(s.acc, d, p.AttachPublicKey)
	ctx, cancel := context.WithTimeout(e.ctx, 180*time.Second)
	defer cancel()
	if err := s.acc.Send(ctx, d); err != nil {
		// Nothing written is lost: queue in the Outbox (Linux compose.go).
		return e.queueSendFailure(s.acc, d, err)
	}
	return Response{OK: true, Result: map[string]string{"id": d.ID}}
}

// appendSignature adds the configured signature to new messages (action 0).
// With SignatureUseHTML, the draft gets an HTML body so the signature renders.
func (e *Engine) appendSignature(d *protonmail.Draft) {
	if d == nil || d.Action != protonmail.ActionNew {
		return
	}
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()
	if cfg.SignatureUseHTML {
		sig := richtext.CleanSignature(cfg.SignatureHTML)
		if sig == "" {
			return
		}
		htmlSig := richtext.SignatureHTML(sig)
		text, _ := mailparse.HTMLToText(sig)
		var lines []string
		for _, l := range strings.Split(text, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
		plainSig := "-- \n" + strings.Join(lines, "\n")
		main := d.Body
		d.Body = strings.TrimRight(main, "\n")
		if d.Body != "" {
			d.Body += "\n\n"
		}
		d.Body += plainSig
		if d.HTML != "" {
			d.HTML = d.HTML + htmlSig
		} else {
			d.HTML = plainBodyHTML(main) + htmlSig
		}
		return
	}
	sig := strings.TrimSpace(cfg.Signature)
	if sig == "" {
		return
	}
	d.Body = strings.TrimRight(d.Body, "\n")
	if d.Body != "" {
		d.Body += "\n\n"
	}
	d.Body += "-- \n" + sig
}

func plainBodyHTML(text string) string {
	esc := html.EscapeString(text)
	esc = strings.ReplaceAll(esc, "\n", "<br>")
	return `<div style="font-family:sans-serif">` + esc + `</div>`
}

// syncKeyAttachment adds or removes the sender's public key attachment (Linux compose).
func (e *Engine) syncKeyAttachment(acc mailbox.Account, d *protonmail.Draft, want bool) {
	idx := -1
	for i, att := range d.Attachments {
		if att.PublicKey {
			idx = i
		}
	}
	switch {
	case want && idx >= 0 && d.Attachments[idx].KeyAddressID != d.FromAddressID:
		e.syncKeyAttachment(acc, d, false)
		e.syncKeyAttachment(acc, d, true)
	case want && idx < 0:
		key, name, err := acc.PublicKeyAttachment(d.FromAddressID)
		if err != nil {
			return
		}
		d.Attachments = append(d.Attachments, &protonmail.Outgoing{
			Name: name, MIMEType: "application/pgp-keys", Data: []byte(key), Size: int64(len(key)),
			PublicKey: true, KeyAddressID: d.FromAddressID,
		})
	case !want && idx >= 0:
		if d.Attachments[idx].UploadedID != "" {
			d.Recreate = true
		}
		d.Attachments = append(d.Attachments[:idx], d.Attachments[idx+1:]...)
	}
}

func (e *Engine) mailOpenDraft(params json.RawMessage) Response {
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
	d, msg, err := s.acc.OpenDraft(ctx, p.ID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{
		"id": d.ID, "fromAddressId": d.FromAddressID,
		"to": formatAddrs(d.To), "cc": formatAddrs(d.CC), "bcc": formatAddrs(d.BCC),
		"subject": d.Subject, "body": d.Body, "html": d.HTML,
		"parentId": d.ParentID, "action": int(d.Action),
		"message": messageToDTO(msg),
	}}
}

func (e *Engine) mailDeleteDraft(params json.RawMessage) Response {
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
	if err := s.acc.DeleteDraft(ctx, p.ID); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) mailPlanEncryption(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		To      string `json:"to"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	modes := s.acc.PlanEncryption(e.ctx, parseAddressList(p.To))
	out := make([]map[string]any, 0, len(modes))
	for _, m := range modes {
		out = append(out, map[string]any{
			"address": m.Address, "scheme": m.Scheme, "keyFp": m.KeyFP,
		})
	}
	return Response{OK: true, Result: out}
}
