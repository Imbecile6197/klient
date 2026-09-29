package imapmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/libormacak/klient/internal/mailbox"
	"github.com/libormacak/klient/internal/mailparse"
	"github.com/libormacak/klient/internal/pgp"
	"github.com/libormacak/klient/internal/pgpmime"
	"github.com/libormacak/klient/internal/protonmail"
)

// raw returns the full RFC 822 message and its summary, from the cache when
// possible (drafts are always fetched).
func (a *Account) raw(ctx context.Context, id string) ([]byte, protonmail.Summary, error) {
	if a.cache != nil {
		if m, ok := a.cache.Message(id); ok && !m.IsDraft() && m.Body != "" {
			return []byte(m.Body), m.MessageMetadata, nil
		}
	}
	ref, err := parseID(id)
	if err != nil {
		return nil, protonmail.Summary{}, err
	}
	var body []byte
	var meta protonmail.Summary
	err = a.with(func(c *imapclient.Client) error {
		sel, err := c.Select(ref.mailbox, nil).Wait()
		if err != nil {
			return err
		}
		if sel.UIDValidity != ref.uidValidity {
			return fmt.Errorf("zpráva už na serveru není (složka byla změněna), obnovte seznam")
		}
		opts := *fetchMeta
		opts.BodySection = []*imap.FetchItemBodySection{{Peek: true}}
		msgs, err := c.Fetch(imap.UIDSetNum(ref.uid), &opts).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return fmt.Errorf("zpráva už na serveru není")
		}
		meta = a.summary(ref.mailbox, sel.UIDValidity, msgs[0])
		if len(msgs[0].BodySection) > 0 {
			body = msgs[0].BodySection[0].Bytes
		}
		return nil
	})
	if err != nil {
		if IsOffline(err) {
			return nil, meta, fmt.Errorf("zpráva není uložená pro offline čtení a server je nedostupný")
		}
		return nil, meta, err
	}
	if a.cache != nil && !meta.IsDraft() {
		_ = a.cache.PutMessage(proton.Message{MessageMetadata: meta, Body: string(body)})
	}
	return body, meta, nil
}

// headerBlock is the header part of a raw message.
func headerBlock(raw []byte) string {
	for _, sep := range [][]byte{[]byte("\r\n\r\n"), []byte("\n\n")} {
		if i := bytes.Index(raw, sep); i >= 0 {
			return string(raw[:i])
		}
	}
	return string(raw)
}

// Get downloads and parses one message.
func (a *Account) Get(ctx context.Context, id string) (*protonmail.Message, error) {
	raw, meta, err := a.raw(ctx, id)
	if err != nil {
		return nil, err
	}
	sender := ""
	if meta.Sender != nil {
		sender = meta.Sender.Address
	}
	headers := mailparse.ParseHeaderBlock(headerBlock(raw), nil)
	a.learnAutocrypt(headers, sender)
	display := raw
	encryption := "Bez šifrování end-to-end – zpráva je u poskytovatele uložená čitelně (na cestě chráněná TLS)"
	entity, enc, sig := a.open(ctx, raw, sender)
	switch {
	case entity != nil:
		display = entity
	case enc != "":
		display = nil // encrypted but cannot be opened
	}
	if enc != "" {
		encryption = enc
	}
	body := mailparse.Body{Text: encryption}
	if display != nil {
		body = mailparse.Parse(display, "multipart/mixed")
	}
	msg := &protonmail.Message{
		Meta:       meta,
		Headers:    headers,
		Text:       body.Text,
		HTML:       body.HTML,
		Links:      body.Links,
		Encryption: encryption,
		Signature:  sig,
	}
	if pgp.HasClearSigned(msg.Text) && sender != "" {
		msg.Text, msg.Signature = pgp.VerifyClearSigned(msg.Text, a.keyFor(ctx, sender))
	}
	if sender != "" && a.IsOwnAddress(sender) && msg.Signature != pgp.SigInvalid {
		msg.Signature = pgp.SigOwn
	}
	for _, p := range body.Attachments {
		msg.Attachments = append(msg.Attachments, protonmail.InlineAttachment(p.Filename, p.MIMEType, p.ContentID, p.Data))
	}
	return msg, nil
}

// learnAutocrypt stores a sender's key from the Autocrypt header, unless a
// key for them is already known (so a forged header cannot replace it).
func (a *Account) learnAutocrypt(headers map[string][]string, sender string) {
	if sender == "" || a.IsOwnAddress(sender) || pgp.LocalKey(sender) != nil {
		return
	}
	for k, vs := range headers {
		if !strings.EqualFold(k, "Autocrypt") || len(vs) == 0 {
			continue
		}
		if armored, ok := pgpmime.ParseAutocrypt(vs[0], sender); ok {
			_, _ = pgp.ImportKey(armored)
		}
	}
}

func (a *Account) AttachmentData(ctx context.Context, att protonmail.Attachment) ([]byte, error) {
	if d := att.InlineData(); d != nil {
		return d, nil
	}
	return nil, fmt.Errorf("příloha nemá data")
}

func (a *Account) InlineImages(ctx context.Context, msg *protonmail.Message) map[string]string {
	out := map[string]string{}
	for _, att := range msg.Attachments {
		if att.ContentID == "" || !strings.Contains(msg.HTML, att.ContentID) {
			continue
		}
		mt := att.MIMEType
		if mt == "" {
			mt = "application/octet-stream"
		}
		out[att.ContentID] = "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(att.InlineData())
	}
	return out
}

func (a *Account) ExportEML(ctx context.Context, id string) ([]byte, error) {
	raw, _, err := a.raw(ctx, id)
	return raw, err
}

// SyncOffline stores the newest n inbox messages with bodies in the cache.
func (a *Account) SyncOffline(ctx context.Context, n int, progress func(done, total int)) error {
	if a.cache == nil || n <= 0 {
		return nil
	}
	var metas []protonmail.Summary
	for page := 0; len(metas) < n; page++ {
		ms, err := a.List(ctx, protonmail.InboxID, page, 100)
		if err != nil {
			return err
		}
		metas = append(metas, ms...)
		if len(ms) < 100 {
			break
		}
	}
	if len(metas) > n {
		metas = metas[:n]
	}
	for i, m := range metas {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !m.IsDraft() && !a.cache.HasBody(m.ID) {
			if _, _, err := a.raw(ctx, m.ID); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		if progress != nil && (i%25 == 0 || i == len(metas)-1) {
			progress(i+1, len(metas))
		}
	}
	return a.cache.Prune(n, n*5)
}

// ---- Contacts --------------------------------------------------------------------

// Contacts are the people from recent mail (IMAP has no address book).
func (a *Account) Contacts(ctx context.Context) ([]protonmail.Contact, error) {
	return a.RecentAddresses(500), nil
}

func (a *Account) InvalidateContacts() {}

func (a *Account) RecentAddresses(limit int) []protonmail.Contact {
	if a.cache == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []protonmail.Contact
	add := func(name, email string) {
		k := strings.ToLower(email)
		if email == "" || seen[k] || a.IsOwnAddress(email) || len(out) >= limit {
			return
		}
		seen[k] = true
		out = append(out, protonmail.Contact{Name: name, Email: email, Recent: true})
	}
	var all []protonmail.Summary
	for _, f := range []string{protonmail.SentID, protonmail.InboxID} {
		if ms, err := a.cache.List(f, 0, 300); err == nil {
			all = append(all, ms...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Time > all[j].Time })
	for _, m := range all {
		if m.Flags.Has(proton.MessageFlagSent) {
			for _, r := range append(append([]*mail.Address{}, m.ToList...), m.CCList...) {
				add(r.Name, r.Address)
			}
		} else if m.Sender != nil {
			add(m.Sender.Name, m.Sender.Address)
		}
	}
	return out
}

func (a *Account) AddContact(ctx context.Context, name, email string) error {
	return mailbox.ErrUnsupported
}

func (a *Account) DeleteContact(ctx context.Context, contactID string) error {
	return mailbox.ErrUnsupported
}

// ---- Features the IMAP accounts do not have (yet) ----------------------------------

func (a *Account) AutoReply(ctx context.Context) (protonmail.AutoReply, error) {
	return protonmail.AutoReply{}, mailbox.ErrUnsupported
}
func (a *Account) SetAutoReply(ctx context.Context, r protonmail.AutoReply) error {
	return mailbox.ErrUnsupported
}
