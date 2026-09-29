package imapmail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/pgpmime"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// content writes the body of a draft as a standalone MIME entity (its own
// Content-Type header + body), ready to be signed or encrypted.
func content(d *protonmail.Draft) ([]byte, error) {
	var buf bytes.Buffer
	textHeader := func() gomail.InlineHeader {
		var th gomail.InlineHeader
		th.Set("Content-Type", "text/plain; charset=utf-8")
		th.Set("Content-Transfer-Encoding", "quoted-printable")
		return th
	}
	if len(d.Attachments) == 0 && d.HTML == "" {
		var h gomail.Header
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, d.Body); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	var h gomail.Header
	mw, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	iw, err := mw.CreateInline()
	if err != nil {
		return nil, err
	}
	pw, err := iw.CreatePart(textHeader())
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(pw, d.Body); err != nil {
		return nil, err
	}
	pw.Close()
	if d.HTML != "" {
		var hh gomail.InlineHeader
		hh.Set("Content-Type", "text/html; charset=utf-8")
		hh.Set("Content-Transfer-Encoding", "quoted-printable")
		hw, err := iw.CreatePart(hh)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(hw, d.HTML); err != nil {
			return nil, err
		}
		hw.Close()
	}
	iw.Close()
	for _, att := range d.Attachments {
		var ah gomail.AttachmentHeader
		mt := att.MIMEType
		if mt == "" {
			mt = "application/octet-stream"
		}
		ah.Set("Content-Type", mt)
		ah.SetFilename(att.Name)
		ah.Set("Content-Transfer-Encoding", "base64")
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(att.Data); err != nil {
			return nil, err
		}
		aw.Close()
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// headers are the top-level headers of a message (no Content-Type).
func (a *Account) headers(d *protonmail.Draft, parent parentRef) (gomail.Header, error) {
	var h gomail.Header
	h.SetDate(time.Now())
	if !d.DeliveryTime.IsZero() {
		h.SetDate(d.DeliveryTime) // scheduled: the list shows when it leaves
	}
	h.SetAddressList("From", []*gomail.Address{{Name: a.set.Name, Address: a.set.Email}})
	conv := func(l []*mail.Address) []*gomail.Address {
		out := make([]*gomail.Address, 0, len(l))
		for _, ad := range l {
			out = append(out, &gomail.Address{Name: ad.Name, Address: ad.Address})
		}
		return out
	}
	if len(d.To) > 0 {
		h.SetAddressList("To", conv(d.To))
	}
	if len(d.CC) > 0 {
		h.SetAddressList("Cc", conv(d.CC))
	}
	h.SetSubject(d.Subject)
	_, domain, _ := strings.Cut(a.set.Email, "@")
	if err := h.GenerateMessageIDWithHostname(domain); err != nil {
		return h, err
	}
	if parent.id != "" {
		h.SetMsgIDList("In-Reply-To", []string{parent.id})
		h.SetMsgIDList("References", append(parent.refs, parent.id))
	}
	h.Set("MIME-Version", "1.0")
	h.Set("User-Agent", "Klient")
	if kr, err := pgp.OwnKeyRing(a.set.Email); err == nil {
		if k, err := kr.GetKey(0); err == nil {
			if ac, err := pgpmime.AutocryptHeader(a.set.Email, k); err == nil {
				h.Set("Autocrypt", ac)
			}
		}
	}
	return h, nil
}

// assemble joins top headers with a MIME entity, or with a signed or
// encrypted body of the given Content-Type.
func assemble(h gomail.Header, contentType string, body []byte) ([]byte, error) {
	var buf bytes.Buffer
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	var tmp bytes.Buffer
	if err := textproto.WriteHeader(&tmp, h.Header.Header); err != nil {
		return nil, err
	}
	hdr := tmp.Bytes()
	if contentType == "" {
		// body is an entity with its own headers: merge the header blocks.
		hdr = bytes.TrimSuffix(hdr, []byte("\r\n"))
	}
	buf.Write(hdr)
	buf.Write(body)
	return buf.Bytes(), nil
}

// build creates the RFC 822 message of a draft, signed if asked and a key
// is set up (used for drafts and plain sending).
func (a *Account) build(d *protonmail.Draft, parent parentRef) ([]byte, error) {
	h, err := a.headers(d, parent)
	if err != nil {
		return nil, err
	}
	entity, err := content(d)
	if err != nil {
		return nil, err
	}
	return assemble(h, "", entity)
}

// parentRef is the Message-ID of the message a draft answers and that
// message's own References, so the reply keeps the whole chain.
type parentRef struct {
	id   string
	refs []string
}

func (a *Account) parentMessageID(ctx context.Context, d *protonmail.Draft) parentRef {
	if d.ParentID == "" || d.Action == protonmail.ActionForward {
		return parentRef{}
	}
	raw, meta, err := a.raw(ctx, d.ParentID)
	if err != nil || meta.ExternalID == "" {
		return parentRef{}
	}
	var refs []string
	for _, r := range msgIDRe.FindAllString(headerValue(headerBlock(raw), "References"), -1) {
		refs = append(refs, strings.Trim(r, "<>"))
	}
	return parentRef{id: meta.ExternalID, refs: refs}
}

// appendTo stores a message in a folder and returns its new ID.
func (a *Account) appendTo(folderID string, data []byte, flags []imap.Flag) (string, error) {
	var id string
	err := a.with(func(c *imapclient.Client) error {
		mb, err := a.mailboxOf(folderID)
		if err != nil {
			return err
		}
		cmd := c.Append(mb, int64(len(data)), &imap.AppendOptions{Flags: flags, Time: time.Now()})
		if _, err := cmd.Write(data); err != nil {
			return err
		}
		if err := cmd.Close(); err != nil {
			return err
		}
		res, err := cmd.Wait()
		if err != nil {
			return err
		}
		if res != nil && res.UID != 0 {
			id = makeID(mb, res.UIDValidity, res.UID)
		}
		return nil
	})
	return id, err
}

// SaveDraft stores the draft in the Drafts folder (replacing the previous
// version, since IMAP messages cannot be edited).
func (a *Account) SaveDraft(ctx context.Context, d *protonmail.Draft) error {
	if !a.HasFolder(protonmail.DraftsID) {
		return errors.New("schránka nemá složku Koncepty")
	}
	data, err := a.forSelf(d, a.parentMessageID(ctx, d))
	if err != nil {
		return err
	}
	id, err := a.appendTo(protonmail.DraftsID, data, []imap.Flag{imap.FlagDraft, imap.FlagSeen})
	if err != nil {
		return fmt.Errorf("uložení konceptu selhalo: %w", err)
	}
	if d.ID != "" && d.ID != id {
		_ = a.deleteIDs(d.ID)
	}
	d.ID = id
	for _, att := range d.Attachments {
		att.UploadedID = "" // data stays local; the draft is rewritten on every save
	}
	return nil
}

// Send delivers the message over SMTP and files a copy in Sent. Gmail files
// sent mail by itself, so no copy is added there.
func (a *Account) Send(ctx context.Context, d *protonmail.Draft) error {
	if !d.DeliveryTime.IsZero() {
		return a.schedule(ctx, d)
	}
	out, err := a.prepare(ctx, d, a.parentMessageID(ctx, d))
	if err != nil {
		return fmt.Errorf("příprava zprávy selhala: %w", err)
	}
	if len(out.keyed) > 0 {
		if err := a.smtpSend(out.keyed, out.encrypted); err != nil {
			return err
		}
	}
	if len(out.clear) > 0 {
		if err := a.smtpSend(out.clear, out.plain); err != nil {
			if len(out.keyed) > 0 {
				return fmt.Errorf("šifrovaná část odešla, ale ostatním příjemcům ne: %w", err)
			}
			return err
		}
	}
	// Gmail files sent mail by itself (only the plain copy would be kept
	// there; that is what its servers received).
	if a.set.Kind != "gmail" && a.HasFolder(protonmail.SentID) {
		if _, err := a.appendTo(protonmail.SentID, out.sentCopy, []imap.Flag{imap.FlagSeen}); err != nil {
			return fmt.Errorf("zpráva odešla, ale kopii se nepodařilo uložit do Odeslaných: %w", err)
		}
	}
	if d.ID != "" {
		_ = a.deleteIDs(d.ID)
		d.ID = ""
	}
	if d.ParentID != "" && d.Action != protonmail.ActionForward {
		_ = a.store([]string{d.ParentID}, imap.StoreFlagsAdd, imap.FlagAnswered)
	}
	return nil
}

func (a *Account) smtpClient() (*smtp.Client, error) {
	addr := net.JoinHostPort(a.set.SMTPHost, strconv.Itoa(a.set.SMTPPort))
	tc := tlsConfig(a.set.SMTPHost)
	var c *smtp.Client
	var err error
	if a.set.SMTPSecurity == "starttls" {
		c, err = smtp.DialStartTLS(addr, tc)
	} else {
		c, err = smtp.DialTLS(addr, tc)
	}
	if err != nil {
		return nil, &netErr{err}
	}
	if err := c.Auth(sasl.NewPlainClient("", a.set.Username, a.password)); err != nil {
		c.Close()
		return nil, fmt.Errorf("SMTP server odmítl přihlášení: %w", err)
	}
	return c, nil
}

func (a *Account) smtpSend(rcpts []string, data []byte) error {
	c, err := a.smtpClient()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SendMail(a.set.Email, rcpts, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("odeslání selhalo: %w", err)
	}
	return c.Quit()
}

// TestSMTP checks the SMTP login (for the account wizard).
func TestSMTP(ctx context.Context, a *Account) error {
	c, err := a.smtpClient()
	if err != nil {
		return err
	}
	return c.Quit()
}

func (a *Account) DeleteDraft(ctx context.Context, id string) error { return a.deleteIDs(id) }

// OpenDraft loads a draft for editing; attachments come with their data.
func (a *Account) OpenDraft(ctx context.Context, id string) (*protonmail.Draft, *protonmail.Message, error) {
	msg, err := a.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	d := &protonmail.Draft{
		ID: id, FromAddressID: "0",
		To: msg.Meta.ToList, CC: msg.Meta.CCList, BCC: msg.Meta.BCCList,
		Subject: msg.Meta.Subject, Body: msg.Text,
	}
	for _, att := range msg.Attachments {
		if att.ContentID != "" {
			continue
		}
		data := att.InlineData()
		d.Attachments = append(d.Attachments, &protonmail.Outgoing{Name: att.Name, MIMEType: att.MIMEType, Data: data, Size: int64(len(data))})
	}
	return d, msg, nil
}

func (a *Account) ForwardAttachments(ctx context.Context, msg *protonmail.Message) ([]*protonmail.Outgoing, error) {
	var out []*protonmail.Outgoing
	for _, att := range msg.Attachments {
		if att.ContentID != "" {
			continue
		}
		data := att.InlineData()
		out = append(out, &protonmail.Outgoing{Name: att.Name, MIMEType: att.MIMEType, Data: data, Size: int64(len(data))})
	}
	return out, nil
}
