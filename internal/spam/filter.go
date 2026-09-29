package spam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/libormacak/klient/internal/ai"
	"github.com/libormacak/klient/internal/config"
	"github.com/libormacak/klient/internal/mailparse"
	"github.com/libormacak/klient/internal/protonmail"
)

// Decision is the stored outcome for one message.
type Decision struct {
	MessageID string     `json:"message_id"`
	From      string     `json:"from"`
	Subject   string     `json:"subject"`
	Spam      bool       `json:"spam"`
	Verdict   ai.Verdict `json:"verdict"`
	Hits      []string   `json:"hits"`
	Source    string     `json:"source"` // "ai", "blocklist", "allowlist", "denylist", "user"
	Time      time.Time  `json:"time"`
}

type state struct {
	Allow     map[string]bool     `json:"allow"` // addresses or "@domain"
	Deny      map[string]bool     `json:"deny"`
	Decisions map[string]Decision `json:"decisions"`
	Contacts  map[string]bool     `json:"contacts"` // addresses the user has written to
}

// Filter classifies incoming mail. The AI makes the decision; blocklist hits
// and user lists are passed to it as evidence. Without an AI client it falls
// back to lists and blocklists only.
type Filter struct {
	Lists *Blocklists
	cfg   config.Config

	mu    sync.Mutex
	st    state
	aiCli *ai.Client
}

func NewFilter(cfg config.Config, lists *Blocklists) *Filter {
	f := &Filter{Lists: lists, cfg: cfg}
	f.st = state{Allow: map[string]bool{}, Deny: map[string]bool{}, Decisions: map[string]Decision{}, Contacts: map[string]bool{}}
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, &f.st)
	}
	for _, m := range []*map[string]bool{&f.st.Allow, &f.st.Deny, &f.st.Contacts} {
		if *m == nil {
			*m = map[string]bool{}
		}
	}
	if f.st.Decisions == nil {
		f.st.Decisions = map[string]Decision{}
	}
	return f
}

func (f *Filter) SetConfig(cfg config.Config) {
	f.mu.Lock()
	f.cfg = cfg
	f.mu.Unlock()
}

func (f *Filter) SetAI(c *ai.Client) {
	f.mu.Lock()
	f.aiCli = c
	f.mu.Unlock()
}

func statePath() string { return filepath.Join(config.DataDir(), "spamfilter.json") }

func (f *Filter) saveLocked() {
	// Keep the decision log bounded.
	if len(f.st.Decisions) > 5000 {
		cutoff := time.Now().AddDate(0, -3, 0)
		for id, d := range f.st.Decisions {
			if d.Time.Before(cutoff) {
				delete(f.st.Decisions, id)
			}
		}
	}
	_ = os.MkdirAll(config.DataDir(), 0o700)
	b, _ := json.Marshal(f.st)
	tmp := statePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, statePath())
	}
}

func listed(m map[string]bool, addr string) bool {
	addr = strings.ToLower(addr)
	if m[addr] {
		return true
	}
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return m[addr[i:]]
	}
	return false
}

// Decision returns a stored decision for a message, if any.
func (f *Filter) Decision(id string) (Decision, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.st.Decisions[id]
	return d, ok
}

// RecordContact remembers that the user sent mail to addr.
func (f *Filter) RecordContact(addr string) {
	f.mu.Lock()
	f.st.Contacts[strings.ToLower(addr)] = true
	f.saveLocked()
	f.mu.Unlock()
}

// Feedback records a user correction: the sender goes to the allow or deny
// list so future mail is handled accordingly.
func (f *Filter) Feedback(msgID, from string, isSpam bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	from = strings.ToLower(from)
	if isSpam {
		delete(f.st.Allow, from)
		f.st.Deny[from] = true
	} else {
		delete(f.st.Deny, from)
		f.st.Allow[from] = true
	}
	d := f.st.Decisions[msgID]
	d.MessageID, d.From, d.Spam, d.Source, d.Time = msgID, from, isSpam, "user", time.Now()
	f.st.Decisions[msgID] = d
	f.saveLocked()
}

var ipRe = regexp.MustCompile(`\[(\d{1,3}(?:\.\d{1,3}){3}|[0-9a-fA-F:]{3,})\]`)

// receivedIPs extracts public IPs from Received headers.
func receivedIPs(headers map[string][]string) []netip.Addr {
	var out []netip.Addr
	for k, vals := range headers {
		if !strings.EqualFold(k, "Received") {
			continue
		}
		for _, v := range vals {
			for _, m := range ipRe.FindAllStringSubmatch(v, -1) {
				if ip, err := netip.ParseAddr(m[1]); err == nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
					out = append(out, ip)
				}
			}
		}
	}
	return out
}

func header(headers map[string][]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return strings.Join(v, " | ")
		}
	}
	return ""
}

// Evaluate classifies msg. It does not move anything; see Apply.
func (f *Filter) Evaluate(ctx context.Context, msg *protonmail.Message) Decision {
	from := ""
	if msg.Meta.Sender != nil {
		from = strings.ToLower(msg.Meta.Sender.Address)
	}
	d := Decision{MessageID: msg.Meta.ID, From: from, Subject: msg.Meta.Subject, Time: time.Now()}

	f.mu.Lock()
	allowed, denied := listed(f.st.Allow, from), listed(f.st.Deny, from)
	known := f.st.Contacts[from]
	aiCli := f.aiCli
	cfg := f.cfg
	f.mu.Unlock()

	// Evidence from blocklists.
	for _, ip := range receivedIPs(msg.Headers) {
		if feed := f.Lists.CheckIP(ip); feed != "" {
			d.Hits = append(d.Hits, fmt.Sprintf("IP %s v %s", ip, feed))
		}
	}
	if i := strings.LastIndexByte(from, '@'); i >= 0 {
		if feed := f.Lists.CheckDomain(from[i+1:]); feed != "" {
			d.Hits = append(d.Hits, fmt.Sprintf("doména odesílatele %s v %s", from[i+1:], feed))
		}
	}
	seen := map[string]bool{}
	for _, link := range msg.Links {
		u, err := url.Parse(link)
		if err != nil || seen[u.Hostname()] {
			continue
		}
		seen[u.Hostname()] = true
		if feed := f.Lists.CheckDomain(u.Hostname()); feed != "" {
			d.Hits = append(d.Hits, fmt.Sprintf("odkaz na %s v %s", u.Hostname(), feed))
		}
	}

	var flags []string
	fl := msg.Meta.Flags
	for _, x := range []struct {
		f    proton.MessageFlag
		name string
	}{
		{proton.MessageFlagSpamAuto, "Proton: spam"},
		{proton.MessageFlagPhishingAuto, "Proton: phishing"},
		{proton.MessageFlagSPFFail, "SPF selhal"},
		{proton.MessageFlagDKIMFail, "DKIM selhal"},
		{proton.MessageFlagDMARCFail, "DMARC selhal"},
		{proton.MessageFlagDMARCPass, "DMARC v pořádku"},
	} {
		if fl.Has(x.f) {
			flags = append(flags, x.name)
		}
	}

	if aiCli.HasSpam() && cfg.SpamFilterEnabled {
		body := msg.Text
		if r := []rune(body); len(r) > cfg.SpamBodyChars {
			body = string(r[:cfg.SpamBodyChars]) + "\n[… zkráceno …]"
		}
		to := ""
		for i, a := range msg.Meta.ToList {
			if i > 0 {
				to += ", "
			}
			to += mailparse.DisplayAddress(a)
		}
		sender := from
		if msg.Meta.Sender != nil {
			sender = mailparse.DisplayAddress(msg.Meta.Sender)
		}
		in := ai.SpamInput{
			From: sender, To: to, Subject: msg.Meta.Subject, Body: body,
			ReplyTo:        header(msg.Headers, "Reply-To"),
			Authentication: header(msg.Headers, "Authentication-Results"),
			BlocklistHits:  d.Hits, ProtonFlags: flags,
			UserAllowed: allowed, UserBlocked: denied, KnownContact: known,
		}
		// Local models on a CPU need minutes, not seconds.
		cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		v, err := aiCli.ClassifySpam(cctx, in)
		cancel()
		if err == nil {
			d.Verdict, d.Source = v, "ai"
			d.Spam = v.SpamProbability >= cfg.SpamThreshold
			f.store(d)
			return d
		}
		d.Verdict.Reason = "AI nedostupná: " + err.Error()
	}

	// Fallback without AI.
	switch {
	case allowed:
		d.Source, d.Spam = "allowlist", false
	case denied:
		d.Source, d.Spam = "denylist", true
	case len(d.Hits) > 0:
		d.Source, d.Spam = "blocklist", true
	default:
		d.Source = "none"
	}
	f.store(d)
	return d
}

func (f *Filter) store(d Decision) {
	f.mu.Lock()
	f.st.Decisions[d.MessageID] = d
	f.saveLocked()
	f.mu.Unlock()
}

// ProcessNew evaluates a newly arrived inbox message and moves it to Spam when
// the decision says so. It returns the decision and whether it was moved.
// Mailbox is what the filter needs from an account.
type Mailbox interface {
	Get(ctx context.Context, id string) (*protonmail.Message, error)
	Move(ctx context.Context, folderID string, ids ...string) error
}

func (f *Filter) ProcessNew(ctx context.Context, acc Mailbox, meta protonmail.Summary) (Decision, bool, error) {
	inInbox := false
	for _, l := range meta.LabelIDs {
		if l == protonmail.InboxID {
			inInbox = true
		}
	}
	if !inInbox {
		return Decision{}, false, nil
	}
	if d, ok := f.Decision(meta.ID); ok {
		return d, false, nil
	}
	msg, err := acc.Get(ctx, meta.ID)
	if err != nil {
		return Decision{}, false, err
	}
	d := f.Evaluate(ctx, msg)
	if !d.Spam {
		return d, false, nil
	}
	if err := acc.Move(ctx, protonmail.SpamID, meta.ID); err != nil {
		return d, false, err
	}
	return d, true, nil
}
