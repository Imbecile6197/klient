package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/shared"
	"github.com/Imbecile6197/klient/internal/spam"
	"github.com/Imbecile6197/klient/internal/syncstate"
	"github.com/Imbecile6197/klient/internal/usage"
)

// Sync between the user's computers: the shared state (package shared over
// syncstate) is stored in the first Proton account. Everything here is an
// addition: with sync off, or when it fails, every computer works on its
// own exactly as before, and mail is never held up by it.

// How often the shared state is read, how long to wait after a change
// before sending it (to send several at once), and how long a computer
// waits for the main computer's spam verdict.
const (
	syncEvery    = 2 * time.Minute
	syncSettle   = 5 * time.Second
	waitForMain  = 3 * time.Minute
	pollForMain  = 15 * time.Second
	usageHistory = 40 * 24 * time.Hour
)

type syncer struct {
	hub    *shared.Hub
	kick   chan struct{}
	cancel context.CancelFunc
	seeded bool // this run has added its own values to the shared state
}

// hub is the shared state when sync is on and running (any thread).
func (a *App) hub() *shared.Hub {
	a.shareMu.Lock()
	defer a.shareMu.Unlock()
	if a.share == nil {
		return nil
	}
	return a.share.hub
}

// kickSync asks for a sync soon (any thread).
func (a *App) kickSync() {
	a.shareMu.Lock()
	s := a.share
	a.shareMu.Unlock()
	if s == nil {
		return
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func syncPath() string { return filepath.Join(config.DataDir(), "sync.json") }

func randomNode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func defaultComputerName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "Klient"
}

// startSync starts the sync when it is on. UI thread.
func (a *App) startSync() {
	if demoMode() || !a.cfg.SyncEnabled {
		return
	}
	a.shareMu.Lock()
	running := a.share != nil
	a.shareMu.Unlock()
	if running {
		return
	}
	if a.cfg.SyncNode == "" {
		a.cfg.SyncNode = randomNode()
	}
	if a.cfg.SyncName == "" {
		a.cfg.SyncName = defaultComputerName()
	}
	_ = config.Save(a.cfg)
	e := syncstate.NewEngine(protonBackend{a}, syncPath(), a.cfg.SyncNode)
	h := shared.NewHub(e, a.cfg.SyncNode)
	ctx, cancel := context.WithCancel(a.ctx)
	s := &syncer{hub: h, kick: make(chan struct{}, 1), cancel: cancel}
	a.shareMu.Lock()
	a.share = s
	a.shareMu.Unlock()
	// What this computer last knew of the shared state (its local copy):
	// the settings and senders it had seen, so that only real changes are
	// sent later and nothing old is sent over newer values.
	cfg := a.cfg
	if h.ApplySettings(&cfg) {
		a.applySharedSettings(cfg)
	}
	h.SharedLists()
	for _, ss := range a.sessions {
		a.attachPlan(ss.acc)
	}
	go a.syncLoop(ctx, s)
}

// stopSync turns the sync off on this computer. UI thread.
func (a *App) stopSync() {
	a.shareMu.Lock()
	s := a.share
	a.share = nil
	a.shareMu.Unlock()
	if s == nil {
		return
	}
	h := s.hub
	go func() {
		// The others must not keep waiting for this computer.
		if h.Main() == h.Node {
			h.SetMain("")
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			_, _ = h.E.Sync(ctx)
			cancel()
		}
		s.cancel()
	}()
	if a.usage != nil {
		a.usage.SetOthers(nil)
	}
}

func (a *App) syncLoop(ctx context.Context, s *syncer) {
	for {
		a.syncOnce(ctx, s)
		select {
		case <-ctx.Done():
			return
		case <-time.After(syncEvery):
		case <-s.kick:
			// Changes often come in bursts: send them together.
			select {
			case <-ctx.Done():
				return
			case <-time.After(syncSettle):
			}
		}
	}
}

// syncOnce reads and stores the shared state once and applies what came
// from the other computers. Off the UI thread.
func (a *App) syncOnce(ctx context.Context, s *syncer) {
	h := s.hub
	name, accounts := make(chan string, 1), make(chan []string, 1)
	ui(func() {
		name <- a.cfg.SyncName
		var l []string
		for _, ss := range a.sessions {
			l = append(l, ss.acc.Username())
		}
		accounts <- l
	})
	h.Heartbeat(<-name, <-accounts)
	if a.usage != nil {
		h.PushUsage(usageForSync(a.usage.DaysSince(time.Now().Add(-usageHistory).Format("2006-01-02"))))
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	_, err := h.E.Sync(cctx)
	cancel()
	if err != nil {
		if ctx.Err() == nil && !protonmail.IsOffline(err) && err != errNoSyncAccount {
			log.Printf("sync: %v", err)
		}
		ui(a.refreshSyncPrefs)
		return
	}
	if !s.seeded {
		// First sync of this run: add what this computer has and the
		// shared state does not (the first computer brings everything).
		done := make(chan struct{})
		ui(func() {
			h.SeedSettings(a.cfg)
			h.SeedLists(listsForSync(a.filter.SenderLists()))
			a.seedPlans(h)
			h.SeedMain()
			close(done)
		})
		<-done
		s.seeded = true
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, err = h.E.Sync(cctx)
		cancel()
		if err != nil {
			ui(a.refreshSyncPrefs)
			return
		}
	}
	ui(func() {
		if a.hub() != h {
			return // turned off meanwhile
		}
		cfg := a.cfg
		if h.ApplySettings(&cfg) {
			a.applySharedSettings(cfg)
		}
		a.applyPlans(h)
		l := h.SharedLists()
		if !sameLists(l, listsForSync(a.filter.SenderLists())) {
			a.filter.SetSenderLists(spam.SenderLists{Allow: l.Allow, Deny: l.Deny, Contacts: l.Contacts})
		}
		if a.usage != nil {
			a.usage.SetOthers(usageFromSync(h.OthersUsage()))
			if a.mv != nil {
				a.mv.updateUsage()
			}
		}
		a.refreshSyncPrefs()
	})
}

// applySharedSettings takes over settings changed on another computer.
func (a *App) applySharedSettings(cfg config.Config) {
	aiChanged := cfg.AssistantProvider != a.cfg.AssistantProvider || cfg.AssistantModel != a.cfg.AssistantModel ||
		cfg.SpamProvider != a.cfg.SpamProvider || cfg.SpamModel != a.cfg.SpamModel
	a.cfg = cfg
	a.saveConfig() // sends nothing back: the values count as seen
	if aiChanged {
		a.initAI()
	}
	if a.mv != nil {
		a.mv.updateFilterStatus()
	}
}

// syncSettingsChanged sends settings the user changed (after saveConfig).
func (a *App) syncSettingsChanged() {
	if h := a.hub(); h != nil {
		h.PushSettings(a.cfg)
		a.kickSync()
	}
}

// syncListsChanged sends sender list changes (spam marks, contacts).
func (a *App) syncListsChanged() {
	if h := a.hub(); h != nil {
		h.PushLists(listsForSync(a.filter.SenderLists()))
		a.kickSync()
	}
}

func listsForSync(l spam.SenderLists) shared.Lists {
	return shared.Lists{Allow: l.Allow, Deny: l.Deny, Contacts: l.Contacts}
}

func sameLists(a, b shared.Lists) bool {
	eq := func(x, y map[string]bool) bool {
		if len(x) != len(y) {
			return false
		}
		for k := range x {
			if !y[k] {
				return false
			}
		}
		return true
	}
	return eq(a.Allow, b.Allow) && eq(a.Deny, b.Deny) && eq(a.Contacts, b.Contacts)
}

func usageForSync(days map[string]map[string]usage.Counts) map[string]map[string]shared.Counts {
	out := map[string]map[string]shared.Counts{}
	for day, m := range days {
		c := map[string]shared.Counts{}
		for k, v := range m {
			c[k] = shared.Counts{Requests: v.Requests, In: v.In, Cached: v.Cached, Out: v.Out}
		}
		out[day] = c
	}
	return out
}

func usageFromSync(o map[string]map[string]map[string]shared.Counts) map[string]map[string]map[string]*usage.Counts {
	out := map[string]map[string]map[string]*usage.Counts{}
	for node, days := range o {
		out[node] = map[string]map[string]*usage.Counts{}
		for day, m := range days {
			c := map[string]*usage.Counts{}
			for k, v := range m {
				c[k] = &usage.Counts{Requests: v.Requests, In: v.In, Cached: v.Cached, Out: v.Out}
			}
			out[node][day] = c
		}
	}
	return out
}

// ---- spam verdicts ---------------------------------------------------------

// sharedVerdict takes over the verdict another computer made on meta, if
// there is one: it is remembered here like a verdict of this computer.
func (a *App) sharedVerdict(meta protonmail.Summary) (spam.Decision, bool) {
	h := a.hub()
	if h == nil || meta.ExternalID == "" {
		return spam.Decision{}, false
	}
	v, ok := h.Verdict(meta.ExternalID)
	if !ok || v.By == h.Node {
		return spam.Decision{}, false
	}
	from := ""
	if meta.Sender != nil {
		from = strings.ToLower(meta.Sender.Address)
	}
	d := spam.Decision{
		MessageID: meta.ID, From: from, Subject: meta.Subject, Spam: v.Spam,
		Verdict: ai.Verdict{SpamProbability: v.Prob, Category: v.Category},
		Source:  "sync", Time: time.Now(),
	}
	a.filter.Remember(d)
	return d, true
}

// publishVerdict shares this computer's verdict. A fallback verdict made
// because the AI was not available is not shared: another computer (or a
// later check) should judge the message properly.
func (a *App) publishVerdict(meta protonmail.Summary, d spam.Decision) {
	h := a.hub()
	// No MessageID: nothing was judged (the message is not in the inbox).
	if h == nil || meta.ExternalID == "" || d.MessageID == "" || d.Source == "none" || d.Source == "sync" {
		return
	}
	h.PublishVerdict(meta.ExternalID, shared.Verdict{Spam: d.Spam, Prob: d.Verdict.SpamProbability, Source: d.Source, Category: d.Verdict.Category})
	a.kickSync()
}

// awaitMain waits (off the UI thread) for the main computer's verdict when
// another computer is the main one, runs and has this account open.
func (a *App) awaitMain(acc mailbox.Account, meta protonmail.Summary) (spam.Decision, bool) {
	h := a.hub()
	if h == nil || meta.ExternalID == "" || h.IsMain() || !h.MainAlive() || !h.MainHas(acc.Username()) {
		return spam.Decision{}, false
	}
	deadline := time.Now().Add(waitForMain)
	for time.Now().Before(deadline) {
		a.kickSync()
		select {
		case <-a.ctx.Done():
			return spam.Decision{}, false
		case <-time.After(pollForMain):
		}
		if a.hub() != h {
			return spam.Decision{}, false
		}
		if d, ok := a.sharedVerdict(meta); ok {
			return d, true
		}
	}
	log.Printf("sync: no verdict from the main computer in %s, judging here", waitForMain)
	return spam.Decision{}, false
}

// ---- storage -------------------------------------------------------------------

var errNoSyncAccount = errNoSync{}

type errNoSync struct{}

func (errNoSync) Error() string { return "sync: no Proton account is open" }

// protonBackend stores the shared state in the first open Proton account.
type protonBackend struct{ a *App }

func (b protonBackend) account() (*protonmail.Account, error) {
	ch := make(chan *protonmail.Account, 1)
	ui(func() {
		for _, s := range b.a.sessions {
			if p := mailbox.AsProton(s.acc); p != nil && !s.acc.StartedOffline() {
				ch <- p
				return
			}
		}
		ch <- nil
	})
	select {
	case p := <-ch:
		if p == nil {
			return nil, errNoSyncAccount
		}
		return p, nil
	case <-b.a.ctx.Done():
		return nil, b.a.ctx.Err()
	}
}

func (b protonBackend) Load(ctx context.Context) ([]syncstate.Version, error) {
	p, err := b.account()
	if err != nil {
		return nil, err
	}
	return p.SyncBackend().Load(ctx)
}

func (b protonBackend) Save(ctx context.Context, data []byte) error {
	p, err := b.account()
	if err != nil {
		return err
	}
	return p.SyncBackend().Save(ctx, data)
}

func (b protonBackend) Delete(ctx context.Context, ids []string) error {
	p, err := b.account()
	if err != nil {
		return err
	}
	return p.SyncBackend().Delete(ctx, ids)
}

// hasProton reports whether a Proton account is open (sync needs one).
func (a *App) hasProton() bool {
	for _, s := range a.sessions {
		if mailbox.AsProton(s.acc) != nil {
			return true
		}
	}
	return false
}

// ---- snoozed and scheduled mail of IMAP accounts ----------------------------

// attachPlan connects an IMAP account's plan (snoozed and scheduled mail)
// to the sync. Without sync the hooks do nothing and this computer carries
// out its own plan, as before. UI thread.
func (a *App) attachPlan(acc mailbox.Account) {
	im, ok := acc.(*imapmail.Account)
	if !ok {
		return
	}
	user := acc.Username()
	if h := a.hub(); h != nil {
		h.SharedPlan(user) // what this computer last knew: only changes are sent
	}
	im.OnPlanChange(func() {
		if h := a.hub(); h != nil {
			p := im.Plan()
			h.PushPlan(user, shared.Plan{Snoozed: p.Snoozed, Scheduled: p.Scheduled})
			a.kickSync()
		}
	})
	// With several computers only one returns snoozed mail and sends
	// scheduled mail: the main one, when it runs and has this account.
	im.RunsDue(func() bool {
		h := a.hub()
		return h == nil || h.IsMain() || !h.MainAlive() || !h.MainHas(user)
	})
}

// seedPlans shares the plans of the open IMAP accounts (first sync of a
// run). UI thread.
func (a *App) seedPlans(h *shared.Hub) {
	for _, s := range a.sessions {
		if im, ok := s.acc.(*imapmail.Account); ok {
			p := im.Plan()
			h.SeedPlan(s.acc.Username(), shared.Plan{Snoozed: p.Snoozed, Scheduled: p.Scheduled})
		}
	}
}

// applyPlans takes over plans changed on other computers. UI thread.
func (a *App) applyPlans(h *shared.Hub) {
	for _, s := range a.sessions {
		im, ok := s.acc.(*imapmail.Account)
		if !ok {
			continue
		}
		sp := h.SharedPlan(s.acc.Username())
		local := im.Plan()
		if sameTimes(sp.Snoozed, local.Snoozed) && sameTimes(sp.Scheduled, local.Scheduled) {
			continue
		}
		if err := im.SetPlan(imapmail.Plan{Snoozed: sp.Snoozed, Scheduled: sp.Scheduled}); err == nil && a.mv != nil && a.showing(s.acc) {
			a.mv.scheduleRefresh()
		}
	}
}

func sameTimes(x, y map[string]int64) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if w, ok := y[k]; !ok || w != v {
			return false
		}
	}
	return true
}
