package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/pgp"
	"github.com/Imbecile6197/klient/internal/pgpmime"
)

// ownKeyGroup manages the PGP key of a non-Proton account: create, import,
// share, back up and remove.
func (a *App) ownKeyGroup(d *adw.PreferencesDialog, page *adw.PreferencesPage) *adw.PreferencesGroup {
	email := a.acc.Email()
	g := adw.NewPreferencesGroup()
	g.SetTitle(fmt.Sprintf(i18n.T("My Key – %s"), email))
	var rows []gtk.Widgetter
	var refresh func()
	add := func(w gtk.Widgetter) {
		g.Add(w)
		rows = append(rows, w)
	}
	button := func(icon, tip string, f func()) *gtk.Button {
		b := gtk.NewButtonFromIconName(icon)
		b.SetTooltipText(tip)
		b.SetVAlign(gtk.AlignCenter)
		b.AddCSSClass("flat")
		b.ConnectClicked(f)
		return b
	}
	saveFile := func(name, data string) {
		dlg := gtk.NewFileDialog()
		dlg.SetInitialName(name)
		dlg.Save(context.Background(), a.gtkWindow(), func(res gio.AsyncResulter) {
			file, err := dlg.SaveFinish(res)
			if err != nil || file == nil {
				return
			}
			if err := os.WriteFile(file.Path(), []byte(data), 0o600); err != nil {
				d.AddToast(adw.NewToast(err.Error()))
				return
			}
			d.AddToast(adw.NewToast(i18n.T("Saved")))
		})
	}
	refresh = func() {
		for _, r := range rows {
			g.Remove(r)
		}
		rows = nil
		if !pgp.HasOwnKey(email) {
			g.SetDescription(i18n.T("Without a key, mail from this account goes out unencrypted and unsigned. With a key, Klient encrypts messages to everyone whose key it knows (WKD, Autocrypt, imported keys) and signs them for everyone else. Drafts and copies in Sent are stored encrypted on the server."))
			validity := adw.NewComboRow()
			validity.SetTitle(i18n.T("Validity of a new key"))
			validity.SetSubtitle(i18n.T("An expiring key stops being used by itself if you lose it; you can extend it any time before"))
			validity.SetModel(gtk.NewStringList(validityNames()))
			validity.SetSelected(1)
			add(validity)
			create := adw.NewButtonRow()
			create.SetTitle(i18n.T("Create a New Key"))
			create.SetStartIconName("list-add-symbolic")
			create.ConnectActivated(func() {
				create.SetSensitive(false)
				years := validityYears[validity.Selected()]
				go func() {
					err := pgp.GenerateOwnKeyFor(a.acc.DisplayName(), email, years)
					ui(func() {
						create.SetSensitive(true)
						if err != nil {
							d.AddToast(adw.NewToast(i18n.T("Creating the key failed: ") + err.Error()))
							return
						}
						d.AddToast(adw.NewToast(i18n.T("Key created – from now on your mail is encrypted and signed")))
						refresh()
					})
				}()
			})
			add(create)
			imp := adw.NewButtonRow()
			imp.SetTitle(i18n.T("Import a Private Key…"))
			imp.SetStartIconName("document-open-symbolic")
			imp.ConnectActivated(func() {
				dlg := gtk.NewFileDialog()
				dlg.SetTitle(i18n.T("Private Key (.asc)"))
				dlg.Open(context.Background(), a.gtkWindow(), func(res gio.AsyncResulter) {
					file, err := dlg.OpenFinish(res)
					if err != nil || file == nil {
						return
					}
					b, err := os.ReadFile(file.Path())
					if err != nil {
						d.AddToast(adw.NewToast(err.Error()))
						return
					}
					a.askText(i18n.T("Key Passphrase"), i18n.T("If the key has no passphrase, leave the field empty."), true, func(pass string, ok bool) {
						if !ok {
							return
						}
						if err := pgp.ImportOwnKey(string(b), pass, email); err != nil {
							d.AddToast(adw.NewToast(err.Error()))
							return
						}
						d.AddToast(adw.NewToast(i18n.T("Key imported")))
						refresh()
					})
				})
			})
			add(imp)
			return
		}
		g.SetDescription(i18n.T("Messages to people with a known key are encrypted; the others are signed. Send your public key to your contacts (Klient also adds it to the Autocrypt header)."))
		pub, k, err := pgp.OwnPublicKey(email)
		row := adw.NewActionRow()
		row.SetTitle(i18n.T("Key fingerprint"))
		if err != nil {
			row.SetSubtitle(err.Error())
		} else {
			row.SetSubtitle(formatFP(k.GetFingerprint()))
			row.SetSubtitleSelectable(true)
			row.AddSuffix(button("edit-copy-symbolic", i18n.T("Copy the Public Key"), func() {
				a.win.Clipboard().SetText(pub)
				d.AddToast(adw.NewToast(i18n.T("Public key copied")))
			}))
			row.AddSuffix(button("document-save-symbolic", i18n.T("Save the Public Key"), func() { saveFile(email+".asc", pub) }))
		}
		add(row)
		if err == nil {
			det := pgp.Details(k)
			exp := adw.NewActionRow()
			exp.SetTitle(i18n.T("Valid until"))
			switch {
			case det.Expires.IsZero():
				exp.SetSubtitle(i18n.T("no expiry"))
			case det.Expired:
				exp.SetSubtitle(keyDate(det.Expires) + " – " + i18n.T("expired"))
				exp.AddCSSClass("error")
			case time.Until(det.Expires) < 60*24*time.Hour:
				exp.SetSubtitle(keyDate(det.Expires) + " – " + i18n.T("expires soon, extend it"))
				exp.AddCSSClass("warning")
			default:
				exp.SetSubtitle(keyDate(det.Expires))
			}
			ext := gtk.NewButtonWithLabel(i18n.T("Extend…"))
			ext.SetVAlign(gtk.AlignCenter)
			ext.AddCSSClass("flat")
			ext.ConnectClicked(func() {
				q := adw.NewAlertDialog(i18n.T("Extend the Key"), i18n.T("How long should the key be valid from today? Contacts learn the new date from your next message (Autocrypt) or a new copy of your public key."))
				names := validityNames()
				for i, n := range names {
					q.AddResponse(fmt.Sprint(i), n)
				}
				q.AddResponse("cancel", i18n.T("Cancel"))
				q.SetCloseResponse("cancel")
				q.SetPreferWideLayout(true)
				q.ConnectResponse(func(r string) {
					var i int
					if _, err := fmt.Sscan(r, &i); err != nil || i < 0 || i >= len(validityYears) {
						return
					}
					if err := pgp.SetOwnExpiry(email, validityYears[i]); err != nil {
						d.AddToast(adw.NewToast(err.Error()))
						return
					}
					d.AddToast(adw.NewToast(i18n.T("The key validity was changed")))
					refresh()
				})
				q.Present(d)
			})
			exp.AddSuffix(ext)
			add(exp)

			publish := adw.NewButtonRow()
			publish.SetTitle(i18n.T("Publish on keys.openpgp.org…"))
			publish.SetStartIconName("network-server-symbolic")
			publish.ConnectActivated(func() {
				q := adw.NewAlertDialog(i18n.T("Publish the Public Key?"), fmt.Sprintf(i18n.T("Klient uploads your public key to keys.openpgp.org and the server sends a confirmation e-mail to %s. After you click the link in it, anyone can find your key by your address and send you encrypted mail. Only the public key leaves the computer; you can remove it there any time."), email))
				q.AddResponse("cancel", i18n.T("Cancel"))
				q.AddResponse("publish", i18n.T("Publish"))
				q.SetResponseAppearance("publish", adw.ResponseSuggested)
				q.SetCloseResponse("cancel")
				q.ConnectResponse(func(r string) {
					if r != "publish" {
						return
					}
					publish.SetSensitive(false)
					go func() {
						st, err := pgpmime.Publish(a.ctx, pub, []string{email}, i18n.Lang())
						ui(func() {
							publish.SetSensitive(true)
							switch {
							case err != nil:
								d.AddToast(adw.NewToast(err.Error()))
							case st[email] == "published":
								d.AddToast(adw.NewToast(i18n.T("The key is already published for this address")))
							case st[email] == "pending":
								d.AddToast(adw.NewToast(fmt.Sprintf(i18n.T("Check your mail: keys.openpgp.org sent a confirmation link to %s"), email)))
							default:
								d.AddToast(adw.NewToast(i18n.T("The key was uploaded, but the address could not be confirmed")))
							}
						})
					}()
				})
				q.Present(d)
			})
			add(publish)

			revoke := adw.NewButtonRow()
			revoke.SetTitle(i18n.T("Save a Revocation Certificate…"))
			revoke.SetStartIconName("action-unavailable-symbolic")
			revoke.ConnectActivated(func() {
				q := adw.NewAlertDialog(i18n.T("Revocation Certificate"), i18n.T("If your key is lost or stolen, publish this certificate (keys.openpgp.org) or send it to your contacts: their apps then stop using the key. Keep it somewhere safe – anyone who has it can revoke your key. Saving it does not revoke anything."))
				q.AddResponse("cancel", i18n.T("Cancel"))
				q.AddResponse("save", i18n.T("Save…"))
				q.SetResponseAppearance("save", adw.ResponseSuggested)
				q.SetCloseResponse("cancel")
				q.ConnectResponse(func(r string) {
					if r != "save" {
						return
					}
					cert, err := pgp.RevocationCertificate(email)
					if err != nil {
						d.AddToast(adw.NewToast(err.Error()))
						return
					}
					saveFile(email+"-"+i18n.T("revocation")+"-"+time.Now().Format("2006-01-02")+".asc", cert)
				})
				q.Present(d)
			})
			add(revoke)
		}
		backup := adw.NewButtonRow()
		backup.SetTitle(i18n.T("Back Up the Private Key…"))
		backup.SetStartIconName("drive-harddisk-symbolic")
		backup.ConnectActivated(func() {
			a.askText(i18n.T("Backup Password"), i18n.T("The backup is encrypted with this password. It cannot be restored without it."), true, func(pass string, ok bool) {
				if !ok || pass == "" {
					return
				}
				armored, err := pgp.ExportOwnPrivate(email, pass)
				if err != nil {
					d.AddToast(adw.NewToast(err.Error()))
					return
				}
				saveFile(email+"-soukromy-klic-"+time.Now().Format("2006-01-02")+".asc", armored)
			})
		})
		add(backup)
		del := adw.NewButtonRow()
		del.SetTitle(i18n.T("Remove Key"))
		del.SetStartIconName("user-trash-symbolic")
		del.AddCSSClass("destructive-action")
		del.ConnectActivated(func() {
			q := adw.NewAlertDialog(i18n.T("Remove the Key?"), i18n.T("Without a backup of the key, Klient will no longer open messages encrypted with it (including your drafts and sent mail)."))
			q.AddResponse("cancel", i18n.T("Cancel"))
			q.AddResponse("delete", i18n.C("key", "Remove"))
			q.SetResponseAppearance("delete", adw.ResponseDestructive)
			q.SetCloseResponse("cancel")
			q.ConnectResponse(func(r string) {
				if r != "delete" {
					return
				}
				if err := pgp.DeleteOwnKey(email); err != nil {
					d.AddToast(adw.NewToast(err.Error()))
				}
				refresh()
			})
			q.Present(d)
		})
		add(del)
	}
	refresh()
	return g
}

// Validity choices for a new or extended key.
var validityYears = []int{1, 2, 5, 0}

func validityNames() []string {
	var out []string
	for _, y := range validityYears {
		if y == 0 {
			out = append(out, i18n.T("No expiry"))
		} else {
			out = append(out, fmt.Sprintf(i18n.N("%d year", "%d years", y), y))
		}
	}
	return out
}

// formatFP groups a fingerprint by four characters.
func formatFP(fp string) string {
	var out []byte
	for i, c := range []byte(fp) {
		if i > 0 && i%4 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}
