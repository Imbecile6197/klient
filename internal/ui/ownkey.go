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
			create := adw.NewButtonRow()
			create.SetTitle(i18n.T("Create a New Key"))
			create.SetStartIconName("list-add-symbolic")
			create.ConnectActivated(func() {
				create.SetSensitive(false)
				go func() {
					err := pgp.GenerateOwnKey(a.acc.DisplayName(), email)
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
