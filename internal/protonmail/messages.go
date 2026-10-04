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
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/pgp"
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
	{proton.InboxLabel, i18n.T("Inbox"), "mail-send-receive-symbolic"},
	{proton.SnoozedLabel, i18n.T("Snoozed"), "alarm-symbolic"},
	{proton.StarredLabel, i18n.T("Starred"), "starred-symbolic"},
	{proton.DraftsLabel, i18n.T("Drafts"), "document-edit-symbolic"},
	{proton.AllScheduledLabel, i18n.T("Scheduled"), "appointment-soon-symbolic"},
	{proton.SentLabel, i18n.T("Sent"), "mail-send-symbolic"},
	{proton.ArchiveLabel, i18n.T("Archive"), "folder-documents-symbolic"},
	{proton.SpamLabel, i18n.T("Spam"), "mail-mark-junk-symbolic"},
	{proton.TrashLabel, i18n.T("Trash"), "user-trash-symbolic"},
	{proton.AllMailLabel, i18n.T("All Mail"), "mail-read-symbolic"},
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
			return m, errors.New(i18n.T("the message is not saved for offline reading and the server is unavailable"))
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
		return nil, fmt.Errorf(i18n.T("invalid message body: %w"), err)
	}
	plain, err := kr.Decrypt(enc, nil, 0)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("decryption failed: %w"), err)
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
	// Inline PGP messages (Mailvelope and others) encrypted to the
	// address's key inside Proton's own encryption.
	if pgp.HasInlineEncrypted(msg.Text) {
		if text, st, ok := pgp.DecryptInline(msg.Text, kr, senderKR); ok {
			msg.Text, msg.Signature, msg.HTML = text, st, ""
			msg.Links = mailparse.FindLinks(text)
		}
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
		msg.Encryption = i18n.T("Sent message, stored encrypted (zero-access)")
	case raw.Flags.Has(proton.MessageFlagInternal) && raw.Flags.Has(proton.MessageFlagE2E):
		msg.Encryption = i18n.T("End-to-end encrypted (Proton)")
	case raw.Flags.Has(proton.MessageFlagE2E):
		msg.Encryption = i18n.T("End-to-end encrypted (PGP)")
	case raw.Flags.Has(proton.MessageFlagReceived):
		msg.Encryption = i18n.T("Received unencrypted, stored encrypted (zero-access)")
	default:
		msg.Encryption = i18n.T("Stored encrypted")
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
		case proton.PGPInlineScheme, proton.PGPMIMEScheme:
			mode.Scheme = "pgp"
		}
		mode.KeyFP = pgp.Fingerprint(p.PubKey)
		out = append(out, mode)
	}
	return out
}

// prefsFor decides how a recipient gets the message, as the Proton apps do:
// Proton users end-to-end; others with a known key (published, pinned in
// the Proton contact, or imported in Klient) PGP/MIME, which keeps the HTML
// and the attachments in one encrypted part; everyone else in the clear,
// signed as PGP/MIME when sign is set (or the contact asks for it).
func (a *Account) prefsFor(ctx context.Context, email string, sign bool) proton.SendPreferences {
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cs, _ := a.contactSettings(lookupCtx, email)
	keys, rtype, err := a.client.GetPublicKeys(lookupCtx, email)
	if err == nil && len(keys) > 0 && rtype == proton.RecipientTypeInternal {
		if kr, err := keys.GetKeyRing(); err == nil {
			return proton.SendPreferences{
				Encrypt: true, PubKey: kr, SignatureType: proton.DetachedSignature,
				EncryptionScheme: proton.InternalScheme, MIMEType: rfc822.TextPlain,
			}
		}
	}
	encrypt := cs.Encrypt == nil || *cs.Encrypt
	var kr *crypto.KeyRing
	switch {
	case !encrypt:
	case len(cs.Keys) > 0:
		kr, _ = crypto.NewKeyRing(nil)
		for _, k := range cs.Keys {
			if !k.IsExpired() && !k.IsRevoked() {
				_ = kr.AddKey(k)
			}
		}
		if kr.CountEntities() == 0 {
			kr = nil
		}
	case err == nil && len(keys) > 0:
		kr, _ = keys.GetKeyRing()
	}
	if kr == nil && encrypt {
		kr = pgp.LocalKey(email)
	}
	if kr != nil {
		if cs.Scheme != nil && *cs.Scheme == proton.PGPInlineScheme {
			return proton.SendPreferences{
				Encrypt: true, PubKey: kr, SignatureType: proton.DetachedSignature,
				EncryptionScheme: proton.PGPInlineScheme, MIMEType: rfc822.TextPlain,
			}
		}
		return proton.SendPreferences{
			Encrypt: true, PubKey: kr, SignatureType: proton.DetachedSignature,
			EncryptionScheme: proton.PGPMIMEScheme, MIMEType: rfc822.MultipartMixed,
		}
	}
	if cs.Sign != nil {
		sign = *cs.Sign
	}
	if sign {
		return proton.SendPreferences{
			SignatureType:    proton.DetachedSignature,
			EncryptionScheme: proton.ClearMIMEScheme, MIMEType: rfc822.MultipartMixed,
		}
	}
	return proton.SendPreferences{
		SignatureType:    proton.NoSignature,
		EncryptionScheme: proton.ClearScheme, MIMEType: rfc822.TextPlain,
	}
}

// contactSettings reads the encryption settings and pinned keys of an
// address from the user's Proton contacts (the signed vCard), cached for a
// few minutes.
func (a *Account) contactSettings(ctx context.Context, email string) (proton.ContactSettings, bool) {
	email = strings.ToLower(email)
	a.mu.RLock()
	c, ok := a.contactCache[email]
	userKR := a.userKR
	a.mu.RUnlock()
	if ok && time.Since(c.at) < 5*time.Minute {
		return c.settings, c.found
	}
	var out proton.ContactSettings
	found := false
	if userKR != nil {
		if emails, err := a.client.GetAllContactEmails(ctx, email); err == nil {
			for _, ce := range emails {
				contact, err := a.client.GetContact(ctx, ce.ContactID)
				if err != nil {
					continue
				}
				if s, err := contact.GetSettings(userKR, ce.Email, proton.CardTypeSigned); err == nil {
					out, found = s, true
					break
				}
			}
		} else {
			return out, false // not cached: try again next time
		}
	}
	a.mu.Lock()
	if a.contactCache == nil {
		a.contactCache = map[string]cachedContact{}
	}
	a.contactCache[email] = cachedContact{settings: out, found: found, at: time.Now()}
	a.mu.Unlock()
	return out, found
}

type cachedContact struct {
	settings proton.ContactSettings
	found    bool
	at       time.Time
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
			return nil, fmt.Errorf(i18n.T("attachment %s: %w"), att.Name, err)
		}
		atts[att.ID] = data
	}
	return proton.BuildRFC822(kr, raw, atts)
}
