package macbridge

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/secrets"
)

// recoverSession handles a revoked session. Proton rotates the refresh token
// on every refresh, so if another process refreshed it (new token in the
// keyring) we continue with that one; otherwise the user must log in again.
func (e *Engine) recoverSession(s *session) {
	if s == nil || !e.sessionAlive(s) {
		return
	}
	s.mu.Lock()
	if s.recovering {
		s.mu.Unlock()
		return
	}
	s.recovering = true
	s.mu.Unlock()

	old := s.acc
	id := old.Username()
	go func() {
		defer func() {
			s.mu.Lock()
			s.recovering = false
			s.mu.Unlock()
		}()

		if mailbox.AsProton(old) == nil {
			e.emit(Event{
				Type:    "deauth",
				Account: id,
				Payload: map[string]any{
					"kind":    "imap",
					"message": i18n.T("the server rejected the password"),
				},
			})
			return
		}

		var acc mailbox.Account
		err := errors.New(i18n.T("the session has ended"))
		if p := mailbox.AsProton(old); p != nil {
			if st, _ := secrets.LoadProtonSession(old.Username()); st != nil && st.RefreshToken != p.RefreshToken() {
				ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
				pa, rerr := protonmail.Resume(ctx, e.mgr, st)
				cancel()
				if rerr == nil {
					acc = mailbox.Proton(pa)
					err = nil
				} else {
					err = rerr
				}
			}
		}

		if err == nil && acc != nil {
			e.mu.Lock()
			if !e.sessionAliveLocked(s) {
				e.mu.Unlock()
				acc.Close()
				return
			}
			oldCancel := s.cancel
			ctx, cancel := context.WithCancel(e.ctx)
			s.mu.Lock()
			s.ctx = ctx
			s.cancel = cancel
			s.events = false
			s.loopsStarted = false
			s.acc = acc
			s.mu.Unlock()
			e.mu.Unlock()
			// Stop the old event stream / sweep / offline sync tied to the previous ctx.
			if oldCancel != nil {
				oldCancel()
			}
			old.Close()
			acc.OnDeauth(func() { e.recoverSession(s) })
			e.startEvents(s)
			go e.offlineSyncLoop(s)
			e.emit(Event{Type: "mailChanged", Account: id})
			e.emit(Event{Type: "sessionRecovered", Account: id})
			log.Printf("macbridge: recovered Proton session for %s", id)
			return
		}

		e.mu.Lock()
		if cur, ok := e.sessions[id]; ok && cur == s {
			delete(e.sessions, id)
		}
		e.mu.Unlock()
		s.cancel()
		old.Close()
		e.clearOutboxAccount(id)
		e.emit(Event{
			Type:    "deauth",
			Account: id,
			Payload: map[string]any{
				"kind":    "proton",
				"message": i18n.T("The Proton session has ended (for example after logging out on the web or in another app). Please log in again."),
			},
		})
	}()
}
