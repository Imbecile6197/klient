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
//
// Transient failures (network, keyring blip, 5xx) must NOT tear down the
// session or push the login UI — only genuine auth loss does.
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
			// IMAP: password rejected. Keep the session so dismissing any UI
			// does not look like a logout; Swift shows status, not Proton login.
			e.emit(Event{
				Type:    "deauth",
				Account: id,
				Payload: map[string]any{
					"kind":       "imap",
					"needsLogin": false,
					"message":    i18n.T("the server rejected the password"),
				},
			})
			return
		}

		st, loadErr := loadProtonSessionRetry(old.Username())
		if loadErr != nil {
			log.Printf("macbridge: recover %s: keyring unavailable: %v (not logging out)", id, loadErr)
			e.emit(Event{
				Type:    "sessionRecoverFailed",
				Account: id,
				Payload: map[string]any{"reason": "keyring", "message": loadErr.Error()},
			})
			e.scheduleRecoverRetry(s)
			return
		}
		if st == nil {
			e.forceProtonDeauth(s, old, id)
			return
		}

		// Always try Resume from the keyring — even when the token string
		// matches the in-memory one. After deauthOnce the old client cannot
		// refresh; a fresh Resume may still succeed (or fail as auth-lost).
		ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
		pa, rerr := protonmail.Resume(ctx, e.mgr, st)
		cancel()
		if rerr != nil {
			if protonmail.IsAuthLost(rerr) {
				log.Printf("macbridge: recover %s: auth lost: %v", id, rerr)
				e.forceProtonDeauth(s, old, id)
				return
			}
			log.Printf("macbridge: recover %s: resume transient failure: %v (not logging out)", id, rerr)
			e.emit(Event{
				Type:    "sessionRecoverFailed",
				Account: id,
				Payload: map[string]any{"reason": "transient", "message": rerr.Error()},
			})
			e.scheduleRecoverRetry(s)
			return
		}

		acc := mailbox.Proton(pa)
		e.mu.Lock()
		if !e.sessionAliveLocked(s) {
			e.mu.Unlock()
			acc.Close()
			return
		}
		oldCancel := s.cancel
		ctx2, cancel2 := context.WithCancel(e.ctx)
		s.mu.Lock()
		s.ctx = ctx2
		s.cancel = cancel2
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
	}()
}

func loadProtonSessionRetry(username string) (*secrets.ProtonSession, error) {
	var last error
	for i := 0; i < 4; i++ {
		st, err := secrets.LoadProtonSession(username)
		if err == nil {
			return st, nil
		}
		last = err
		time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
	}
	if last == nil {
		last = errors.New("keyring unavailable")
	}
	return nil, last
}

func (e *Engine) scheduleRecoverRetry(s *session) {
	if s == nil {
		return
	}
	time.AfterFunc(8*time.Second, func() {
		if e.ctx.Err() != nil || !e.sessionAlive(s) {
			return
		}
		e.recoverSession(s)
	})
}

func (e *Engine) forceProtonDeauth(s *session, old mailbox.Account, id string) {
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
			"kind":       "proton",
			"needsLogin": true,
			"message":    i18n.T("The Proton session has ended (for example after logging out on the web or in another app). Please log in again."),
		},
	})
}
