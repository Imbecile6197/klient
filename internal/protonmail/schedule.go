package protonmail

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/Imbecile6197/klient/internal/i18n"
)

const (
	ScheduledID = proton.AllScheduledLabel
	SnoozedID   = proton.SnoozedLabel
)

// IsScheduled reports whether a message waits in Proton's outbox for its
// scheduled delivery time (then Time is that delivery time).
func IsScheduled(s Summary) bool {
	for _, l := range s.LabelIDs {
		if l == ScheduledID {
			return true
		}
	}
	return false
}

// CancelScheduled stops a scheduled message; it becomes a draft again.
func (a *Account) CancelScheduled(ctx context.Context, id string) error {
	if err := a.client.CancelSend(ctx, id); err != nil {
		return apiErr(i18n.T("cancelling the scheduled sending failed"), err)
	}
	if a.cache != nil {
		_ = a.cache.Delete(id)
	}
	return nil
}

// Snooze hides conversations until t; Proton returns them to the inbox (as
// unread) by itself, even while Klient is not running.
func (a *Account) Snooze(ctx context.Context, conversationIDs []string, t time.Time) error {
	if len(conversationIDs) == 0 {
		return errors.New(i18n.T("the message cannot be snoozed (the conversation ID is missing)"))
	}
	if err := a.client.SnoozeConversations(ctx, conversationIDs, t.Unix()); err != nil {
		return apiErr(i18n.T("snoozing failed"), err)
	}
	if a.cache != nil {
		m := map[string]int64{}
		a.cache.Get("snoozed", &m)
		for _, id := range conversationIDs {
			m[id] = t.Unix()
		}
		_ = a.cache.Set("snoozed", m)
	}
	return nil
}

func (a *Account) Unsnooze(ctx context.Context, conversationIDs []string) error {
	if err := a.client.UnsnoozeConversations(ctx, conversationIDs); err != nil {
		return apiErr(i18n.T("unsnoozing failed"), err)
	}
	if a.cache != nil {
		m := map[string]int64{}
		if a.cache.Get("snoozed", &m) {
			for _, id := range conversationIDs {
				delete(m, id)
			}
			_ = a.cache.Set("snoozed", m)
		}
	}
	return nil
}

// SnoozedUntil is when a conversation snoozed from this computer comes back
// (zero if unknown).
func (a *Account) SnoozedUntil(conversationID string) time.Time {
	if a.cache == nil || conversationID == "" {
		return time.Time{}
	}
	m := map[string]int64{}
	if a.cache.Get("snoozed", &m) && m[conversationID] > 0 {
		return time.Unix(m[conversationID], 0)
	}
	return time.Time{}
}

// AutoReply is the vacation responder as shown in the UI.
type AutoReply struct {
	Enabled bool
	Subject string
	Message string
	Start   time.Time // zero = from now
	End     time.Time // zero = until switched off
}

func (a *Account) AutoReply(ctx context.Context) (AutoReply, error) {
	s, err := a.client.GetMailSettings(ctx)
	if err != nil {
		return AutoReply{}, apiErr(i18n.T("loading the automatic reply failed"), err)
	}
	ar := s.AutoResponder
	out := AutoReply{Enabled: ar.IsEnabled, Subject: ar.Subject, Message: ar.Message}
	if ar.Repeat == 0 {
		if ar.StartTime > 0 {
			out.Start = time.Unix(ar.StartTime, 0)
		}
		if ar.EndTime > 0 {
			out.End = time.Unix(ar.EndTime, 0)
		}
	}
	return out, nil
}

// SetAutoReply stores the responder on the server: a fixed interval when an
// end is given, otherwise permanent until switched off.
func (a *Account) SetAutoReply(ctx context.Context, r AutoReply) error {
	ar := proton.AutoResponder{
		IsEnabled: r.Enabled, Subject: r.Subject, Message: r.Message,
		Zone: LocalZone(), DaysSelected: []int{}, Repeat: 4,
	}
	if !r.End.IsZero() {
		start := r.Start
		if start.IsZero() {
			start = time.Now()
		}
		ar.Repeat, ar.StartTime, ar.EndTime = 0, start.Unix(), r.End.Unix()
	}
	if _, err := a.client.SetAutoResponder(ctx, ar); err != nil {
		return apiErr(i18n.T("saving the automatic reply failed"), err)
	}
	return nil
}

// LocalZone is the IANA name of the system time zone, e.g. "Europe/Prague".
func LocalZone() string {
	if tz := os.Getenv("TZ"); tz != "" && !strings.HasPrefix(tz, ":") {
		return tz
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if i := strings.Index(p, "zoneinfo/"); i >= 0 {
			return p[i+len("zoneinfo/"):]
		}
	}
	return "Europe/Prague"
}
