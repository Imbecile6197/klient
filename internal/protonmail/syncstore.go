package protonmail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/bradenaw/juniper/stream"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/syncstate"
)

// SyncFolderName is the folder that holds what Klient shares between the
// user's computers. Each version is a message encrypted by Proton with the
// user's key, so only the user's Klient can read it.
const SyncFolderName = "Klient"

// syncHeader marks Klient's sync messages; so does the host part of their
// Message-ID, which (unlike the subject) is the same in every language and
// is known from the metadata alone.
const (
	syncHeader = "X-Klient-Sync"
	syncIDHost = "klient.local"
)

// syncSubject starts the subject of every sync message: plain ASCII and the
// same in every language, so it is recognised from the metadata alone.
const syncSubject = "[Klient sync] Data shared between your computers - do not change or delete"

// Subjects of the sync messages Klient 0.12.0 wrote (translated).
var legacySyncSubjects = []string{
	"Klient – data shared between your computers (do not change or delete)",
	"Klient – data sdílená mezi vašimi počítači (neměňte ani nemažte)",
}

// IsSyncMessage reports whether m is one of Klient's sync messages: they
// are Klient's own data, never mail for the user – no notification, not in
// any list.
func IsSyncMessage(m Summary) bool {
	if strings.HasSuffix(strings.ToLower(strings.Trim(m.ExternalID, "<> ")), "@"+syncIDHost) {
		return true
	}
	subject := m.Subject
	if dec, err := new(mime.WordDecoder).DecodeHeader(subject); err == nil {
		subject = dec
	}
	if strings.HasPrefix(subject, "[Klient sync]") {
		return true
	}
	for _, s := range legacySyncSubjects {
		if subject == s {
			return true
		}
	}
	return false
}

// withoutSync drops Klient's sync messages from a list.
func withoutSync(ms []Summary) []Summary {
	out := ms[:0:0]
	for _, m := range ms {
		if !IsSyncMessage(m) {
			out = append(out, m)
		}
	}
	return out
}

var syncLabelID struct {
	sync.Mutex
	ids map[string]string // user ID -> folder ID
}

// syncFolder finds the sync folder (made when create and missing).
func (a *Account) syncFolder(ctx context.Context, create bool) (string, error) {
	syncLabelID.Lock()
	defer syncLabelID.Unlock()
	if id := syncLabelID.ids[a.userID]; id != "" {
		return id, nil
	}
	labels, err := a.client.GetLabels(ctx, proton.LabelTypeFolder)
	if err != nil {
		return "", err
	}
	id := ""
	for _, l := range labels {
		if l.Name == SyncFolderName && len(l.Path) <= 1 {
			id = l.ID
			break
		}
	}
	if id == "" && create {
		l, err := a.client.CreateLabel(ctx, proton.CreateLabelReq{Name: SyncFolderName, Color: labelColors[0], Type: proton.LabelTypeFolder})
		if err != nil {
			return "", err
		}
		id = l.ID
	}
	if id != "" {
		if syncLabelID.ids == nil {
			syncLabelID.ids = map[string]string{}
		}
		syncLabelID.ids[a.userID] = id
	}
	return id, nil
}

// IsSyncFolder reports whether a folder is the sync folder (hidden in the
// folder list).
func (a *Account) IsSyncFolder(id string) bool {
	syncLabelID.Lock()
	defer syncLabelID.Unlock()
	return id != "" && syncLabelID.ids[a.userID] == id
}

// SyncBackend stores the shared state in the account.
func (a *Account) SyncBackend() syncstate.Backend { return protonSync{a} }

type protonSync struct{ a *Account }

func (p protonSync) Load(ctx context.Context) ([]syncstate.Version, error) {
	a := p.a
	id, err := a.syncFolder(ctx, false)
	if err != nil || id == "" {
		return nil, err
	}
	metas, err := a.client.GetMessageMetadataPage(ctx, 0, 50, proton.MessageFilter{LabelID: id})
	if err != nil {
		return nil, err
	}
	var out []syncstate.Version
	present := map[string]bool{}
	for _, m := range metas {
		present[m.ID] = true
		// A stored version never changes: download each one only once.
		if body, ok := syncBodies.get(m.ID); ok {
			out = append(out, syncstate.Version{ID: m.ID, Data: body, Time: time.Unix(m.Time, 0)})
			continue
		}
		msg, err := a.client.GetMessage(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		a.mu.RLock()
		kr := a.addrKRs[msg.AddressID]
		a.mu.RUnlock()
		if kr == nil {
			continue
		}
		body, err := msg.Decrypt(kr)
		if err != nil {
			continue // not readable: skipped like a damaged version
		}
		syncBodies.put(m.ID, body)
		out = append(out, syncstate.Version{ID: m.ID, Data: body, Time: time.Unix(m.Time, 0)})
	}
	syncBodies.keep(present)
	return out, nil
}

// syncBodies are the versions already downloaded, by message ID.
var syncBodies = bodyCache{m: map[string][]byte{}}

type bodyCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (c *bodyCache) get(id string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[id]
	return b, ok
}

func (c *bodyCache) put(id string, b []byte) {
	c.mu.Lock()
	c.m[id] = b
	c.mu.Unlock()
}

// keep forgets versions that are gone.
func (c *bodyCache) keep(present map[string]bool) {
	c.mu.Lock()
	for id := range c.m {
		if !present[id] {
			delete(c.m, id)
		}
	}
	c.mu.Unlock()
}

func (p protonSync) Save(ctx context.Context, data []byte) error {
	a := p.a
	id, err := a.syncFolder(ctx, true)
	if err != nil {
		return err
	}
	a.mu.RLock()
	var addr proton.Address
	for _, ad := range a.addrs {
		if ad.Status == proton.AddressStatusEnabled && (addr.ID == "" || ad.Order < addr.Order) {
			addr = ad
		}
	}
	kr := a.addrKRs[addr.ID]
	a.mu.RUnlock()
	if kr == nil {
		return errors.New(i18n.T("no address to store the shared data under"))
	}
	raw := syncMessage(addr.Email, data)
	res, err := a.client.ImportMessages(ctx, kr, 1, 1, proton.ImportReq{
		Metadata: proton.ImportMetadata{AddressID: addr.ID, LabelIDs: []string{id}, Unread: false, Flags: proton.MessageFlagReceived},
		Message:  raw,
	})
	if err != nil {
		return err
	}
	results, err := stream.Collect(ctx, res)
	if err != nil {
		return err
	}
	for _, r := range results {
		if r.Code != proton.SuccessCode {
			return fmt.Errorf("%s (%d)", r.Error(), r.Code)
		}
	}
	return nil
}

func (p protonSync) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return p.a.client.DeleteMessage(ctx, ids...)
}

// syncMessage is the mail holding one version: a short note for the user
// who finds it in the web interface, then the data.
func syncMessage(addr string, data []byte) []byte {
	idb := make([]byte, 12)
	_, _ = rand.Read(idb)
	host := syncIDHost
	head := fmt.Sprintf("From: Klient <%s>\r\nTo: <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\n%s: 1\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n",
		addr, addr,
		syncSubject,
		time.Now().Format(time.RFC1123Z),
		hex.EncodeToString(idb), host, syncHeader)
	note := i18n.T("Klient keeps the data it shares between your computers here (spam verdicts, sender lists, rules, settings). Klient replaces this message itself; please do not change or delete it.") + "\r\n\r\n"
	return append([]byte(head+note), data...)
}
