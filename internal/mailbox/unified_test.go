package mailbox

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/Imbecile6197/klient/internal/protonmail"
)

// fakeAccount implements the calls the combined view makes; the embedded
// interface makes any other call panic.
type fakeAccount struct {
	Account
	id     string
	msgs   []protonmail.Summary // newest first
	moved  map[string][]string  // folder -> IDs
	read   []string
	counts map[string]int
}

func (f *fakeAccount) UserID() string           { return f.id }
func (f *fakeAccount) HasFolder(id string) bool { return id != protonmail.StarredID || f.id == "a" }
func (f *fakeAccount) List(_ context.Context, folder string, page, size int) ([]protonmail.Summary, error) {
	if page > 0 {
		return nil, nil
	}
	return f.msgs, nil
}
func (f *fakeAccount) Get(_ context.Context, id string) (*protonmail.Message, error) {
	return &protonmail.Message{Meta: protonmail.Summary{ID: id, ConversationID: "c-" + id},
		Attachments: []protonmail.Attachment{{ID: "att-" + id}}}, nil
}
func (f *fakeAccount) AttachmentData(_ context.Context, att protonmail.Attachment) ([]byte, error) {
	return []byte(f.id + ":" + att.ID), nil
}
func (f *fakeAccount) Move(_ context.Context, folder string, ids ...string) error {
	if f.moved == nil {
		f.moved = map[string][]string{}
	}
	f.moved[folder] = append(f.moved[folder], ids...)
	return nil
}
func (f *fakeAccount) MarkRead(_ context.Context, ids ...string) error {
	f.read = append(f.read, ids...)
	return nil
}
func (f *fakeAccount) UnreadCounts(context.Context) (map[string]int, error) { return f.counts, nil }

func sum(id string, t int64) protonmail.Summary {
	s := protonmail.Summary{}
	s.ID, s.ConversationID, s.Time = id, "c-"+id, t
	return s
}

func TestUnified(t *testing.T) {
	ctx := context.Background()
	// Both accounts use the same IDs: the combined view must keep them apart.
	a := &fakeAccount{id: "a", msgs: []protonmail.Summary{sum("1", 300), sum("2", 100)}, counts: map[string]int{protonmail.InboxID: 2}}
	b := &fakeAccount{id: "b", msgs: []protonmail.Summary{sum("1", 200)}, counts: map[string]int{protonmail.InboxID: 3}}
	u := NewUnified(func() []Account { return []Account{a, b} })

	list, err := u.List(ctx, protonmail.InboxID, 0, 50)
	if err != nil || len(list) != 3 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
	if list[0].Time != 300 || list[1].Time != 200 || list[2].Time != 100 {
		t.Errorf("not newest first: %v %v %v", list[0].Time, list[1].Time, list[2].Time)
	}
	if list[0].ID == list[1].ID {
		t.Fatal("equal IDs of two accounts were not told apart")
	}
	if acc, id, ok := u.Resolve(list[1].ID); !ok || acc != b || id != "1" {
		t.Errorf("Resolve = %v %q %v", acc, id, ok)
	}
	if acc, id, _ := u.Resolve(list[0].ConversationID); acc != a || id != "c-1" {
		t.Errorf("conversation resolves to %v %q", acc, id)
	}

	// Actions reach the right account with its own IDs.
	if err := u.Move(ctx, protonmail.TrashID, list[0].ID, list[1].ID, list[2].ID); err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{"a": a.moved[protonmail.TrashID], "b": b.moved[protonmail.TrashID]}
	sort.Strings(got["a"])
	if !reflect.DeepEqual(got, map[string][]string{"a": {"1", "2"}, "b": {"1"}}) {
		t.Errorf("moved %v", got)
	}
	_ = u.MarkRead(ctx, list[1].ID)
	if len(a.read) != 0 || !reflect.DeepEqual(b.read, []string{"1"}) {
		t.Errorf("read a=%v b=%v", a.read, b.read)
	}

	// A message and its attachments keep pointing at their account.
	msg, err := u.Get(ctx, list[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := u.AttachmentData(ctx, msg.Attachments[0])
	if err != nil || string(data) != "b:att-1" {
		t.Errorf("attachment = %q, %v", data, err)
	}
	owner, real, err := u.UnwrapMessage(msg)
	if err != nil || owner != b || real.Meta.ID != "1" || real.Attachments[0].ID != "att-1" {
		t.Errorf("UnwrapMessage = %v %+v %v", owner, real.Meta.ID, err)
	}

	counts, _ := u.UnreadCounts(ctx)
	if counts[protonmail.InboxID] != 5 {
		t.Errorf("unread inbox = %d", counts[protonmail.InboxID])
	}
	if _, _, ok := u.Resolve("plain-id"); ok {
		t.Error("an ID without an account resolved")
	}
	if u.HasFolder(protonmail.DraftsID) || !u.HasFolder(protonmail.InboxID) {
		t.Error("unexpected folders")
	}
}
