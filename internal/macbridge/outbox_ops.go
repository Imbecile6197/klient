package macbridge

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// Outbox keeps messages whose sending failed (parity with Linux outbox.go).

const outboxKey = "outbox"

var outboxRetry = time.Minute

type outboxItem struct {
	ID      string           `json:"id"`
	Draft   protonmail.Draft `json:"draft"`
	Error   string           `json:"error"`
	Waiting bool             `json:"waiting"`
	Added   time.Time        `json:"added"`

	account string // username; set when loaded / added, not persisted in each row's own cache file
	sending bool
}

func networkError(err error) bool {
	if err == nil {
		return false
	}
	if protonmail.IsOffline(err) || imapmail.IsOffline(err) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

func newOutboxID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (e *Engine) loadOutbox(acc mailbox.Account) {
	if acc == nil || acc.Cache() == nil {
		return
	}
	var items []*outboxItem
	if !acc.Cache().Get(outboxKey, &items) {
		return
	}
	id := acc.Username()
	e.mu.Lock()
	for _, it := range items {
		it.account = id
		e.outbox = append(e.outbox, it)
	}
	e.mu.Unlock()
	e.emitOutboxChanged()
}

func (e *Engine) saveOutbox(acc mailbox.Account) {
	if acc == nil || acc.Cache() == nil {
		return
	}
	id := acc.Username()
	e.mu.Lock()
	items := []*outboxItem{}
	for _, it := range e.outbox {
		if it.account == id {
			items = append(items, it)
		}
	}
	e.mu.Unlock()
	_ = acc.Cache().Set(outboxKey, items)
}

func (e *Engine) clearOutboxAccount(accountID string) {
	e.mu.Lock()
	var filtered []*outboxItem
	for _, it := range e.outbox {
		if it.account != accountID {
			filtered = append(filtered, it)
		}
	}
	e.outbox = filtered
	e.mu.Unlock()
	e.emitOutboxChanged()
}

func (e *Engine) addToOutbox(acc mailbox.Account, d *protonmail.Draft, err error) *outboxItem {
	it := &outboxItem{
		ID:      newOutboxID(),
		Draft:   *d,
		Error:   err.Error(),
		Waiting: networkError(err),
		Added:   time.Now(),
		account: acc.Username(),
	}
	it.Draft.DeliveryTime = time.Time{}
	e.mu.Lock()
	e.outbox = append(e.outbox, it)
	e.mu.Unlock()
	e.saveOutbox(acc)
	e.emitOutboxChanged()
	payload := map[string]any{
		"id": it.ID, "waiting": it.Waiting, "error": it.Error,
		"subject": it.Draft.Subject, "account": it.account,
	}
	e.emit(Event{Type: "outboxQueued", Account: it.account, Payload: payload})
	return it
}

func (e *Engine) removeFromOutbox(it *outboxItem) {
	e.mu.Lock()
	for i, x := range e.outbox {
		if x == it {
			e.outbox = append(e.outbox[:i], e.outbox[i+1:]...)
			break
		}
	}
	accID := it.account
	e.mu.Unlock()
	if s, err := e.session(accID); err == nil {
		e.saveOutbox(s.acc)
	}
	e.emitOutboxChanged()
}

func (e *Engine) findOutbox(id string) *outboxItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, it := range e.outbox {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (e *Engine) shownOutbox() []*outboxItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []*outboxItem
	for _, it := range e.outbox {
		if _, ok := e.sessions[it.account]; ok {
			out = append(out, it)
		}
	}
	return out
}

func (e *Engine) emitOutboxChanged() {
	items := e.outboxListDTO()
	e.emit(Event{Type: "outboxChanged", Payload: map[string]any{
		"count": len(items),
		"items": items,
	}})
}

func (e *Engine) outboxListDTO() []map[string]any {
	items := e.shownOutbox()
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		var to []string
		for _, r := range append(append([]*mail.Address{}, it.Draft.To...), it.Draft.CC...) {
			if r != nil {
				to = append(to, r.Address)
			}
		}
		email := ""
		if s, err := e.session(it.account); err == nil {
			email = s.acc.Email()
		}
		status := it.Error
		sending := false
		e.mu.Lock()
		sending = it.sending
		waiting := it.Waiting
		e.mu.Unlock()
		switch {
		case sending:
			status = "sending"
		case waiting:
			status = "waiting"
		}
		out = append(out, map[string]any{
			"id": it.ID, "account": it.account, "email": email,
			"subject": it.Draft.Subject, "to": strings.Join(to, ", "),
			"error": it.Error, "waiting": waiting, "sending": sending,
			"status": status, "added": it.Added.Unix(),
		})
	}
	return out
}

func (e *Engine) outboxList(_ json.RawMessage) Response {
	items := e.outboxListDTO()
	return Response{OK: true, Result: map[string]any{
		"count": len(items),
		"items": items,
	}}
}

func (e *Engine) sendFromOutbox(it *outboxItem, manual bool) {
	s, err := e.session(it.account)
	if err != nil {
		return
	}
	e.mu.Lock()
	if it.sending {
		e.mu.Unlock()
		return
	}
	it.sending = true
	e.mu.Unlock()
	e.emitOutboxChanged()

	d := it.Draft
	go func() {
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
		defer cancel()
		err := s.acc.Send(ctx, &d)
		e.mu.Lock()
		it.sending = false
		e.mu.Unlock()
		if err == nil {
			e.removeFromOutbox(it)
			subj := d.Subject
			if subj == "" {
				subj = "(no subject)"
			}
			e.emit(Event{Type: "outboxSent", Account: it.account, Payload: map[string]any{
				"subject": subj,
			}})
			e.emit(Event{Type: "mailChanged", Account: it.account})
			return
		}
		e.mu.Lock()
		it.Draft.ID = d.ID
		it.Error = err.Error()
		it.Waiting = networkError(err)
		e.mu.Unlock()
		e.saveOutbox(s.acc)
		e.emitOutboxChanged()
		if manual {
			e.emit(Event{Type: "outboxSendFailed", Account: it.account, Payload: map[string]any{
				"error": err.Error(),
			}})
		}
	}()
}

func (e *Engine) outboxLoop() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(outboxRetry):
		}
		e.mu.Lock()
		var waiting []*outboxItem
		for _, it := range e.outbox {
			if it.Waiting && !it.sending {
				waiting = append(waiting, it)
			}
		}
		e.mu.Unlock()
		for _, it := range waiting {
			e.sendFromOutbox(it, false)
		}
	}
}

func (e *Engine) outboxSend(params json.RawMessage) Response {
	var p struct {
		ID string `json:"id"`
	}
	if err := parseParams(params, &p); err != nil || p.ID == "" {
		return Response{OK: false, Error: "id required"}
	}
	it := e.findOutbox(p.ID)
	if it == nil {
		return Response{OK: false, Error: "outbox item not found"}
	}
	e.sendFromOutbox(it, true)
	return Response{OK: true}
}

func (e *Engine) outboxDelete(params json.RawMessage) Response {
	var p struct {
		ID string `json:"id"`
	}
	if err := parseParams(params, &p); err != nil || p.ID == "" {
		return Response{OK: false, Error: "id required"}
	}
	it := e.findOutbox(p.ID)
	if it == nil {
		return Response{OK: false, Error: "outbox item not found"}
	}
	e.mu.Lock()
	if it.sending {
		e.mu.Unlock()
		return Response{OK: false, Error: "sending"}
	}
	e.mu.Unlock()
	e.removeFromOutbox(it)
	return Response{OK: true}
}

func draftToComposeDTO(d *protonmail.Draft) map[string]any {
	atts := make([]map[string]string, 0, len(d.Attachments))
	for _, a := range d.Attachments {
		if a == nil {
			continue
		}
		atts = append(atts, map[string]string{
			"name":     a.Name,
			"mimeType": a.MIMEType,
			"data":     base64.StdEncoding.EncodeToString(a.Data),
		})
	}
	return map[string]any{
		"id": d.ID, "fromAddressId": d.FromAddressID,
		"to": formatAddrs(d.To), "cc": formatAddrs(d.CC), "bcc": formatAddrs(d.BCC),
		"subject": d.Subject, "body": d.Body, "html": d.HTML,
		"parentId": d.ParentID, "action": int(d.Action),
		"attachments": atts,
	}
}

func (e *Engine) outboxEdit(params json.RawMessage) Response {
	var p struct {
		ID string `json:"id"`
	}
	if err := parseParams(params, &p); err != nil || p.ID == "" {
		return Response{OK: false, Error: "id required"}
	}
	it := e.findOutbox(p.ID)
	if it == nil {
		return Response{OK: false, Error: "outbox item not found"}
	}
	e.mu.Lock()
	if it.sending {
		e.mu.Unlock()
		return Response{OK: false, Error: "sending"}
	}
	draft := it.Draft
	account := it.account
	e.mu.Unlock()
	e.removeFromOutbox(it)
	result := draftToComposeDTO(&draft)
	result["account"] = account
	return Response{OK: true, Result: result}
}

func (e *Engine) queueSendFailure(acc mailbox.Account, d *protonmail.Draft, err error) Response {
	it := e.addToOutbox(acc, d, err)
	return Response{OK: true, Result: map[string]any{
		"queued":   true,
		"outboxId": it.ID,
		"waiting":  it.Waiting,
		"error":    err.Error(),
		"subject":  d.Subject,
	}}
}
