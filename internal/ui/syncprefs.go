package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/shared"
	"github.com/Imbecile6197/klient/internal/syncstate"
)

// refreshSyncPrefs updates an open sync page (status, computers). UI thread.
func (a *App) refreshSyncPrefs() {
	for _, f := range a.syncPrefs {
		f()
	}
}

// syncStatusText describes the state of the sync.
func (a *App) syncStatusText() string {
	h := a.hub()
	if h == nil {
		return i18n.T("Sync is off")
	}
	st := h.E.Status()
	var text string
	switch {
	case st.ReadOnly:
		text = i18n.T("A newer Klient changed the shared data – update Klient on this computer")
	case errors.Is(st.Err, errNoSyncAccount) || (st.Err != nil && protonmail.IsOffline(st.Err)):
		text = i18n.T("Waiting for the Proton account")
	case st.Err != nil:
		text = fmt.Sprintf(i18n.T("Sync failed: %s"), st.Err.Error())
	case st.LastSync.IsZero():
		text = i18n.T("Waiting for the Proton account")
	default:
		text = fmt.Sprintf(i18n.T("Last synced %s"), formatWhen(st.LastSync))
	}
	if st.Pending {
		text += i18n.T(" – changes are waiting to be sent")
	}
	return text
}

func (a *App) syncPage(d *adw.PreferencesDialog) *adw.PreferencesPage {
	p := adw.NewPreferencesPage()
	p.SetTitle(i18n.T("Sync"))
	p.SetIconName("emblem-synchronizing-symbolic")
	p.SetName("sync")

	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Sync Between Computers"))
	g.SetDescription(i18n.T("Klient on your other computers shares spam verdicts, sender lists, rules, signatures, AI settings and spending through your Proton account (a folder “Klient”, encrypted by Proton). API keys and passwords stay on each computer."))
	on := adw.NewSwitchRow()
	on.SetUseMarkup(false) // names and addresses may contain & or <
	on.SetTitle(i18n.T("Sync this computer"))
	on.SetActive(a.cfg.SyncEnabled)
	if !a.hasProton() && !a.cfg.SyncEnabled {
		on.SetSubtitle(i18n.T("Needs a Proton account – the shared data is kept there"))
		on.SetSensitive(false)
	}
	g.Add(on)
	name := adw.NewEntryRow()
	name.SetUseMarkup(false)
	name.SetTitle(i18n.T("Name of this computer"))
	name.SetText(a.cfg.SyncName)
	if name.Text() == "" {
		name.SetText(defaultComputerName())
	}
	name.SetShowApplyButton(true)
	name.ConnectApply(func() {
		a.cfg.SyncName = strings.TrimSpace(name.Text())
		if a.cfg.SyncName == "" {
			a.cfg.SyncName = defaultComputerName()
		}
		a.saveConfig()
		a.kickSync()
	})
	g.Add(name)
	status := adw.NewActionRow()
	status.SetUseMarkup(false)
	status.SetTitle(i18n.T("Sync status"))
	now := gtk.NewButtonWithLabel(i18n.T("Sync Now"))
	now.SetVAlign(gtk.AlignCenter)
	now.ConnectClicked(func() {
		a.kickSync()
		d.AddToast(newToast(i18n.T("Syncing…")))
	})
	status.AddSuffix(now)
	g.Add(status)
	p.Add(g)

	cg := adw.NewPreferencesGroup()
	cg.SetTitle(i18n.T("Your Computers"))
	cg.SetDescription(i18n.T("The main computer does the AI work: the spam filter, the morning overview and summaries. The others wait for its results and do the work themselves only when it is switched off."))
	p.Add(cg)
	var rows []gtk.Widgetter

	rg := adw.NewPreferencesGroup()
	restore := adw.NewButtonRow()
	restore.SetTitle(i18n.T("Restore an Earlier Version…"))
	restore.ConnectActivated(func() { a.chooseSyncVersion(d) })
	rg.Add(restore)
	p.Add(rg)

	refresh := func() {
		h := a.hub()
		status.SetSubtitle(a.syncStatusText())
		now.SetSensitive(h != nil)
		name.SetSensitive(a.cfg.SyncEnabled)
		cg.SetVisible(h != nil)
		rg.SetVisible(h != nil)
		for _, r := range rows {
			cg.Remove(r)
		}
		rows = nil
		if h == nil {
			return
		}
		main := h.Main()
		comps := h.Computers()
		for _, c := range comps {
			c := c
			r := adw.NewActionRow()
			r.SetUseMarkup(false)
			title := c.Name
			if c.ID == h.Node {
				title = fmt.Sprintf(i18n.T("%s (this computer)"), c.Name)
			}
			r.SetTitle(title)
			var sub []string
			if c.ID == main {
				sub = append(sub, i18n.T("main computer"))
			}
			sub = append(sub, fmt.Sprintf(i18n.T("last seen %s"), formatWhen(time.Unix(c.Seen, 0))))
			r.SetSubtitle(strings.Join(sub, " · "))
			if c.ID != main {
				b := gtk.NewButtonWithLabel(i18n.T("Make Main"))
				b.SetVAlign(gtk.AlignCenter)
				b.ConnectClicked(func() {
					h.SetMain(c.ID)
					a.kickSync()
					a.refreshSyncPrefs()
				})
				r.AddSuffix(b)
			}
			if c.ID != h.Node && c.ID != main && time.Since(time.Unix(c.Seen, 0)) > shared.AliveFor {
				b := gtk.NewButtonFromIconName("user-trash-symbolic")
				b.SetTooltipText(i18n.T("Remove Computer"))
				b.SetVAlign(gtk.AlignCenter)
				b.AddCSSClass("flat")
				b.ConnectClicked(func() {
					h.Forget(c.ID)
					a.kickSync()
					a.refreshSyncPrefs()
				})
				r.AddSuffix(b)
			}
			cg.Add(r)
			rows = append(rows, r)
		}
		if len(comps) < 2 {
			r := adw.NewActionRow()
			r.SetUseMarkup(false)
			r.SetTitle(i18n.T("No other computer yet – switch sync on in Klient on the other computer too"))
			r.AddCSSClass("dim-label")
			cg.Add(r)
			rows = append(rows, r)
		}
	}
	on.NotifyProperty("active", func() {
		if on.Active() == a.cfg.SyncEnabled {
			return
		}
		a.cfg.SyncEnabled = on.Active()
		a.saveConfig()
		if on.Active() {
			a.startSync()
		} else {
			a.stopSync()
		}
		refresh()
	})
	a.syncPrefs = append(a.syncPrefs, refresh)
	d.ConnectClosed(func() { a.syncPrefs = nil })
	refresh()
	return p
}

// chooseSyncVersion lists the stored versions; the chosen one comes back
// on all computers.
func (a *App) chooseSyncVersion(parent *adw.PreferencesDialog) {
	h := a.hub()
	if h == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()
		vs, err := h.E.Versions(ctx)
		ui(func() {
			if err != nil {
				parent.AddToast(newToast(fmt.Sprintf(i18n.T("Sync failed: %s"), err.Error())))
				return
			}
			if len(vs) < 2 {
				parent.AddToast(newToast(i18n.T("No earlier version is stored")))
				return
			}
			dlg := adw.NewDialog()
			dlg.SetTitle(i18n.T("Earlier Versions"))
			dlg.SetContentWidth(420)
			list := gtk.NewListBox()
			list.AddCSSClass("boxed-list")
			list.SetSelectionMode(gtk.SelectionNone)
			for _, v := range vs[1:] { // the first one is the current state
				v := v
				r := adw.NewActionRow()
				r.SetUseMarkup(false)
				r.SetTitle(formatWhen(v.Time))
				b := gtk.NewButtonWithLabel(i18n.T("Restore This Version"))
				b.SetVAlign(gtk.AlignCenter)
				b.ConnectClicked(func() {
					a.confirmSyncRestore(parent, dlg, h, v)
				})
				r.AddSuffix(b)
				list.Append(r)
			}
			box := gtk.NewBox(gtk.OrientationVertical, 12)
			box.SetMarginTop(12)
			box.SetMarginBottom(12)
			box.SetMarginStart(12)
			box.SetMarginEnd(12)
			box.Append(list)
			tv := adw.NewToolbarView()
			tv.AddTopBar(adw.NewHeaderBar())
			tv.SetContent(box)
			dlg.SetChild(tv)
			dlg.Present(a.win)
		})
	}()
}

func (a *App) confirmSyncRestore(parent *adw.PreferencesDialog, dlg *adw.Dialog, h *shared.Hub, v syncstate.Version) {
	ad := adw.NewAlertDialog(i18n.T("Restore This Version"), fmt.Sprintf(i18n.T("The shared data as it was %s comes back on all computers. Changes made since then are undone."), formatWhen(v.Time)))
	ad.AddResponse("cancel", i18n.T("Cancel"))
	ad.AddResponse("restore", i18n.T("Restore This Version"))
	ad.SetResponseAppearance("restore", adw.ResponseDestructive)
	ad.ConnectResponse(func(resp string) {
		if resp != "restore" {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
			defer cancel()
			err := h.E.Restore(ctx, v)
			ui(func() {
				if err != nil {
					parent.AddToast(newToast(fmt.Sprintf(i18n.T("Sync failed: %s"), err.Error())))
					return
				}
				dlg.Close()
				parent.AddToast(newToast(i18n.T("The earlier version was restored")))
				a.kickSync()
			})
		}()
	})
	ad.Present(a.win)
}

// aiComputerGroup chooses the computer doing the AI work (only with sync on
// and another computer known).
func (a *App) aiComputerGroup() *adw.PreferencesGroup {
	h := a.hub()
	if h == nil {
		return nil
	}
	comps := h.Computers()
	if len(comps) < 2 {
		return nil
	}
	g := adw.NewPreferencesGroup()
	g.SetTitle(i18n.T("Computer for AI Work"))
	g.SetDescription(i18n.T("The spam filter, the morning overview and summaries run on this computer; the others wait for the results, so you do not pay twice."))
	combo := adw.NewComboRow()
	combo.SetUseMarkup(false)
	combo.SetTitle(i18n.T("AI tasks are done by"))
	var names []string
	sel := uint(0)
	main := h.Main()
	for i, c := range comps {
		n := c.Name
		if c.ID == h.Node {
			n = fmt.Sprintf(i18n.T("%s (this computer)"), c.Name)
		}
		names = append(names, n)
		if c.ID == main {
			sel = uint(i)
		}
	}
	combo.SetModel(gtk.NewStringList(names))
	combo.SetSelected(sel)
	combo.NotifyProperty("selected", func() {
		i := int(combo.Selected())
		if i < 0 || i >= len(comps) || comps[i].ID == h.Main() {
			return
		}
		h.SetMain(comps[i].ID)
		a.kickSync()
		a.refreshSyncPrefs()
	})
	g.Add(combo)
	return g
}
