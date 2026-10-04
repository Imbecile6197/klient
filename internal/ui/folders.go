package ui

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- Hidden folders ----------------------------------------------------------------

func (a *App) hiddenFolders() []string {
	if a.acc == nil {
		return nil
	}
	return a.cfg.HiddenFolders[a.acc.Username()]
}

func (a *App) setFolderHidden(id string, hidden bool) {
	if a.cfg.HiddenFolders == nil {
		a.cfg.HiddenFolders = map[string][]string{}
	}
	key := a.acc.Username()
	list := slices.DeleteFunc(slices.Clone(a.cfg.HiddenFolders[key]), func(x string) bool { return x == id })
	if hidden {
		list = append(list, id)
	}
	a.cfg.HiddenFolders[key] = list
	a.saveConfig()
}

// ---- Context menu of the sidebar folders -------------------------------------------

// folderMenu opens a menu for a folder row on right click or long press.
func (m *mainView) folderMenu(row gtk.Widgetter, f protonmail.Folder, hidden bool) {
	show := func(x, y float64) {
		pop := m.folderPopover(f, hidden)
		pop.SetParent(row)
		rect := gdk.NewRectangle(int(x), int(y), 1, 1)
		pop.SetPointingTo(&rect)
		pop.SetHasArrow(false)
		pop.ConnectClosed(func() { glibIdleUnparent(pop) })
		pop.Popup()
	}
	click := gtk.NewGestureClick()
	click.SetButton(gdk.BUTTON_SECONDARY)
	click.ConnectPressed(func(n int, x, y float64) { show(x, y) })
	gtk.BaseWidget(row).AddController(click)
	long := gtk.NewGestureLongPress()
	long.ConnectPressed(func(x, y float64) { show(x, y) })
	gtk.BaseWidget(row).AddController(long)
}

// glibIdleUnparent removes a closed popover once GTK is done with it.
func glibIdleUnparent(pop *gtk.Popover) {
	ui(func() { pop.Unparent() })
}

func (m *mainView) folderPopover(f protonmail.Folder, hidden bool) *gtk.Popover {
	pop := gtk.NewPopover()
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	item := func(label string, destructive bool, do func()) {
		b := gtk.NewButtonWithLabel(label)
		b.AddCSSClass("flat")
		if destructive {
			b.AddCSSClass("destructive-action")
		}
		if l, ok := b.Child().(*gtk.Label); ok {
			l.SetXAlign(0)
		}
		b.ConnectClicked(func() {
			pop.Popdown()
			do()
		})
		box.Append(b)
	}
	user := !isSystemFolder(f.ID)
	unified := m.a.isUnified()

	if f.ID == protonmail.TrashID || f.ID == protonmail.SpamID {
		label := i18n.T("Empty Trash…")
		if f.ID == protonmail.SpamID {
			label = i18n.T("Empty Spam…")
		}
		item(label, true, func() { m.confirmEmpty(f) })
	}
	if !unified {
		item(i18n.T("New Folder…"), false, func() { m.folderNameDialog(protonmail.Folder{}) })
	}
	if user && !unified {
		item(i18n.T("Rename…"), false, func() { m.folderNameDialog(f) })
		if hidden {
			item(i18n.T("Show in the Sidebar"), false, func() {
				m.a.setFolderHidden(f.ID, false)
				m.reloadFolders()
			})
		} else {
			item(i18n.T("Hide from the Sidebar"), false, func() {
				m.a.setFolderHidden(f.ID, true)
				m.a.toast(fmt.Sprintf(i18n.T("%s is hidden – right-click a folder and choose Show Hidden Folders to get it back"), f.Name))
				m.reloadFolders()
			})
		}
		item(i18n.T("Delete…"), true, func() { m.confirmDeleteFolder(f) })
	}
	if n := len(m.a.hiddenFolders()); n > 0 && !unified {
		label := fmt.Sprintf(i18n.T("Show Hidden Folders (%d)"), n)
		if m.showHidden {
			label = i18n.T("Leave Hidden Folders Out")
		}
		item(label, false, func() {
			m.showHidden = !m.showHidden
			m.reloadFolders()
		})
	}
	pop.SetChild(box)
	return pop
}

func isSystemFolder(id string) bool {
	for _, f := range protonmail.Folders {
		if f.ID == id {
			return true
		}
	}
	return false
}

// folderNameDialog creates a folder (f.ID == "") or renames f.
func (m *mainView) folderNameDialog(f protonmail.Folder) {
	acc := m.a.acc
	rename := f.ID != ""
	title := i18n.T("New Folder")
	if rename {
		title = fmt.Sprintf(i18n.T("Rename %s"), f.Name)
	}
	d := adw.NewAlertDialog(title, i18n.T("A slash nests it in another folder, e.g. “Work/Projects”."))
	group := adw.NewPreferencesGroup()
	name := adw.NewEntryRow()
	name.SetTitle(i18n.C("folder", "Name"))
	name.SetText(f.Name)
	group.Add(name)
	label := adw.NewSwitchRow()
	label.SetTitle(i18n.T("Label instead of a folder"))
	label.SetSubtitle(i18n.T("A message can have several labels but be in only one folder"))
	label.SetVisible(!rename && acc.Caps().Labels && acc.Kind() == mailbox.KindProton)
	group.Add(label)
	d.SetExtraChild(group)
	d.AddResponse("cancel", i18n.T("Cancel"))
	ok := i18n.T("Create")
	if rename {
		ok = i18n.T("Rename")
	}
	d.AddResponse("ok", ok)
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetDefaultResponse("ok")
	d.SetCloseResponse("cancel")
	save := func() {
		text, isLabel := name.Text(), label.Active()
		go func() {
			ctx, cancel := context.WithTimeout(m.a.ctx, time.Minute)
			defer cancel()
			var err error
			if rename {
				err = acc.RenameFolder(ctx, f.ID, text)
			} else {
				err = acc.CreateFolder(ctx, text, isLabel)
			}
			ui(func() {
				if err != nil {
					m.a.toast(i18n.T("The folder could not be saved: ") + err.Error())
					return
				}
				if rename {
					m.a.toast(fmt.Sprintf(i18n.T("Renamed to %s"), text))
				} else {
					m.a.toast(fmt.Sprintf(i18n.T("Folder %s created"), text))
				}
				if m.a.mv == m {
					m.reloadFolders()
				}
			})
		}()
	}
	name.ConnectEntryActivated(func() {
		d.Close()
		save()
	})
	d.ConnectResponse(func(r string) {
		if r == "ok" {
			save()
		}
	})
	d.Present(m.a.win)
	name.GrabFocus()
}

func (m *mainView) confirmDeleteFolder(f protonmail.Folder) {
	acc := m.a.acc
	body := i18n.T("The folder and the messages in it will be deleted from the server.")
	switch {
	case acc.Kind() == mailbox.KindProton:
		body = i18n.T("Messages in the folder go back to the inbox; with a label, only the label is removed from the messages.")
	case acc.Kind() == mailbox.KindGmail:
		body = i18n.T("Only the label is removed; the messages stay in All Mail.")
	}
	d := adw.NewAlertDialog(fmt.Sprintf(i18n.T("Delete %s?"), f.Name), body)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("delete", i18n.T("Delete"))
	d.SetResponseAppearance("delete", adw.ResponseDestructive)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "delete" {
			return
		}
		go func() {
			err := acc.DeleteFolder(m.a.ctx, f.ID)
			ui(func() {
				if err != nil {
					m.a.toast(i18n.T("The folder could not be deleted: ") + err.Error())
					return
				}
				m.a.setFolderHidden(f.ID, false)
				m.a.toast(fmt.Sprintf(i18n.T("%s deleted"), f.Name))
				if m.a.mv == m {
					if m.folder.ID == f.ID {
						m.selectFolder(0)
					}
					m.reloadFolders()
				}
			})
		}()
	})
	d.Present(m.a.win)
}

// confirmEmpty permanently deletes everything in Trash or Spam.
func (m *mainView) confirmEmpty(f protonmail.Folder) {
	acc := m.a.acc
	title := i18n.T("Empty the Trash?")
	if f.ID == protonmail.SpamID {
		title = i18n.T("Empty Spam?")
	}
	body := i18n.T("All messages in the folder will be deleted permanently. This cannot be undone.")
	if m.a.isUnified() {
		body = i18n.T("All messages in the folder will be deleted permanently in every account. This cannot be undone.")
	}
	d := adw.NewAlertDialog(title, body)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("empty", i18n.T("Empty"))
	d.SetResponseAppearance("empty", adw.ResponseDestructive)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "empty" {
			return
		}
		// A toast that stays until the end and shows how far it got.
		busy := i18n.T("Emptying the Trash…")
		if f.ID == protonmail.SpamID {
			busy = i18n.T("Emptying Spam…")
		}
		progressToast := adw.NewToast(busy)
		progressToast.SetTimeout(0)
		m.a.toasts.AddToast(progressToast)
		go func() {
			ctx, cancel := context.WithTimeout(m.a.ctx, 10*time.Minute)
			defer cancel()
			n, err := acc.EmptyFolder(ctx, f.ID, time.Time{}, func(done, total int) {
				ui(func() {
					if total > 0 {
						progressToast.SetTitle(busy + " " + fmt.Sprintf(i18n.T("%d of %d"), done, total))
					}
				})
			})
			ui(func() {
				progressToast.Dismiss()
				if err != nil {
					m.a.toast(i18n.T("Emptying failed: ") + err.Error())
				} else {
					m.a.toast(fmt.Sprintf(i18n.N("%d message deleted permanently", "%d messages deleted permanently", n), n))
				}
				if m.a.mv == m {
					if m.folder.ID == f.ID {
						m.clearReader()
					}
					m.scheduleRefresh()
				}
			})
		}()
	})
	d.Present(m.a.win)
}

// ---- Automatic emptying -------------------------------------------------------------

// autoEmptyLoop deletes messages older than Config.AutoEmptyDays from Trash
// and Spam of every account, a few minutes after the start and then twice a
// day.
func (a *App) autoEmptyLoop() {
	wait := 3 * time.Minute
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 12 * time.Hour
		var days int
		var accs []mailbox.Account
		done := make(chan struct{})
		ui(func() {
			days, accs = a.cfg.AutoEmptyDays, a.openAccounts()
			close(done)
		})
		<-done
		if days <= 0 {
			continue
		}
		before := time.Now().AddDate(0, 0, -days)
		for _, acc := range accs {
			for _, f := range []string{protonmail.TrashID, protonmail.SpamID} {
				if !acc.HasFolder(f) {
					continue
				}
				n, err := acc.EmptyFolder(a.ctx, f, before, nil)
				switch {
				case err != nil:
					log.Printf("auto-empty %s of %s: %v", f, acc.Email(), err)
				case n > 0:
					log.Printf("auto-empty: %d messages older than %d days deleted from %s of %s", n, days, protonmail.FolderByID(f).Name, acc.Email())
				}
			}
		}
	}
}
