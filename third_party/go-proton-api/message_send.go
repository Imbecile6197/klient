package proton

import (
	"context"
	"fmt"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/go-resty/resty/v2"
)

func (c *Client) CreateDraft(ctx context.Context, addrKR *crypto.KeyRing, req CreateDraftReq) (Message, error) {
	var res struct {
		Message Message
	}

	kr, err := addrKR.FirstKey()
	if err != nil {
		return Message{}, fmt.Errorf("failed to get first key: %w", err)
	}

	enc, err := kr.Encrypt(crypto.NewPlainMessageFromString(req.Message.Body), nil)
	if err != nil {
		return Message{}, fmt.Errorf("failed to encrypt draft: %w", err)
	}

	arm, err := enc.GetArmored()
	if err != nil {
		return Message{}, fmt.Errorf("failed to armor draft: %w", err)
	}

	req.Message.Body = arm

	if err := c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(req).SetResult(&res).Post("/mail/v4/messages")
	}); err != nil {
		return Message{}, err
	}

	return res.Message, nil
}

func (c *Client) UpdateDraft(ctx context.Context, draftID string, addrKR *crypto.KeyRing, req UpdateDraftReq) (Message, error) {
	var res struct {
		Message Message
	}

	// klient patch: always encrypt, like CreateDraft. An empty body used to be
	// sent as "" and the API rejected it ("Body is required"), which made
	// messages with only an attachment impossible to save or send.
	{
		kr, err := addrKR.FirstKey()
		if err != nil {
			return Message{}, fmt.Errorf("failed to get first key: %w", err)
		}

		enc, err := kr.Encrypt(crypto.NewPlainMessageFromString(req.Message.Body), nil)
		if err != nil {
			return Message{}, fmt.Errorf("failed to encrypt draft: %w", err)
		}

		arm, err := enc.GetArmored()
		if err != nil {
			return Message{}, fmt.Errorf("failed to armor draft: %w", err)
		}

		req.Message.Body = arm
	}

	if err := c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(req).SetResult(&res).Put("/mail/v4/messages/" + draftID)
	}); err != nil {
		return Message{}, err
	}

	return res.Message, nil
}

func (c *Client) SendDraft(ctx context.Context, draftID string, req SendDraftReq) (Message, error) {
	var res struct {
		Sent Message
	}

	if err := c.do(ctx, func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(req).SetResult(&res).Post("/mail/v4/messages/" + draftID)
	}); err != nil {
		return Message{}, err
	}

	return res.Sent, nil
}
