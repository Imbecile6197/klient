package macbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

func (e *Engine) mailMarkUnread(params json.RawMessage) Response {
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
	if err := s.acc.MarkUnread(ctx, p.IDs...); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) mailSetLabel(params json.RawMessage) Response {
	var p struct {
		Account string   `json:"account"`
		Label   string   `json:"label"`
		On      bool     `json:"on"`
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
	if err := s.acc.SetLabel(ctx, p.Label, p.On, p.IDs...); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) mailSearch(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Folder  string `json:"folder"`
		Query   string `json:"query"`
		Max     int    `json:"max"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.Max <= 0 {
		p.Max = 50
	}
	if p.Folder == "" {
		p.Folder = protonmail.AllMailID
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 90*time.Second)
	defer cancel()
	list, err := s.acc.Search(ctx, p.Folder, p.Query, p.Max)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	out := make([]summaryDTO, 0, len(list))
	for _, m := range list {
		out = append(out, e.summaryToDTO(m))
	}
	return Response{OK: true, Result: out}
}

func (e *Engine) mailThread(params json.RawMessage) Response {
	var p struct {
		Account        string `json:"account"`
		ConversationID string `json:"conversationId"`
		IncludeHidden  bool   `json:"includeHidden"`
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
	list, err := s.acc.ThreadMessages(ctx, p.ConversationID, p.IncludeHidden)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	out := make([]summaryDTO, 0, len(list))
	for _, m := range list {
		out = append(out, e.summaryToDTO(m))
	}
	return Response{OK: true, Result: out}
}

func (e *Engine) mailUnreadCounts(params json.RawMessage) Response {
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
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	counts, err := s.acc.UnreadCounts(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: counts}
}

func (e *Engine) mailAttachment(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		MsgID   string `json:"messageId"`
		AttID   string `json:"attachmentId"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 120*time.Second)
	defer cancel()
	msg, err := s.acc.Get(ctx, p.MsgID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	var att *protonmail.Attachment
	for i := range msg.Attachments {
		if msg.Attachments[i].ID == p.AttID || msg.Attachments[i].Name == p.AttID {
			att = &msg.Attachments[i]
			break
		}
	}
	if att == nil {
		return Response{OK: false, Error: "attachment not found"}
	}
	data, err := s.acc.AttachmentData(ctx, *att)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{
		"name":     att.Name,
		"mimeType": att.MIMEType,
		"size":     att.Size,
		"data":     base64.StdEncoding.EncodeToString(data),
	}}
}

func (e *Engine) mailExportEML(params json.RawMessage) Response {
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
	b, err := s.acc.ExportEML(ctx, p.ID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{
		"data": base64.StdEncoding.EncodeToString(b),
	}}
}

func (e *Engine) mailGetFull(params json.RawMessage) Response {
	// Richer get used by the Swift reader (attachments meta + labels + safe HTML).
	var p struct {
		Account     string `json:"account"`
		ID          string `json:"id"`
		AllowRemote *bool  `json:"allowRemote"`
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
	msg, err := s.acc.Get(ctx, p.ID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	_ = s.acc.MarkRead(ctx, p.ID)
	dto := messageToDTO(msg)
	atts := make([]map[string]any, 0, len(msg.Attachments))
	for _, a := range msg.Attachments {
		atts = append(atts, map[string]any{
			"id": a.ID, "name": a.Name, "size": a.Size, "mimeType": a.MIMEType, "contentId": a.ContentID,
		})
	}
	inline := s.acc.InlineImages(ctx, msg)
	sender := ""
	senderAddr := ""
	if msg.Meta.Sender != nil {
		sender = mailparse.DisplayAddress(msg.Meta.Sender)
		senderAddr = senderEmail(msg.Meta.Sender)
	}
	allow := e.remoteAllowed(senderAddr)
	if p.AllowRemote != nil {
		allow = *p.AllowRemote || allow
	}
	bodyHTML, hasRemote := prepareMessageHTML(msg.HTML, msg.Text, inline, allow)
	u := mailparse.ParseUnsubscribe(msg.Headers)
	var unsub map[string]any
	if u.Any() {
		unsub = map[string]any{
			"http": u.HTTP, "mailto": u.Mailto, "oneClick": u.OneClick,
		}
	}
	return Response{OK: true, Result: map[string]any{
		"id": dto.ID, "subject": dto.Subject, "sender": sender, "senderEmail": senderAddr,
		"to": dto.To, "time": dto.Time, "unread": dto.Unread, "text": dto.Text, "html": dto.HTML,
		"bodyHTML": bodyHTML, "hasRemoteContent": hasRemote, "remoteAllowed": allow,
		"unsubscribe": unsub,
		"labels": msg.Meta.LabelIDs, "attachments": atts, "inlineImages": inline,
		"encryption": msg.Encryption, "conversationId": msg.Meta.ConversationID,
	}}
}

func (e *Engine) mailCaps(params json.RawMessage) Response {
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
	c := s.acc.Caps()
	return Response{OK: true, Result: map[string]any{
		"kind":             s.acc.Kind(),
		"labels":           c.Labels,
		"serverSnooze":     c.ServerSnooze,
		"serverSchedule":   c.ServerSchedule,
		"localSnooze":      c.LocalSnooze,
		"localSchedule":    c.LocalSchedule,
		"autoReply":        c.AutoReply,
		"e2e":              c.E2E,
		"contacts":         c.Contacts,
		"fullTextSearch":   c.FullTextSearch,
		"canSnooze":        c.CanSnooze(),
		"canSchedule":      c.CanSchedule(),
		"startedOffline":   s.acc.StartedOffline(),
		"storage":          storageDTO(s.acc.Storage()),
		"sendAddresses":    sendAddrsDTO(s),
	}}
}

func storageDTO(st protonmail.StorageInfo) map[string]any {
	out := map[string]any{
		"used": st.Used, "max": st.Max,
		"mail": st.Mail, "drive": st.Drive,
		"split": st.Split, "driveMax": st.DriveMax,
	}
	if !st.Updated.IsZero() {
		out["updated"] = st.Updated.Unix()
	}
	return out
}

func sendAddrsDTO(s *session) []map[string]string {
	addrs := s.acc.SendAddresses()
	out := make([]map[string]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, map[string]string{
			"id": a.ID, "email": a.Email, "name": a.DisplayName,
		})
	}
	return out
}
