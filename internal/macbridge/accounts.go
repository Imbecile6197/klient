package macbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/demo"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/secrets"
	"github.com/Imbecile6197/klient/internal/spam"
)

func prepareDemoEnv() error {
	realData := config.DataDir()
	dir, err := os.MkdirTemp("", "klient-demo-")
	if err != nil {
		return err
	}
	for env, sub := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
		os.Setenv(env, filepath.Join(dir, sub))
	}
	secrets.UseMemory()
	src := filepath.Join(realData, "blocklists")
	dst := filepath.Join(config.DataDir(), "blocklists")
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if b, err := os.ReadFile(path); err == nil {
			_ = os.WriteFile(filepath.Join(dst, rel), b, 0o644)
		}
		return nil
	})
	return nil
}

func (e *Engine) startDemo() Response {
	e.mu.Lock()
	e.cfg.AssistantProvider, e.cfg.AssistantModel = ai.ProviderOllama, "qwen3.5:4b"
	e.cfg.SpamProvider, e.cfg.SpamModel = ai.ProviderOllama, "qwen3.5:4b"
	e.cfg.LocalOnly = true
	e.ai = ai.New(e.ctx, ai.Settings{
		AssistantProvider: e.cfg.AssistantProvider, AssistantModel: e.cfg.AssistantModel,
		SpamProvider: e.cfg.SpamProvider, SpamModel: e.cfg.SpamModel,
		LocalOnly: e.cfg.LocalOnly,
	}, func(string) string { return "" })
	e.filter.SetAI(e.ai)

	acc := demo.New()
	for _, v := range acc.Verdicts() {
		e.filter.Remember(spam.Decision{
			MessageID: v.ID, From: v.From, Subject: v.Subject, Spam: v.Spam, Source: "ai", Time: time.Now(),
			Verdict: ai.Verdict{SpamProbability: v.Probability, Category: v.Category, Reason: v.Reason},
		})
	}
	e.cfg.Accounts = []string{acc.Username()}
	demo.Offline = os.Getenv("KLIENT_DEMO_OFFLINE") != ""
	s1 := e.replaceSessionLocked(acc)
	var s2 *session
	if os.Getenv("KLIENT_DEMO") == "2" {
		second := demo.NewSecond()
		e.cfg.Accounts = append(e.cfg.Accounts, second.Username())
		s2 = e.replaceSessionLocked(second)
	}
	res := e.listAccountsLocked()
	e.mu.Unlock()
	e.startSessionLoops(s1)
	e.startSessionLoops(s2)
	return res
}

func (e *Engine) listAccountsLocked() Response {
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

func (e *Engine) resumeAll() Response {
	e.mu.Lock()
	if name, err := secrets.MigrateLegacySession(); err == nil && name != "" {
		found := false
		for _, a := range e.cfg.Accounts {
			if a == name {
				found = true
				break
			}
		}
		if !found {
			e.cfg.Accounts = append(e.cfg.Accounts, name)
			_ = config.Save(e.cfg)
		}
	}
	accounts := append([]string{}, e.cfg.Accounts...)
	mgr := e.mgr
	e.mu.Unlock()

	var errs []string
	for _, name := range accounts {
		if strings.HasPrefix(name, "imap:") {
			if err := e.resumeIMAP(name); err != nil {
				errs = append(errs, name+": "+err.Error())
			}
			continue
		}
		s, err := secrets.LoadProtonSession(name)
		if err != nil {
			errs = append(errs, name+": "+err.Error())
			continue
		}
		if s == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
		acc, err := protonmail.Resume(ctx, mgr, s)
		cancel()
		if err != nil {
			errs = append(errs, name+": "+err.Error())
			continue
		}
		e.addSession(mailbox.Proton(acc))
	}
	e.mu.Lock()
	res := e.listAccountsLocked()
	e.mu.Unlock()
	if len(errs) > 0 {
		res.Result = map[string]any{"accounts": res.Result, "warnings": errs}
	}
	return res
}

func (e *Engine) resumeIMAP(id string) error {
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()
	var set *config.MailServer
	for i := range cfg.MailServers {
		if cfg.MailServers[i].ID() == id {
			set = &cfg.MailServers[i]
			break
		}
	}
	if set == nil {
		return fmt.Errorf("imap account settings missing")
	}
	pass, err := secrets.LoadMailPassword(set.ID())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	acc, err := imapmail.Open(ctx, *set, pass)
	if err != nil {
		return err
	}
	e.addSession(acc)
	return nil
}

func (e *Engine) protonLoginStart(params json.RawMessage) Response {
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	p.Username = strings.TrimSpace(p.Username)
	return e.protonLoginAttempt(p.Username, []byte(p.Password), nil)
}

// protonLoginContinueHV retries login after the user finished verify.proton.me.
func (e *Engine) protonLoginContinueHV(params json.RawMessage) Response {
	var p struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	p.Username = strings.TrimSpace(p.Username)
	e.mu.Lock()
	hv := e.pendingHV[p.Username]
	e.mu.Unlock()
	if hv == nil {
		return Response{OK: false, Error: "no pending human verification – sign in again"}
	}
	return e.protonLoginAttempt(p.Username, hv.password, hv.hv)
}

func (e *Engine) protonLoginAttempt(username string, password []byte, hv *proton.APIHVDetails) Response {
	if username == "" || len(password) == 0 {
		return Response{OK: false, Error: "username and password are required"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	e.mu.Lock()
	mgr := e.mgr
	e.mu.Unlock()
	pl, err := protonmail.Login(ctx, mgr, username, password, hv)
	if err != nil {
		var hvErr *protonmail.HVRequiredError
		if errors.As(err, &hvErr) {
			e.mu.Lock()
			e.pendingHV[username] = &hvLogin{
				password: append([]byte(nil), password...),
				hv:       hvErr.HV,
				url:      hvErr.URL(),
			}
			e.mu.Unlock()
			return Response{OK: true, Result: map[string]any{
				"username": username,
				"needsHV":  true,
				"hvURL":    hvErr.URL(),
			}}
		}
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	if old := e.pendingHV[username]; old != nil {
		for i := range old.password {
			old.password[i] = 0
		}
		delete(e.pendingHV, username)
	}
	e.pending[username] = pl
	e.mu.Unlock()
	return Response{OK: true, Result: map[string]any{
		"username":             username,
		"needsHV":              false,
		"needsTOTP":            pl.NeedsTOTP(),
		"needsMailboxPassword": pl.NeedsMailboxPassword(),
	}}
}

func (e *Engine) pendingLogin(username string) *protonmail.PendingLogin {
	username = strings.TrimSpace(username)
	e.mu.Lock()
	defer e.mu.Unlock()
	if pl := e.pending[username]; pl != nil {
		return pl
	}
	// Tolerate different casing typed in the UI.
	for k, pl := range e.pending {
		if strings.EqualFold(k, username) {
			return pl
		}
	}
	return nil
}

func (e *Engine) protonLoginTOTP(params json.RawMessage) Response {
	var p struct {
		Username string `json:"username"`
		Code     string `json:"code"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	pl := e.pendingLogin(p.Username)
	if pl == nil {
		return Response{OK: false, Error: "no pending login"}
	}
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.TrimSpace(p.Code))
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	if err := pl.SubmitTOTP(ctx, code); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{
		"needsMailboxPassword": pl.NeedsMailboxPassword(),
	}}
}

func (e *Engine) protonLoginFinish(params json.RawMessage) Response {
	var p struct {
		Username        string `json:"username"`
		MailboxPassword string `json:"mailboxPassword"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	pl := e.pendingLogin(p.Username)
	if pl == nil {
		return Response{OK: false, Error: "no pending login"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 120*time.Second)
	defer cancel()
	var boxPass []byte
	if p.MailboxPassword != "" {
		boxPass = []byte(p.MailboxPassword)
	}
	acc, err := pl.Finish(ctx, boxPass)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	for k := range e.pending {
		if strings.EqualFold(k, strings.TrimSpace(p.Username)) || k == acc.Username() {
			delete(e.pending, k)
		}
	}
	s := e.replaceSessionLocked(mailbox.Proton(acc))
	found := false
	for _, a := range e.cfg.Accounts {
		if a == acc.Username() {
			found = true
			break
		}
	}
	if !found {
		e.cfg.Accounts = append(e.cfg.Accounts, acc.Username())
	}
	e.cfg.ActiveAccount = acc.Username()
	_ = config.Save(e.cfg)
	res := e.listAccountsLocked()
	e.mu.Unlock()
	e.startSessionLoops(s)
	if m, ok := res.Result.([]accountInfo); ok {
		res.Result = map[string]any{"accounts": m, "username": acc.Username()}
	}
	return res
}

func (e *Engine) imapOpen(params json.RawMessage) Response {
	var p struct {
		Server   config.MailServer `json:"server"`
		Password string            `json:"password"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	acc, err := imapmail.Open(ctx, p.Server, p.Password)
	if err != nil {
		if res, ok := certErrorResult(err); ok {
			return Response{OK: false, Error: err.Error(), Result: res}
		}
		return Response{OK: false, Error: err.Error()}
	}
	if err := secrets.SaveMailPassword(p.Server.ID(), p.Password); err != nil {
		acc.Close()
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	id := p.Server.ID()
	found := false
	for i := range e.cfg.MailServers {
		if e.cfg.MailServers[i].ID() == id {
			e.cfg.MailServers[i] = p.Server
			found = true
			break
		}
	}
	if !found {
		e.cfg.MailServers = append(e.cfg.MailServers, p.Server)
	}
	af := false
	for _, a := range e.cfg.Accounts {
		if a == id {
			af = true
			break
		}
	}
	if !af {
		e.cfg.Accounts = append(e.cfg.Accounts, id)
	}
	e.cfg.ActiveAccount = id
	_ = config.Save(e.cfg)
	s := e.replaceSessionLocked(acc)
	res := e.listAccountsLocked()
	e.mu.Unlock()
	e.startSessionLoops(s)
	return res
}

func (e *Engine) logout(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	s, ok := e.sessions[p.Account]
	if ok {
		delete(e.sessions, p.Account)
	}
	e.mu.Unlock()
	if !ok {
		return Response{OK: false, Error: "account not found"}
	}
	s.cancel()
	_ = s.acc.Logout(e.ctx)
	s.acc.Close()
	e.clearOutboxAccount(p.Account)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.listAccountsLocked()
}
