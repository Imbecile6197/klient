package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/secrets"
)

// showAddAccount lets the user pick the service of a new account.
func (a *App) showAddAccount() {
	d := adw.NewDialog()
	d.SetTitle(i18n.T("Add Account"))
	d.SetContentWidth(520)
	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())
	grid := gtk.NewFlowBox()
	grid.SetSelectionMode(gtk.SelectionNone)
	grid.SetMaxChildrenPerLine(2)
	grid.SetMinChildrenPerLine(2)
	grid.SetHomogeneous(true)
	grid.SetRowSpacing(12)
	grid.SetColumnSpacing(12)
	grid.SetMarginTop(12)
	grid.SetMarginBottom(24)
	grid.SetMarginStart(24)
	grid.SetMarginEnd(24)
	tile := func(kind mailbox.Kind, title, sub string, open func()) {
		b := gtk.NewButton()
		b.AddCSSClass("provider-tile")
		box := gtk.NewBox(gtk.OrientationVertical, 8)
		box.SetMarginTop(16)
		box.SetMarginBottom(16)
		box.Append(providerLogo(kind, 48))
		t := gtk.NewLabel(title)
		t.AddCSSClass("heading")
		s := gtk.NewLabel(sub)
		s.AddCSSClass("dim-label")
		s.AddCSSClass("caption")
		s.SetWrap(true)
		s.SetJustify(gtk.JustifyCenter)
		box.Append(t)
		box.Append(s)
		b.SetChild(box)
		b.ConnectClicked(func() {
			d.Close()
			open()
		})
		grid.Append(b)
	}
	tile(mailbox.KindProton, "Proton Mail", i18n.T("Encrypted mail, full support"), func() { a.showLogin("", "") })
	tile(mailbox.KindGmail, "Gmail", i18n.T("With an app password"), func() { a.imapLogin(config.MailServer{Kind: "gmail"}) })
	tile(mailbox.KindSeznam, "Seznam.cz", "seznam.cz, email.cz, post.cz", func() { a.imapLogin(config.MailServer{Kind: "seznam"}) })
	tile(mailbox.KindIMAP, i18n.T("Other Service"), i18n.T("Any mailbox with IMAP and SMTP"), func() { a.imapLogin(config.MailServer{Kind: "imap"}) })
	tv.SetContent(grid)
	d.SetChild(tv)
	d.Present(a.win)
}

// imapLogin asks for the address and password of an IMAP/SMTP account and
// logs in. prefill may carry an existing configuration (password renewal).
func (a *App) imapLogin(prefill config.MailServer) {
	d := adw.NewDialog()
	d.SetContentWidth(520)
	d.SetContentHeight(640)
	kind := mailbox.Kind(prefill.Kind)
	if kind == "" {
		kind = mailbox.KindIMAP
	}
	d.SetTitle(providerName(kind))
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	login := gtk.NewButtonWithLabel(i18n.T("Log In"))
	login.AddCSSClass("suggested-action")
	hb.PackEnd(login)
	tv.AddTopBar(hb)
	toasts := adw.NewToastOverlay()

	page := adw.NewPreferencesPage()
	head := adw.NewPreferencesGroup()
	logo := providerLogo(kind, 64)
	logo.SetMarginBottom(6)
	head.Add(logo)
	page.Add(head)

	g := adw.NewPreferencesGroup()
	email := adw.NewEntryRow()
	email.SetTitle(i18n.T("Email address"))
	email.SetText(prefill.Email)
	name := adw.NewEntryRow()
	name.SetTitle(i18n.T("Your name (as recipients will see it)"))
	name.SetText(prefill.Name)
	pass := adw.NewPasswordEntryRow()
	pass.SetTitle(i18n.T("Password"))
	g.Add(email)
	g.Add(name)
	g.Add(pass)
	var extra *adw.PreferencesGroup
	switch kind {
	case mailbox.KindGmail:
		pass.SetTitle(i18n.T("App password (16 characters)"))
		g.SetDescription(i18n.T("Gmail does not allow logging in with your normal password. Turn on two-step verification in your Google account and create an app password. IMAP access must be enabled in Gmail (Settings → Forwarding and POP/IMAP)."))
		link := gtk.NewButtonWithLabel(i18n.T("Create an App Password"))
		link.AddCSSClass("pill")
		link.SetHAlign(gtk.AlignCenter)
		link.SetMarginTop(12)
		link.ConnectClicked(func() {
			gtk.NewURILauncher("https://myaccount.google.com/apppasswords").Launch(context.Background(), a.gtkWindow(), nil)
		})
		extra = adw.NewPreferencesGroup()
		extra.Add(link)
	case mailbox.KindSeznam:
		pass.SetTitle(i18n.T("Password (an app password with two-factor authentication)"))
		g.SetDescription(i18n.T("Without two-factor authentication, log in with the same password as on the web. With two-factor authentication turned on, Seznam does not let mail programs in with your normal password: in your Seznam account open Security → Two-factor authentication → App password → Set up and enter that password here (it works for IMAP, SMTP and the calendar and must differ from your login password)."))
		link := gtk.NewButtonWithLabel(i18n.T("Open the Seznam Account"))
		link.AddCSSClass("pill")
		link.SetHAlign(gtk.AlignCenter)
		link.ConnectClicked(func() {
			gtk.NewURILauncher("https://ucet.seznam.cz/").Launch(context.Background(), a.gtkWindow(), nil)
		})
		extra = adw.NewPreferencesGroup()
		extra.Add(link)
	default:
		g.SetDescription(i18n.T("Klient looks up the servers from the address (in the Thunderbird database – only the domain is sent). If that fails, fill them in below."))
	}
	page.Add(g)
	if extra != nil {
		page.Add(extra)
	}

	// Errors stay visible in full (a toast would cut them off).
	errLabel := gtk.NewLabel("")
	errLabel.SetWrap(true)
	errLabel.SetXAlign(0)
	errLabel.SetSelectable(true)
	errLabel.AddCSSClass("error")
	errGroup := adw.NewPreferencesGroup()
	errGroup.Add(errLabel)
	errGroup.SetVisible(false)
	page.Add(errGroup)
	showErr := func(msg string) {
		errLabel.SetText(msg)
		errGroup.SetVisible(msg != "")
	}

	// Server settings: found automatically, editable.
	sg := adw.NewPreferencesGroup()
	sg.SetTitle(i18n.T("Servers"))
	servers := adw.NewExpanderRow()
	servers.SetTitle(i18n.T("Server settings"))
	servers.SetSubtitle(i18n.T("Detected automatically"))
	user := adw.NewEntryRow()
	user.SetTitle(i18n.T("Username"))
	imapHost := adw.NewEntryRow()
	imapHost.SetTitle("IMAP server")
	imapPort := adw.NewEntryRow()
	imapPort.SetTitle("IMAP port")
	smtpHost := adw.NewEntryRow()
	smtpHost.SetTitle("SMTP server")
	smtpPort := adw.NewEntryRow()
	smtpPort.SetTitle("SMTP port")
	secNames := []string{"SSL/TLS", "STARTTLS"}
	imapSec := adw.NewComboRow()
	imapSec.SetTitle(i18n.T("IMAP security"))
	imapSec.SetModel(gtk.NewStringList(secNames))
	smtpSec := adw.NewComboRow()
	smtpSec.SetTitle(i18n.T("SMTP security"))
	smtpSec.SetModel(gtk.NewStringList(secNames))
	for _, r := range []gtk.Widgetter{user, imapHost, imapPort, imapSec, smtpHost, smtpPort, smtpSec} {
		servers.AddRow(r)
	}
	sg.Add(servers)
	page.Add(sg)

	secIndex := func(s string) uint {
		if s == "starttls" {
			return 1
		}
		return 0
	}
	fill := func(s config.MailServer) {
		user.SetText(s.Username)
		imapHost.SetText(s.IMAPHost)
		imapPort.SetText(strconv.Itoa(s.IMAPPort))
		imapSec.SetSelected(secIndex(s.IMAPSecurity))
		smtpHost.SetText(s.SMTPHost)
		smtpPort.SetText(strconv.Itoa(s.SMTPPort))
		smtpSec.SetSelected(secIndex(s.SMTPSecurity))
		servers.SetSubtitle(s.IMAPHost + " · " + s.SMTPHost)
	}
	collect := func() (config.MailServer, error) {
		s := config.MailServer{
			Kind:     string(imapmail.KindForEmail(email.Text())),
			Email:    strings.TrimSpace(email.Text()),
			Name:     strings.TrimSpace(name.Text()),
			Username: strings.TrimSpace(user.Text()),
			IMAPHost: strings.TrimSpace(imapHost.Text()),
			SMTPHost: strings.TrimSpace(smtpHost.Text()),
		}
		if prefill.Kind == "gmail" || prefill.Kind == "seznam" {
			s.Kind = prefill.Kind
		}
		var err1, err2 error
		s.IMAPPort, err1 = strconv.Atoi(strings.TrimSpace(imapPort.Text()))
		s.SMTPPort, err2 = strconv.Atoi(strings.TrimSpace(smtpPort.Text()))
		if err1 != nil || err2 != nil {
			return s, errors.New(i18n.T("the port must be a number"))
		}
		s.IMAPSecurity = []string{"ssl", "starttls"}[imapSec.Selected()]
		s.SMTPSecurity = []string{"ssl", "starttls"}[smtpSec.Selected()]
		if s.Username == "" {
			s.Username = s.Email
		}
		return s, nil
	}
	discovered := ""
	if prefill.IMAPHost != "" {
		fill(prefill)
		discovered = prefill.Email
	}
	discover := func(done func()) {
		addr := strings.TrimSpace(email.Text())
		if addr == "" || addr == discovered {
			done()
			return
		}
		servers.SetSubtitle(i18n.T("Looking up the settings…"))
		go func() {
			s, err := imapmail.Discover(a.ctx, addr)
			ui(func() {
				discovered = addr
				fill(s)
				if err != nil {
					servers.SetExpanded(true)
					toasts.AddToast(adw.NewToast(err.Error()))
				}
				done()
			})
		}()
	}
	email.ConnectEntryActivated(func() { discover(func() { name.GrabFocus() }) })

	run := func() {
		if strings.TrimSpace(email.Text()) == "" || pass.Text() == "" {
			toasts.AddToast(adw.NewToast(i18n.T("Fill in the address and password")))
			return
		}
		login.SetSensitive(false)
		login.SetLabel(i18n.T("Logging in…"))
		showErr("")
		discover(func() {
			s, err := collect()
			if err != nil {
				login.SetSensitive(true)
				login.SetLabel(i18n.T("Log In"))
				toasts.AddToast(adw.NewToast(err.Error()))
				return
			}
			password := strings.ReplaceAll(pass.Text(), " ", "") // app passwords are shown with spaces
			if s.Kind != "gmail" {
				password = pass.Text()
			}
			go func() {
				ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
				defer cancel()
				acc, err := imapmail.Open(ctx, s, password)
				if err == nil {
					if serr := imapmail.TestSMTP(ctx, acc); serr != nil {
						acc.Close()
						acc, err = nil, fmt.Errorf(i18n.T("receiving works, but sending does not: %w"), serr)
					}
				}
				ui(func() {
					login.SetSensitive(true)
					login.SetLabel(i18n.T("Log In"))
					if err != nil {
						msg := err.Error()
						if errors.Is(err, imapmail.ErrAuth) {
							switch s.Kind {
							case "gmail":
								msg = i18n.T("Google rejected the login. Use an app password, not your Google account password, and check that IMAP is enabled in Gmail.") + "\n\n" + fmt.Sprintf(i18n.T("Server response: %s"), msg)
							case "seznam":
								msg = i18n.T("Seznam rejected the password. If you have two-factor authentication turned on, enter an app password (Seznam account → Security → Two-factor authentication → App password). After a failed attempt Seznam usually sends you an email with instructions.") + "\n\n" + fmt.Sprintf(i18n.T("Server response: %s"), msg)
							}
						}
						servers.SetExpanded(!errors.Is(err, imapmail.ErrAuth))
						showErr(msg)
						return
					}
					if err := secrets.SaveMailPassword(s.ID(), password); err != nil {
						acc.Close()
						toasts.AddToast(adw.NewToast(i18n.T("The password cannot be saved to the keyring: ") + err.Error()))
						return
					}
					a.saveMailServer(s)
					d.Close()
					a.onLoggedIn(acc)
				})
			}()
		})
	}
	login.ConnectClicked(run)
	pass.ConnectEntryActivated(run)

	toasts.SetChild(page)
	tv.SetContent(toasts)
	d.SetChild(tv)
	d.Present(a.win)
	if prefill.Email != "" {
		pass.GrabFocus()
	} else {
		email.GrabFocus()
	}
}

// saveMailServer stores (or replaces) an IMAP account in the config.
func (a *App) saveMailServer(s config.MailServer) {
	for i, m := range a.cfg.MailServers {
		if m.ID() == s.ID() {
			a.cfg.MailServers[i] = s
			a.saveConfig()
			return
		}
	}
	a.cfg.MailServers = append(a.cfg.MailServers, s)
	a.saveConfig()
}

func (a *App) mailServer(id string) (config.MailServer, bool) {
	for _, m := range a.cfg.MailServers {
		if m.ID() == id {
			return m, true
		}
	}
	return config.MailServer{}, false
}

// openIMAP resumes a saved IMAP account (runs off the UI thread).
func (a *App) openIMAP(ctx context.Context, id string) (mailbox.Account, config.MailServer, error) {
	s, ok := a.mailServerSync(id)
	if !ok {
		return nil, s, fmt.Errorf(i18n.T("the settings of account %s are missing"), strings.TrimPrefix(id, "imap:"))
	}
	pw, err := secrets.LoadMailPassword(id)
	if err != nil {
		return nil, s, err
	}
	if pw == "" {
		return nil, s, imapmail.ErrAuth
	}
	acc, err := imapmail.Open(ctx, s, pw)
	if err != nil {
		return nil, s, err
	}
	return acc, s, nil
}

// mailServerSync reads the config from a goroutine.
func (a *App) mailServerSync(id string) (config.MailServer, bool) {
	type res struct {
		s  config.MailServer
		ok bool
	}
	ch := make(chan res)
	ui(func() {
		s, ok := a.mailServer(id)
		ch <- res{s, ok}
	})
	r := <-ch
	return r.s, r.ok
}
