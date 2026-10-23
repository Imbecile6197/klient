package macbridge

import (
	"encoding/json"

	"github.com/Imbecile6197/klient/internal/mailbox"
)

// cacheStats returns offline cache message/body counts for one account, or
// every open account when account is empty / unified (Linux cachedAccounts).
func (e *Engine) cacheStats(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
	}
	_ = parseParams(params, &p)
	accs := e.cachedAccounts(p.Account)
	if len(accs) == 0 {
		return Response{OK: true, Result: map[string]any{
			"available": false, "messages": 0, "bodies": 0,
		}}
	}
	msgs, bodies := 0, 0
	for _, acc := range accs {
		m, b := acc.Cache().Stats()
		msgs, bodies = msgs+m, bodies+b
	}
	return Response{OK: true, Result: map[string]any{
		"available": true, "messages": msgs, "bodies": bodies,
	}}
}

// cacheClear empties the offline cache for the given account (or all when
// unified / empty). Outbox rows are rewritten so queued mail is kept.
func (e *Engine) cacheClear(params json.RawMessage) Response {
	var p struct {
		Account string `json:"account"`
	}
	_ = parseParams(params, &p)
	accs := e.cachedAccounts(p.Account)
	if len(accs) == 0 {
		return Response{OK: false, Error: "the cache is not available"}
	}
	for _, acc := range accs {
		_ = acc.Cache().Clear()
		e.saveOutbox(acc)
	}
	return Response{OK: true, Result: map[string]any{"cleared": len(accs)}}
}

func (e *Engine) cachedAccounts(account string) []mailbox.Account {
	e.mu.Lock()
	defer e.mu.Unlock()
	var want []mailbox.Account
	if account == "" || account == unifiedID {
		for _, s := range e.sessions {
			want = append(want, s.acc)
		}
	} else if s, ok := e.sessions[account]; ok {
		want = append(want, s.acc)
	}
	var out []mailbox.Account
	for _, acc := range want {
		if acc.Cache() != nil {
			out = append(out, acc)
		}
	}
	return out
}
