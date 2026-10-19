package imapmail

import (
	"context"
	"errors"
	"io"
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
		// The time limits are read once, here, so that tests can set them.
		last := watchState{renew: idleRenew, answer: idleAnswer}
		wait := 5 * time.Second
		for ctx.Err() == nil {
			start := time.Now()
			err := a.watch(ctx, &last, onNew, onChange)
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
			log.Printf("IMAP %s: watching for new mail stopped (%v), retrying in %s", a.set.Email, err, wait)
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

// idleRenew is how long one IDLE runs; idleAnswer is how long the server may
// take to end it. The IMAP library waits for an answer without a time
// limit, so a connection that silently stopped answering (after a network
// change, a VPN reconnect or a suspend) would stop the watching for good –
// new mail then reached the list without the spam check.
var (
	idleRenew  = 10 * time.Minute
	idleAnswer = 30 * time.Second
)

// answered runs a command on the watching connection; without an answer in
// time it closes the connection so that Events reconnects.
func answered(c *imapclient.Client, limit time.Duration, f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		c.Close()
		// The library does not always end a pending command when the
		// connection closes; do not wait for it for long.
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return errNoAnswer
	}
}

var errNoAnswer = errors.New("the server stopped answering")

func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// watchState is kept across reconnects. uid is the newest inbox message the
// watching knows; set lets an empty inbox (UID 0) count as known, so that
// mail arriving while the watching reconnects is not skipped.
type watchState struct {
	uid           imap.UID
	set           bool
	renew, answer time.Duration
}

func (a *Account) watch(ctx context.Context, last *watchState, onNew func(protonmail.Summary), onChange func()) error {
	arrived := make(chan struct{}, 1)
	changed := make(chan struct{}, 1)
	c, err := a.dialWith(&imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				notify(arrived)
			}
		},
		Expunge: func(uint32) { notify(changed) },
		// The data must be read out: until it is, the library stops reading
		// the connection and the watching waits for good. Data with an
		// envelope is no change made elsewhere but the answer to our own
		// "UID n:*", which servers give the newest message even when it is
		// older than n; taking it for a change refreshed the lists without
		// end.
		Fetch: func(m *imapclient.FetchMessageData) {
			own := false
			for item := m.Next(); item != nil; item = m.Next() {
				switch it := item.(type) {
				case imapclient.FetchItemDataEnvelope:
					own = true
				case imapclient.FetchItemDataBodySection:
					_, _ = io.Copy(io.Discard, it.Literal)
				case imapclient.FetchItemDataBinarySection:
					_, _ = io.Copy(io.Discard, it.Literal)
				}
			}
			if !own {
				notify(changed)
			}
		},
	})
	if err != nil {
		return err
	}
	defer c.Close()
	go func() {
		<-ctx.Done()
		c.Close()
	}()
	if a.StartedOffline() {
		// Klient started without a connection and it is back: read the
		// folders again and let the window refresh (banner, folders, list).
		_ = a.with(func(*imapclient.Client) error { return nil })
		onChange()
	}

	if !last.set {
		st, err := c.Status("INBOX", &imap.StatusOptions{UIDNext: true}).Wait()
		if err != nil {
			return err
		}
		last.uid, last.set = st.UIDNext-1, true
	}
	sel, err := c.Select("INBOX", nil).Wait()
	if err != nil {
		return err
	}

	fetchNew := func() error {
		set := imap.UIDSet{imap.UIDRange{Start: last.uid + 1, Stop: 0}} // lastUID+1:*
		var msgs []*imapclient.FetchMessageBuffer
		err := answered(c, 2*time.Minute, func() (err error) {
			msgs, err = c.Fetch(set, fetchMeta).Collect()
			return err
		})
		if err != nil {
			return err
		}
		sort.Slice(msgs, func(i, j int) bool { return msgs[i].UID < msgs[j].UID })
		var fresh []protonmail.Summary
		for _, m := range msgs {
			if m.UID <= last.uid { // "n:*" always returns the last message
				continue
			}
			last.uid = m.UID
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
			// Servers drop IDLE after ~30 minutes; renew well before that,
			// which also finds out soon when the connection died silently.
			timer := time.NewTimer(last.renew)
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
			if err := answered(c, last.answer, cmd.Wait); err != nil {
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
		if err := answered(c, last.answer, func() error { return c.Noop().Wait() }); err != nil {
			return err
		}
		select {
		case <-changed:
			onChange()
		default:
		}
	}
}
