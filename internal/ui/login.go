package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-proton-api"
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/libormacak/klient/internal/mailbox"
	"github.com/libormacak/klient/internal/protonmail"
)

// showLogin shows the login form. username prefills the login name (for an
// expired session). With other accounts logged in, it has a way back.
func (a *App) showLogin(errMsg, username string) {
	a.mv = nil
	sp := adw.NewStatusPage()
	sp.SetIconName("mail-unread-symbolic")
	sp.SetTitle("Proton Mail")
	sp.SetDescription("Přihlaste se svým účtem Proton. Heslo se nikam neukládá, v klíčence systému zůstane jen token relace a odvozený klíč schránky.")
	if len(a.sessions) > 0 {
		sp.SetTitle("Přidat účet Proton")
	}

	group := adw.NewPreferencesGroup()
	user := adw.NewEntryRow()
	user.SetTitle("Uživatelské jméno nebo e-mail")
	user.SetText(username)
	pass := adw.NewPasswordEntryRow()
	pass.SetTitle("Heslo")
	group.Add(user)
	group.Add(pass)

	errLabel := gtk.NewLabel(errMsg)
	errLabel.AddCSSClass("error")
	errLabel.SetWrap(true)
	errLabel.SetVisible(errMsg != "")

	btn := gtk.NewButtonWithLabel("Přihlásit se")
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
		btn.SetLabel("Přihlásit se")
	}
	var attempt func(hv *proton.APIHVDetails)
	attempt = func(hv *proton.APIHVDetails) {
		username := strings.TrimSpace(user.Text())
		password := []byte(pass.Text())
		if username == "" || len(password) == 0 {
			return
		}
		btn.SetSensitive(false)
		btn.SetLabel("Přihlašování…")
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
							fail(fmt.Errorf("přihlášení zrušeno – ověření nebylo dokončeno"))
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

	other := gtk.NewButtonWithLabel("Jiná služba – Gmail, Seznam, IMAP…")
	other.AddCSSClass("flat")
	other.SetHAlign(gtk.AlignCenter)
	other.ConnectClicked(a.showAddAccount)
	box.Append(other)

	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	hb.SetShowTitle(false)
	if len(a.sessions) > 0 {
		back := gtk.NewButtonWithLabel("Zpět")
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
	d := adw.NewAlertDialog("Ověření v prohlížeči",
		"Proton chce ověřit, že se přihlašuje člověk. Otevře se jeho stránka verify.proton.me – vyřešte tam ověření a pak se sem vraťte a klikněte na „Pokračovat“.")
	d.AddResponse("cancel", "Zrušit")
	d.AddResponse("open", "Otevřít ověření")
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
			c := adw.NewAlertDialog("Dokončili jste ověření?",
				"Po úspěšném ověření v prohlížeči klikněte na „Pokračovat“ a přihlášení proběhne znovu.\n\nStránka se neotevřela? Adresa:\n"+hvErr.URL())
			c.SetBodyUseMarkup(false)
			c.AddResponse("cancel", "Zrušit")
			c.AddResponse("continue", "Pokračovat")
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
			a.askText("Heslo schránky", "Váš účet používá samostatné heslo pro odemčení schránky.", true, func(v string, ok bool) {
				if !ok {
					fail(fmt.Errorf("přihlášení zrušeno"))
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
	a.askText("Dvoufázové ověření", "Zadejte šestimístný kód z ověřovací aplikace.", false, func(code string, ok bool) {
		if !ok {
			fail(fmt.Errorf("přihlášení zrušeno"))
			return
		}
		go func() {
			err := pl.SubmitTOTP(a.ctx, strings.TrimSpace(code))
			ui(func() {
				if err != nil {
					fail(fmt.Errorf("neplatný kód: %w", err))
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
	d.AddResponse("cancel", "Zrušit")
	d.AddResponse("ok", "Pokračovat")
	d.SetResponseAppearance("ok", adw.ResponseSuggested)
	d.SetDefaultResponse("ok")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) { done(text(), r == "ok") })
	d.Present(a.win)
}
