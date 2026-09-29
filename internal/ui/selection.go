package ui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/protonmail"
)

// Selection mode: checkboxes on the rows and an action bar for bulk actions,
// entered with the header button or Ctrl/Shift+click.

func (m *mainView) buildSelection(tv interface{ AddBottomBar(gtk.Widgetter) }, hb interface{ PackEnd(gtk.Widgetter) }) {
	m.selectBtn = gtk.NewToggleButton()
	m.selectBtn.SetIconName("object-select-symbolic")
	m.selectBtn.SetTooltipText("Vybrat více zpráv")
	m.selectBtn.ConnectToggled(func() {
		if m.selectBtn.Active() {
			m.enterSelection()
		} else {
			m.leaveSelection()
		}
	})
	hb.PackEnd(m.selectBtn)

	m.actionBar = gtk.NewActionBar()
	m.selLabel = gtk.NewLabel("")
	m.selLabel.AddCSSClass("caption-heading")
	all := gtk.NewButtonWithLabel("Vše")
	all.SetTooltipText("Vybrat vše")
	all.AddCSSClass("flat")
	all.ConnectClicked(m.selectAll)
	m.actionBar.PackStart(all)
	m.actionBar.SetCenterWidget(m.selLabel)

	btn := func(icon, tip string, f func()) *gtk.Button {
		b := gtk.NewButtonFromIconName(icon)
		b.SetTooltipText(tip)
		b.AddCSSClass("flat")
		b.ConnectClicked(f)
		m.bulkBtns = append(m.bulkBtns, b)
		return b
	}
	m.actionBar.PackEnd(btn("user-trash-symbolic", "Do koše", func() { m.moveCurrent(protonmail.TrashID, "Přesunuto do koše") }))
	m.actionBar.PackEnd(btn("mail-mark-junk-symbolic", "Spam / není spam", m.toggleSpam))
	move := gtk.NewMenuButton()
	move.SetIconName("folder-open-symbolic")
	move.SetTooltipText("Přesunout do složky")
	move.AddCSSClass("flat")
	m.bulkMove = move
	m.actionBar.PackEnd(move)
	m.actionBar.PackEnd(btn("folder-documents-symbolic", "Archivovat", func() { m.moveCurrent(protonmail.ArchiveID, "Archivováno") }))
	m.actionBar.PackEnd(btn("starred-symbolic", "Hvězdička", m.toggleStar))
	m.actionBar.PackEnd(btn("mail-unread-symbolic", "Označit jako nepřečtené", m.markUnread))
	m.actionBar.PackEnd(btn("mail-read-symbolic", "Označit jako přečtené", m.markReadSelected))
	m.actionBar.SetRevealed(false)
	tv.AddBottomBar(m.actionBar)

	// Ctrl+click toggles a row, Shift+click selects a range; both enter
	// selection mode from normal browsing.
	click := gtk.NewGestureClick()
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(n int, x, y float64) {
		state := click.CurrentEventState()
		ctrl := state&gdk.ControlMask != 0
		shift := state&gdk.ShiftMask != 0
		if !ctrl && !shift && !m.selecting {
			return
		}
		row := m.msgList.RowAtY(int(y))
		if row == nil {
			return
		}
		click.SetState(gtk.EventSequenceClaimed)
		if !m.selecting {
			m.selectBtn.SetActive(true)
			// Include the conversation open in the reader, as file managers do.
			if m.thread != nil {
				for i, t := range m.items {
					if t.Latest.ID == m.thread.item.Latest.ID {
						m.setChecked(i, true)
						m.lastToggled = i
					}
				}
			}
		}
		m.toggleRow(row.Index(), shift)
	})
	m.msgList.AddController(click)
}

func (m *mainView) enterSelection() {
	if m.selecting {
		return
	}
	m.selecting = true
	m.selected = map[int]bool{}
	for _, c := range m.checks {
		c.SetVisible(true)
		c.SetActive(false)
	}
	m.msgList.SetSelectionMode(gtk.SelectionNone)
	m.bulkMove.SetMenuModel(m.moveBtn.MenuModel())
	m.actionBar.SetRevealed(true)
	m.updateSelection()
}

func (m *mainView) leaveSelection() {
	if !m.selecting {
		return
	}
	m.selecting = false
	m.selected = map[int]bool{}
	for _, c := range m.checks {
		c.SetVisible(false)
		c.SetActive(false)
	}
	m.msgList.SetSelectionMode(gtk.SelectionBrowse)
	m.actionBar.SetRevealed(false)
	if m.selectBtn.Active() {
		m.selectBtn.SetActive(false)
	}
}

func (m *mainView) setChecked(i int, on bool) {
	if i < 0 || i >= len(m.checks) {
		return
	}
	if on {
		m.selected[i] = true
	} else {
		delete(m.selected, i)
	}
	m.checks[i].SetActive(on)
}

// toggleRow flips one row, or with shift selects everything between the
// last toggled row and this one.
func (m *mainView) toggleRow(i int, shift bool) {
	if shift && m.lastToggled >= 0 && m.lastToggled < len(m.items) {
		a, b := m.lastToggled, i
		if a > b {
			a, b = b, a
		}
		for j := a; j <= b; j++ {
			m.setChecked(j, true)
		}
	} else {
		m.setChecked(i, !m.selected[i])
	}
	m.lastToggled = i
	m.updateSelection()
}

func (m *mainView) selectAll() {
	all := len(m.selected) == len(m.items)
	for i := range m.items {
		m.setChecked(i, !all)
	}
	m.updateSelection()
}

func (m *mainView) updateSelection() {
	if m.selLabel == nil {
		return
	}
	n := len(m.selected)
	m.selLabel.SetText(fmt.Sprintf("Vybráno: %d", n))
	for _, b := range m.bulkBtns {
		b.SetSensitive(n > 0)
	}
	m.bulkMove.SetSensitive(n > 0)
}

func (m *mainView) hasSelection() bool { return m.selecting && len(m.selected) > 0 }

// targets are the conversations an action applies to: the selection in
// selection mode, otherwise the one open in the reader.
func (m *mainView) targets() []protonmail.Thread {
	if m.hasSelection() {
		var out []protonmail.Thread
		for i := range m.items {
			if m.selected[i] {
				out = append(out, m.items[i])
			}
		}
		return out
	}
	if m.thread != nil {
		return []protonmail.Thread{m.thread.item}
	}
	return nil
}

func idsOf(ts []protonmail.Thread) []string {
	var ids []string
	for _, t := range ts {
		ids = append(ids, t.IDs()...)
	}
	return ids
}

func (m *mainView) markReadSelected() {
	ids := idsOf(m.targets())
	if len(ids) == 0 {
		return
	}
	m.leaveSelection()
	go func() {
		err := m.a.acc.MarkRead(m.a.ctx, ids...)
		ui(func() {
			if err != nil {
				m.a.toast("Označení selhalo: " + err.Error())
			}
			m.scheduleRefresh()
		})
	}()
}
