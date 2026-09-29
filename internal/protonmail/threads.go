package protonmail

import (
	"context"
	"sort"

	"github.com/ProtonMail/go-proton-api"
)

// Thread is one Proton conversation as shown in a folder list.
type Thread struct {
	ConversationID string
	Latest         Summary   // newest message of the thread in the list
	Messages       []Summary // messages of the thread present in the list, newest first
}

func (t Thread) Unread() bool {
	for _, m := range t.Messages {
		if bool(m.Unread) {
			return true
		}
	}
	return false
}

func (t Thread) Starred() bool {
	for _, m := range t.Messages {
		if m.Starred() {
			return true
		}
	}
	return false
}

func (t Thread) HasAttachments() bool {
	for _, m := range t.Messages {
		if m.NumAttachments > 0 {
			return true
		}
	}
	return false
}

// IDs returns the IDs of the thread's messages in the list.
func (t Thread) IDs() []string {
	ids := make([]string, len(t.Messages))
	for i, m := range t.Messages {
		ids[i] = m.ID
	}
	return ids
}

// GroupThreads groups a newest-first message list into conversations,
// keeping the order of each conversation's newest message.
func GroupThreads(msgs []Summary) []Thread {
	var out []Thread
	index := map[string]int{}
	for _, m := range msgs {
		key := m.ConversationID
		if key == "" {
			key = "msg:" + m.ID
		}
		if i, ok := index[key]; ok {
			out[i].Messages = append(out[i].Messages, m)
			continue
		}
		index[key] = len(out)
		out = append(out, Thread{ConversationID: m.ConversationID, Latest: m, Messages: []Summary{m}})
	}
	return out
}

// ThreadMessages returns all messages of a conversation across folders,
// oldest first. Messages in Trash/Spam are left out unless includeHidden
// (e.g. when the Trash folder itself is open), like the Proton web client.
func (a *Account) ThreadMessages(ctx context.Context, conversationID string, includeHidden bool) ([]Summary, error) {
	// One page is plenty for a conversation, and it bounds the download should
	// the server ever ignore the filter; the ID check below guards the result.
	msgs, err := a.client.GetMessageMetadataPage(ctx, 0, 150, proton.MessageFilter{ConversationID: conversationID, Desc: true})
	if err != nil && IsOffline(err) && a.cache != nil {
		msgs, err = a.cache.Conversation(conversationID)
	}
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, m := range msgs {
		if m.ConversationID != conversationID {
			continue
		}
		hidden := false
		for _, l := range m.LabelIDs {
			if l == TrashID || l == SpamID {
				hidden = true
			}
		}
		if hidden && !includeHidden {
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out, nil
}
