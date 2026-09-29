package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

type sample struct {
	msg    *protonmail.Message
	isSpam bool // taken from the Spam folder
}

// compareModels lets local models judge the same messages from the user's
// own mailbox and shows the results side by side. Everything runs locally.
func (a *App) compareModels(parent gtk.Widgetter) {
	if a.acc == nil {
		return
	}
	d := adw.NewDialog()
	d.SetTitle("Porovnání lokálních modelů")
	d.SetContentWidth(760)
	d.SetContentHeight(720)
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	start := gtk.NewButtonWithLabel("Spustit")
	start.AddCSSClass("suggested-action")
	hb.PackEnd(start)
	tv.AddTopBar(hb)

	page := adw.NewPreferencesPage()
	pick := adw.NewPreferencesGroup()
	pick.SetTitle("Modely")
	pick.SetDescription("Každý model posoudí 5 nejnovějších zpráv z doručené pošty a 3 ze spamu a shrne jednu zprávu. Vše běží v tomto počítači; na procesoru to zabere zhruba 5–15 minut a modely se nejdřív stáhnou.")
	var checks []*gtk.CheckButton
	for _, m := range localModels {
		row := adw.NewActionRow()
		row.SetTitle(m.label)
		row.SetSubtitle(m.name + " · " + m.size)
		c := gtk.NewCheckButton()
		c.SetActive(m.defaultOn)
		row.AddPrefix(c)
		row.SetActivatableWidget(c)
		pick.Add(row)
		checks = append(checks, c)
	}
	page.Add(pick)

	statusGroup := adw.NewPreferencesGroup()
	status := gtk.NewLabel("")
	status.SetXAlign(0)
	status.SetWrap(true)
	status.AddCSSClass("dim-label")
	bar := gtk.NewProgressBar()
	bar.SetVisible(false)
	sbox := gtk.NewBox(gtk.OrientationVertical, 6)
	sbox.Append(status)
	sbox.Append(bar)
	statusGroup.Add(sbox)
	page.Add(statusGroup)

	tv.SetContent(page)
	d.SetChild(tv)

	ctx, cancel := context.WithCancel(a.ctx)
	d.ConnectClosed(cancel)
	acc := a.acc
	threshold := a.cfg.SpamThreshold
	var results []*adw.PreferencesGroup

	start.ConnectClicked(func() {
		var chosen []string
		for i, c := range checks {
			if c.Active() {
				chosen = append(chosen, localModels[i].name)
			}
		}
		if len(chosen) == 0 {
			status.SetText("Vyberte alespoň jeden model")
			return
		}
		start.SetSensitive(false)
		pick.SetSensitive(false)
		for _, g := range results {
			page.Remove(g)
		}
		results = nil
		bar.SetVisible(true)
		setStatus := func(s string, frac float64) {
			ui(func() {
				status.SetText(s)
				if frac >= 0 {
					bar.SetFraction(frac)
				} else {
					bar.Pulse()
				}
			})
		}
		go func() {
			err := a.runComparison(ctx, acc, chosen, threshold, setStatus, func(g *adw.PreferencesGroup) {
				page.Add(g)
				results = append(results, g)
			}, chosen, d)
			ui(func() {
				start.SetSensitive(true)
				pick.SetSensitive(true)
				bar.SetVisible(false)
				if err != nil && ctx.Err() == nil {
					status.SetText("Porovnání selhalo: " + err.Error())
				}
			})
		}()
	})
	d.Present(parent)
}

func (a *App) runComparison(ctx context.Context, acc mailbox.Account, models []string, threshold float64,
	setStatus func(string, float64), addResult func(*adw.PreferencesGroup), all []string, d *adw.Dialog) error {
	if a.ollama.Installed() == "" {
		setStatus("Instaluji Ollamu…", -1)
		errc := make(chan string, 1)
		a.updateOllama(true, func(s string, f float64) {
			setStatus(s, f)
			if strings.HasPrefix(s, "Hotovo s chybou") {
				errc <- s
			}
		})
		select {
		case e := <-errc:
			return fmt.Errorf("%s", strings.TrimPrefix(e, "Hotovo s chybou – "))
		default:
		}
		if a.ollama.Installed() == "" {
			return fmt.Errorf("Ollamu se nepodařilo nainstalovat")
		}
	}
	release, err := a.ollama.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	cl := a.localClient()

	// Download missing models.
	have := map[string]bool{}
	if list, err := cl.List(ctx); err == nil {
		for _, m := range list {
			have[m.Name] = true
		}
	}
	for _, m := range models {
		if have[m] {
			continue
		}
		err := cl.Pull(ctx, m, func(st string, done, total int64) {
			if total > 0 {
				setStatus(fmt.Sprintf("Stahuji %s: %s z %s", m, humanSize(done), humanSize(total)), float64(done)/float64(total))
			} else {
				setStatus("Stahuji "+m+": "+st, -1)
			}
		})
		if err != nil {
			return fmt.Errorf("stažení %s: %w", m, err)
		}
	}

	// Messages from the user's mailbox (decrypted locally).
	setStatus("Vybírám zprávy ze schránky…", -1)
	var samples []sample
	for _, src := range []struct {
		folder string
		n      int
		spam   bool
	}{{protonmail.InboxID, 5, false}, {protonmail.SpamID, 3, true}} {
		list, err := acc.List(ctx, src.folder, 0, 20)
		if err != nil {
			return err
		}
		n := 0
		for _, s := range list {
			if n >= src.n || s.IsDraft() {
				continue
			}
			if msg, err := acc.Get(ctx, s.ID); err == nil {
				samples = append(samples, sample{msg, src.spam})
				n++
			}
		}
	}
	if len(samples) == 0 {
		return fmt.Errorf("ve schránce nejsou žádné zprávy k porovnání")
	}

	steps := float64(len(models) * (len(samples) + 1))
	step := 0.0
	for _, model := range models {
		c := ai.New(ctx, ai.Settings{
			AssistantProvider: ai.ProviderOllama, AssistantModel: model,
			SpamProvider: ai.ProviderOllama, SpamModel: model,
			Local: a.ollama, LocalOnly: true,
		}, func(string) string { return "" })
		type verdict struct {
			v   ai.Verdict
			err error
			dur time.Duration
		}
		var verdicts []verdict
		for i, s := range samples {
			setStatus(fmt.Sprintf("%s posuzuje zprávu %d z %d…", model, i+1, len(samples)), step/steps)
			from := ""
			if s.msg.Meta.Sender != nil {
				from = mailparse.DisplayAddress(s.msg.Meta.Sender)
			}
			t0 := time.Now()
			v, err := c.ClassifySpam(ctx, ai.SpamInput{From: from, Subject: s.msg.Meta.Subject, Body: s.msg.Text})
			verdicts = append(verdicts, verdict{v, err, time.Since(t0)})
			step++
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		setStatus(model+" píše shrnutí…", step/steps)
		first := samples[0].msg
		from := ""
		if first.Meta.Sender != nil {
			from = mailparse.DisplayAddress(first.Meta.Sender)
		}
		t0 := time.Now()
		summary, serr := c.Summarize(ctx, from, first.Meta.Subject, first.Text)
		sumDur := time.Since(t0)
		step++

		correct, answered := 0, 0
		var total time.Duration
		for i, v := range verdicts {
			total += v.dur
			if v.err != nil {
				continue
			}
			answered++
			if (v.v.SpamProbability >= threshold) == samples[i].isSpam {
				correct++
			}
		}
		model := model
		ui(func() {
			g := adw.NewPreferencesGroup()
			g.SetTitle(model)
			g.SetDescription(fmt.Sprintf("Správně %d z %d · %.0f s na zprávu · shrnutí za %.0f s",
				correct, len(samples), total.Seconds()/float64(len(samples)), sumDur.Seconds()))
			use := gtk.NewButtonWithLabel("Použít")
			use.AddCSSClass("suggested-action")
			use.SetVAlign(gtk.AlignCenter)
			use.ConnectClicked(func() { a.chooseComparedModel(model, all, d) })
			g.SetHeaderSuffix(use)
			for i, v := range verdicts {
				s := samples[i]
				row := adw.NewActionRow()
				row.SetTitle(orDefault(s.msg.Meta.Subject, "(bez předmětu)"))
				row.SetTitleLines(1)
				where := "Doručená pošta"
				if s.isSpam {
					where = "Spam"
				}
				icon := "object-select-symbolic"
				switch {
				case v.err != nil:
					row.SetSubtitle(where + " → chyba: " + ai.Explain(ai.ProviderOllama, v.err).Text)
					icon = "dialog-error-symbolic"
				default:
					row.SetSubtitle(fmt.Sprintf("%s → %s %.0f %% · %.0f s · %s", where, categoryName(v.v.Category),
						v.v.SpamProbability*100, v.dur.Seconds(), v.v.Reason))
					if (v.v.SpamProbability >= threshold) != s.isSpam {
						icon = "dialog-warning-symbolic"
					}
				}
				row.SetSubtitleLines(3)
				img := gtk.NewImageFromIconName(icon)
				if icon != "object-select-symbolic" {
					img.AddCSSClass("warning")
				} else {
					img.AddCSSClass("success")
				}
				row.AddPrefix(img)
				g.Add(row)
			}
			sum := adw.NewExpanderRow()
			sum.SetTitle("Shrnutí zprávy „" + orDefault(first.Meta.Subject, "(bez předmětu)") + "“")
			text := summary
			if serr != nil {
				text = "Chyba: " + ai.Explain(ai.ProviderOllama, serr).Text
			}
			l := gtk.NewLabel(strings.TrimSpace(text))
			l.SetWrap(true)
			l.SetWrapMode(pango.WrapWordChar)
			l.SetXAlign(0)
			l.SetSelectable(true)
			for _, f := range []func(int){l.SetMarginTop, l.SetMarginBottom, l.SetMarginStart, l.SetMarginEnd} {
				f(12)
			}
			sum.AddRow(l)
			g.Add(sum)
			addResult(g)
		})
	}
	setStatus(fmt.Sprintf("Hotovo. Posouzeno %d zpráv; vyberte model tlačítkem Použít. (Za „správně“ se považuje shoda se složkou, ve které zpráva leží.)", len(samples)), 1)
	return nil
}

// chooseComparedModel switches both AI roles to model and offers to delete
// the other compared models.
func (a *App) chooseComparedModel(model string, compared []string, d *adw.Dialog) {
	a.useLocalModel(model)
	var others []string
	for _, m := range compared {
		if m != model {
			others = append(others, m)
		}
	}
	if len(others) == 0 {
		d.Close()
		a.toast("Lokální AI používá " + model)
		return
	}
	q := adw.NewAlertDialog("Používá se "+model, "Smazat ostatní porovnávané modely, ať nezabírají místo na disku?\n\n"+strings.Join(others, "\n"))
	q.AddResponse("keep", "Ponechat")
	q.AddResponse("delete", "Smazat")
	q.SetResponseAppearance("delete", adw.ResponseDestructive)
	q.SetCloseResponse("keep")
	q.ConnectResponse(func(r string) {
		if r == "delete" {
			go func() {
				release, err := a.ollama.Acquire(a.ctx)
				defer release()
				if err != nil {
					return
				}
				for _, m := range others {
					_ = ollama.NewClient(a.ollama.Addr()).Delete(a.ctx, m)
				}
			}()
		}
		d.Close()
		a.toast("Lokální AI používá " + model)
	})
	q.Present(d)
}
