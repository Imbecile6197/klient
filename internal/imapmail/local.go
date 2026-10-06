package imapmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- Threads -----------------------------------------------------------------------

var msgIDRe = regexp.MustCompile(`<[^<>\s]+>`)

// headerValue extracts one (possibly folded) header from a header block.
func headerValue(block, name string) string {
	var out []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n") {
		if in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			out = append(out, strings.TrimSpace(line))
			continue
		}
		in = false
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), name) {
			in = true
			out = append(out, strings.TrimSpace(v))
		}
	}
	return strings.Join(out, " ")
}

// conversationID groups a message with its replies: the thread's root is the
// first ID in References, else the message it answers, else itself.
func conversationID(references string, inReplyTo []string, messageID string) string {
	root := ""
	if ids := msgIDRe.FindAllString(references, -1); len(ids) > 0 {
		root = ids[0]
	} else if len(inReplyTo) > 0 {
		root = inReplyTo[0]
	} else {
		root = messageID
	}
	root = strings.Trim(strings.TrimSpace(root), "<>")
	if root == "" {
		return ""
	}
	return "t." + base64.RawURLEncoding.EncodeToString([]byte(root))
}

func rootOf(conversationID string) (string, bool) {
	if !strings.HasPrefix(conversationID, "t.") {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(conversationID[2:])
	return string(b), err == nil && len(b) > 0
}

// ThreadMessages finds all messages of a conversation in the usual folders
// (and Trash/Spam when includeHidden), oldest first.
func (a *Account) ThreadMessages(ctx context.Context, conversationID string, includeHidden bool) ([]protonmail.Summary, error) {
	root, ok := rootOf(conversationID)
	if !ok {
		return nil, nil
	}
	folders := []string{protonmail.InboxID, protonmail.SentID, protonmail.ArchiveID, protonmail.SnoozedID}
	if includeHidden {
		folders = append(folders, protonmail.TrashID, protonmail.SpamID)
	}
	criteria := &imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{
		{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: root}}},
		{Or: [][2]imap.SearchCriteria{{
			{Header: []imap.SearchCriteriaHeaderField{{Key: "References", Value: root}}},
			{Header: []imap.SearchCriteriaHeaderField{{Key: "In-Reply-To", Value: root}}},
		}}},
	}}}
	var out []protonmail.Summary
	// One message can sit in several of these folders: Gmail keeps every
	// message also in All Mail (its archive), mail to yourself is in Inbox
	// and Sent. Show each once, the copy from the first folder.
	seen := map[string]bool{}
	err := a.with(func(c *imapclient.Client) error {
		for _, f := range folders {
			mb, err := a.mailboxOf(f)
			if err != nil {
				continue
			}
			sel, err := c.Select(mb, nil).Wait()
			if err != nil {
				continue
			}
			res, err := c.UIDSearch(criteria, nil).Wait()
			if err != nil {
				return err
			}
			found, err := a.fetchUIDs(c, mb, sel.UIDValidity, pageOf(res.AllUIDs(), 0, 150))
			if err != nil {
				return err
			}
			for _, s := range found {
				if s.ConversationID != conversationID {
					continue
				}
				if s.ExternalID != "" {
					if seen[s.ExternalID] {
						continue
					}
					seen[s.ExternalID] = true
				}
				out = append(out, s)
			}
		}
		return nil
	})
	if err != nil && IsOffline(err) && a.cache != nil {
		cached, cerr := a.cache.Conversation(conversationID)
		return uniqueMessages(cached), cerr
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out, err
}

// uniqueMessages keeps the first copy of each Message-ID.
func uniqueMessages(in []protonmail.Summary) []protonmail.Summary {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if s.ExternalID != "" {
			if seen[s.ExternalID] {
				continue
			}
			seen[s.ExternalID] = true
		}
		out = append(out, s)
	}
	return out
}

// ---- Local folders (snooze, scheduled) --------------------------------------------------

var localFolderNames = map[string]string{
	protonmail.SnoozedID:   i18n.T("Snoozed"),
	protonmail.ScheduledID: i18n.T("Scheduled"),
}

// ensureFolder creates the mailbox of a Klient folder on first use.
func (a *Account) ensureFolder(id string) (string, error) {
	var name string
	err := a.with(func(c *imapclient.Client) error {
		a.meta.RLock()
		mb, ok := a.roles[id]
		delim := a.delim
		a.meta.RUnlock()
		if ok {
			name = mb
			return nil
		}
		want := localFolderNames[id]
		err := c.Create(want, nil).Wait()
		if err != nil && delim != 0 {
			// Some servers keep all folders under INBOX.
			want = "INBOX" + string(delim) + want
			err = c.Create(want, nil).Wait()
		}
		if err != nil {
			return fmt.Errorf(i18n.T("cannot create folder %s on the server: %w"), localFolderNames[id], err)
		}
		if err := a.loadFolders(); err != nil {
			return err
		}
		a.meta.Lock()
		name = a.roles[id]
		if name == "" {
			a.roles[id], name = want, want
		}
		a.meta.Unlock()
		return nil
	})
	return name, err
}

type localState struct {
	Snoozed   map[string]int64 // conversation ID -> return time
	Scheduled map[string]int64 // message ID in the Scheduled folder -> send time
}

func (a *Account) loadLocal() localState {
	st := localState{Snoozed: map[string]int64{}, Scheduled: map[string]int64{}}
	if a.cache != nil {
		a.cache.Get("imap-local", &st)
	}
	if st.Snoozed == nil {
		st.Snoozed = map[string]int64{}
	}
	if st.Scheduled == nil {
		st.Scheduled = map[string]int64{}
	}
	return st
}

func (a *Account) saveLocal(st localState) error {
	if a.cache == nil {
		return errors.New(i18n.T("without the offline cache Klient cannot remember the time – turn it on in Preferences"))
	}
	return a.cache.Set("imap-local", st)
}

// inFolder returns the IDs of a conversation's messages in one folder.
func (a *Account) inFolder(ctx context.Context, conversationID, folderID string) []string {
	msgs, _ := a.ThreadMessages(ctx, conversationID, false)
	var ids []string
	for _, m := range msgs {
		for _, l := range m.LabelIDs {
			if l == folderID {
				ids = append(ids, m.ID)
			}
		}
	}
	return ids
}

// Snooze moves conversations from the inbox to i18n.T("Snoozed"); Klient moves
// them back at t (if it runs then, otherwise as soon as it starts).
func (a *Account) Snooze(ctx context.Context, conversationIDs []string, t time.Time) error {
	if _, err := a.ensureFolder(protonmail.SnoozedID); err != nil {
		return err
	}
	st := a.loadLocal()
	for _, conv := range conversationIDs {
		ids := a.inFolder(ctx, conv, protonmail.InboxID)
		if len(ids) == 0 {
			continue
		}
		if err := a.Move(ctx, protonmail.SnoozedID, ids...); err != nil {
			return err
		}
		st.Snoozed[conv] = t.Unix()
	}
	return a.saveLocal(st)
}

// Unsnooze returns conversations to the inbox as unread.
func (a *Account) Unsnooze(ctx context.Context, conversationIDs []string) error {
	st := a.loadLocal()
	for _, conv := range conversationIDs {
		if ids := a.inFolder(ctx, conv, protonmail.SnoozedID); len(ids) > 0 {
			if err := a.Move(ctx, protonmail.InboxID, ids...); err != nil {
				return err
			}
			// Moved messages get new IDs; mark the newest inbox copies unread.
			if back := a.inFolder(ctx, conv, protonmail.InboxID); len(back) > 0 {
				_ = a.MarkUnread(ctx, back...)
			}
		}
		delete(st.Snoozed, conv)
	}
	return a.saveLocal(st)
}

func (a *Account) SnoozedUntil(conversationID string) time.Time {
	if t := a.loadLocal().Snoozed[conversationID]; t > 0 {
		return time.Unix(t, 0)
	}
	return time.Time{}
}

// schedule stores the message in i18n.T("Scheduled"); Klient sends it at its time.
func (a *Account) schedule(ctx context.Context, d *protonmail.Draft) error {
	if _, err := a.ensureFolder(protonmail.ScheduledID); err != nil {
		return err
	}
	data, err := a.forSelf(d, a.parentMessageID(ctx, d))
	if err != nil {
		return err
	}
	id, err := a.appendTo(protonmail.ScheduledID, data, []imap.Flag{imap.FlagSeen})
	if err != nil {
		return fmt.Errorf(i18n.T("scheduling failed: %w"), err)
	}
	if id == "" {
		return errors.New(i18n.T("the server did not return the ID of the saved message (no UIDPLUS support), so it cannot be scheduled"))
	}
	st := a.loadLocal()
	st.Scheduled[id] = d.DeliveryTime.Unix()
	if err := a.saveLocal(st); err != nil {
		_ = a.deleteIDs(id)
		return err
	}
	if d.ID != "" {
		_ = a.deleteIDs(d.ID)
		d.ID = ""
	}
	return nil
}

// CancelScheduled moves a scheduled message back to Drafts.
func (a *Account) CancelScheduled(ctx context.Context, id string) error {
	st := a.loadLocal()
	delete(st.Scheduled, id)
	if err := a.saveLocal(st); err != nil {
		return err
	}
	if !a.HasFolder(protonmail.DraftsID) {
		return nil
	}
	if err := a.Move(ctx, protonmail.DraftsID, id); err != nil {
		return err
	}
	return nil
}

// localLoop returns snoozed mail and sends scheduled mail when due.
func (a *Account) localLoop(ctx context.Context, onChange func()) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		a.runDue(ctx, onChange)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (a *Account) runDue(ctx context.Context, onChange func()) {
	st := a.loadLocal()
	now := time.Now().Unix()
	changed := false
	for conv, t := range st.Snoozed {
		if t <= now {
			if err := a.Unsnooze(ctx, []string{conv}); err != nil {
				log.Printf("IMAP %s: returning a snoozed message failed: %v", a.set.Email, err)
				continue
			}
			changed = true
		}
	}
	st = a.loadLocal()
	for id, t := range st.Scheduled {
		if t > now {
			continue
		}
		d, _, err := a.OpenDraft(ctx, id)
		if err == nil {
			d.ID = id // Send deletes it from i18n.T("Scheduled") afterwards
			err = a.Send(ctx, d)
		}
		if err != nil {
			log.Printf("IMAP %s: sending a scheduled message failed: %v", a.set.Email, err)
			continue // retried on the next tick
		}
		st = a.loadLocal()
		delete(st.Scheduled, id)
		_ = a.saveLocal(st)
		changed = true
	}
	if changed && onChange != nil {
		onChange()
	}
}

// ---- Gmail labels -------------------------------------------------------------------

// gmailLabel adds or removes a Gmail label. Over IMAP a label is a mailbox:
// copying a message there adds the label (the message stays where it is),
// deleting it from there removes just that label.
func (a *Account) gmailLabel(ctx context.Context, labelID string, on bool, ids []string) error {
	label, err := a.mailboxOf(labelID)
	if err != nil {
		return err
	}
	if on {
		groups, err := groupIDs(ids)
		if err != nil {
			return err
		}
		return a.with(func(c *imapclient.Client) error {
			for mb, uids := range groups {
				if mb == label {
					continue
				}
				if _, err := c.Select(mb, nil).Wait(); err != nil {
					return err
				}
				if _, err := c.Copy(imap.UIDSetNum(uids...), label).Wait(); err != nil {
					return err
				}
			}
			return nil
		})
	}
	// Find the copies in the label mailbox by Message-ID.
	var msgIDs []string
	for _, id := range ids {
		if r, err := parseID(id); err == nil && r.mailbox == label {
			msgIDs = append(msgIDs, "") // already the label copy
			continue
		}
		if _, meta, err := a.raw(ctx, id); err == nil && meta.ExternalID != "" {
			msgIDs = append(msgIDs, meta.ExternalID)
		}
	}
	return a.with(func(c *imapclient.Client) error {
		if _, err := c.Select(label, nil).Wait(); err != nil {
			return err
		}
		var uids []imap.UID
		for _, id := range ids {
			if r, err := parseID(id); err == nil && r.mailbox == label {
				uids = append(uids, r.uid)
			}
		}
		for _, mid := range msgIDs {
			if mid == "" {
				continue
			}
			res, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
			if err != nil {
				return err
			}
			uids = append(uids, res.AllUIDs()...)
		}
		if len(uids) == 0 {
			return nil
		}
		return a.expungeUIDs(c, imap.UIDSetNum(uids...))
	})
}
