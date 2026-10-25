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
		if m.ConversationID != conversationID || IsSyncMessage(m) {
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
	kept, hidden := uniqueCopies(out)
	a.rememberCopies(hidden)
	return kept, nil
}

// uniqueCopies keeps one copy of each Message-ID: mail sent to several of
// the user's addresses (or forwarded in from elsewhere) is stored once per
// address. The copy in the inbox is kept; hidden maps it to the others.
func uniqueCopies(in []Summary) (kept []Summary, hidden map[string][]string) {
	groups := map[string][]int{} // Message-ID -> indexes in in
	var order []string
	for i, m := range in {
		k := m.ExternalID
		if k == "" {
			k = "\x00" + m.ID // no Message-ID: always its own
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	hidden = map[string][]string{}
	for _, k := range order {
		idx := groups[k]
		pick := idx[0]
		for _, i := range idx {
			if hasLabel(in[i], InboxID) {
				pick = i
				break
			}
		}
		m := in[pick]
		for _, i := range idx {
			if i == pick {
				continue
			}
			hidden[m.ID] = append(hidden[m.ID], in[i].ID)
			// An unread hidden copy makes the shown one unread, so opening
			// it marks both read.
			if bool(in[i].Unread) {
				m.Unread = true
			}
		}
		kept = append(kept, m)
	}
	return kept, hidden
}

// rememberCopies records hidden copies so that actions on the shown
// message (read, move, labels) apply to them too.
func (a *Account) rememberCopies(hidden map[string][]string) {
	if len(hidden) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.copies == nil {
		a.copies = map[string][]string{}
	}
	for id, others := range hidden {
		a.copies[id] = others
	}
}

// withCopies adds the hidden copies of the messages to ids.
func (a *Account) withCopies(ids []string) []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.copies) == 0 {
		return ids
	}
	out := append([]string{}, ids...)
	for _, id := range ids {
		out = append(out, a.copies[id]...)
	}
	return out
}

func hasLabel(m Summary, id string) bool {
	for _, l := range m.LabelIDs {
		if l == id {
			return true
		}
	}
	return false
}
