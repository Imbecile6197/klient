// Package macbridge is the Go side of the macOS native UI: JSON methods and
// events over a thin C API (cmd/klientcore). Linux GTK UI does not use this.
package macbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ProtonMail/go-proton-api"

	aipkg "github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/richtext"
	"github.com/Imbecile6197/klient/internal/secrets"
	"github.com/Imbecile6197/klient/internal/spam"
	"github.com/Imbecile6197/klient/internal/usage"
)

// EventHandler receives JSON events for the Swift UI (main-thread hop is Swift's job).
type EventHandler func(json string)

// Engine holds logged-in accounts and dispatches bridge methods.
type Engine struct {
	mu sync.Mutex

	cfg     config.Config
	mgr     *proton.Manager
	lists   *spam.Blocklists
	filter  *spam.Filter
	ai      *aipkg.Client
	ollama  *ollama.Runtime
	usage   *usage.Store
	onEvent EventHandler

	sessions  map[string]*session // username / id
	pending   map[string]*protonmail.PendingLogin
	pendingHV map[string]*hvLogin // username → password + HV token mid-login
	outbox    []*outboxItem
	ctx       context.Context
	cancel    context.CancelFunc
	demoMode  bool
}

// hvLogin is a Proton login waiting for the user to finish verify.proton.me.
type hvLogin struct {
	password []byte
	hv       *proton.APIHVDetails
	url      string
}

type session struct {
	acc          mailbox.Account
	ctx          context.Context
	cancel       context.CancelFunc
	checked      map[string]time.Time // hold until spam filter decides
	guard        map[string]bool
	events       bool
	loopsStarted bool // catch-up + sweep once per session
	catchingUp   bool // one catchUp at a time (StatusUp + sweep overlap)
	recovering   bool
	mu           sync.Mutex
}

// maxHold releases a message even if its check never finishes.
const maxHold = 6 * time.Minute

func (s *session) done() <-chan struct{} {
	if s == nil || s.ctx == nil {
		ch := make(chan struct{})
		return ch
	}
	return s.ctx.Done()
}

func (s *session) hold(id string) {
	s.mu.Lock()
	if s.checked == nil {
		s.checked = map[string]time.Time{}
	}
	s.checked[id] = time.Now()
	s.mu.Unlock()
}

func (s *session) release(id string) {
	s.mu.Lock()
	delete(s.checked, id)
	s.mu.Unlock()
}

func (s *session) held() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for id, t := range s.checked {
		if time.Since(t) < maxHold {
			out[id] = true
		} else {
			delete(s.checked, id)
		}
	}
	return out
}

func (s *session) begin(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guard[id] {
		return false
	}
	if s.guard == nil {
		s.guard = map[string]bool{}
	}
	s.guard[id] = true
	return true
}

func (s *session) end(id string) {
	s.mu.Lock()
	delete(s.guard, id)
	s.mu.Unlock()
}

// Global is the process-wide engine used by the C exports.
var Global *Engine

// Init creates the process engine. Safe to call once.
func Init(onEvent EventHandler) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		onEvent:   onEvent,
		sessions:  map[string]*session{},
		pending:   map[string]*protonmail.PendingLogin{},
		pendingHV: map[string]*hvLogin{},
		ctx:       ctx,
		cancel:    cancel,
	}
	if os.Getenv("KLIENT_DEMO") != "" {
		e.demoMode = true
		if err := prepareDemoEnv(); err != nil {
			// Never fall through to the real Application Support config —
			// startDemo sets LocalOnly and would permanently lock AI providers.
			log.Fatalf("macbridge: demo env: %v", err)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		log.Printf("macbridge: config: %v", err)
		cfg = config.Default()
	}
	e.cfg = cfg
	i18n.Apply(cfg.Language)
	e.mgr = protonmail.NewManager(cfg)
	e.lists = spam.NewBlocklists()
	e.filter = spam.NewFilter(cfg, e.lists)
	e.ai = newAIClient(ctx, cfg)
	e.filter.SetAI(e.ai)
	imapmail.SetKeyServer(cfg.KeyServerLookup)
	imapmail.SetProtectSubject(cfg.ProtectSubject)
	// Meter AI spend like Linux startUsage — without this hook ai.usage stays empty.
	e.usage = usage.Open(usage.Path())
	e.usage.OnChange = func() {
		e.emit(Event{Type: "usageChanged"})
	}
	aipkg.SetUsageHook(e.usage.Add)
	Global = e
	e.mgr.AddStatusObserver(func(st proton.Status) {
		offline := st == proton.StatusDown
		e.emit(Event{Type: "offlineStatus", Payload: map[string]any{
			"offline": offline,
			"source":  "proton",
		}})
		if st == proton.StatusUp {
			e.mu.Lock()
			sessions := make([]*session, 0, len(e.sessions))
			for id, s := range e.sessions {
				if id == unifiedID {
					continue
				}
				sessions = append(sessions, s)
			}
			e.mu.Unlock()
			for _, s := range sessions {
				e.startEvents(s)
			}
		}
	})
	go e.lists.RunUpdater(e.ctx, e.cfg, func(err error) {
		payload := map[string]any{}
		if err != nil {
			payload["error"] = err.Error()
		}
		e.emit(Event{Type: "blocklistsUpdated", Payload: payload})
	})
	go e.outboxLoop()
	go e.autoEmptyLoop()
	return e
}

func newAIClient(ctx context.Context, cfg config.Config) *aipkg.Client {
	return newAIClientWithLocal(ctx, cfg, nil)
}

func newAIClientWithLocal(ctx context.Context, cfg config.Config, local aipkg.LocalBackend) *aipkg.Client {
	return aipkg.New(ctx, aipkg.Settings{
		AssistantProvider: cfg.AssistantProvider, AssistantModel: cfg.AssistantModel,
		SpamProvider: cfg.SpamProvider, SpamModel: cfg.SpamModel,
		Local: local, LocalOnly: cfg.LocalOnly,
	}, func(provider string) string {
		key, err := secrets.LoadAPIKey(provider)
		if err != nil {
			log.Printf("API key %s: %v", provider, err)
		}
		return key
	})
}

func (e *Engine) emit(ev Event) {
	if e.onEvent == nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	e.onEvent(string(b))
}

// Call runs one JSON request and returns a JSON response.
func Call(reqJSON string) string {
	if Global == nil {
		return mustJSON(Response{OK: false, Error: "engine not initialized"})
	}
	var req Request
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		return mustJSON(Response{OK: false, Error: err.Error()})
	}
	res := Global.dispatch(req)
	res.ID = req.ID
	return mustJSON(res)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"marshal"}`
	}
	return string(b)
}

func (e *Engine) dispatch(req Request) Response {
	switch req.Method {
	case "version":
		return Response{OK: true, Result: map[string]string{"version": Version}}
	case "echo":
		return Response{OK: true, Result: req.Params}
	case "config.get":
		e.mu.Lock()
		cfg := e.cfg
		e.mu.Unlock()
		return Response{OK: true, Result: cfg}
	case "config.save":
		return e.saveConfig(req.Params)
	case "accounts.list":
		return e.listAccounts()
	case "accounts.resumeAll":
		return e.resumeAll()
	case "proton.loginStart":
		return e.protonLoginStart(req.Params)
	case "proton.loginContinueHV":
		return e.protonLoginContinueHV(req.Params)
	case "proton.loginTOTP":
		return e.protonLoginTOTP(req.Params)
	case "proton.loginFinish":
		return e.protonLoginFinish(req.Params)
	case "imap.open":
		return e.imapOpen(req.Params)
	case "imap.trustCert":
		return e.imapTrustCert(req.Params)
	case "account.logout":
		return e.logout(req.Params)
	case "mail.list":
		return e.mailList(req.Params)
	case "mail.get":
		return e.mailGet(req.Params)
	case "mail.getFull":
		return e.mailGetFull(req.Params)
	case "mail.markRead":
		return e.mailMarkRead(req.Params)
	case "mail.markUnread":
		return e.mailMarkUnread(req.Params)
	case "mail.move":
		return e.mailMove(req.Params)
	case "mail.setLabel":
		return e.mailSetLabel(req.Params)
	case "mail.search":
		return e.mailSearch(req.Params)
	case "mail.thread":
		return e.mailThread(req.Params)
	case "mail.unreadCounts":
		return e.mailUnreadCounts(req.Params)
	case "mail.attachment":
		return e.mailAttachment(req.Params)
	case "mail.exportEML":
		return e.mailExportEML(req.Params)
	case "mail.caps":
		return e.mailCaps(req.Params)
	case "mail.saveDraft":
		return e.mailSaveDraft(req.Params)
	case "mail.send":
		return e.mailSend(req.Params)
	case "mail.openDraft":
		return e.mailOpenDraft(req.Params)
	case "mail.deleteDraft":
		return e.mailDeleteDraft(req.Params)
	case "mail.planEncryption":
		return e.mailPlanEncryption(req.Params)
	case "mail.snooze":
		return e.mailSnooze(req.Params)
	case "mail.unsnooze":
		return e.mailUnsnooze(req.Params)
	case "mail.cancelScheduled":
		return e.mailCancelScheduled(req.Params)
	case "mail.allowRemote":
		return e.mailAllowRemote(req.Params)
	case "mail.unsubscribe":
		return e.mailUnsubscribe(req.Params)
	case "folders.list":
		return e.foldersList(req.Params)
	case "folders.create":
		return e.foldersCreate(req.Params)
	case "folders.rename":
		return e.foldersRename(req.Params)
	case "folders.delete":
		return e.foldersDelete(req.Params)
	case "folders.empty":
		return e.foldersEmpty(req.Params)
	case "contacts.list":
		return e.contactsList(req.Params)
	case "contacts.recent":
		return e.contactsRecent(req.Params)
	case "contacts.add":
		return e.contactsAdd(req.Params)
	case "contacts.delete":
		return e.contactsDelete(req.Params)
	case "autoReply.get":
		return e.autoReplyGet(req.Params)
	case "autoReply.set":
		return e.autoReplySet(req.Params)
	case "ai.status":
		return e.aiStatus(req.Params)
	case "ai.test":
		return e.aiTest(req.Params)
	case "ai.summarize":
		return e.aiSummarize(req.Params)
	case "ai.draftReply":
		return e.aiDraftReply(req.Params)
	case "ai.improve":
		return e.aiImprove(req.Params)
	case "ai.askMail":
		return e.aiAskMail(req.Params)
	case "ai.digest":
		return e.aiDigest(req.Params)
	case "ai.suggestLabel":
		return e.aiSuggestLabel(req.Params)
	case "ai.usage":
		return e.usageSummary(req.Params)
	case "secrets.saveAPIKey":
		return e.secretsSaveAPIKey(req.Params)
	case "spam.feedback":
		return e.spamFeedback(req.Params)
	case "spam.decision":
		return e.spamDecision(req.Params)
	case "spam.scanInbox":
		return e.spamScanInbox(req.Params)
	case "imap.discover":
		return e.imapDiscover(req.Params)
	case "pgp.ownPublicKey":
		return e.pgpOwnPublicKey(req.Params)
	case "pgp.importKey":
		return e.pgpImportContactKey(req.Params)
	case "pgp.listKeys":
		return e.pgpListKeys(req.Params)
	case "pgp.deleteKey":
		return e.pgpDeleteKey(req.Params)
	case "pgp.ownKeyStatus":
		return e.pgpOwnKeyStatus(req.Params)
	case "pgp.generateOwnKey":
		return e.pgpGenerateOwnKey(req.Params)
	case "pgp.importOwnKey":
		return e.pgpImportOwnKey(req.Params)
	case "pgp.setOwnExpiry":
		return e.pgpSetOwnExpiry(req.Params)
	case "pgp.publishOwnKey":
		return e.pgpPublishOwnKey(req.Params)
	case "pgp.revocationCert":
		return e.pgpRevocationCert(req.Params)
	case "pgp.exportOwnPrivate":
		return e.pgpExportOwnPrivate(req.Params)
	case "pgp.deleteOwnKey":
		return e.pgpDeleteOwnKey(req.Params)
	case "mail.systemLabels":
		return e.systemLabels(req.Params)
	case "rules.applyInbox":
		return e.rulesApplyInbox(req.Params)
	case "accounts.unified":
		return e.accountsEnableUnified(req.Params)
	case "invite.parse":
		return e.inviteParse(req.Params)
	case "invite.reply":
		return e.inviteReply(req.Params)
	case "invite.saveICS":
		return e.inviteSaveICS(req.Params)
	case "ollama.status":
		return e.ollamaStatus(req.Params)
	case "ollama.install":
		return e.ollamaInstall(req.Params)
	case "ollama.start":
		return e.ollamaStart(req.Params)
	case "ollama.listModels":
		return e.ollamaListModels(req.Params)
	case "ollama.pull":
		return e.ollamaPull(req.Params)
	case "ollama.delete":
		return e.ollamaDelete(req.Params)
	case "ai.listModels":
		return e.aiListModels(req.Params)
	case "update.check":
		return e.updateCheck(req.Params)
	case "update.download":
		return e.updateDownload(req.Params)
	case "about":
		return e.about(req.Params)
	case "about.releaseNotes":
		return e.aboutReleaseNotes(req.Params)
	case "about.whatsNew":
		return e.aboutWhatsNew(req.Params)
	case "help.prepare":
		return e.helpPrepare(req.Params)
	case "outbox.list":
		return e.outboxList(req.Params)
	case "outbox.send":
		return e.outboxSend(req.Params)
	case "outbox.delete":
		return e.outboxDelete(req.Params)
	case "outbox.edit":
		return e.outboxEdit(req.Params)
	case "demo.start":
		return e.startDemo()
	case "cache.stats":
		return e.cacheStats(req.Params)
	case "cache.clear":
		return e.cacheClear(req.Params)
	case "pgp.importAttachment":
		return e.pgpImportAttachment(req.Params)
	case "pgp.senderKeyNotice":
		return e.pgpSenderKeyNotice(req.Params)
	case "pgp.acceptPending":
		return e.pgpAcceptPending(req.Params)
	case "pgp.rejectPending":
		return e.pgpRejectPending(req.Params)
	case "keyring.smoke":
		return keyringSmoke()
	case "methods":
		return Response{OK: true, Result: allMethods()}
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown method %q", req.Method)}
	}
}

func (e *Engine) saveConfig(params json.RawMessage) Response {
	var cfg config.Config
	if err := json.Unmarshal(params, &cfg); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	cfg.SignatureHTML = richtext.CleanSignature(cfg.SignatureHTML)
	if err := config.Save(cfg); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	e.cfg = cfg
	e.filter.SetConfig(cfg)
	local := e.ollama
	e.ai = newAIClientWithLocal(e.ctx, cfg, local)
	e.filter.SetAI(e.ai)
	e.mu.Unlock()
	imapmail.SetKeyServer(cfg.KeyServerLookup)
	imapmail.SetProtectSubject(cfg.ProtectSubject)
	// Apply Preferences → Language immediately so What's New / Help / AI
	// prompts switch without waiting for a restart.
	i18n.Apply(cfg.Language)
	e.emit(Event{Type: "configChanged", Payload: map[string]any{
		"language": cfg.Language,
		"lang":     i18n.Lang(),
	}})
	return Response{OK: true}
}

func (e *Engine) listAccounts() Response {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]accountInfo, 0, len(e.sessions))
	for _, s := range e.sessions {
		out = append(out, accountInfo{
			ID:          s.acc.UserID(),
			Kind:        string(s.acc.Kind()),
			Email:       s.acc.Email(),
			DisplayName: s.acc.DisplayName(),
			Username:    s.acc.Username(),
		})
	}
	return Response{OK: true, Result: out}
}

// addSession registers acc and starts event/offline loops.
// Must NOT be called while holding e.mu — startEvents → sessionAlive takes e.mu
// and would deadlock (broke Proton loginFinish / resumeAll).
func (e *Engine) addSession(acc mailbox.Account) {
	e.mu.Lock()
	s := e.replaceSessionLocked(acc)
	e.mu.Unlock()
	e.startSessionLoops(s)
}

// replaceSessionLocked swaps the session map entry. Caller must hold e.mu.
// Does not start loops — call startSessionLoops after unlocking.
func (e *Engine) replaceSessionLocked(acc mailbox.Account) *session {
	id := acc.Username()
	if old, ok := e.sessions[id]; ok {
		old.cancel()
		old.acc.Close()
		var filtered []*outboxItem
		for _, it := range e.outbox {
			if it.account != id {
				filtered = append(filtered, it)
			}
		}
		e.outbox = filtered
	}
	ctx, cancel := context.WithCancel(e.ctx)
	s := &session{
		acc: acc, ctx: ctx, cancel: cancel,
		checked: map[string]time.Time{}, guard: map[string]bool{},
	}
	e.sessions[id] = s
	return s
}

func (e *Engine) startSessionLoops(s *session) {
	if s == nil {
		return
	}
	acc := s.acc
	acc.OnDeauth(func() { e.recoverSession(s) })
	e.startEvents(s)
	go e.offlineSyncLoop(s)
	go e.loadOutbox(acc)
	if acc.StartedOffline() {
		e.emit(Event{Type: "offlineStatus", Account: acc.Username(), Payload: map[string]any{
			"offline": true, "source": "started",
		}})
	}
}

func (e *Engine) sessionAlive(s *session) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionAliveLocked(s)
}

func (e *Engine) sessionAliveLocked(s *session) bool {
	if s == nil {
		return false
	}
	cur, ok := e.sessions[s.acc.Username()]
	return ok && cur == s
}

func (e *Engine) session(id string) (*session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[id]
	if !ok {
		return nil, fmt.Errorf("account not found")
	}
	return s, nil
}
