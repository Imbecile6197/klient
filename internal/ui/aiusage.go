package ui

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	aipkg "github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/usage"
)

// startUsage opens the AI usage statistics and records every request.
func (a *App) startUsage() {
	a.usage = usage.Open(usage.Path())
	a.usage.OnChange = func() {
		ui(func() {
			if a.mv != nil {
				a.mv.updateUsage()
			}
		})
	}
	aipkg.SetUsageHook(a.usage.Add)
}

// money writes an amount in USD as the interface language does.
func money(v float64) string {
	switch {
	case v == 0:
		v = 0
	case v < 0.01:
		return i18n.T("< $0.01")
	}
	s := strconv.FormatFloat(math.Round(v*100)/100, 'f', 2, 64)
	if i18n.Lang() == "cs" {
		return strings.ReplaceAll(s, ".", ",") + " $"
	}
	return "$" + s
}

// moneyOf is the cost of lines, with "+ ?" when some price is unknown.
func moneyOf(lines []usage.Line) string {
	cost, unpriced := usage.Total(lines)
	s := money(cost)
	if unpriced {
		s += " + ?"
	}
	return s
}

func taskName(task string) string {
	switch task {
	case aipkg.TaskSpam:
		return i18n.T("Spam filter")
	case aipkg.TaskSummary:
		return i18n.T("Summaries")
	case aipkg.TaskReply:
		return i18n.T("Reply suggestions")
	case aipkg.TaskImprove:
		return i18n.T("Writing with AI")
	case aipkg.TaskLabel:
		return i18n.T("AI labels")
	case aipkg.TaskSearch, aipkg.TaskAnswer:
		return i18n.T("Questions about mail")
	case aipkg.TaskDigest:
		return i18n.T("Morning overview")
	case aipkg.TaskTest:
		return i18n.T("Connection tests")
	}
	return i18n.T("AI assistant")
}

// newUsageLabel is the line under the spam filter status: this month's
// AI spending, with a link to the details.
func (m *mainView) newUsageLabel() *gtk.Label {
	l := gtk.NewLabel("")
	l.AddCSSClass("dim-label")
	l.AddCSSClass("caption")
	l.SetWrap(true)
	l.SetXAlign(0)
	l.SetMarginStart(12)
	l.SetMarginEnd(12)
	l.SetMarginBottom(8)
	l.ConnectActivateLink(func(string) bool {
		m.a.showUsage()
		return true
	})
	return l
}

func (m *mainView) updateUsage() {
	if m.usageLabel == nil || m.a.usage == nil {
		return
	}
	now := time.Now()
	from, to := usage.Period(now, 0)
	month := m.a.usage.Lines(from, to, m.a.cfg.AIPrices)
	today := m.a.usage.Lines(usage.Today(now), usage.Today(now), m.a.cfg.AIPrices)
	todayCost, _ := usage.Total(today)
	text := fmt.Sprintf(i18n.T("AI spending this month: %s (today %s)"), moneyOf(month), money(todayCost))
	m.usageLabel.SetMarkup(html.EscapeString(text) + ` · <a href="usage">` + html.EscapeString(i18n.T("Details")) + `</a>`)
}

// showUsage shows what the AI cost, per model and task.
func (a *App) showUsage() {
	d := adw.NewDialog()
	d.SetTitle(i18n.T("AI Spending"))
	d.SetContentWidth(560)
	d.SetContentHeight(620)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())
	d.SetChild(tv)

	periods := []struct {
		name     string
		from, to func(time.Time) string
	}{
		{i18n.T("This month"), func(t time.Time) string { f, _ := usage.Period(t, 0); return f }, func(t time.Time) string { _, l := usage.Period(t, 0); return l }},
		{i18n.T("Last month"), func(t time.Time) string { f, _ := usage.Period(t, -1); return f }, func(t time.Time) string { _, l := usage.Period(t, -1); return l }},
		{i18n.T("Today"), usage.Today, usage.Today},
		{i18n.T("All time"), func(time.Time) string { return "0000" }, func(time.Time) string { return "9999" }},
	}
	var build func(sel uint)
	build = func(sel uint) {
		page := adw.NewPreferencesPage()
		top := adw.NewPreferencesGroup()
		period := adw.NewComboRow()
		period.SetUseMarkup(false) // names and addresses may contain & or <
		period.SetTitle(i18n.T("Period"))
		var names []string
		for _, p := range periods {
			names = append(names, p.name)
		}
		period.SetModel(gtk.NewStringList(names))
		period.SetSelected(sel)
		period.NotifyProperty("selected", func() { build(period.Selected()) })
		top.Add(period)

		now := time.Now()
		p := periods[sel]
		lines := a.usage.Lines(p.from(now), p.to(now), a.cfg.AIPrices)
		total := adw.NewActionRow()
		total.SetUseMarkup(false) // names and addresses may contain & or <
		total.SetTitle(i18n.T("Total"))
		var req, tokens int64
		for _, l := range lines {
			req += l.Requests
			tokens += l.In + l.Cached + l.Out
		}
		total.SetSubtitle(fmt.Sprintf(i18n.N("%d request", "%d requests", int(req)), req) + " · " + fmt.Sprintf(i18n.T("%s tokens"), thousands(tokens)))
		sum := gtk.NewLabel(moneyOf(lines))
		sum.AddCSSClass("title-3")
		total.AddSuffix(sum)
		top.Add(total)
		top.SetDescription(i18n.T("An estimate from the providers' price lists (without VAT), counted from the tokens each provider reports. The exact amount is in your account with the provider. Klient counts from version 0.11.0."))
		page.Add(top)

		g := adw.NewPreferencesGroup()
		g.SetTitle(i18n.T("By Model and Task"))
		if len(lines) == 0 {
			g.SetDescription(i18n.T("No AI requests in this period."))
		}
		for _, l := range lines {
			l := l
			row := adw.NewActionRow()
			row.SetUseMarkup(false)
			row.SetTitle(l.Model + " · " + taskName(l.Task))
			row.SetSubtitle(fmt.Sprintf(i18n.N("%d request", "%d requests", int(l.Requests)), l.Requests) + " · " +
				fmt.Sprintf(i18n.T("%s in, %s out"), thousands(l.In+l.Cached), thousands(l.Out)))
			if aipkg.IsLocal(l.Provider) {
				row.SetSubtitle(row.Subtitle() + " · " + i18n.T("on this computer, free"))
			}
			if l.Priced {
				row.AddSuffix(gtk.NewLabel(money(l.Cost)))
			} else {
				set := gtk.NewButtonWithLabel(i18n.T("Set Price…"))
				set.SetVAlign(gtk.AlignCenter)
				set.AddCSSClass("flat")
				set.SetTooltipText(i18n.T("Klient does not know the price of this model"))
				set.ConnectClicked(func() {
					a.askPrice(d, l.Model, func() { build(sel) })
				})
				row.AddSuffix(set)
			}
			g.Add(row)
		}
		page.Add(g)
		tv.SetContent(page)
	}
	build(0)
	d.Present(a.gtkWindow())
}

// askPrice lets the user enter the price of a model the price list lacks.
func (a *App) askPrice(parent gtk.Widgetter, model string, done func()) {
	q := adw.NewAlertDialog(fmt.Sprintf(i18n.T("Price of %s"), model), i18n.T("USD per million tokens, as in the provider's price list."))
	box := gtk.NewBox(gtk.OrientationVertical, 6)
	cur := a.cfg.AIPrices[model]
	entry := func(title string, v float64) *adw.EntryRow {
		e := adw.NewEntryRow()
		e.SetTitle(title)
		if v > 0 {
			e.SetText(strconv.FormatFloat(v, 'f', -1, 64))
		}
		e.SetInputPurpose(gtk.InputPurposeNumber)
		return e
	}
	in := entry(i18n.T("Input"), cur.In)
	cached := entry(i18n.T("Cached input"), cur.Cached)
	out := entry(i18n.T("Output"), cur.Out)
	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list")
	list.SetSelectionMode(gtk.SelectionNone)
	list.Append(in)
	list.Append(cached)
	list.Append(out)
	box.Append(list)
	q.SetExtraChild(box)
	q.AddResponse("cancel", i18n.T("Cancel"))
	q.AddResponse("save", i18n.T("Save"))
	q.SetResponseAppearance("save", adw.ResponseSuggested)
	q.SetCloseResponse("cancel")
	num := func(e *adw.EntryRow) float64 {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(e.Text()), ",", "."), 64)
		return max(v, 0)
	}
	q.ConnectResponse(func(r string) {
		if r != "save" {
			return
		}
		if a.cfg.AIPrices == nil {
			a.cfg.AIPrices = map[string]config.ModelPrice{}
		}
		a.cfg.AIPrices[model] = config.ModelPrice{In: num(in), Cached: num(cached), Out: num(out)}
		a.saveConfig()
		if a.mv != nil {
			a.mv.updateUsage()
		}
		done()
	})
	q.Present(parent)
}

// thousands writes a count with thin groups ("1 234 567").
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(c)
	}
	return b.String()
}
