package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gio/v2"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/power"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// AI results are kept in the account's encrypted cache, so a summary is
// computed once per conversation state and shown instantly afterwards.

type cachedSummary struct {
	LatestID string // newest message the summary covers
	Text     string
	At       int64
}

func summaryKey(convID, latestID string) string {
	if convID == "" {
		return "ai-summary:msg:" + latestID
	}
	return "ai-summary:conv:" + convID
}

// latestReal is the newest non-draft message of a conversation (metas oldest first).
func latestReal(metas []protonmail.Summary) (protonmail.Summary, bool) {
	for i := len(metas) - 1; i >= 0; i-- {
		if !metas[i].IsDraft() {
			return metas[i], true
		}
	}
	return protonmail.Summary{}, false
}

// cachedSummaryFor returns the stored summary if it still covers the newest message.
func cachedSummaryFor(acc mailbox.Account, metas []protonmail.Summary) (string, bool) {
	latest, ok := latestReal(metas)
	if !ok || acc.Cache() == nil {
		return "", false
	}
	var cs cachedSummary
	if acc.Cache().Get(summaryKey(latest.ConversationID, latest.ID), &cs) && cs.LatestID == latest.ID && cs.Text != "" {
		return cs.Text, true
	}
	return "", false
}

// summarizeThread summarises a conversation (metas oldest first), using and
// filling the cache. have holds already decrypted messages. Off the UI thread.
func (a *App) summarizeThread(ctx context.Context, acc mailbox.Account, metas []protonmail.Summary, have map[string]*protonmail.Message) (string, error) {
	if text, ok := cachedSummaryFor(acc, metas); ok {
		return text, nil
	}
	latest, ok := latestReal(metas)
	if !ok {
		return "", errors.New(i18n.T("the thread contains only drafts"))
	}
	var sb strings.Builder
	n := 0
	for _, meta := range metas {
		if meta.IsDraft() {
			continue
		}
		msg := have[meta.ID]
		if msg == nil {
			var err error
			if msg, err = acc.Get(ctx, meta.ID); err != nil {
				return "", err
			}
		}
		from := ""
		if msg.Meta.Sender != nil {
			from = mailparse.DisplayAddress(msg.Meta.Sender)
		}
		fmt.Fprintf(&sb, "--- Message from %s, %s ---\n%s\n\n", from,
			time.Unix(msg.Meta.Time, 0).Format("2006-01-02 15:04"), msg.Text)
		n++
	}
	who := fmt.Sprintf("a thread of %d messages", n)
	if n == 1 && latest.Sender != nil {
		who = mailparse.DisplayAddress(latest.Sender)
	}
	out, err := a.ai.Summarize(ctx, who, latest.Subject, sb.String())
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if acc.Cache() != nil {
		_ = acc.Cache().Set(summaryKey(latest.ConversationID, latest.ID), cachedSummary{latest.ID, out, time.Now().Unix()})
	}
	return out, nil
}

// ---- Precomputing ----------------------------------------------------------------

type precomputeJob struct {
	acc  mailbox.Account
	meta protonmail.Summary
}

// queuePrecompute asks the background worker to summarise a new message's
// conversation. Only with a local model: with a cloud one it would send all
// incoming mail away without being asked.
func (a *App) queuePrecompute(acc mailbox.Account, meta protonmail.Summary) {
	if !a.cfg.PrecomputeSummaries || !a.ai.AssistantLocal() {
		return
	}
	select {
	case a.precompute <- precomputeJob{acc, meta}:
	default: // queue full: those are summarised on demand
	}
}

// canPrecompute: on mains power, not in power-saver mode, and the local model
// is not busy with something the user is waiting for.
func (a *App) canPrecompute() bool {
	if !power.OnAC() || !a.ollama.Idle() {
		return false
	}
	if m := gio.PowerProfileMonitorDupDefault(); m != nil && m.PowerSaverEnabled() {
		return false
	}
	return true
}

func (a *App) precomputeLoop() {
	for {
		var job precomputeJob
		select {
		case <-a.ctx.Done():
			return
		case job = <-a.precompute:
		}
		for !a.canPrecompute() {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
		ok := make(chan bool)
		ui(func() { ok <- a.sessionOf(job.acc) != nil && a.cfg.PrecomputeSummaries && a.ai.AssistantLocal() })
		if !<-ok {
			continue
		}
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
		metas := []protonmail.Summary{job.meta}
		if job.meta.ConversationID != "" {
			if ms, err := job.acc.ThreadMessages(ctx, job.meta.ConversationID, false); err == nil && len(ms) > 0 {
				metas = ms
			}
		}
		sort.SliceStable(metas, func(i, j int) bool { return metas[i].Time < metas[j].Time })
		if _, err := a.summarizeThread(ctx, job.acc, metas, nil); err != nil && ctx.Err() == nil {
			log.Printf("precomputing a summary: %v", err)
		}
		cancel()
	}
}
