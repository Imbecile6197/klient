package protonmail

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ProtonMail/go-proton-api"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// labelColors are Proton's own label colours; new folders get one by turns.
var labelColors = []string{"#8080FF", "#DB60D6", "#415DF0", "#1DA583", "#F9B04A", "#E85E5E", "#5EC7B7", "#A839A4"}

// CreateFolder creates a folder, or with label a label.
func (a *Account) CreateFolder(ctx context.Context, name string, label bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New(i18n.T("enter a name"))
	}
	typ := proton.LabelTypeFolder
	if label {
		typ = proton.LabelTypeLabel
	}
	existing, _ := a.UserLabels(ctx)
	_, err := a.client.CreateLabel(ctx, proton.CreateLabelReq{
		Name: name, Color: labelColors[len(existing)%len(labelColors)], Type: typ,
	})
	return err
}

// RenameFolder renames a folder or label, keeping its colour.
func (a *Account) RenameFolder(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New(i18n.T("enter a name"))
	}
	labels, err := a.client.GetLabels(ctx, proton.LabelTypeFolder, proton.LabelTypeLabel)
	if err != nil {
		return err
	}
	for _, l := range labels {
		if l.ID == id {
			_, err := a.client.UpdateLabel(ctx, id, proton.UpdateLabelReq{Name: name, Color: l.Color, ParentID: l.ParentID})
			return err
		}
	}
	return errors.New(i18n.T("system folders cannot be changed"))
}

// DeleteFolder deletes a folder (its messages go back to the inbox, as on
// the Proton web) or a label.
func (a *Account) DeleteFolder(ctx context.Context, id string) error {
	return a.client.DeleteLabel(ctx, id)
}

// EmptyFolder permanently deletes the messages of Trash or Spam; with a
// non-zero olderThan only those older than it. It returns how many.
func (a *Account) EmptyFolder(ctx context.Context, folderID string, olderThan time.Time) (int, error) {
	if folderID != TrashID && folderID != SpamID {
		return 0, errors.New(i18n.T("only Trash and Spam can be emptied"))
	}
	var ids []string
	// Newest first: with olderThan the rest of the folder is older still.
	for page := 0; page < 100; page++ {
		msgs, err := a.client.GetMessageMetadataPage(ctx, page, 150, proton.MessageFilter{LabelID: folderID, Desc: true})
		if err != nil {
			return 0, err
		}
		for _, m := range msgs {
			if olderThan.IsZero() || time.Unix(m.Time, 0).Before(olderThan) {
				ids = append(ids, m.ID)
			}
		}
		if len(msgs) < 150 {
			break
		}
	}
	for i := 0; i < len(ids); i += 150 {
		end := min(i+150, len(ids))
		if err := a.client.DeleteMessage(ctx, ids[i:end]...); err != nil {
			return i, err
		}
	}
	return len(ids), nil
}
