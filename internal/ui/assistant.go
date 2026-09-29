package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/libormacak/klient/internal/ai"
	"github.com/libormacak/klient/internal/mailbox"
	"github.com/libormacak/klient/internal/mailparse"
	"github.com/libormacak/klient/internal/protonmail"
)

// ---- AI labels ----------------------------------------------------------------

// tagLabels are the user's labels (not folders) of an account.
func tagLabels(ctx context.Context, acc mailbox.Account) []protonmail.UserLabel {
	all, err := acc.UserLabels(ctx)
	if err != nil {
		return nil
	}
	var out []protonmail.UserLabel
	for _, l := range all {
		if !l.Folder {
			out = append(out, l)
		}
	}
	return out
}

func labelNames(ls []protonmail.UserLabel) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

// suggestLabel asks the AI which label fits a message. Runs off the UI thread.
func (a *App) suggestLabel(ctx context.Context, acc mailbox.Account, msg *protonmail.Message) (protonmail.UserLabel, ai.LabelChoice, error) {
	labels := tagLabels(ctx, acc)
	if len(labels) == 0 {
		return protonmail.UserLabel{}, ai.LabelChoice{}, fmt.Errorf("nemáte žádné štítky – vytvořte je na webu Protonu")
	}
	from := ""
	if msg.Meta.Sender != nil {
		from = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	// The choice is cached per message (with the label list it was made from).
	key := "ai-label:" + msg.Meta.ID
	var cached struct {
		Labels string
		Choice ai.LabelChoice
	}
	names := strings.Join(labelNames(labels), "\x00")
	var ch ai.LabelChoice
	if c := acc.Cache(); c != nil && c.Get(key, &cached) && cached.Labels == names {
		ch = cached.Choice
	} else {
		var err error
		ch, err = a.ai.SuggestLabel(ctx, labelNames(labels), from, msg.Meta.Subject, msg.Text)
		if err != nil {
			return protonmail.UserLabel{}, ch, err
		}
		if c := acc.Cache(); c != nil {
			cached.Labels, cached.Choice = names, ch
			_ = c.Set(key, cached)
		}
	}
	for _, l := range labels {
		if l.Name == ch.Label {
			return l, ch, nil
		}
	}
	return protonmail.UserLabel{}, ch, nil
}

// autoLabel labels a new message when the AI is confident. It returns the
// label name ("" when nothing was applied). Runs off the UI thread.
func (a *App) autoLabel(acc mailbox.Account, meta protonmail.Summary) string {
	if !a.ai.HasAssistant() {
		return ""
	}
	ctx, cancel := context.WithTimeout(a.ctx, 6*time.Minute)
	defer cancel()
	msg, err := acc.Get(ctx, meta.ID)
	if err != nil {
		return ""
	}
	l, ch, err := a.suggestLabel(ctx, acc, msg)
	if err != nil || l.ID == "" || ch.Confidence < 0.6 {
		return ""
	}
	if acc.SetLabel(ctx, l.ID, true, meta.ID) != nil {
		return ""
	}
	return l.Name
}

// suggestLabelFor is the "AI: navrhnout štítek" message action.
func (m *mainView) suggestLabelFor(msg *protonmail.Message) {
	if !m.a.ai.HasAssistant() {
		m.a.toastWithAction("AI asistent není nastavený", "Nastavit", m.a.openPreferences)
		return
	}
	m.a.toast("AI vybírá štítek…")
	acc := m.a.acc
	go func() {
		l, ch, err := m.a.suggestLabel(m.a.ctx, acc, msg)
		ui(func() {
			switch {
			case err != nil:
				m.a.toast("Návrh štítku selhal: " + ai.Explain(m.a.cfg.AssistantProvider, err).Text)
			case l.ID == "":
				m.a.toast("AI nenašla vhodný štítek: " + ch.Reason)
			case hasLabel(msg.Meta, l.ID):
				m.a.toast("Zpráva už má štítek " + l.Name)
			default:
				m.a.toastWithAction(fmt.Sprintf("AI navrhuje štítek „%s“ – %s", l.Name, ch.Reason), "Použít", func() {
					go func() {
						err := acc.SetLabel(m.a.ctx, l.ID, true, msg.Meta.ID)
						ui(func() {
							if err != nil {
								m.a.toast("Přidání štítku selhalo: " + err.Error())
								return
							}
							m.a.toast("Přidán štítek " + l.Name)
							m.scheduleRefresh()
						})
					}()
				})
			}
		})
	}()
}

// ---- Ask your mail -------------------------------------------------------------

func today() string {
	days := []string{"neděle", "pondělí", "úterý", "středa", "čtvrtek", "pátek", "sobota"}
	now := time.Now()
	return days[now.Weekday()] + " " + now.Format("2. 1. 2006")
}

func docOf(msg *protonmail.Message) ai.MailDoc {
	from := ""
	if msg.Meta.Sender != nil {
		from = mailparse.DisplayAddress(msg.Meta.Sender)
	}
	return ai.MailDoc{From: from, Date: time.Unix(msg.Meta.Time, 0).Format("2. 1. 2006 15:04"), Subject: msg.Meta.Subject, Body: msg.Text}
}

// askMail answers questions about the mailbox: the AI picks search terms,
// the matching messages are decrypted locally and the AI answers from them.
func (a *App) askMail() {
	if a.acc == nil {
		return
	}
	if !a.ai.HasAssistant() {
		a.toastWithAction("AI asistent není nastavený", "Nastavit", a.openPreferences)
		return
	}
	d := adw.NewDialog()
	d.SetTitle("Zeptat se pošty")
	d.SetContentWidth(640)
	d.SetContentHeight(620)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())

	entry := gtk.NewEntry()
	entry.SetPlaceholderText("Např. „Kolik byla poslední faktura za elektřinu?“")
	entry.SetHExpand(true)
	ask := gtk.NewButtonWithLabel("Zeptat se")
	ask.AddCSSClass("suggested-action")
	row := gtk.NewBox(gtk.OrientationHorizontal, 6)
	row.Append(entry)
	row.Append(ask)

	noteText := "AI navrhne, co hledat, Klient zprávy najde a dešifruje u vás a AI z nich odpoví. Obsah nalezených zpráv se pošle zvolenému poskytovateli AI."
	if a.ai.AssistantLocal() {
		noteText = "AI navrhne, co hledat, Klient zprávy najde a dešifruje a lokální model z nich odpoví – vše zůstává ve vašem počítači. Na procesoru to může trvat několik minut."
	}
	note := gtk.NewLabel(noteText)
	note.SetWrap(true)
	note.SetXAlign(0)
	note.AddCSSClass("dim-label")
	note.AddCSSClass("caption")

	status := gtk.NewLabel("")
	status.SetXAlign(0)
	status.AddCSSClass("dim-label")
	answer := gtk.NewLabel("")
	answer.SetWrap(true)
	answer.SetWrapMode(pango.WrapWordChar)
	answer.SetXAlign(0)
	answer.SetSelectable(true)
	card := gtk.NewBox(gtk.OrientationVertical, 0)
	card.AddCSSClass("card")
	card.AddCSSClass("summary-card")
	answer.SetMarginTop(12)
	answer.SetMarginBottom(12)
	answer.SetMarginStart(12)
	answer.SetMarginEnd(12)
	card.Append(answer)
	card.SetVisible(false)

	sources := adw.NewPreferencesGroup()
	sources.SetTitle("Zdroje")
	sources.SetVisible(false)
	var sourceRows []gtk.Widgetter

	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetMarginTop(12)
	box.SetMarginBottom(18)
	box.SetMarginStart(18)
	box.SetMarginEnd(18)
	box.Append(row)
	box.Append(note)
	box.Append(status)
	box.Append(card)
	box.Append(sources)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetChild(box)
	sw.SetVExpand(true)
	tv.SetContent(sw)
	d.SetChild(tv)

	acc := a.acc
	run := func() {
		q := strings.TrimSpace(entry.Text())
		if q == "" {
			return
		}
		ask.SetSensitive(false)
		card.SetVisible(false)
		for _, r := range sourceRows {
			sources.Remove(r)
		}
		sourceRows = nil
		sources.SetVisible(false)
		status.SetText("Vymýšlím, co hledat…")
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
			defer cancel()
			queries, err := a.ai.SearchPlan(ctx, q, today())
			var found []protonmail.Summary
			if err == nil {
				ui(func() { status.SetText("Hledám: " + strings.Join(queries, ", ") + "…") })
				seen := map[string]bool{}
				for _, sq := range queries {
					res, _ := acc.Search(ctx, protonmail.AllMailID, sq, 3000)
					for _, s := range res {
						if !seen[s.ID] && !s.IsDraft() {
							seen[s.ID] = true
							found = append(found, s)
						}
					}
				}
				sort.Slice(found, func(i, j int) bool { return found[i].Time > found[j].Time })
				if n := a.ai.MaxDocs(10, 5); len(found) > n {
					found = found[:n]
				}
			}
			var docs []ai.MailDoc
			var used []protonmail.Summary
			if err == nil && len(found) > 0 {
				ui(func() { status.SetText(fmt.Sprintf("Čtu %d zpráv…", len(found))) })
				for _, s := range found {
					if msg, e := acc.Get(ctx, s.ID); e == nil {
						docs = append(docs, docOf(msg))
						used = append(used, s)
					}
				}
			}
			out := ""
			if err == nil && len(docs) > 0 {
				ui(func() { status.SetText("Píšu odpověď…") })
				out, err = a.ai.AnswerFromMail(ctx, q, today(), docs)
			}
			ui(func() {
				ask.SetSensitive(true)
				switch {
				case err != nil:
					status.SetText("Nepovedlo se: " + ai.Explain(a.cfg.AssistantProvider, err).Text)
					return
				case len(docs) == 0:
					status.SetText("Nenašel jsem žádné zprávy (hledáno: " + strings.Join(queries, ", ") + "). Zkuste otázku formulovat jinak, třeba se jménem odesílatele.")
					return
				}
				status.SetText(fmt.Sprintf("Odpověď z %d nalezených zpráv", len(docs)))
				answer.SetText(strings.TrimSpace(out))
				card.SetVisible(true)
				for i, s := range used {
					s := s
					r := adw.NewActionRow()
					r.SetTitle(fmt.Sprintf("[%d] %s", i+1, orDefault(s.Subject, "(bez předmětu)")))
					from := ""
					if s.Sender != nil {
						from = mailparse.DisplayName(s.Sender)
					}
					r.SetSubtitle(from + " · " + time.Unix(s.Time, 0).Format("2. 1. 2006"))
					r.SetActivatable(true)
					r.AddSuffix(gtk.NewImageFromIconName("go-next-symbolic"))
					r.ConnectActivated(func() {
						d.Close()
						if a.mv != nil && a.acc == acc {
							a.mv.openThread(protonmail.Thread{ConversationID: s.ConversationID, Latest: s, Messages: []protonmail.Summary{s}})
						}
					})
					sources.Add(r)
					sourceRows = append(sourceRows, r)
				}
				sources.SetVisible(true)
			})
		}()
	}
	ask.ConnectClicked(run)
	entry.ConnectActivate(run)
	d.Present(a.win)
	entry.GrabFocus()
}

// ---- Morning overview --------------------------------------------------------

// digestLoop sends the morning overview once a day after DigestHour.
func (a *App) digestLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		ready := make(chan bool)
		ui(func() {
			now := time.Now()
			ready <- a.cfg.Digest && a.ai.HasAssistant() && len(a.sessions) > 0 &&
				now.Hour() >= a.cfg.DigestHour && a.cfg.DigestLast != now.Format("2006-01-02")
		})
		if !<-ready {
			continue
		}
		ui(func() {
			a.cfg.DigestLast = time.Now().Format("2006-01-02")
			a.saveConfig()
		})
		text, n, err := a.buildDigest()
		ui(func() {
			if err != nil || n == 0 {
				return
			}
			a.digest = text
			note := gio.NewNotification("Ranní přehled: " + unreadText(n))
			first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
			note.SetBody(first)
			note.SetDefaultAction("app.digest")
			a.app.SendNotification("digest", note)
		})
	}
}

// buildDigest summarises the unread inbox of every account. Off the UI thread.
func (a *App) buildDigest() (string, int, error) {
	accs := make(chan []mailbox.Account)
	ui(func() {
		var l []mailbox.Account
		for _, s := range a.sessions {
			l = append(l, s.acc)
		}
		accs <- l
	})
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Minute)
	defer cancel()
	perAccount := a.ai.MaxDocs(20, 10)
	var docs []ai.MailDoc
	for _, acc := range <-accs {
		msgs, err := acc.List(ctx, protonmail.InboxID, 0, 100)
		if err != nil {
			continue
		}
		n := 0
		for _, s := range msgs {
			if !bool(s.Unread) || n >= perAccount {
				continue
			}
			if d, ok := a.filter.Decision(s.ID); ok && d.Spam {
				continue
			}
			if msg, err := acc.Get(ctx, s.ID); err == nil {
				doc := docOf(msg)
				if len(a.cfg.Accounts) > 1 {
					doc.Subject += " (účet " + acc.Email() + ")"
				}
				docs = append(docs, doc)
				n++
			}
		}
	}
	if len(docs) == 0 {
		return "Žádná nepřečtená pošta. 🎉", 0, nil
	}
	out, err := a.ai.Digest(ctx, today(), docs)
	return out, len(docs), err
}

// showDigest shows the last overview; generate builds a fresh one.
func (a *App) showDigest(generate bool) {
	a.showWindow()
	if !a.ai.HasAssistant() {
		a.toastWithAction("AI asistent není nastavený", "Nastavit", a.openPreferences)
		return
	}
	d := adw.NewDialog()
	d.SetTitle("Přehled nepřečtené pošty")
	d.SetContentWidth(620)
	d.SetContentHeight(560)
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	refresh := gtk.NewButtonFromIconName("view-refresh-symbolic")
	refresh.SetTooltipText("Připravit znovu")
	hb.PackStart(refresh)
	tv.AddTopBar(hb)
	label := gtk.NewLabel("")
	label.SetWrap(true)
	label.SetWrapMode(pango.WrapWordChar)
	label.SetXAlign(0)
	label.SetYAlign(0)
	label.SetSelectable(true)
	label.SetMarginTop(12)
	label.SetMarginBottom(18)
	label.SetMarginStart(18)
	label.SetMarginEnd(18)
	stack := gtk.NewStack()
	stack.AddNamed(spinnerBox(), "loading")
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetChild(label)
	stack.AddNamed(sw, "text")
	tv.SetContent(stack)
	d.SetChild(tv)

	build := func() {
		stack.SetVisibleChildName("loading")
		refresh.SetSensitive(false)
		go func() {
			text, _, err := a.buildDigest()
			ui(func() {
				refresh.SetSensitive(true)
				if err != nil {
					text = "Přehled se nepodařilo připravit: " + ai.Explain(a.cfg.AssistantProvider, err).Text
				} else {
					a.digest = text
				}
				label.SetText(text)
				stack.SetVisibleChildName("text")
			})
		}()
	}
	refresh.ConnectClicked(build)
	if generate || a.digest == "" {
		build()
	} else {
		label.SetText(a.digest)
		stack.SetVisibleChildName("text")
	}
	d.Present(a.win)
}

// ---- Keyboard shortcuts ------------------------------------------------------

func (a *App) showShortcuts() {
	d := adw.NewDialog()
	d.SetTitle("Klávesové zkratky")
	d.SetContentWidth(520)
	d.SetContentHeight(680)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())
	p := adw.NewPreferencesPage()
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{"Obecné", [][2]string{
			{"<Control>n", "Nová zpráva"},
			{"<Control>f", "Hledat"},
			{"<Control>j", "Zeptat se pošty (AI)"},
			{"<Control><Shift>k", "Kontakty"},
			{"F5", "Obnovit"},
			{"<Control>comma", "Předvolby"},
			{"<Control>question", "Klávesové zkratky"},
			{"<Control>q", "Ukončit"},
		}},
		{"Zprávy", [][2]string{
			{"<Control>r", "Odpovědět"},
			{"<Control><Shift>r", "Odpovědět všem"},
			{"<Control>l", "Přeposlat"},
			{"<Control>e", "Archivovat"},
			{"Delete", "Do koše"},
			{"<Control>d", "Hvězdička"},
			{"<Control>h", "Odložit"},
			{"<Control><Shift>u", "Označit jako nepřečtené"},
		}},
		{"Seznam zpráv", [][2]string{
			{"<Control>a", "Vybrat všechny zprávy"},
			{"Escape", "Zrušit výběr"},
		}},
		{"Psaní zprávy", [][2]string{
			{"<Control>Return", "Odeslat"},
			{"<Control>s", "Uložit koncept"},
			{"<Control>b", "Tučně"},
			{"<Control>i", "Kurzíva"},
			{"<Control>u", "Podtržení"},
			{"<Control>k", "Odkaz"},
			{"Escape", "Zavřít (koncept se uloží)"},
		}},
	}
	for _, g := range groups {
		grp := adw.NewPreferencesGroup()
		grp.SetTitle(g.title)
		for _, k := range g.keys {
			r := adw.NewActionRow()
			r.SetTitle(k[1])
			sl := gtk.NewShortcutLabel(k[0])
			sl.SetVAlign(gtk.AlignCenter)
			r.AddSuffix(sl)
			grp.Add(r)
		}
		p.Add(grp)
	}
	mouse := adw.NewPreferencesGroup()
	mouse.SetTitle("Myš")
	for _, t := range [][2]string{
		{"Ctrl/Shift + klik", "Výběr více zpráv"},
		{"Přetažení zprávy na složku", "Přesunout do složky"},
		{"Přetažení souborů do okna zprávy", "Přiložit soubory"},
	} {
		r := adw.NewActionRow()
		r.SetTitle(t[1])
		l := gtk.NewLabel(t[0])
		l.AddCSSClass("dim-label")
		r.AddSuffix(l)
		mouse.Add(r)
	}
	p.Add(mouse)
	tv.SetContent(p)
	d.SetChild(tv)
	d.Present(a.win)
}
