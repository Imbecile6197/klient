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

	"github.com/libormacak/klient/internal/mailparse"
	"github.com/libormacak/klient/internal/pgp"
)

// FolderByID returns the system folder with the given ID.
func FolderByID(id string) Folder {
	for _, f := range Folders {
		if f.ID == id {
			return f
		}
	}
	return Folder{ID: id, Name: id}
}

// Folder is a system folder shown in the sidebar.
type Folder struct {
	ID   string
	Name string
	Icon string
}

var Folders = []Folder{
	{proton.InboxLabel, "Doručená pošta", "mail-send-receive-symbolic"},
	{proton.SnoozedLabel, "Odložené", "alarm-symbolic"},
	{proton.StarredLabel, "Označené hvězdičkou", "starred-symbolic"},
	{proton.DraftsLabel, "Koncepty", "document-edit-symbolic"},
	{proton.AllScheduledLabel, "Naplánované", "appointment-soon-symbolic"},
	{proton.SentLabel, "Odeslané", "mail-send-symbolic"},
	{proton.ArchiveLabel, "Archiv", "folder-documents-symbolic"},
	{proton.SpamLabel, "Spam", "mail-mark-junk-symbolic"},
	{proton.TrashLabel, "Koš", "user-trash-symbolic"},
	{proton.AllMailLabel, "Všechna pošta", "mail-read-symbolic"},
}

const (
	InboxID   = proton.InboxLabel
	SpamID    = proton.SpamLabel
	TrashID   = proton.TrashLabel
	ArchiveID = proton.ArchiveLabel
	StarredID = proton.StarredLabel
	DraftsID  = proton.DraftsLabel
	SentID    = proton.SentLabel
	AllMailID = proton.AllMailLabel
)

type Summary = proton.MessageMetadata

// List returns one page of messages in a folder, newest first.
func (a *Account) List(ctx context.Context, labelID string, page, pageSize int) ([]Summary, error) {
	msgs, err := a.client.GetMessageMetadataPage(ctx, page, pageSize, proton.MessageFilter{LabelID: labelID, Desc: true})
	if err == nil {
		if a.cache != nil {
			_ = a.cache.PutMetadata(msgs...)
		}
		return msgs, nil
	}
	if IsOffline(err) && a.cache != nil {
		return a.cache.List(labelID, page, pageSize)
	}
	return nil, err
}

// rawMessage returns the message as Proton stores it (body PGP-encrypted),
// from the offline cache when possible. Drafts are always fetched fresh.
func (a *Account) rawMessage(ctx context.Context, id string) (proton.Message, error) {
	if a.cache != nil {
		if m, ok := a.cache.Message(id); ok && !m.IsDraft() {
			return m, nil
		}
	}
	m, err := a.client.GetMessage(ctx, id)
	if err != nil {
		if IsOffline(err) {
			return m, errors.New("zpráva není uložená pro offline čtení a server je nedostupný")
		}
		return m, err
	}
	if a.cache != nil && !m.IsDraft() {
		_ = a.cache.PutMessage(m)
	}
	return m, nil
}

// Attachment is a regular (Proton-side) attachment; its data is fetched and
// decrypted on demand.
type Attachment struct {
	ID        string
	Name      string
	Size      int64
	MIMEType  string
	ContentID string // set for inline images referenced as cid: from HTML

	keyPackets string
	addressID  string
	inline     []byte // for PGP/MIME parts already decrypted with the body
}

// InlineAttachment is an attachment whose data is already at hand (parts of
// a MIME message, e.g. from an IMAP server).
func InlineAttachment(name, mimeType, contentID string, data []byte) Attachment {
	return Attachment{Name: name, Size: int64(len(data)), MIMEType: mimeType, ContentID: contentID, inline: data}
}

// InlineData returns the data of an attachment created by InlineAttachment.
func (a Attachment) InlineData() []byte { return a.inline }

// Message is a fully decrypted message ready for display.
type Message struct {
	Meta        Summary
	Headers     map[string][]string
	Text        string
	HTML        string // original HTML body, "" for plain-text messages
	Links       []string
	Attachments []Attachment
	Encryption  string // human readable: how the message was protected
	Signature   pgp.SignatureStatus
	SenderKeyFP string
}

// Get downloads and decrypts one message and verifies its signature when the
// sender's public key is available (Proton key server, WKD or local store).
func (a *Account) Get(ctx context.Context, id string) (*Message, error) {
	raw, err := a.rawMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	kr, err := a.addrKR(raw.AddressID)
	if err != nil {
		return nil, err
	}
	enc, err := crypto.NewPGPMessageFromArmored(raw.Body)
	if err != nil {
		return nil, fmt.Errorf("neplatné tělo zprávy: %w", err)
	}
	plain, err := kr.Decrypt(enc, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("dešifrování selhalo: %w", err)
	}

	body := mailparse.Parse(plain.GetBinary(), string(raw.MIMEType))
	msg := &Message{
		Meta:    raw.MessageMetadata,
		Headers: mailparse.ParseHeaderBlock(raw.Header, raw.ParsedHeaders.Values),
		Text:    body.Text,
		HTML:    body.HTML,
		Links:   body.Links,
	}

	// Signature: embedded signatures are checked against the sender's keys.
	var senderKR *crypto.KeyRing
	if raw.Sender != nil && raw.Flags.Has(proton.MessageFlagReceived) {
		senderKR = a.senderKeys(ctx, raw.Sender.Address)
		msg.SenderKeyFP = pgp.Fingerprint(senderKR)
	}
	if senderKR != nil {
		_, verr := kr.Decrypt(enc, senderKR, crypto.GetUnixTime())
		msg.Signature = pgp.StatusFromError(verr)
	}
	// Inline cleartext-signed PGP (common with external PGP users).
	if pgp.HasClearSigned(msg.Text) {
		msg.Text, msg.Signature = pgp.VerifyClearSigned(msg.Text, senderKR)
	}
	// The user's own messages (sent copies, drafts, mail to self) are stored
	// encrypted to their own key; there is no foreign signature to check.
	if raw.Sender != nil && a.IsOwnAddress(raw.Sender.Address) {
		msg.Signature = pgp.SigOwn
	}

	switch {
	case raw.Flags.Has(proton.MessageFlagSent) && !raw.Flags.Has(proton.MessageFlagReceived):
		// Proton's flags on sent copies describe the stored copy, not how each
		// recipient received it, so don't claim end-to-end here.
		msg.Encryption = "Odeslaná zpráva, uložena šifrovaně (zero-access)"
	case raw.Flags.Has(proton.MessageFlagInternal) && raw.Flags.Has(proton.MessageFlagE2E):
		msg.Encryption = "End-to-end šifrováno (Proton)"
	case raw.Flags.Has(proton.MessageFlagE2E):
		msg.Encryption = "End-to-end šifrováno (PGP)"
	case raw.Flags.Has(proton.MessageFlagReceived):
		msg.Encryption = "Přijato nešifrovaně, uloženo šifrovaně (zero-access)"
	default:
		msg.Encryption = "Uloženo šifrovaně"
	}

	for _, att := range raw.Attachments {
		cid := ""
		for k, v := range att.Headers.Values {
			if strings.EqualFold(k, "content-id") && len(v) > 0 {
				cid = strings.Trim(v[0], "<> ")
			}
		}
		msg.Attachments = append(msg.Attachments, Attachment{
			ID: att.ID, Name: att.Name, Size: att.Size, MIMEType: string(att.MIMEType),
			ContentID: cid, keyPackets: att.KeyPackets, addressID: raw.AddressID,
		})
	}
	for _, p := range body.Attachments {
		msg.Attachments = append(msg.Attachments, InlineAttachment(p.Filename, p.MIMEType, p.ContentID, p.Data))
	}
	return msg, nil
}

// senderKeys looks up a sender's public keys: Proton (internal or WKD) first,
// then the local key store.
func (a *Account) senderKeys(ctx context.Context, email string) *crypto.KeyRing {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if keys, _, err := a.client.GetPublicKeys(ctx, email); err == nil && len(keys) > 0 {
		if kr, err := keys.GetKeyRing(); err == nil {
			return kr
		}
	}
	return pgp.LocalKey(email)
}

// AttachmentData returns the decrypted content of an attachment.
func (a *Account) AttachmentData(ctx context.Context, att Attachment) ([]byte, error) {
	if att.inline != nil {
		return att.inline, nil
	}
	kr, err := a.addrKR(att.addressID)
	if err != nil {
		return nil, err
	}
	data, err := a.client.GetAttachment(ctx, att.ID)
	if err != nil {
		return nil, err
	}
	kp, err := base64.StdEncoding.DecodeString(att.keyPackets)
	if err != nil {
		return nil, err
	}
	dec, err := kr.Decrypt(crypto.NewPGPSplitMessage(kp, data).GetPGPMessage(), nil, 0)
	if err != nil {
		return nil, err
	}
	return dec.GetBinary(), nil
}

func (a *Account) MarkRead(ctx context.Context, ids ...string) error {
	return a.client.MarkMessagesRead(ctx, ids...)
}

// Move puts messages into a system folder (Inbox, Spam, Trash, Archive).
func (a *Account) Move(ctx context.Context, folderID string, ids ...string) error {
	return a.client.LabelMessages(ctx, ids, folderID)
}

// Recipient encryption decided per address when sending.
type RecipientMode struct {
	Address string
	Scheme  string // "proton", "pgp", "clear"
	KeyFP   string
}

// PlanEncryption reports how each recipient would be protected, so the
// compose window can show it before sending.
func (a *Account) PlanEncryption(ctx context.Context, addrs []*mail.Address) []RecipientMode {
	var out []RecipientMode
	for _, addr := range addrs {
		p := a.prefsFor(ctx, addr.Address, true)
		mode := RecipientMode{Address: addr.Address, Scheme: "clear"}
		switch p.EncryptionScheme {
		case proton.InternalScheme:
			mode.Scheme = "proton"
		case proton.PGPInlineScheme:
			mode.Scheme = "pgp"
		}
		mode.KeyFP = pgp.Fingerprint(p.PubKey)
		out = append(out, mode)
	}
	return out
}

func (a *Account) prefsFor(ctx context.Context, email string, sign bool) proton.SendPreferences {
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	keys, rtype, err := a.client.GetPublicKeys(lookupCtx, email)
	if err == nil && len(keys) > 0 {
		if kr, err := keys.GetKeyRing(); err == nil {
			scheme := proton.PGPInlineScheme
			if rtype == proton.RecipientTypeInternal {
				scheme = proton.InternalScheme
			}
			return proton.SendPreferences{
				Encrypt: true, PubKey: kr, SignatureType: proton.DetachedSignature,
				EncryptionScheme: scheme, MIMEType: rfc822.TextPlain,
			}
		}
	}
	if kr := pgp.LocalKey(email); kr != nil {
		return proton.SendPreferences{
			Encrypt: true, PubKey: kr, SignatureType: proton.DetachedSignature,
			EncryptionScheme: proton.PGPInlineScheme, MIMEType: rfc822.TextPlain,
		}
	}
	sig := proton.NoSignature
	if sign {
		sig = proton.DetachedSignature
	}
	return proton.SendPreferences{
		Encrypt: false, SignatureType: sig,
		EncryptionScheme: proton.ClearScheme, MIMEType: rfc822.TextPlain,
	}
}

// Events streams mailbox events; newMessages receives metadata of every
// message created since the stream started.
func (a *Account) Events(ctx context.Context, onNew func(Summary), onChange func()) error {
	from, err := a.client.GetLatestEventID(ctx)
	if err != nil {
		return err
	}
	go func() {
		for ev := range a.client.NewEventStream(ctx, 30*time.Second, 10*time.Second, from) {
			changed := false
			for _, m := range ev.Messages {
				changed = true
				if a.cache != nil {
					if m.Action == proton.EventDelete {
						_ = a.cache.Delete(m.ID)
					} else if m.Message.ID != "" {
						_ = a.cache.PutMetadata(m.Message)
					}
				}
				if m.Action == proton.EventCreate && m.Message.Flags.Has(proton.MessageFlagReceived) {
					onNew(m.Message)
				}
			}
			if changed {
				onChange()
			}
		}
	}()
	return nil
}

// InlineImages downloads the attachments referenced from the HTML body as
// cid: and returns them as data: URIs keyed by content ID.
func (a *Account) InlineImages(ctx context.Context, msg *Message) map[string]string {
	out := map[string]string{}
	if !strings.Contains(strings.ToLower(msg.HTML), "cid:") {
		return out
	}
	for _, att := range msg.Attachments {
		if att.ContentID == "" || !strings.Contains(msg.HTML, att.ContentID) {
			continue
		}
		data, err := a.AttachmentData(ctx, att)
		if err != nil {
			continue
		}
		mt := att.MIMEType
		if mt == "" {
			mt = "application/octet-stream"
		}
		out[att.ContentID] = "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return out
}

// SyncOffline stores the newest n messages (metadata and encrypted bodies)
// in the cache, a few at a time so it does not hog the connection.
func (a *Account) SyncOffline(ctx context.Context, n int, progress func(done, total int)) error {
	if a.cache == nil || n <= 0 {
		return nil
	}
	var metas []Summary
	for page := 0; len(metas) < n; page++ {
		ms, err := a.client.GetMessageMetadataPage(ctx, page, 150, proton.MessageFilter{LabelID: AllMailID, Desc: true})
		if err != nil {
			return err
		}
		metas = append(metas, ms...)
		if len(ms) < 150 {
			break
		}
	}
	if len(metas) > n {
		metas = metas[:n]
	}
	if err := a.cache.PutMetadata(metas...); err != nil {
		return err
	}
	for i, m := range metas {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !m.IsDraft() && !a.cache.HasBody(m.ID) {
			full, err := a.client.GetMessage(ctx, m.ID)
			if err != nil {
				return err
			}
			_ = a.cache.PutMessage(full)
			time.Sleep(150 * time.Millisecond)
		}
		if progress != nil && (i%25 == 0 || i == len(metas)-1) {
			progress(i+1, len(metas))
		}
	}
	return a.cache.Prune(n, n*5)
}

// ExportEML rebuilds the message as a standard RFC 822 file (.eml) with its
// attachments, decrypted.
func (a *Account) ExportEML(ctx context.Context, id string) ([]byte, error) {
	raw, err := a.rawMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	kr, err := a.addrKR(raw.AddressID)
	if err != nil {
		return nil, err
	}
	atts := map[string][]byte{}
	for _, att := range raw.Attachments {
		data, err := a.client.GetAttachment(ctx, att.ID) // still encrypted; BuildRFC822 decrypts
		if err != nil {
			return nil, fmt.Errorf("příloha %s: %w", att.Name, err)
		}
		atts[att.ID] = data
	}
	return proton.BuildRFC822(kr, raw, atts)
}
