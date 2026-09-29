package ui

import (
	"net/mail"
	"strings"
	"unicode"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"golang.org/x/text/unicode/norm"

	"github.com/libormacak/klient/internal/protonmail"
)

// fold lowercases and strips diacritics, so "macak" finds "Macák".
func fold(s string) string {
	var sb strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// matchContacts returns up to max entries whose name or address contains
// the query; names starting with it come first.
func matchContacts(all []protonmail.Contact, query string, max int) []protonmail.Contact {
	q := fold(query)
	var first, rest []protonmail.Contact
	for _, c := range all {
		name, email := fold(c.Name), fold(c.Email)
		switch {
		case strings.HasPrefix(name, q) || strings.HasPrefix(email, q):
			first = append(first, c)
		case strings.Contains(name, q) || strings.Contains(email, q):
			rest = append(rest, c)
		}
	}
	out := append(first, rest...)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// addressBook merges Proton contacts with recent correspondents (deduplicated
// by address). Call it off the UI thread: it may hit the network.
func (a *App) addressBook() []protonmail.Contact {
	contacts, _ := a.acc.Contacts(a.ctx)
	seen := map[string]bool{}
	var out []protonmail.Contact
	for _, c := range contacts {
		seen[strings.ToLower(c.Email)] = true
		out = append(out, c)
	}
	for _, c := range a.acc.RecentAddresses(300) {
		if !seen[strings.ToLower(c.Email)] {
			seen[strings.ToLower(c.Email)] = true
			out = append(out, c)
		}
	}
	return out
}

// openContacts shows the Proton address book: search, write, add, delete.
func (a *App) openContacts() {
	if a.acc == nil {
		return
	}
	d := adw.NewDialog()
	d.SetTitle("Kontakty")
	d.SetContentWidth(520)
	d.SetContentHeight(680)
	toasts := adw.NewToastOverlay()
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	add := gtk.NewButtonFromIconName("list-add-symbolic")
	add.SetTooltipText("Nový kontakt")
	hb.PackStart(add)
	tv.AddTopBar(hb)

	search := gtk.NewSearchEntry()
	search.SetPlaceholderText("Hledat jméno nebo adresu")
	search.SetMarginStart(12)
	search.SetMarginEnd(12)
	search.SetMarginTop(6)
	search.SetMarginBottom(6)
	tv.AddTopBar(search)

	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list")
	list.SetSelectionMode(gtk.SelectionNone)
	list.SetMarginStart(12)
	list.SetMarginEnd(12)
	list.SetMarginBottom(12)
	list.SetVAlign(gtk.AlignStart)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetChild(list)
	stack := gtk.NewStack()
	stack.AddNamed(spinnerBox(), "loading")
	empty := adw.NewStatusPage()
	empty.SetIconName("avatar-default-symbolic")
	empty.SetTitle("Žádné kontakty")
	stack.AddNamed(empty, "empty")
	stack.AddNamed(sw, "list")
	tv.SetContent(stack)
	toasts.SetChild(tv)
	d.SetChild(toasts)

	var contacts []protonmail.Contact
	var render func()
	render = func() {
		clearListBox(list)
		shown := contacts
		if q := strings.TrimSpace(search.Text()); q != "" {
			shown = matchContacts(contacts, q, len(contacts))
		}
		for _, c := range shown {
			c := c
			row := adw.NewActionRow()
			row.SetTitle(orDefault(c.Name, c.Email))
			row.SetSubtitle(c.Email)
			row.AddPrefix(adw.NewAvatar(36, orDefault(c.Name, c.Email), true))
			write := gtk.NewButtonFromIconName("mail-message-new-symbolic")
			write.SetTooltipText("Napsat zprávu")
			write.SetVAlign(gtk.AlignCenter)
			write.AddCSSClass("flat")
			write.ConnectClicked(func() {
				d.Close()
				a.composer(&protonmail.Draft{SignExternal: true, To: []*mail.Address{{Name: c.Name, Address: c.Email}}}, nil, protonmail.ActionNew)
			})
			del := gtk.NewButtonFromIconName("user-trash-symbolic")
			del.SetTooltipText("Smazat kontakt")
			del.SetVAlign(gtk.AlignCenter)
			del.AddCSSClass("flat")
			del.ConnectClicked(func() {
				confirm := adw.NewAlertDialog("Smazat kontakt?", orDefault(c.Name, c.Email)+" bude odstraněn z adresáře Protonu.")
				confirm.AddResponse("cancel", "Zrušit")
				confirm.AddResponse("delete", "Smazat")
				confirm.SetResponseAppearance("delete", adw.ResponseDestructive)
				confirm.SetCloseResponse("cancel")
				confirm.ConnectResponse(func(r string) {
					if r != "delete" {
						return
					}
					go func() {
						err := a.acc.DeleteContact(a.ctx, c.ContactID)
						ui(func() {
							if err != nil {
								toasts.AddToast(adw.NewToast("Smazání selhalo: " + err.Error()))
								return
							}
							var keep []protonmail.Contact
							for _, x := range contacts {
								if x.ContactID != c.ContactID {
									keep = append(keep, x)
								}
							}
							contacts = keep
							render()
						})
					}()
				})
				confirm.Present(d)
			})
			row.AddSuffix(write)
			row.AddSuffix(del)
			list.Append(row)
		}
		if len(contacts) == 0 {
			stack.SetVisibleChildName("empty")
		} else {
			stack.SetVisibleChildName("list")
		}
	}
	load := func() {
		stack.SetVisibleChildName("loading")
		go func() {
			c, err := a.acc.Contacts(a.ctx)
			ui(func() {
				if err != nil {
					toasts.AddToast(adw.NewToast("Kontakty se nepodařilo načíst: " + err.Error()))
				}
				contacts = c
				render()
			})
		}()
	}
	search.ConnectSearchChanged(render)
	add.ConnectClicked(func() {
		a.addContactDialog(d, "", "", func() { load() })
	})
	load()
	d.Present(a.win)
}

// addContactDialog asks for name and address and saves a new contact.
func (a *App) addContactDialog(parent gtk.Widgetter, name, email string, done func()) {
	d := adw.NewAlertDialog("Nový kontakt", "Kontakt se uloží do adresáře Protonu (podepsaná vCard).")
	group := adw.NewPreferencesGroup()
	nameRow := adw.NewEntryRow()
	nameRow.SetTitle("Jméno")
	nameRow.SetText(name)
	emailRow := adw.NewEntryRow()
	emailRow.SetTitle("E-mail")
	emailRow.SetText(email)
	group.Add(nameRow)
	group.Add(emailRow)
	d.SetExtraChild(group)
	d.AddResponse("cancel", "Zrušit")
	d.AddResponse("save", "Uložit")
	d.SetResponseAppearance("save", adw.ResponseSuggested)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "save" {
			return
		}
		addr, err := mail.ParseAddress(strings.TrimSpace(emailRow.Text()))
		if err != nil {
			a.toast("Neplatná e-mailová adresa")
			return
		}
		n := strings.TrimSpace(nameRow.Text())
		go func() {
			err := a.acc.AddContact(a.ctx, n, addr.Address)
			ui(func() {
				if err != nil {
					a.toast("Kontakt se nepodařilo uložit: " + err.Error())
					return
				}
				a.toast("Kontakt uložen")
				if done != nil {
					done()
				}
			})
		}()
	})
	d.Present(parent)
}
