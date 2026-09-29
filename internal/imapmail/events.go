package imapmail

import (
	"context"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/Imbecile6197/klient/internal/protonmail"
)

// Events watches the inbox on its own connection with IMAP IDLE (the server
// reports new mail at once) or, without IDLE, by polling every minute.
// For every new unread message onNew runs first – Klient holds it until the
// spam filter has judged it – and only then onChange refreshes the lists.
func (a *Account) Events(ctx context.Context, onNew func(protonmail.Summary), onChange func()) error {
	go a.localLoop(ctx, onChange)
	go func() {
		var lastUID imap.UID
		wait := 5 * time.Second
		for ctx.Err() == nil {
			start := time.Now()
			err := a.watch(ctx, &lastUID, onNew, onChange)
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, ErrAuth) {
				if a.deauth != nil {
					a.deauth()
				}
				return
			}
			if time.Since(start) > time.Minute {
				wait = 5 * time.Second // it ran fine for a while
			}
			log.Printf("IMAP %s: sledování nové pošty přerušeno (%v), znovu za %s", a.set.Email, err, wait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, 5*time.Minute)
		}
	}()
	return nil
}

func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (a *Account) watch(ctx context.Context, lastUID *imap.UID, onNew func(protonmail.Summary), onChange func()) error {
	arrived := make(chan struct{}, 1)
	changed := make(chan struct{}, 1)
	c, err := a.dialWith(&imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				notify(arrived)
			}
		},
		Expunge: func(uint32) { notify(changed) },
		Fetch:   func(*imapclient.FetchMessageData) { notify(changed) },
	})
	if err != nil {
		return err
	}
	defer c.Close()
	go func() {
		<-ctx.Done()
		c.Close()
	}()

	if *lastUID == 0 {
		st, err := c.Status("INBOX", &imap.StatusOptions{UIDNext: true}).Wait()
		if err != nil {
			return err
		}
		*lastUID = st.UIDNext - 1
	}
	sel, err := c.Select("INBOX", nil).Wait()
	if err != nil {
		return err
	}

	fetchNew := func() error {
		set := imap.UIDSet{imap.UIDRange{Start: *lastUID + 1, Stop: 0}} // lastUID+1:*
		msgs, err := c.Fetch(set, fetchMeta).Collect()
		if err != nil {
			return err
		}
		sort.Slice(msgs, func(i, j int) bool { return msgs[i].UID < msgs[j].UID })
		var fresh []protonmail.Summary
		for _, m := range msgs {
			if m.UID <= *lastUID { // "n:*" always returns the last message
				continue
			}
			*lastUID = m.UID
			s := a.summary("INBOX", sel.UIDValidity, m)
			fresh = append(fresh, s)
		}
		if len(fresh) == 0 {
			return nil
		}
		if a.cache != nil {
			_ = a.cache.PutMetadata(fresh...)
		}
		for _, s := range fresh {
			if bool(s.Unread) {
				onNew(s) // spam check and hold before the list shows it
			}
		}
		onChange()
		return nil
	}

	idle := c.Caps().Has(imap.CapIdle)
	for {
		if err := fetchNew(); err != nil {
			return err
		}
		if idle {
			cmd, err := c.Idle()
			if err != nil {
				return err
			}
			// Servers drop IDLE after ~30 minutes; renew before that.
			timer := time.NewTimer(25 * time.Minute)
			gotChange := false
			select {
			case <-ctx.Done():
			case <-arrived:
			case <-changed:
				gotChange = true
			case <-timer.C:
			case <-c.Closed():
			}
			timer.Stop()
			_ = cmd.Close()
			if err := cmd.Wait(); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if gotChange {
				onChange()
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Minute):
		}
		if err := c.Noop().Wait(); err != nil {
			return err
		}
		select {
		case <-changed:
			onChange()
		default:
		}
	}
}
