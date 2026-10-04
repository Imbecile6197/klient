package protonmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/ProtonMail/gluon/rfc822"
	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mimebuild"
)

// MaxAttachmentsSize is Proton's limit for all attachments of one message.
const MaxAttachmentsSize = 25 << 20

// Outgoing is an attachment of a message being written. Data is set for new
// files; UploadedID is set once it is stored in the draft on the server.
type Outgoing struct {
	Name       string
	MIMEType   string
	Data       []byte
	Size       int64
	UploadedID string
	// PublicKey marks the sender's own public key, attached on request.
	PublicKey    bool
	KeyAddressID string
}

type ComposeAction int

const (
	ActionNew ComposeAction = iota
	ActionReply
	ActionReplyAll
	ActionForward
)

// Draft is the state of a message being written.
type Draft struct {
	ID            string // "" until first saved
	FromAddressID string
	To, CC, BCC   []*mail.Address
	Subject       string
	Body          string // plain text (always set)
	HTML          string // formatted version, "" = plain-text message
	Attachments   []*Outgoing
	// Set when an already uploaded attachment was removed: the draft is then
	// recreated because the API has no call to delete a single attachment.
	Recreate bool

	ParentID string
	Action   ComposeAction
	// SignExternal adds a detached PGP signature to unencrypted mail.
	SignExternal bool
	// DeliveryTime schedules the message on the server; zero sends now.
	DeliveryTime time.Time
}

func (a *Account) fromAddress(id string) (*mail.Address, error) {
	for _, addr := range a.SendAddresses() {
		if addr.ID == id {
			return &mail.Address{Name: addr.DisplayName, Address: addr.Email}, nil
		}
	}
	return nil, errors.New(i18n.T("sender address not found"))
}

// SaveDraft creates or updates the draft on the server (encrypted with the
// address key) and uploads new attachments. It sets d.ID.
func (a *Account) SaveDraft(ctx context.Context, d *Draft) error {
	kr, err := a.addrKR(d.FromAddressID)
	if err != nil {
		return err
	}
	from, err := a.fromAddress(d.FromAddressID)
	if err != nil {
		return err
	}
	tmpl := proton.DraftTemplate{
		Subject: d.Subject, Sender: from,
		ToList: nonNil(d.To), CCList: nonNil(d.CC), BCCList: nonNil(d.BCC),
		Body: d.Body, MIMEType: rfc822.TextPlain,
	}
	if d.HTML != "" {
		tmpl.Body, tmpl.MIMEType = d.HTML, rfc822.TextHTML
	}

	if d.ID != "" && d.Recreate {
		// Kept attachments that only exist on the server must be fetched
		// before the old draft is deleted.
		old, err := a.client.GetMessage(ctx, d.ID)
		if err != nil {
			return err
		}
		for _, att := range d.Attachments {
			if att.Data != nil || att.UploadedID == "" {
				continue
			}
			for _, raw := range old.Attachments {
				if raw.ID == att.UploadedID {
					data, err := a.AttachmentData(ctx, Attachment{ID: raw.ID, keyPackets: raw.KeyPackets, addressID: old.AddressID})
					if err != nil {
						return fmt.Errorf(i18n.T("attachment %s: %w"), att.Name, err)
					}
					att.Data = data
				}
			}
		}
		_ = a.client.DeleteMessage(ctx, d.ID)
		d.ID = ""
		for _, att := range d.Attachments {
			att.UploadedID = ""
		}
		d.Recreate = false
	}

	if d.ID == "" {
		req := proton.CreateDraftReq{Message: tmpl, ParentID: d.ParentID}
		switch d.Action {
		case ActionReply:
			req.Action = proton.ReplyAction
		case ActionReplyAll:
			req.Action = proton.ReplyAllAction
		case ActionForward:
			req.Action = proton.ForwardAction
		}
		msg, err := a.client.CreateDraft(ctx, kr, req)
		if err != nil {
			return apiErr(i18n.T("saving the draft failed"), err)
		}
		d.ID = msg.ID
	} else {
		if _, err := a.client.UpdateDraft(ctx, d.ID, kr, proton.UpdateDraftReq{Message: tmpl}); err != nil {
			return apiErr(i18n.T("updating the draft failed"), err)
		}
	}

	for _, att := range d.Attachments {
		if att.UploadedID != "" {
			continue
		}
		if att.Data == nil {
			return fmt.Errorf(i18n.T("attachment %s has no data"), att.Name)
		}
		mt := att.MIMEType
		if mt == "" {
			mt = "application/octet-stream"
		}
		up, err := a.client.UploadAttachment(ctx, kr, proton.CreateAttachmentReq{
			MessageID: d.ID, Filename: att.Name, MIMEType: rfc822.MIMEType(mt),
			Disposition: proton.AttachmentDisposition, Body: att.Data,
		})
		if err != nil {
			return apiErr(fmt.Sprintf(i18n.T("uploading attachment %s failed"), att.Name), err)
		}
		att.UploadedID = up.ID
	}
	return nil
}

func nonNil(l []*mail.Address) []*mail.Address {
	if l == nil {
		return []*mail.Address{}
	}
	return l
}

// Send saves the draft and sends it. Every recipient gets the strongest
// protection available: Proton E2E, PGP with a known key, or plain (signed).
// Attachments are encrypted with the same per-recipient scheme as the body.
func (a *Account) Send(ctx context.Context, d *Draft) error {
	if err := a.SaveDraft(ctx, d); err != nil {
		return err
	}
	kr, err := a.addrKR(d.FromAddressID)
	if err != nil {
		return err
	}
	draft, err := a.client.GetMessage(ctx, d.ID)
	if err != nil {
		return err
	}
	attKeys := map[string]*crypto.SessionKey{}
	for _, att := range draft.Attachments {
		kp, err := base64.StdEncoding.DecodeString(att.KeyPackets)
		if err != nil {
			return err
		}
		key, err := kr.DecryptSessionKey(kp)
		if err != nil {
			return fmt.Errorf(i18n.T("key of attachment %s: %w"), att.Name, err)
		}
		attKeys[att.ID] = key
	}

	// One package per body format: HTML where the recipient's scheme allows
	// it, plain text for PGP/Inline and signed clear-text recipients.
	byType := map[rfc822.MIMEType]map[string]proton.SendPreferences{}
	for _, list := range [][]*mail.Address{d.To, d.CC, d.BCC} {
		for _, addr := range list {
			p := a.prefsFor(ctx, addr.Address, d.SignExternal)
			if d.HTML != "" && htmlAllowed(p) {
				p.MIMEType = rfc822.TextHTML
			}
			if byType[p.MIMEType] == nil {
				byType[p.MIMEType] = map[string]proton.SendPreferences{}
			}
			byType[p.MIMEType][strings.ToLower(addr.Address)] = p
		}
	}
	var req proton.SendDraftReq
	if !d.DeliveryTime.IsZero() {
		req.DeliveryTime = d.DeliveryTime.Unix()
	}
	for mt, prefs := range byType {
		if mt == rfc822.MultipartMixed {
			// PGP/MIME: text, HTML and attachments in one (encrypted or
			// signed) MIME body.
			entity, err := a.mimeBody(ctx, d, draft.Attachments, attKeys)
			if err != nil {
				return err
			}
			if err := req.AddMIMEPackage(kr, string(entity), prefs); err != nil {
				return fmt.Errorf(i18n.T("encrypting the message failed: %w"), err)
			}
			continue
		}
		body := d.Body
		if mt == rfc822.TextHTML {
			body = d.HTML
		}
		if err := req.AddTextPackage(kr, body, mt, prefs, attKeys); err != nil {
			return fmt.Errorf(i18n.T("encrypting the message failed: %w"), err)
		}
	}
	if _, err := a.client.SendDraft(ctx, d.ID, req); err != nil {
		return apiErr(i18n.T("sending failed"), err)
	}
	return nil
}

// mimeBody writes the draft as one MIME entity with the attachments, which
// are downloaded and decrypted from the saved draft.
func (a *Account) mimeBody(ctx context.Context, d *Draft, atts []proton.Attachment, keys map[string]*crypto.SessionKey) ([]byte, error) {
	var parts []mimebuild.Attachment
	for _, att := range atts {
		enc, err := a.client.GetAttachment(ctx, att.ID)
		if err != nil {
			return nil, apiErr(fmt.Sprintf(i18n.T("downloading attachment %s failed"), att.Name), err)
		}
		plain, err := keys[att.ID].Decrypt(enc)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("key of attachment %s: %w"), att.Name, err)
		}
		parts = append(parts, mimebuild.Attachment{Name: att.Name, MIMEType: string(att.MIMEType), Data: plain.GetBinary()})
	}
	return mimebuild.Content(d.Body, d.HTML, parts)
}

// DeleteDraft removes a draft permanently.
func (a *Account) DeleteDraft(ctx context.Context, id string) error {
	return a.client.DeleteMessage(ctx, id)
}

// OpenDraft loads a saved draft for editing.
func (a *Account) OpenDraft(ctx context.Context, id string) (*Draft, *Message, error) {
	msg, err := a.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	d := &Draft{
		ID: id, FromAddressID: msg.Meta.AddressID,
		To: msg.Meta.ToList, CC: msg.Meta.CCList, BCC: msg.Meta.BCCList,
		Subject: msg.Meta.Subject, Body: msg.Text, SignExternal: true,
	}
	for _, att := range msg.Attachments {
		if att.ID != "" {
			d.Attachments = append(d.Attachments, &Outgoing{Name: att.Name, MIMEType: att.MIMEType, Size: att.Size, UploadedID: att.ID})
		}
	}
	return d, msg, nil
}

// ForwardAttachments downloads and decrypts the attachments of msg so they can
// be attached to a forwarded message.
func (a *Account) ForwardAttachments(ctx context.Context, msg *Message) ([]*Outgoing, error) {
	var out []*Outgoing
	for _, att := range msg.Attachments {
		data, err := a.AttachmentData(ctx, att)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("attachment %s: %w"), att.Name, err)
		}
		out = append(out, &Outgoing{Name: att.Name, MIMEType: att.MIMEType, Data: data, Size: int64(len(data))})
	}
	return out, nil
}

// PublicKeyAttachment returns the armored public key of an address and a
// file name for it in Proton's style ("publickey - a@b.cz - 0x1234ABCD.asc").
func (a *Account) PublicKeyAttachment(addressID string) (string, string, error) {
	kr, err := a.addrKR(addressID)
	if err != nil {
		return "", "", err
	}
	k, err := kr.GetKey(0)
	if err != nil {
		return "", "", err
	}
	armored, err := k.GetArmoredPublicKey()
	if err != nil {
		return "", "", err
	}
	email := ""
	for _, addr := range a.SendAddresses() {
		if addr.ID == addressID {
			email = addr.Email
		}
	}
	fp := strings.ToUpper(k.GetFingerprint())
	if len(fp) > 8 {
		fp = fp[:8]
	}
	return armored, fmt.Sprintf("publickey - %s - 0x%s.asc", email, fp), nil
}

// IsOwnAddress reports whether addr belongs to the account.
func (a *Account) IsOwnAddress(addr string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, own := range a.addrs {
		if strings.EqualFold(own.Email, addr) {
			return true
		}
	}
	return false
}

// htmlAllowed reports whether a recipient may get the HTML body: Proton
// internal mail and unsigned clear-text mail can; PGP/Inline and signed
// clear-text (a detached signature over text/plain) cannot.
func htmlAllowed(p proton.SendPreferences) bool {
	switch p.EncryptionScheme {
	case proton.InternalScheme:
		return true
	case proton.ClearScheme:
		return p.SignatureType == proton.NoSignature
	}
	return false
}
