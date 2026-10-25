package imapmail

import (
	"context"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Plan is what an account has planned: conversations snoozed until a time
// and messages in the Scheduled folder with their send time (unix seconds).
// IDs come from the server, so they are the same on every computer.
type Plan struct {
	Snoozed   map[string]int64
	Scheduled map[string]int64
}

// planHooks connect the plan to the sync between computers.
type planHooks struct {
	mu       sync.Mutex
	onChange func()      // the plan changed on this computer
	runsDue  func() bool // whether this computer carries out the plan
}

// Plan returns a copy of the plan.
func (a *Account) Plan() Plan {
	st := a.loadLocal()
	return Plan{Snoozed: copyTimes(st.Snoozed), Scheduled: copyTimes(st.Scheduled)}
}

// SetPlan replaces the plan with the shared one; it reports no change back.
func (a *Account) SetPlan(p Plan) error {
	if a.cache == nil {
		return nil
	}
	return a.cache.Set("imap-local", localState{Snoozed: copyTimes(p.Snoozed), Scheduled: copyTimes(p.Scheduled)})
}

// OnPlanChange is called (in a goroutine) when the user or this computer
// changed the plan.
func (a *Account) OnPlanChange(f func()) {
	a.hooks.mu.Lock()
	a.hooks.onChange = f
	a.hooks.mu.Unlock()
}

// RunsDue decides whether this computer returns snoozed mail and sends
// scheduled mail (with several computers only one should).
func (a *Account) RunsDue(f func() bool) {
	a.hooks.mu.Lock()
	a.hooks.runsDue = f
	a.hooks.mu.Unlock()
}

func (a *Account) planChanged() {
	a.hooks.mu.Lock()
	f := a.hooks.onChange
	a.hooks.mu.Unlock()
	if f != nil {
		go f()
	}
}

func (a *Account) runsDueHere() bool {
	a.hooks.mu.Lock()
	f := a.hooks.runsDue
	a.hooks.mu.Unlock()
	return f == nil || f()
}

func copyTimes(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// onServer reports whether a message still is on the server (not from the
// offline cache): a scheduled message another computer sent meanwhile must
// not be sent again.
func (a *Account) onServer(ctx context.Context, id string) (bool, error) {
	ref, err := parseID(id)
	if err != nil {
		return false, nil
	}
	found := false
	err = a.with(func(c *imapclient.Client) error {
		sel, err := c.Select(ref.mailbox, nil).Wait()
		if err != nil {
			return err
		}
		if sel.UIDValidity != ref.uidValidity {
			return nil
		}
		msgs, err := c.Fetch(imap.UIDSetNum(ref.uid), &imap.FetchOptions{UID: true}).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.UID == ref.uid {
				found = true
			}
		}
		return nil
	})
	return found, err
}
