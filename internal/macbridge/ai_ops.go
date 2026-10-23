package macbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/secrets"
	"github.com/Imbecile6197/klient/internal/usage"
)

func (e *Engine) aiSummarize(params json.RawMessage) Response {
	var p struct {
		From    string `json:"from"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: "AI assistant is not configured"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Minute)
	defer cancel()
	text, err := e.ai.Summarize(ctx, p.From, p.Subject, p.Body)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"summary": text}}
}

func (e *Engine) aiDraftReply(params json.RawMessage) Response {
	var p struct {
		From        string `json:"from"`
		Subject     string `json:"subject"`
		Body        string `json:"body"`
		Instruction string `json:"instruction"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: "AI assistant is not configured"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Minute)
	defer cancel()
	text, err := e.ai.DraftReply(ctx, p.From, p.Subject, p.Body, p.Instruction)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"reply": text}}
}

func (e *Engine) aiImprove(params json.RawMessage) Response {
	var p struct {
		Draft       string `json:"draft"`
		Instruction string `json:"instruction"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: "AI assistant is not configured"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Minute)
	defer cancel()
	text, err := e.ai.Improve(ctx, p.Draft, p.Instruction)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"text": text}}
}

func (e *Engine) aiAskMail(params json.RawMessage) Response {
	var p struct {
		Account  string `json:"account"`
		Question string `json:"question"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: "AI assistant is not configured"}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
	defer cancel()
	today := time.Now().Format("2006-01-02")
	queries, err := e.ai.SearchPlan(ctx, p.Question, today)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	var docs []ai.MailDoc
	seen := map[string]bool{}
	for _, q := range queries {
		list, err := s.acc.Search(ctx, "", q, 20)
		if err != nil {
			continue
		}
		for _, m := range list {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			msg, err := s.acc.Get(ctx, m.ID)
			if err != nil {
				continue
			}
			from := ""
			if msg.Meta.Sender != nil {
				from = msg.Meta.Sender.Address
			}
			docs = append(docs, ai.MailDoc{
				From: from, Subject: msg.Meta.Subject, Body: msg.Text, Date: time.Unix(msg.Meta.Time, 0).Format("2006-01-02"),
			})
			if len(docs) >= e.ai.MaxDocs(12, 6) {
				break
			}
		}
		if len(docs) >= e.ai.MaxDocs(12, 6) {
			break
		}
	}
	answer, err := e.ai.AnswerFromMail(ctx, p.Question, today, docs)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"answer": answer, "docs": len(docs)}}
}

func (e *Engine) aiDigest(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: "AI assistant is not configured"}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
	defer cancel()
	list, err := s.acc.List(ctx, protonmail.InboxID, 0, 40)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	var docs []ai.MailDoc
	for _, m := range list {
		if !bool(m.Unread) {
			continue
		}
		msg, err := s.acc.Get(ctx, m.ID)
		if err != nil {
			continue
		}
		from := ""
		if msg.Meta.Sender != nil {
			from = msg.Meta.Sender.Address
		}
		docs = append(docs, ai.MailDoc{
			From: from, Subject: msg.Meta.Subject, Body: msg.Text, Date: time.Unix(msg.Meta.Time, 0).Format("2006-01-02"),
		})
		if len(docs) >= e.ai.MaxDocs(20, 8) {
			break
		}
	}
	today := time.Now().Format("2006-01-02")
	text, err := e.ai.Digest(ctx, today, docs)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"digest": text, "messages": len(docs)}}
}

func (e *Engine) aiSuggestLabel(params json.RawMessage) Response {
	var p struct {
		Account   string `json:"account"`
		MessageID string `json:"messageId"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: i18n.T("The AI assistant is not set up")}
	}
	s, err := e.session(p.Account)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 6*time.Minute)
	defer cancel()
	msg, err := s.acc.Get(ctx, p.MessageID)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	label, ch, err := e.suggestLabel(ctx, s.acc, msg)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{
		"label": label.Name, "labelId": label.ID,
		"confidence": ch.Confidence, "reason": ch.Reason,
	}}
}

func (e *Engine) suggestLabel(ctx context.Context, acc mailbox.Account, msg *protonmail.Message) (protonmail.UserLabel, ai.LabelChoice, error) {
	labels, err := acc.UserLabels(ctx)
	if err != nil {
		return protonmail.UserLabel{}, ai.LabelChoice{}, err
	}
	var tagLabels []protonmail.UserLabel
	var names []string
	for _, l := range labels {
		if l.Folder {
			continue
		}
		tagLabels = append(tagLabels, l)
		names = append(names, l.Name)
	}
	if len(tagLabels) == 0 {
		return protonmail.UserLabel{}, ai.LabelChoice{}, fmt.Errorf("%s", i18n.T("you have no labels – create them on the Proton website"))
	}
	from := ""
	if msg.Meta.Sender != nil {
		from = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	ch, err := e.ai.SuggestLabel(ctx, names, from, msg.Meta.Subject, msg.Text)
	if err != nil {
		return protonmail.UserLabel{}, ch, err
	}
	for _, l := range tagLabels {
		if l.Name == ch.Label {
			return l, ch, nil
		}
	}
	return protonmail.UserLabel{}, ch, nil
}

// autoLabelMessage labels a new message when the AI is confident (Linux parity).
func (e *Engine) autoLabelMessage(acc mailbox.Account, meta protonmail.Summary) string {
	if e.ai == nil || !e.ai.HasAssistant() {
		return ""
	}
	ctx, cancel := context.WithTimeout(e.ctx, 6*time.Minute)
	defer cancel()
	msg, err := acc.Get(ctx, meta.ID)
	if err != nil {
		return ""
	}
	l, ch, err := e.suggestLabel(ctx, acc, msg)
	if err != nil || l.ID == "" || ch.Confidence < 0.6 {
		return ""
	}
	if acc.SetLabel(ctx, l.ID, true, meta.ID) != nil {
		return ""
	}
	return fmt.Sprintf(i18n.T("AI label %s"), l.Name)
}

func (e *Engine) aiStatus(_ json.RawMessage) Response {
	hasA, hasS := false, false
	if e.ai != nil {
		hasA, hasS = e.ai.HasAssistant(), e.ai.HasSpam()
	}
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()

	nets, domains, urls := 0, 0, 0
	var lastUpdate int64
	if e.lists != nil {
		nets, domains, urls = e.lists.Stats()
		if !e.lists.LastUpdate.IsZero() {
			lastUpdate = e.lists.LastUpdate.Unix()
		}
	}

	return Response{OK: true, Result: map[string]any{
		"hasAssistant":        hasA,
		"hasSpam":             hasS,
		"assistantProvider":   cfg.AssistantProvider,
		"assistantModel":      cfg.AssistantModel,
		"assistantLocal":      ai.IsLocal(cfg.AssistantProvider),
		"assistantName":       providerDisplayName(cfg.AssistantProvider),
		"spamProvider":        cfg.SpamProvider,
		"spamModel":           cfg.SpamModel,
		"spamLocal":           ai.IsLocal(cfg.SpamProvider),
		"spamName":            providerDisplayName(cfg.SpamProvider),
		"spamFilterEnabled":   cfg.SpamFilterEnabled,
		"blocklistNetworks":   nets,
		"blocklistDomains":    domains,
		"blocklistURLs":       urls,
		"blocklistLastUpdate": lastUpdate,
	}}
}

// providerDisplayName is the short UI name for a provider ID (e.g. "Claude").
func providerDisplayName(id string) string {
	if p, ok := ai.ProviderByID(id); ok {
		name := p.Name
		if i := strings.IndexByte(name, '('); i > 0 {
			return strings.TrimSpace(name[:i])
		}
		return name
	}
	return id
}

// aiTest sends a tiny completion to verify the provider key / local model.
func (e *Engine) aiTest(params json.RawMessage) Response {
	var p struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()
	provider := p.Provider
	if provider == "" {
		provider = cfg.AssistantProvider
	}
	if cfg.LocalOnly {
		provider = ai.ProviderOllama
	}
	model := p.Model
	if model == "" {
		switch {
		case provider == cfg.AssistantProvider && cfg.AssistantModel != "":
			model = cfg.AssistantModel
		case provider == cfg.SpamProvider && cfg.SpamModel != "":
			model = cfg.SpamModel
		default:
			if info, ok := ai.ProviderByID(provider); ok {
				model = info.SpamModel
			}
		}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()

	var err error
	if ai.IsLocal(provider) {
		r := e.ensureOllama()
		if r.Installed() == "" {
			err = fmt.Errorf("%s", i18n.T("Ollama is not installed"))
		} else if startErr := r.Start(ctx); startErr != nil {
			err = startErr
		} else {
			e.mu.Lock()
			e.ai = newAIClientWithLocal(e.ctx, cfg, r)
			e.filter.SetAI(e.ai)
			e.mu.Unlock()
			client := ollama.NewClient(r.Addr())
			_, _, err = client.Chat(ctx, ollama.ChatRequest{
				Model: model, System: "You are a connection test.", User: "Reply with just the word OK.",
				NumCtx: 2048, KeepAlive: "1m",
			})
		}
	} else {
		key, keyErr := secrets.LoadAPIKey(provider)
		if keyErr != nil {
			err = keyErr
		} else if key == "" {
			err = ai.ErrNoAPIKey
		} else {
			err = ai.Test(ctx, provider, key, model)
		}
		if err == nil {
			// Refresh HasAssistant/HasSpam after a successful cloud probe.
			e.mu.Lock()
			if e.ollama != nil {
				e.ai = newAIClientWithLocal(e.ctx, cfg, e.ollama)
			} else {
				e.ai = newAIClient(e.ctx, cfg)
			}
			e.filter.SetAI(e.ai)
			e.mu.Unlock()
		}
	}
	if err != nil {
		ex := ai.Explain(provider, err)
		msg := ex.Text
		if msg == "" {
			msg = err.Error()
		}
		return Response{OK: false, Error: msg, Result: map[string]any{
			"provider": provider, "model": model, "url": ex.URL,
		}}
	}
	return Response{OK: true, Result: map[string]any{
		"provider": provider, "model": model,
	}}
}

func (e *Engine) aiListModels(params json.RawMessage) Response {
	var p struct {
		Provider string `json:"provider"`
	}
	if err := parseParams(params, &p); err != nil || p.Provider == "" {
		return Response{OK: false, Error: "provider required"}
	}
	if ai.IsLocal(p.Provider) {
		return e.ollamaListModels(nil)
	}
	key, err := secrets.LoadAPIKey(p.Provider)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if key == "" {
		return Response{OK: false, Error: ai.ErrNoAPIKey.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 45*time.Second)
	defer cancel()
	models, err := ai.ListModels(ctx, p.Provider, key)
	if err != nil {
		ex := ai.Explain(p.Provider, err)
		msg := ex.Text
		if msg == "" {
			msg = err.Error()
		}
		return Response{OK: false, Error: msg}
	}
	return Response{OK: true, Result: map[string]any{"models": models}}
}

func (e *Engine) secretsSaveAPIKey(params json.RawMessage) Response {
	var p struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := secrets.SaveAPIKey(p.Provider, p.Key); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	// Rebuild AI client with new keys.
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()
	e.ai = newAIClient(e.ctx, cfg)
	e.filter.SetAI(e.ai)
	return Response{OK: true}
}

func (e *Engine) usageSummary(_ json.RawMessage) Response {
	e.mu.Lock()
	prices := e.cfg.AIPrices
	store := e.usage
	e.mu.Unlock()
	if store == nil {
		store = usage.Open(usage.Path())
	}
	now := time.Now()
	from, to := usage.Period(now, 0)
	lines := store.Lines(from, to, prices)
	cost, unpriced := usage.Total(lines)
	todayKey := usage.Today(now)
	todayLines := store.Lines(todayKey, todayKey, prices)
	todayCost, todayUnpriced := usage.Total(todayLines)
	return Response{OK: true, Result: map[string]any{
		"from": from, "to": to, "lines": lines,
		"cost": cost, "unpriced": unpriced,
		"today": todayKey, "todayCost": todayCost, "todayUnpriced": todayUnpriced,
	}}
}
