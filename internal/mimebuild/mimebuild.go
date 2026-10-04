// Package mimebuild writes outgoing message bodies as MIME entities, shared by
// the IMAP accounts and PGP/MIME sending from Proton.
package mimebuild

import (
	"bytes"
	"io"

	gomail "github.com/emersion/go-message/mail"
)

// Attachment is a file attached to a message.
type Attachment struct {
	Name, MIMEType string
	Data           []byte
}

// Content writes a message body as a standalone MIME entity (its own
// Content-Type header + body), ready to be signed or encrypted: the text,
// the HTML version if any, and the attachments.
func Content(text, html string, atts []Attachment) ([]byte, error) {
	var buf bytes.Buffer
	textHeader := func() gomail.InlineHeader {
		var th gomail.InlineHeader
		th.Set("Content-Type", "text/plain; charset=utf-8")
		th.Set("Content-Transfer-Encoding", "quoted-printable")
		return th
	}
	if len(atts) == 0 && html == "" {
		var h gomail.Header
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, text); err != nil {
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
	if _, err := io.WriteString(pw, text); err != nil {
		return nil, err
	}
	pw.Close()
	if html != "" {
		var hh gomail.InlineHeader
		hh.Set("Content-Type", "text/html; charset=utf-8")
		hh.Set("Content-Transfer-Encoding", "quoted-printable")
		hw, err := iw.CreatePart(hh)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(hw, html); err != nil {
			return nil, err
		}
		hw.Close()
	}
	iw.Close()
	for _, att := range atts {
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
