package proton

// klient patch: endpoints the upstream library does not wrap. Paths and
// payloads follow ProtonMail/WebClients (packages/shared/lib/api).

import (
	"context"

	"github.com/go-resty/resty/v2"
)

// SnoozedLabel is the system label of snoozed conversations.
const SnoozedLabel = "16"

// AutoResponder is the server-side vacation reply.
type AutoResponder struct {
	StartTime    int64
	EndTime      int64
	Repeat       int // 0 fixed interval, 4 permanent
	DaysSelected []int
	Subject      string
	Message      string
	IsEnabled    bool
	Zone         string
}

// SnoozeConversations hides conversations from the inbox until snoozeTime
// (unix seconds); the server returns them to the inbox on its own.
func (c *Client) SnoozeConversations(ctx context.Context, ids []string, snoozeTime int64) error {
	return c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(struct {
			IDs        []string
			SnoozeTime int64
		}{ids, snoozeTime}).Put("/mail/v4/conversations/snooze")
	})
}

func (c *Client) UnsnoozeConversations(ctx context.Context, ids []string) error {
	return c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(struct{ IDs []string }{ids}).Put("/mail/v4/conversations/unsnooze")
	})
}

// CancelSend turns a scheduled (or delayed) message back into a draft.
func (c *Client) CancelSend(ctx context.Context, messageID string) error {
	return c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.Post("/mail/v4/messages/" + messageID + "/cancel_send")
	})
}

func (c *Client) SetAutoResponder(ctx context.Context, ar AutoResponder) (MailSettings, error) {
	var res struct {
		MailSettings MailSettings
	}

	if err := c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(struct{ AutoResponder AutoResponder }{ar}).SetResult(&res).Put("/mail/v4/settings/autoresponder")
	}); err != nil {
		return MailSettings{}, err
	}

	return res.MailSettings, nil
}
