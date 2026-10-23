package macbridge

import "encoding/json"

// Request is one JSON-RPC-style call from Swift.
type Request struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is returned to Swift.
type Response struct {
	ID     json.RawMessage `json:"id,omitempty"`
	OK     bool            `json:"ok"`
	Result any             `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Event is pushed to Swift on the event callback.
type Event struct {
	Type    string `json:"type"`
	Account string `json:"account,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

type accountInfo struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
}

type summaryDTO struct {
	ID             string   `json:"id"`
	Subject        string   `json:"subject"`
	Sender         string   `json:"sender"`
	SenderEmail    string   `json:"senderEmail"`
	Time           int64    `json:"time"`
	Unread         bool     `json:"unread"`
	Starred        bool     `json:"starred"`
	HasAtt         bool     `json:"hasAttachments"`
	ConversationID string   `json:"conversationId"`
	Labels         []string `json:"labels,omitempty"`
	// IsSpam / SpamCategory come from the local spam filter decision store
	// (same source as Linux junk icons), not from Proton labels.
	IsSpam       bool   `json:"isSpam,omitempty"`
	SpamCategory string `json:"spamCategory,omitempty"`
}

type messageDTO struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Sender  string `json:"sender"`
	To      string `json:"to"`
	Time    int64  `json:"time"`
	Unread  bool   `json:"unread"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}
