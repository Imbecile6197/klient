package macbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
		Account        string `json:"account"`
		MessageID      string `json:"messageId"`
		ConversationID string `json:"conversationId"`
		From           string `json:"from"`
		Subject        string `json:"subject"`
		Body           string `json:"body"`
		HTML           string `json:"html"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if e.ai == nil || !e.ai.HasAssistant() {
		return Response{OK: false, Error: "AI assistant is not configured"}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Minute)
	defer cancel()

	from, subject, body := p.From, p.Subject, mailparse.BodyForAI(p.Body, p.HTML)
	if p.Account != "" && (p.MessageID != "" || p.ConversationID != "") {
		s, err := e.session(p.Account)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		from, subject, body, err = e.summarizeInput(ctx, s.acc, p.MessageID, p.ConversationID, from, subject, body)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
	}
	text, err := e.ai.Summarize(ctx, from, subject, body)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"summary": strings.TrimSpace(text)}}
}

// summarizeInput builds the body Linux uses: the whole conversation when possible.
func (e *Engine) summarizeInput(ctx context.Context, acc mailbox.Account, messageID, conversationID, from, subject, fallbackBody string) (string, string, string, error) {
	convID := conversationID
	if convID == "" {
		convID = messageID
	}
	metas, err := acc.ThreadMessages(ctx, convID, false)
	if err != nil || len(metas) == 0 {
		if messageID == "" {
			return from, subject, fallbackBody, nil
		}
		msg, gerr := acc.Get(ctx, messageID)
		if gerr != nil {
			if fallbackBody != "" {
				return from, subject, fallbackBody, nil
			}
			return "", "", "", gerr
		}
		return messageFromAI(msg)
	}
	var sb strings.Builder
	n := 0
	var latest *protonmail.Message
	for _, meta := range metas {
		if meta.IsDraft() {
			continue
		}
		msg, gerr := acc.Get(ctx, meta.ID)
		if gerr != nil {
			continue
		}
		who := ""
		if msg.Meta.Sender != nil {
			who = mailparse.DisplayAddress(msg.Meta.Sender)
		}
		fmt.Fprintf(&sb, "--- Message from %s, %s ---\n%s\n\n", who,
			time.Unix(msg.Meta.Time, 0).Format("2006-01-02 15:04"),
			mailparse.BodyForAI(msg.Text, msg.HTML))
		latest = msg
		n++
	}
	if n == 0 || latest == nil {
		if fallbackBody != "" {
			return from, subject, fallbackBody, nil
		}
		return "", "", "", fmt.Errorf("%s", i18n.T("the thread contains only drafts"))
	}
	who := fmt.Sprintf("a thread of %d messages", n)
	if n == 1 && latest.Meta.Sender != nil {
		who = mailparse.DisplayAddress(latest.Meta.Sender)
	}
	return who, latest.Meta.Subject, sb.String(), nil
}

func messageFromAI(msg *protonmail.Message) (from, subject, body string, err error) {
	if msg.Meta.Sender != nil {
		from = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	return from, msg.Meta.Subject, mailparse.BodyForAI(msg.Text, msg.HTML), nil
}

func (e *Engine) aiDraftReply(params json.RawMessage) Response {
	var p struct {
		Account     string `json:"account"`
		MessageID   string `json:"messageId"`
		From        string `json:"from"`
		Subject     string `json:"subject"`
		Body        string `json:"body"`
		HTML        string `json:"html"`
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

	from, subject, body := p.From, p.Subject, mailparse.BodyForAI(p.Body, p.HTML)
	if p.Account != "" && p.MessageID != "" {
		if s, err := e.session(p.Account); err == nil {
			if msg, gerr := s.acc.Get(ctx, p.MessageID); gerr == nil {
				from, subject, body, _ = messageFromAI(msg)
			}
		}
	}
	text, err := e.ai.DraftReply(ctx, from, subject, body, p.Instruction)
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
	today := time.Now().Format("Monday, January 2, 2006")
	queries, err := e.ai.SearchPlan(ctx, p.Question, today)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}

	// Linux parity: scan All Mail (not an empty/inbox-only scope) and look
	// through thousands of newest metadata rows — Search only matches subject
	// and addresses, so a tiny window misses older insurance/company mail.
	var found []protonmail.Summary
	seen := map[string]bool{}
	for _, q := range queries {
		list, serr := s.acc.Search(ctx, protonmail.AllMailID, q, 3000)
		if serr != nil {
			continue
		}
		for _, m := range list {
			if seen[m.ID] || m.IsDraft() {
				continue
			}
			seen[m.ID] = true
			found = append(found, m)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Time > found[j].Time })
	if n := e.ai.MaxDocs(10, 5); len(found) > n {
		found = found[:n]
	}

	var docs []ai.MailDoc
	for _, m := range found {
		msg, gerr := s.acc.Get(ctx, m.ID)
		if gerr != nil {
			continue
		}
		from := ""
		if msg.Meta.Sender != nil {
			from = mailparse.DisplayAddress(msg.Meta.Sender)
		}
		docs = append(docs, ai.MailDoc{
			From: from, Subject: msg.Meta.Subject,
			Body: mailparse.BodyForAI(msg.Text, msg.HTML),
			Date: time.Unix(msg.Meta.Time, 0).Format("2006-01-02 15:04"),
		})
	}
	if len(docs) == 0 {
		msg := fmt.Sprintf(i18n.T("No messages found (searched for: %s). Try rephrasing the question, for example with the sender's name."), strings.Join(queries, ", "))
		return Response{OK: true, Result: map[string]any{"answer": msg, "docs": 0, "queries": queries}}
	}
	answer, err := e.ai.AnswerFromMail(ctx, p.Question, today, docs)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"answer": strings.TrimSpace(answer), "docs": len(docs), "queries": queries}}
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
	list, err := s.acc.List(ctx, protonmail.InboxID, 0, 100)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	max := e.ai.MaxDocs(20, 8)
	var docs []ai.MailDoc
	for _, m := range list {
		if !bool(m.Unread) {
			continue
		}
		if d, ok := e.filter.Decision(e.ownMessageID(m.ID)); ok && d.Spam {
			continue
		}
		msg, gerr := s.acc.Get(ctx, m.ID)
		if gerr != nil {
			continue
		}
		from := ""
		if msg.Meta.Sender != nil {
			from = mailparse.DisplayAddress(msg.Meta.Sender)
		}
		docs = append(docs, ai.MailDoc{
			From: from, Subject: msg.Meta.Subject,
			Body: mailparse.BodyForAI(msg.Text, msg.HTML),
			Date: time.Unix(msg.Meta.Time, 0).Format("2006-01-02 15:04"),
		})
		if len(docs) >= max {
			break
		}
	}
	if len(docs) == 0 {
		return Response{OK: true, Result: map[string]any{"digest": i18n.T("No unread mail. 🎉"), "messages": 0}}
	}
	today := time.Now().Format("Monday, January 2, 2006")
	text, err := e.ai.Digest(ctx, today, docs)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{"digest": strings.TrimSpace(text), "messages": len(docs)}}
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
	ch, err := e.ai.SuggestLabel(ctx, names, from, msg.Meta.Subject, mailparse.BodyForAI(msg.Text, msg.HTML))
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
	// Rebuild AI client with new keys; keep the Ollama backend if running.
	e.mu.Lock()
	cfg := e.cfg
	local := e.ollama
	e.ai = newAIClientWithLocal(e.ctx, cfg, local)
	e.filter.SetAI(e.ai)
	e.mu.Unlock()
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
