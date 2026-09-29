package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-proton-api"
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// showLogin shows the login form. username prefills the login name (for an
// expired session). With other accounts logged in, it has a way back.
func (a *App) showLogin(errMsg, username string) {
	a.mv = nil
	sp := adw.NewStatusPage()
	sp.SetIconName("mail-unread-symbolic")
	sp.SetTitle(i18n.T("Proton Mail"))
	sp.SetDescription(i18n.T("Log in with your Proton account. The password is not stored anywhere; only the session token and a derived mailbox key stay in the system keyring."))
	if len(a.sessions) > 0 {
		sp.SetTitle(i18n.T("Add a Proton Account"))
	}

	group := adw.NewPreferencesGroup()
	user := adw.NewEntryRow()
	user.SetTitle(i18n.T("Username or email"))
	user.SetText(username)
	pass := adw.NewPasswordEntryRow()
	pass.SetTitle(i18n.T("Password"))
	group.Add(user)
	group.Add(pass)

	errLabel := gtk.NewLabel(errMsg)
	errLabel.AddCSSClass("error")
	errLabel.SetWrap(true)
	errLabel.SetVisible(errMsg != "")

	btn := gtk.NewButtonWithLabel(i18n.T("Log In"))
	btn.AddCSSClass("suggested-action")
	btn.AddCSSClass("pill")
	btn.SetHAlign(gtk.AlignCenter)

	box := gtk.NewBox(gtk.OrientationVertical, 24)
	box.Append(group)
	box.Append(errLabel)
	box.Append(btn)
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(420)
	clamp.SetChild(box)
	sp.SetChild(clamp)

	fail := func(err error) {
		errLabel.SetText(err.Error())
		errLabel.SetVisible(true)
		btn.SetSensitive(true)
		btn.SetLabel(i18n.T("Log In"))
	}
	var attempt func(hv *proton.APIHVDetails)
	attempt = func(hv *proton.APIHVDetails) {
		username := strings.TrimSpace(user.Text())
		password := []byte(pass.Text())
		if username == "" || len(password) == 0 {
			return
		}
		btn.SetSensitive(false)
		btn.SetLabel(i18n.T("Logging in…"))
		errLabel.SetVisible(false)
		go func() {
			pl, err := protonmail.Login(a.ctx, a.mgr, username, password, hv)
			ui(func() {
				var hvErr *protonmail.HVRequiredError
				if errors.As(err, &hvErr) {
					a.humanVerification(hvErr, func(ok bool) {
						if ok {
							attempt(hvErr.HV)
						} else {
							fail(errors.New(i18n.T("login cancelled – the verification was not completed")))
						}
					})
					return
				}
				if err != nil {
					fail(err)
					return
				}
				a.continueLogin(pl, fail)
			})
		}()
	}
	submit := func() { attempt(nil) }
	btn.ConnectClicked(submit)
	pass.ConnectEntryActivated(submit)
	user.ConnectEntryActivated(func() { pass.GrabFocus() })

	other := gtk.NewButtonWithLabel(i18n.T("Other Service – Gmail, Seznam, IMAP…"))
	other.AddCSSClass("flat")
	other.SetHAlign(gtk.AlignCenter)
	other.ConnectClicked(a.showAddAccount)
	box.Append(other)

	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	hb.SetShowTitle(false)
	if len(a.sessions) > 0 {
		back := gtk.NewButtonWithLabel(i18n.T("Back"))
		back.ConnectClicked(func() { a.switchAccount(a.sessions[0].acc) })
		hb.PackStart(back)
	}
	tv.AddTopBar(hb)
	tv.SetContent(sp)
	a.toasts.SetChild(tv)
	if username != "" {
		pass.GrabFocus()
	} else {
		user.GrabFocus()
	}
}

// humanVerification opens Proton's verification page in the browser; the user
// solves the CAPTCHA there and then confirms here so the login is retried.
func (a *App) humanVerification(hvErr *protonmail.HVRequiredError, done func(bool)) {
	d := adw.NewAlertDialog(i18n.T("Verification in the Browser"),
		i18n.T("Proton wants to verify that a human is logging in. Its page verify.proton.me will open – complete the verification there, then come back and click “Continue”."))
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("open", i18n.T("Open Verification"))
	d.SetResponseAppearance("open", adw.ResponseSuggested)
	d.SetDefaultResponse("open")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "open" {
			done(false)
			return
		}
		gtk.NewURILauncher(hvErr.URL()).Launch(context.Background(), a.gtkWindow(), nil)
		// Second dialog, shown after the first one has closed.
		ui(func() {
			c := adw.NewAlertDialog(i18n.T("Have You Completed the Verification?"),
				i18n.T("After the verification in the browser succeeds, click “Continue” and the login will be repeated.")+"\n\n"+i18n.T("Did the page not open? Its address:")+"\n"+hvErr.URL())
			c.SetBodyUseMarkup(false)
			c.AddResponse("cancel", i18n.T("Cancel"))
			c.AddResponse("continue", i18n.T("Continue"))
			c.SetResponseAppearance("continue", adw.ResponseSuggested)
			c.SetDefaultResponse("continue")
			c.SetCloseResponse("cancel")
			c.ConnectResponse(func(r string) { done(r == "continue") })
			c.Present(a.win)
		})
	})
	d.Present(a.win)
}

// continueLogin walks through TOTP and mailbox password prompts as needed.
func (a *App) continueLogin(pl *protonmail.PendingLogin, fail func(error)) {
	finish := func(mailboxPass []byte) {
		go func() {
			acc, err := pl.Finish(a.ctx, mailboxPass)
			ui(func() {
				if err != nil {
					fail(err)
					return
				}
				a.onLoggedIn(mailbox.Proton(acc))
			})
		}()
	}
	afterTOTP := func() {
		if pl.NeedsMailboxPassword() {
			a.askText(i18n.T("Mailbox Password"), i18n.T("Your account uses a separate password to unlock the mailbox."), true, func(v string, ok bool) {
				if !ok {
					fail(errors.New(i18n.T("login cancelled")))
					return
				}
				finish([]byte(v))
			})
			return
		}
		finish(nil)
	}
	if !pl.NeedsTOTP() {
		afterTOTP()
		return
	}
	a.askText(i18n.T("Two-Factor Authentication"), i18n.T("Enter the six-digit code from your authenticator app."), false, func(code string, ok bool) {
		if !ok {
			fail(errors.New(i18n.T("login cancelled")))
			return
		}
		go func() {
			err := pl.SubmitTOTP(a.ctx, strings.TrimSpace(code))
			ui(func() {
				if err != nil {
					fail(fmt.Errorf(i18n.T("invalid code: %w"), err))
					return
				}
				afterTOTP()
			})
		}()
	})
}

// askText shows a modal dialog with one text entry.
func (a *App) askText(heading, body string, secret bool, done func(string, bool)) {
	d := adw.NewAlertDialog(heading, body)
	var text func() string
	var entry gtk.Widgetter
	if secret {
		e := gtk.NewPasswordEntry()
		e.SetShowPeekIcon(true)
		e.SetObjectProperty("activates-default", true)
		text, entry = e.Text, e
	} else {
		e := gtk.NewEntry()
		e.SetActivatesDefault(true)
		e.SetInputPurpose(gtk.InputPurposeDigits)
		text, entry = e.Text, e
	}
	d.SetExtraChild(entry)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("ok", i18n.T("Continue"))
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetDefaultResponse("ok")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) { done(text(), r == "ok") })
	d.Present(a.win)
}
