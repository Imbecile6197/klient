package macbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/ical"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

func (e *Engine) inviteParse(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		MsgID   string `json:"messageId"`
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
	msg, err := s.acc.Get(ctx, p.MsgID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	var invites []map[string]any
	for _, att := range msg.Attachments {
		name := strings.ToLower(att.Name)
		if !strings.Contains(att.MIMEType, "calendar") && !strings.HasSuffix(name, ".ics") {
			continue
		}
		data, err := s.acc.AttachmentData(ctx, att)
		if err != nil {
			continue
		}
		ev, err := ical.Parse(data)
		if err != nil || ev.Method == "REPLY" {
			continue
		}
		invites = append(invites, map[string]any{
			"attachmentId": att.ID,
			"summary":      ev.Summary,
			"location":     ev.Location,
			"method":       ev.Method,
			"organizer":    ev.Organizer.Email,
			"when":         formatEventWhen(ev),
			"ics":          base64.StdEncoding.EncodeToString(data),
		})
	}
	return Response{OK: true, Result: invites}
}

func formatEventWhen(ev *ical.Event) string {
	if ev.Start.IsZero() {
		return ""
	}
	if ev.AllDay {
		return ev.Start.Format("2006-01-02")
	}
	if !ev.End.IsZero() {
		return ev.Start.Format("2006-01-02 15:04") + " – " + ev.End.Format("15:04")
	}
	return ev.Start.Format("2006-01-02 15:04")
}

func (e *Engine) inviteReply(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		MsgID   string `json:"messageId"`
		ICS     string `json:"ics"` // base64
		Status  string `json:"status"` // ACCEPTED, TENTATIVE, DECLINED
		Me      string `json:"me"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	raw, err := base64.StdEncoding.DecodeString(p.ICS)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ev, err := ical.Parse(raw)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Minute)
	defer cancel()
	msg, err := s.acc.Get(ctx, p.MsgID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	me := p.Me
	if me == "" {
		me = s.acc.Email()
	}
	name := s.acc.DisplayName()
	verb := map[string]string{
		"ACCEPTED": "Accepted", "TENTATIVE": "Tentative", "DECLINED": "Declined",
	}[p.Status]
	d := &protonmail.Draft{
		FromAddressID: msg.Meta.AddressID,
		To:            []*mail.Address{{Name: ev.Organizer.Name, Address: ev.Organizer.Email}},
		Subject:       verb + ": " + ev.Summary,
		SignExternal:  true,
		Body: fmt.Sprintf("%s has replied to the invitation “%s” (%s): %s.",
			name, ev.Summary, formatEventWhen(ev), strings.ToLower(verb)),
	}
	for _, ad := range s.acc.SendAddresses() {
		if strings.EqualFold(ad.Email, me) {
			d.FromAddressID = ad.ID
		}
	}
	reply := ev.Reply(me, name, p.Status, time.Now())
	d.Attachments = []*protonmail.Outgoing{{
		Name: "invite.ics", MIMEType: "text/calendar", Data: reply, Size: int64(len(reply)),
	}}
	if err := s.acc.Send(ctx, d); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) inviteSaveICS(params json.RawMessage) Response {
	var p struct {
		ICS     string `json:"ics"`
		Summary string `json:"summary"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	raw, err := base64.StdEncoding.DecodeString(p.ICS)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	dir := filepath.Join(os.TempDir(), "klient-invites")
	_ = os.MkdirAll(dir, 0o700)
	name := p.Summary
	if name == "" {
		name = "event"
	}
	for _, c := range `/\?%*:|"<>.` {
		name = strings.ReplaceAll(name, string(c), "_")
	}
	path := filepath.Join(dir, name+".ics")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"path": path}}
}
