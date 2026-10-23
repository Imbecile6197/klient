package macbridge

import (
	"context"
	"log"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// catchUpMax limits how far back mail that arrived while Klient was not
// running is checked; the inbox is read catchUpPage messages at a time, at
// most catchUpPages pages at start and sweepPages in the periodic sweep.
const (
	catchUpMax   = 7 * 24 * time.Hour
	catchUpPage  = 50
	catchUpPages = 20
	sweepPages   = 4
	sweepEvery   = 5 * time.Minute
	sweepSettle  = 3 * time.Minute
)

// unjudged returns the unread inbox mail of the last catchUpMax that the
// spam filter has not judged, newest first. All pages are read before any
// message is checked: moving spam away would shift the pages.
func unjudged(ctx context.Context, acc mailbox.Account, known func(id string) bool, now time.Time, pages int) []protonmail.Summary {
	since := now.Add(-catchUpMax).Unix()
	var out []protonmail.Summary
	for page := 0; page < pages && ctx.Err() == nil; page++ {
		msgs, err := acc.List(ctx, protonmail.InboxID, page, catchUpPage)
		if err != nil {
			break
		}
		older := false
		for _, m := range msgs {
			if m.Time < since {
				older = true
				continue
			}
			if !bool(m.Unread) || !hasInbox(m) || known(m.ID) {
				continue
			}
			out = append(out, m)
		}
		if older || len(msgs) < catchUpPage {
			break
		}
	}
	return out
}

// sweepLoop is the safety net behind the event streams: should one stop
// reporting mail without an error, the mail would otherwise reach the list
// unchecked.
func (e *Engine) sweepLoop(s *session) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-s.done():
			return
		case <-t.C:
		}
		if !e.sessionAlive(s) {
			return
		}
		e.catchUp(s, false)
	}
}

// catchUp checks unread inbox mail that arrived while Klient was not running
// (or offline). At start without a notification for each; later (sweep) with
// one for mail of the last hour.
func (e *Engine) catchUp(s *session, start bool) {
	acc := s.acc
	pages := catchUpPages
	if !start {
		pages = sweepPages
	}
	known := func(id string) bool {
		_, ok := e.filter.Decision(id)
		return ok
	}
	msgs := unjudged(e.ctx, acc, known, time.Now(), pages)
	settled := time.Now().Add(-sweepSettle).Unix()
	recent := time.Now().Add(-time.Hour).Unix()
	n := 0
	for _, m := range msgs {
		if !start && m.Time > settled {
			continue
		}
		if known(m.ID) {
			continue
		}
		if e.ctx.Err() != nil || !e.sessionAlive(s) {
			return
		}
		held := start && e.holdNew(s, m)
		e.checkNew(s, m, held, !start && m.Time > recent)
		n++
	}
	switch {
	case n > 0 && start:
		log.Printf("macbridge spamfilter: checked %d messages of %s that arrived while Klient was not running", n, acc.Email())
	case n > 0:
		log.Printf("macbridge spamfilter: checked %d messages of %s that the watching for new mail missed", n, acc.Email())
	}
}
