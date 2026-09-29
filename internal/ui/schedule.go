package ui

import (
	"fmt"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// ---- Time helpers -------------------------------------------------------------

var czDays = []string{"ne", "po", "út", "st", "čt", "pá", "so"}

// weekdayShort is the abbreviated day name in the interface language.
func weekdayShort(t time.Time) string {
	if i18n.Lang() == "cs" {
		return czDays[t.Weekday()]
	}
	return t.Format("Mon")
}

// dayDate is "Mon Oct 5, 2026" (in Czech "po 5. 10. 2026").
func dayDate(t time.Time) string {
	return weekdayShort(t) + " " + t.Format(i18n.T("Jan 2, 2006"))
}

// formatWhen is "today at 15:30", "tomorrow at 8:00" or "Mon Oct 5 at 8:00".
func formatWhen(t time.Time) string {
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	day := time.Date(y2, m2, d2, 0, 0, 0, 0, time.Local).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, time.Local))
	hm := fmt.Sprintf("%d:%02d", t.Hour(), t.Minute())
	switch days := int(day.Hours() / 24); {
	case days == 0:
		return fmt.Sprintf(i18n.T("today at %s"), hm)
	case days == 1:
		return fmt.Sprintf(i18n.T("tomorrow at %s"), hm)
	case y1 == y2:
		return fmt.Sprintf(i18n.T("%s at %s"), weekdayShort(t)+" "+t.Format(i18n.T("Jan 2")), hm)
	}
	return fmt.Sprintf(i18n.T("%s at %s"), dayDate(t), hm)
}

// countdown is the remaining time: "in 45 s", "in 12 min", "in 2 h 5 min", "in 3 days".
func countdown(t time.Time) string {
	d := time.Until(t)
	switch {
	case d <= 0:
		return i18n.T("right now")
	case d < time.Minute:
		return fmt.Sprintf(i18n.T("in %d s"), int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf(i18n.T("in %d min"), int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf(i18n.T("in %d h"), h)
		}
		return fmt.Sprintf(i18n.T("in %d h %d min"), h, m)
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf(i18n.N("in %d day", "in %d days", days), days)
}

func at(day time.Time, hour int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, 0, 0, 0, time.Local)
}

type preset struct {
	label string
	t     time.Time
}

func snoozePresets() []preset {
	now := time.Now()
	var out []preset
	if now.Hour() < 17 {
		out = append(out, preset{i18n.T("Later today"), now.Add(3 * time.Hour).Truncate(time.Hour)})
	} else {
		out = append(out, preset{i18n.T("This evening"), at(now, 20)})
		if now.Hour() >= 20 {
			out = out[:0]
		}
	}
	out = append(out, preset{i18n.T("Tomorrow morning"), at(now.AddDate(0, 0, 1), 8)})
	if wd := now.Weekday(); wd >= time.Monday && wd <= time.Thursday {
		out = append(out, preset{i18n.T("This weekend"), at(now.AddDate(0, 0, int(time.Saturday-wd)), 9)})
	}
	out = append(out, preset{i18n.T("Next week"), at(nextMonday(now), 8)})
	return out
}

func sendPresets() []preset {
	now := time.Now()
	out := []preset{
		{i18n.T("Tomorrow morning"), at(now.AddDate(0, 0, 1), 8)},
		{i18n.T("Tomorrow afternoon"), at(now.AddDate(0, 0, 1), 13)},
	}
	if now.Weekday() != time.Sunday {
		out = append(out, preset{i18n.T("Monday morning"), at(nextMonday(now), 8)})
	}
	return out
}

func nextMonday(t time.Time) time.Time {
	days := (8 - int(t.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	return t.AddDate(0, 0, days)
}

// presetPopover lists quick choices and "Custom date and time…" (a picker).
func (a *App) presetPopover(presets []preset, customTitle string, parent func() gtk.Widgetter, pick func(time.Time)) *gtk.Popover {
	pop := gtk.NewPopover()
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	add := func(title, sub string, f func()) {
		b := gtk.NewButton()
		row := gtk.NewBox(gtk.OrientationHorizontal, 12)
		l := gtk.NewLabel(title)
		l.SetXAlign(0)
		l.SetHExpand(true)
		row.Append(l)
		if sub != "" {
			s := gtk.NewLabel(sub)
			s.AddCSSClass("dim-label")
			row.Append(s)
		}
		b.SetChild(row)
		b.AddCSSClass("flat")
		b.ConnectClicked(func() {
			pop.Popdown()
			f()
		})
		box.Append(b)
	}
	for _, p := range presets {
		p := p
		add(p.label, formatWhen(p.t), func() { pick(p.t) })
	}
	box.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	add(i18n.T("Custom date and time…"), "", func() { a.pickDateTime(parent(), customTitle, pick) })
	pop.SetChild(box)
	return pop
}

// pickDateTime asks for a date and time in the future.
func (a *App) pickDateTime(parent gtk.Widgetter, title string, done func(time.Time)) {
	d := adw.NewAlertDialog(title, "")
	cal := gtk.NewCalendar()
	tomorrow := time.Now().AddDate(0, 0, 1)
	cal.SelectDay(glib.NewDateTimeLocal(tomorrow.Year(), int(tomorrow.Month()), tomorrow.Day(), 8, 0, 0))
	hour := gtk.NewSpinButtonWithRange(0, 23, 1)
	hour.SetValue(8)
	hour.SetOrientation(gtk.OrientationVertical)
	minute := gtk.NewSpinButtonWithRange(0, 55, 5)
	minute.SetValue(0)
	minute.SetOrientation(gtk.OrientationVertical)
	minute.ConnectOutput(func() bool {
		minute.SetText(fmt.Sprintf("%02d", int(minute.Value())))
		return true
	})
	timeBox := gtk.NewBox(gtk.OrientationHorizontal, 6)
	timeBox.SetHAlign(gtk.AlignCenter)
	timeBox.Append(hour)
	timeBox.Append(gtk.NewLabel(":"))
	timeBox.Append(minute)
	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.Append(cal)
	box.Append(timeBox)
	d.SetExtraChild(box)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("ok", i18n.T("Confirm"))
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetDefaultResponse("ok")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "ok" {
			return
		}
		dt := cal.Date()
		t := time.Date(dt.Year(), time.Month(dt.Month()), dt.DayOfMonth(), int(hour.Value()), int(minute.Value()), 0, 0, time.Local)
		if t.Before(time.Now().Add(time.Minute)) {
			a.toast(i18n.T("The chosen time has already passed"))
			return
		}
		done(t)
	})
	d.Present(parent)
}

// ---- Snooze ------------------------------------------------------------------

// snoozeButton is the reader's "Snooze" menu.
func (m *mainView) snoozeButton() *gtk.MenuButton {
	btn := gtk.NewMenuButton()
	btn.SetIconName("alarm-symbolic")
	btn.SetTooltipText(i18n.T("Snooze (Ctrl+H) – the message returns to the inbox at the chosen time"))
	// Presets depend on the current time, so build the popover on open.
	btn.SetCreatePopupFunc(func(b *gtk.MenuButton) {
		b.SetPopover(m.a.presetPopover(snoozePresets(), i18n.T("Snooze Until"), func() gtk.Widgetter { return m.a.win }, m.snooze))
	})
	return btn
}

// snoozeCustom asks for the time directly (keyboard shortcut).
func (m *mainView) snoozeCustom() {
	if len(m.snoozeTargets()) == 0 {
		return
	}
	m.a.pickDateTime(m.a.win, i18n.T("Snooze Until"), m.snooze)
}

func (m *mainView) snoozeTargets() []string {
	var ids []string
	seen := map[string]bool{}
	for _, t := range m.targets() {
		if t.ConversationID != "" && !seen[t.ConversationID] {
			seen[t.ConversationID] = true
			ids = append(ids, t.ConversationID)
		}
	}
	return ids
}

func (m *mainView) snooze(t time.Time) {
	ids := m.snoozeTargets()
	if len(ids) == 0 {
		m.a.toast(i18n.T("This message cannot be snoozed"))
		return
	}
	acc := m.a.acc
	m.leaveSelection()
	m.clearReader()
	go func() {
		err := acc.Snooze(m.a.ctx, ids, t)
		ui(func() {
			if err != nil {
				m.a.toast(err.Error())
				return
			}
			m.reloadFolders()
			note := ""
			if !acc.Caps().ServerSnooze {
				note = i18n.T(" (if Klient is running)")
			}
			m.a.toastWithAction(fmt.Sprintf(i18n.T("Snoozed – it returns %s"), formatWhen(t))+note, i18n.T("Undo"), func() {
				go func() {
					err := acc.Unsnooze(m.a.ctx, ids)
					ui(func() {
						if err != nil {
							m.a.toast(err.Error())
						}
						m.scheduleRefresh()
					})
				}()
			})
			m.scheduleRefresh()
		})
	}()
}

// unsnooze returns the open conversation to the inbox now.
func (m *mainView) unsnooze() {
	ids := m.snoozeTargets()
	if len(ids) == 0 {
		return
	}
	acc := m.a.acc
	m.leaveSelection()
	m.clearReader()
	go func() {
		err := acc.Unsnooze(m.a.ctx, ids)
		ui(func() {
			if err != nil {
				m.a.toast(err.Error())
				return
			}
			m.a.toast(i18n.T("Returned to the inbox"))
			m.scheduleRefresh()
		})
	}()
}

// ---- Scheduled messages --------------------------------------------------------

// showScheduledBanner shows when the open scheduled message leaves, counting
// down every second, with a button to cancel it (it becomes a draft).
func (m *mainView) showScheduledBanner(meta protonmail.Summary) {
	when := time.Unix(meta.Time, 0)
	th := m.thread
	update := func() bool {
		if m.thread != th || m.a.mv != m {
			return false
		}
		m.banner.SetTitle(fmt.Sprintf(i18n.T("Scheduled: it will be sent %s (%s)"), formatWhen(when), countdown(when)))
		return time.Until(when) > 0
	}
	update()
	m.banner.SetButtonLabel(i18n.T("Cancel Sending"))
	m.bannerFn = func() { m.cancelScheduled(meta.ID) }
	m.banner.SetRevealed(true)
	glib.TimeoutSecondsAdd(1, update)
}

func (m *mainView) cancelScheduled(id string) {
	acc := m.a.acc
	m.clearReader()
	go func() {
		err := acc.CancelScheduled(m.a.ctx, id)
		ui(func() {
			if err != nil {
				m.a.toast(err.Error())
				return
			}
			m.a.toast(i18n.T("Sending cancelled; the message is in Drafts"))
			m.scheduleRefresh()
			if acc.Kind() == mailbox.KindProton {
				m.a.openDraft(id) // same ID as a draft
			} else {
				m.openFolder(protonmail.FolderByID(protonmail.DraftsID))
			}
		})
	}()
}

// tickCountdowns refreshes the "za 2 h" labels of scheduled rows.
func (m *mainView) tickCountdowns() {
	glib.TimeoutSecondsAdd(30, func() bool {
		if m.a.mv != m {
			return false
		}
		for _, c := range m.countdowns {
			c.label.SetText(countdown(c.t))
		}
		return true
	})
}

type countdownLabel struct {
	label *gtk.Label
	t     time.Time
}

// ---- Undo send ---------------------------------------------------------------

// undoToast counts down the send delay in a toast with an "Undo" button.
// done(true) runs when the time is up, done(false) after Undo.
func (a *App) undoToast(delay int, what string, done func(send bool)) {
	left := delay
	finished := false
	text := func() string { return fmt.Sprintf("%s za %d s", what, left) }
	t := adw.NewToast(text())
	t.SetTimeout(0) // dismissed by us
	t.SetButtonLabel(i18n.T("Undo"))
	t.ConnectButtonClicked(func() {
		if finished {
			return
		}
		finished = true
		done(false)
	})
	a.toasts.AddToast(t)
	glib.TimeoutSecondsAdd(1, func() bool {
		if finished {
			return false
		}
		left--
		if left > 0 {
			t.SetTitle(text())
			return true
		}
		finished = true
		t.Dismiss()
		done(true)
		return false
	})
}

// endOfDay is 23:59 of the day of t.
func endOfDay(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 23, 59, 0, 0, time.Local)
}
