package macbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/pgpmime"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

var errNoAccount = errors.New("no account")

func (e *Engine) pgpOwnPublicKey(params json.RawMessage) Response {
	var p struct {
		Account   string `json:"account"`
		AddressID string `json:"addressId"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.AddressID == "" {
		addrs := s.acc.SendAddresses()
		if len(addrs) == 0 {
			return Response{OK: false, Error: "no address"}
		}
		p.AddressID = addrs[0].ID
	}
	armored, err := s.acc.OwnPublicKey(p.AddressID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"publicKey": armored}}
}

func (e *Engine) pgpImportContactKey(params json.RawMessage) Response {
	var p struct {
		Armored string `json:"armored"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	emails, err := pgp.ImportKey(p.Armored)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"emails": emails}}
}

// pgpImportAttachment imports a public key from a message attachment (Linux importKeyButton).
func (e *Engine) pgpImportAttachment(params json.RawMessage) Response {
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
	if !isKeyAttachment(*att) {
		return Response{OK: false, Error: i18n.T("the attachment does not contain a public PGP key")}
	}
	data, err := s.acc.AttachmentData(ctx, *att)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if !strings.Contains(string(data), "BEGIN PGP PUBLIC KEY BLOCK") {
		return Response{OK: false, Error: i18n.T("the attachment does not contain a public PGP key")}
	}
	emails, err := pgp.ImportKey(string(data))
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"emails": emails}}
}

func isKeyAttachment(att protonmail.Attachment) bool {
	n := strings.ToLower(att.Name)
	return att.MIMEType == "application/pgp-keys" || strings.HasSuffix(n, ".asc") ||
		strings.HasSuffix(n, ".pub") || (strings.HasSuffix(n, ".key") && strings.Contains(n, "pgp")) ||
		strings.HasPrefix(n, "publickey")
}

// pgpSenderKeyNotice returns a notice when the sender's key changed or a
// pending key awaits acceptance (Linux senderKeyNotice).
func (e *Engine) pgpSenderKeyNotice(params json.RawMessage) Response {
	var p struct {
		Sender string `json:"sender"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	sender := strings.TrimSpace(p.Sender)
	if sender == "" || pgp.StoredKey(sender) == nil {
		return Response{OK: true, Result: map[string]any{"show": false}}
	}
	m := pgp.Meta(sender)
	switch {
	case pgp.PendingKey(sender) != nil:
		return Response{OK: true, Result: map[string]any{
			"show": true, "warn": true, "pending": true,
			"text": i18n.T("The sender uses a new key that Klient has not accepted yet."),
			"email": sender,
		}}
	case m.ReplacedFP != "" && time.Since(m.ReplacedAt) < 14*24*time.Hour:
		return Response{OK: true, Result: map[string]any{
			"show": true, "warn": false, "pending": false,
			"text": fmt.Sprintf(i18n.T("The sender's key changed on %s."), m.ReplacedAt.Format("2 Jan 2006")),
			"email": sender,
		}}
	default:
		return Response{OK: true, Result: map[string]any{"show": false}}
	}
}

func (e *Engine) pgpAcceptPending(params json.RawMessage) Response {
	var p struct {
		Email string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := pgp.AcceptPending(p.Email); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) pgpRejectPending(params json.RawMessage) Response {
	var p struct {
		Email string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := pgp.RejectPending(p.Email); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) pgpListKeys(_ json.RawMessage) Response {
	return Response{OK: true, Result: pgp.LocalKeys()}
}

func (e *Engine) pgpDeleteKey(params json.RawMessage) Response {
	var p struct {
		Email string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := pgp.DeleteKey(p.Email); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

// ownKeyEmail resolves the IMAP address for own-key ops (Proton uses account keys).
func (e *Engine) ownKeyEmail(account, email string) (string, *session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if account == "" {
		if email == "" {
			return "", nil, errNoAccount
		}
		return email, nil, nil
	}
	s, err := e.session(account)
	if err != nil {
		return "", nil, err
	}
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(s.acc.Email()))
	}
	return email, s, nil
}

func (e *Engine) pgpOwnKeyStatus(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Email   string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, s, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	out := map[string]any{
		"email":   email,
		"hasKey":  pgp.HasOwnKey(email),
		"kind":    "",
		"managed": true, // IMAP own-key lifecycle
	}
	if s != nil {
		out["kind"] = string(s.acc.Kind())
		out["displayName"] = s.acc.DisplayName()
		// Proton / unified: show public key via account API, no generate/revoke here.
		if s.acc.Kind() != mailbox.KindIMAP {
			out["managed"] = false
			addrID := ""
			if addrs := s.acc.SendAddresses(); len(addrs) > 0 {
				addrID = addrs[0].ID
			}
			if armored, err := s.acc.OwnPublicKey(addrID); err == nil {
				out["publicKey"] = armored
				out["hasKey"] = armored != ""
			}
			return Response{OK: true, Result: out}
		}
	}
	if !pgp.HasOwnKey(email) {
		return Response{OK: true, Result: out}
	}
	pub, k, err := pgp.OwnPublicKey(email)
	if err != nil {
		out["error"] = err.Error()
		return Response{OK: true, Result: out}
	}
	det := pgp.Details(k)
	out["publicKey"] = pub
	out["fingerprint"] = formatFingerprint(det.Fingerprint)
	out["algorithm"] = det.Algorithm
	out["expired"] = det.Expired
	out["revoked"] = det.Revoked
	if !det.Created.IsZero() {
		out["created"] = det.Created.Unix()
	}
	if !det.Expires.IsZero() {
		out["expires"] = det.Expires.Unix()
		out["expiresSoon"] = time.Until(det.Expires) < 60*24*time.Hour && !det.Expired
	}
	return Response{OK: true, Result: out}
}

func formatFingerprint(fp string) string {
	fp = strings.ToUpper(strings.ReplaceAll(fp, " ", ""))
	var b strings.Builder
	for i, c := range fp {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (e *Engine) pgpGenerateOwnKey(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Years   int    `json:"years"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, s, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if s != nil && s.acc.Kind() != mailbox.KindIMAP {
		return Response{OK: false, Error: "own-key lifecycle is for IMAP accounts"}
	}
	name := strings.TrimSpace(p.Name)
	if name == "" && s != nil {
		name = s.acc.DisplayName()
	}
	if name == "" {
		name = email
	}
	if err := pgp.GenerateOwnKeyFor(name, email, p.Years); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"email": email}}
}

func (e *Engine) pgpImportOwnKey(params json.RawMessage) Response {
	var p struct {
		Account    string `json:"account"`
		Email      string `json:"email"`
		Armored    string `json:"armored"`
		Passphrase string `json:"passphrase"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, s, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if s != nil && s.acc.Kind() != mailbox.KindIMAP {
		return Response{OK: false, Error: "own-key lifecycle is for IMAP accounts"}
	}
	if err := pgp.ImportOwnKey(p.Armored, p.Passphrase, email); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"email": email}}
}

func (e *Engine) pgpSetOwnExpiry(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Email   string `json:"email"`
		Years   int    `json:"years"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, _, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := pgp.SetOwnExpiry(email, p.Years); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (e *Engine) pgpPublishOwnKey(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Email   string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, _, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	pub, _, err := pgp.OwnPublicKey(email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	st, err := pgpmime.Publish(e.ctx, pub, []string{email}, i18n.Lang())
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	status := st[email]
	return Response{OK: true, Result: map[string]any{
		"status": status,
		"email":  email,
		"all":    st,
	}}
}

func (e *Engine) pgpRevocationCert(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Email   string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, _, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	cert, err := pgp.RevocationCertificate(email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{
		"certificate": cert,
		"email":       email,
	}}
}

func (e *Engine) pgpExportOwnPrivate(params json.RawMessage) Response {
	var p struct {
		Account    string `json:"account"`
		Email      string `json:"email"`
		Passphrase string `json:"passphrase"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if strings.TrimSpace(p.Passphrase) == "" {
		return Response{OK: false, Error: "passphrase required"}
	}
	email, _, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	armored, err := pgp.ExportOwnPrivate(email, p.Passphrase)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{
		"privateKey": armored,
		"email":      email,
	}}
}

func (e *Engine) pgpDeleteOwnKey(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
		Email   string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	email, _, err := e.ownKeyEmail(p.Account, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := pgp.DeleteOwnKey(email); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}
