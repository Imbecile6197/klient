package imapmail

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- Folder management ---------------------------------------------------------

// boxName turns a folder name with "/" for nesting into a mailbox name with
// the server's delimiter.
func (a *Account) boxName(name string) (string, error) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" {
		return "", errors.New(i18n.T("enter a name"))
	}
	a.meta.RLock()
	delim := a.delim
	a.meta.RUnlock()
	if delim != 0 && delim != '/' {
		if strings.ContainsRune(name, delim) {
			return "", errors.New(i18n.T("the name contains a character the server does not allow"))
		}
		name = strings.ReplaceAll(name, "/", string(delim))
	}
	return name, nil
}

// userMailbox returns the mailbox of one of the user's own folders.
func (a *Account) userMailbox(id string) (string, error) {
	if !strings.HasPrefix(id, "f.") {
		return "", errors.New(i18n.T("system folders cannot be changed"))
	}
	return a.mailboxOf(id)
}

// CreateFolder creates a folder ("Work/Projects" nests it). label only
// matters for services with labels (Gmail labels are mailboxes too).
func (a *Account) CreateFolder(ctx context.Context, name string, label bool) error {
	mb, err := a.boxName(name)
	if err != nil {
		return err
	}
	return a.with(func(c *imapclient.Client) error {
		if err := c.Create(mb, nil).Wait(); err != nil {
			return err
		}
		return a.loadFolders()
	})
}

func (a *Account) RenameFolder(ctx context.Context, id, name string) error {
	old, err := a.userMailbox(id)
	if err != nil {
		return err
	}
	mb, err := a.boxName(name)
	if err != nil {
		return err
	}
	return a.with(func(c *imapclient.Client) error {
		if err := c.Rename(old, mb, nil).Wait(); err != nil {
			return err
		}
		return a.loadFolders()
	})
}

// DeleteFolder deletes a folder with the messages in it (a Gmail label only
// loses the label; the messages stay in All Mail).
func (a *Account) DeleteFolder(ctx context.Context, id string) error {
	mb, err := a.userMailbox(id)
	if err != nil {
		return err
	}
	return a.with(func(c *imapclient.Client) error {
		if err := c.Delete(mb).Wait(); err != nil {
			return err
		}
		return a.loadFolders()
	})
}

// EmptyFolder permanently deletes the messages of Trash or Spam; with a
// non-zero olderThan only those that arrived before it. It returns how many
// were deleted.
func (a *Account) EmptyFolder(ctx context.Context, folderID string, olderThan time.Time) (int, error) {
	if folderID != protonmail.TrashID && folderID != protonmail.SpamID {
		return 0, errors.New(i18n.T("only Trash and Spam can be emptied"))
	}
	a.meta.RLock()
	mb := a.roles[folderID]
	a.meta.RUnlock()
	if mb == "" {
		return 0, nil
	}
	n := 0
	err := a.with(func(c *imapclient.Client) error {
		if _, err := c.Select(mb, nil).Wait(); err != nil {
			return err
		}
		crit := &imap.SearchCriteria{}
		if !olderThan.IsZero() {
			crit.Before = olderThan
		}
		res, err := c.UIDSearch(crit, nil).Wait()
		if err != nil {
			return err
		}
		uids := res.AllUIDs()
		if len(uids) == 0 {
			return nil
		}
		n = len(uids)
		return a.expungeUIDs(c, imap.UIDSetNum(uids...))
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
