package macbridge

import (
	"log"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// offlineSyncLoop keeps the newest messages in the encrypted cache and
// refreshes storage info (Linux offlineSyncLoop).
func (e *Engine) offlineSyncLoop(s *session) {
	acc := s.acc
	for {
		if !e.sessionAlive(s) || e.ctx.Err() != nil || s.acc != acc {
			return
		}
		if err := acc.RefreshUser(e.ctx); err == nil {
			e.emit(Event{
				Type:    "storageChanged",
				Account: acc.Username(),
				Payload: storageDTO(acc.Storage()),
			})
		}
		e.mu.Lock()
		n := e.cfg.OfflineMessages
		e.mu.Unlock()
		if n > 0 {
			if err := acc.SyncOffline(e.ctx, n, nil); err != nil && !protonmail.IsOffline(err) {
				log.Printf("macbridge offline sync %s: %v", acc.Email(), err)
			}
		}
		select {
		case <-e.ctx.Done():
			return
		case <-s.done():
			return
		case <-time.After(30 * time.Minute):
		}
	}
}

// autoEmptyLoop deletes messages older than Config.AutoEmptyDays from Trash
// and Spam of every account, a few minutes after start and then twice a day.
func (e *Engine) autoEmptyLoop() {
	wait := 3 * time.Minute
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 12 * time.Hour
		e.mu.Lock()
		days := e.cfg.AutoEmptyDays
		accs := make([]mailbox.Account, 0, len(e.sessions))
		for _, s := range e.sessions {
			accs = append(accs, s.acc)
		}
		e.mu.Unlock()
		if days <= 0 {
			continue
		}
		before := time.Now().AddDate(0, 0, -days)
		for _, acc := range accs {
			for _, f := range []string{protonmail.TrashID, protonmail.SpamID} {
				if !acc.HasFolder(f) {
					continue
				}
				n, err := acc.EmptyFolder(e.ctx, f, before, nil)
				switch {
				case err != nil:
					log.Printf("macbridge auto-empty: %s of %s: %v", protonmail.FolderByID(f).Name, acc.Email(), err)
				case n > 0:
					log.Printf("macbridge auto-empty: %d messages older than %d days deleted from %s of %s", n, days, protonmail.FolderByID(f).Name, acc.Email())
					e.emit(Event{Type: "mailChanged", Account: acc.Username()})
				}
			}
		}
	}
}
