package ui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- Account switcher ----------------------------------------------------------

// accountSwitcher is the sidebar title: name and address of the shown
// account; with a click it lists all accounts and "Add Account".
func (m *mainView) accountSwitcher() gtk.Widgetter {
	sub := m.a.acc.Email()
	if m.a.isUnified() {
		n := len(m.a.sessions)
		sub = fmt.Sprintf(i18n.N("%d account", "%d accounts", n), n)
	}
	title := adw.NewWindowTitle(m.a.acc.DisplayName(), sub)
	box := gtk.NewBox(gtk.OrientationHorizontal, 6)
	box.Append(providerLogo(m.a.acc.Kind(), 20))
	box.Append(title)
	box.Append(gtk.NewImageFromIconName("pan-down-symbolic"))
	m.accountBtn = gtk.NewMenuButton()
	m.accountBtn.SetChild(box)
	m.accountBtn.AddCSSClass("flat")
	m.accountBtn.SetTooltipText(i18n.T("Accounts"))
	m.accountBtn.SetCreatePopupFunc(func(b *gtk.MenuButton) { b.SetPopover(m.accountPopover()) })
	m.updateAccountBadges()
	return m.accountBtn
}

func (m *mainView) accountPopover() *gtk.Popover {
	pop := gtk.NewPopover()
	box := gtk.NewBox(gtk.OrientationVertical, 2)
	box.SetSizeRequest(280, -1)
	if len(m.a.sessions) > 1 {
		box.Append(m.unifiedButton(pop))
	}
	for _, s := range m.a.sessions {
		s := s
		b := gtk.NewButton()
		b.AddCSSClass("flat")
		row := gtk.NewBox(gtk.OrientationHorizontal, 10)
		// Avatar with the service logo in its corner.
		av := gtk.NewOverlay()
		av.SetChild(adw.NewAvatar(32, s.acc.DisplayName(), true))
		logo := providerLogo(s.acc.Kind(), 16)
		logo.SetHAlign(gtk.AlignEnd)
		logo.SetVAlign(gtk.AlignEnd)
		logo.AddCSSClass("provider-badge")
		av.AddOverlay(logo)
		row.Append(av)
		texts := gtk.NewBox(gtk.OrientationVertical, 0)
		name := gtk.NewLabel(s.acc.DisplayName())
		name.SetXAlign(0)
		name.AddCSSClass("heading")
		name.SetEllipsize(pango.EllipsizeEnd)
		mail := gtk.NewLabel(s.acc.Email() + " · " + providerName(s.acc.Kind()))
		mail.SetXAlign(0)
		mail.AddCSSClass("dim-label")
		mail.AddCSSClass("caption")
		mail.SetEllipsize(pango.EllipsizeEnd)
		texts.Append(name)
		texts.Append(mail)
		texts.SetHExpand(true)
		row.Append(texts)
		if s.unread > 0 {
			c := gtk.NewLabel(fmt.Sprint(s.unread))
			c.AddCSSClass("count-badge")
			c.SetVAlign(gtk.AlignCenter)
			row.Append(c)
		}
		if s.acc == m.a.acc {
			row.Append(gtk.NewImageFromIconName("object-select-symbolic"))
		}
		b.SetChild(row)
		b.ConnectClicked(func() {
			pop.Popdown()
			m.a.restoreUnified = false
			if s.acc != m.a.acc {
				m.a.switchAccount(s.acc)
			}
		})
		box.Append(b)
	}
	box.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	add := gtk.NewButton()
	add.AddCSSClass("flat")
	ac := adw.NewButtonContent()
	ac.SetIconName("list-add-symbolic")
	ac.SetLabel(i18n.T("Add Account…"))
	ac.SetHAlign(gtk.AlignStart)
	add.SetChild(ac)
	add.ConnectClicked(func() {
		pop.Popdown()
		m.a.showAddAccount()
	})
	box.Append(add)
	pop.SetChild(box)
	return pop
}

// updateAccountBadges marks the switcher when another account has unread mail.
func (m *mainView) updateAccountBadges() {
	if m.accountBtn == nil {
		return
	}
	others := 0
	for _, s := range m.a.sessions {
		if !m.a.showing(s.acc) {
			others += s.unread
		}
	}
	if others > 0 {
		m.accountBtn.AddCSSClass("accounts-unread")
		m.accountBtn.SetTooltipText(fmt.Sprintf(i18n.T("Accounts – %s in the other accounts"), unreadText(others)))
	} else {
		m.accountBtn.RemoveCSSClass("accounts-unread")
		m.accountBtn.SetTooltipText(i18n.T("Accounts"))
	}
}

// ---- Drag and drop ---------------------------------------------------------------

const dragPrefix = "klient-messages:"

// makeDraggable lets a list row be dragged onto a folder in the sidebar. With
// a selection, all selected conversations move.
func (m *mainView) makeDraggable(w gtk.Widgetter, t protonmail.Thread) {
	src := gtk.NewDragSource()
	src.SetActions(gdk.ActionMove)
	src.ConnectPrepare(func(x, y float64) *gdk.ContentProvider {
		ids := t.IDs()
		if m.hasSelection() {
			ids = idsOf(m.targets())
		}
		return gdk.NewContentProviderForValue(coreglib.NewValue(dragPrefix + strings.Join(ids, ",")))
	})
	gtk.BaseWidget(w).AddController(src)
}

// folderDropTarget accepts dragged messages on the sidebar folders (labels
// are added, folders move).
func (m *mainView) folderDropTarget() {
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionMove)
	drop.ConnectMotion(func(x, y float64) gdk.DragAction {
		if row := m.folderList.RowAtY(int(y)); row != nil {
			m.folderList.DragHighlightRow(row)
			return gdk.ActionMove
		}
		m.folderList.DragUnhighlightRow()
		return 0
	})
	drop.ConnectLeave(func() { m.folderList.DragUnhighlightRow() })
	drop.ConnectDrop(func(v *coreglib.Value, x, y float64) bool {
		m.folderList.DragUnhighlightRow()
		s, ok := v.GoValue().(string)
		if !ok || !strings.HasPrefix(s, dragPrefix) {
			return false
		}
		row := m.folderList.RowAtY(int(y))
		if row == nil || row.Index() < 0 || row.Index() >= len(m.folders) {
			return false
		}
		f := m.folders[row.Index()]
		ids := strings.Split(strings.TrimPrefix(s, dragPrefix), ",")
		m.dropOn(f, ids)
		return true
	})
	m.folderList.AddController(drop)
}

func (m *mainView) dropOn(f protonmail.Folder, ids []string) {
	if f.ID == m.folder.ID || len(ids) == 0 {
		return
	}
	switch f.ID {
	case protonmail.AllMailID, protonmail.DraftsID, protonmail.SentID, protonmail.ScheduledID, protonmail.SnoozedID:
		m.a.toast(fmt.Sprintf(i18n.T("Messages cannot be moved to the folder %s"), f.Name))
		return
	}
	isLabel := f.Icon == "label-dot"
	acc := m.a.acc
	m.leaveSelection()
	if !isLabel {
		m.clearReader()
	}
	go func() {
		var err error
		switch {
		case f.ID == protonmail.StarredID || isLabel:
			err = acc.SetLabel(m.a.ctx, f.ID, true, ids...)
		default:
			err = acc.Move(m.a.ctx, f.ID, ids...)
		}
		ui(func() {
			if err != nil {
				m.a.toast(i18n.T("Moving failed: ") + err.Error())
				return
			}
			if isLabel || f.ID == protonmail.StarredID {
				m.a.toast(fmt.Sprintf(i18n.T("Added: %s"), f.Name))
			} else {
				m.a.toast(fmt.Sprintf(i18n.T("Moved to the folder %s"), f.Name))
			}
			m.scheduleRefresh()
		})
	}()
}
