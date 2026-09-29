package ui

import (
	"context"
	"os"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/libormacak/klient/internal/pgp"
)

// ownKeyGroup manages the PGP key of a non-Proton account: create, import,
// share, back up and remove.
func (a *App) ownKeyGroup(d *adw.PreferencesDialog, page *adw.PreferencesPage) *adw.PreferencesGroup {
	email := a.acc.Email()
	g := adw.NewPreferencesGroup()
	g.SetTitle("Můj klíč – " + email)
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
			d.AddToast(adw.NewToast("Uloženo"))
		})
	}
	refresh = func() {
		for _, r := range rows {
			g.Remove(r)
		}
		rows = nil
		if !pgp.HasOwnKey(email) {
			g.SetDescription("Bez klíče chodí pošta z tohoto účtu nešifrovaně a nepodepsaná. S klíčem Klient šifruje zprávy všem, jejichž klíč zná (WKD, Autocrypt, importované klíče), ostatním je podepíše. Koncepty a kopie v Odeslaných se na serveru uloží zašifrované.")
			create := adw.NewButtonRow()
			create.SetTitle("Vytvořit nový klíč")
			create.SetStartIconName("list-add-symbolic")
			create.ConnectActivated(func() {
				create.SetSensitive(false)
				go func() {
					err := pgp.GenerateOwnKey(a.acc.DisplayName(), email)
					ui(func() {
						create.SetSensitive(true)
						if err != nil {
							d.AddToast(adw.NewToast("Vytvoření klíče selhalo: " + err.Error()))
							return
						}
						d.AddToast(adw.NewToast("Klíč vytvořen – od teď se pošta šifruje a podepisuje"))
						refresh()
					})
				}()
			})
			add(create)
			imp := adw.NewButtonRow()
			imp.SetTitle("Importovat soukromý klíč…")
			imp.SetStartIconName("document-open-symbolic")
			imp.ConnectActivated(func() {
				dlg := gtk.NewFileDialog()
				dlg.SetTitle("Soukromý klíč (.asc)")
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
					a.askText("Heslo ke klíči", "Pokud klíč heslo nemá, nechte pole prázdné.", true, func(pass string, ok bool) {
						if !ok {
							return
						}
						if err := pgp.ImportOwnKey(string(b), pass, email); err != nil {
							d.AddToast(adw.NewToast(err.Error()))
							return
						}
						d.AddToast(adw.NewToast("Klíč importován"))
						refresh()
					})
				})
			})
			add(imp)
			return
		}
		g.SetDescription("Zprávy lidem se známým klíčem se šifrují, ostatním se podepisují. Veřejný klíč posílejte kontaktům (Klient ho přikládá i do hlavičky Autocrypt).")
		pub, k, err := pgp.OwnPublicKey(email)
		row := adw.NewActionRow()
		row.SetTitle("Otisk klíče")
		if err != nil {
			row.SetSubtitle(err.Error())
		} else {
			row.SetSubtitle(formatFP(k.GetFingerprint()))
			row.SetSubtitleSelectable(true)
			row.AddSuffix(button("edit-copy-symbolic", "Kopírovat veřejný klíč", func() {
				a.win.Clipboard().SetText(pub)
				d.AddToast(adw.NewToast("Veřejný klíč zkopírován"))
			}))
			row.AddSuffix(button("document-save-symbolic", "Uložit veřejný klíč", func() { saveFile(email+".asc", pub) }))
		}
		add(row)
		backup := adw.NewButtonRow()
		backup.SetTitle("Zálohovat soukromý klíč…")
		backup.SetStartIconName("drive-harddisk-symbolic")
		backup.ConnectActivated(func() {
			a.askText("Heslo zálohy", "Záloha se zašifruje tímto heslem. Bez něj ji nepůjde obnovit.", true, func(pass string, ok bool) {
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
		del.SetTitle("Odstranit klíč")
		del.SetStartIconName("user-trash-symbolic")
		del.AddCSSClass("destructive-action")
		del.ConnectActivated(func() {
			q := adw.NewAlertDialog("Odstranit klíč?", "Zprávy zašifrované tímto klíčem (i vaše koncepty a odeslané) už Klient bez zálohy klíče neotevře.")
			q.AddResponse("cancel", "Zrušit")
			q.AddResponse("delete", "Odstranit")
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
