package ui

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"golang.org/x/text/unicode/norm"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/protonmail"
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
	d.SetTitle(i18n.T("Contacts"))
	d.SetContentWidth(520)
	d.SetContentHeight(680)
	toasts := adw.NewToastOverlay()
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	add := gtk.NewButtonFromIconName("list-add-symbolic")
	add.SetTooltipText(i18n.T("New Contact"))
	hb.PackStart(add)
	tv.AddTopBar(hb)

	search := gtk.NewSearchEntry()
	search.SetPlaceholderText(i18n.T("Search for a name or address"))
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
	empty.SetTitle(i18n.T("No Contacts"))
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
			write.SetTooltipText(i18n.T("Write a Message"))
			write.SetVAlign(gtk.AlignCenter)
			write.AddCSSClass("flat")
			write.ConnectClicked(func() {
				d.Close()
				a.composer(&protonmail.Draft{SignExternal: true, To: []*mail.Address{{Name: c.Name, Address: c.Email}}}, nil, protonmail.ActionNew)
			})
			del := gtk.NewButtonFromIconName("user-trash-symbolic")
			del.SetTooltipText(i18n.T("Delete Contact"))
			del.SetVAlign(gtk.AlignCenter)
			del.AddCSSClass("flat")
			del.ConnectClicked(func() {
				confirm := adw.NewAlertDialog(i18n.T("Delete the Contact?"), fmt.Sprintf(i18n.T("%s will be removed from the Proton address book."), orDefault(c.Name, c.Email)))
				confirm.AddResponse("cancel", i18n.T("Cancel"))
				confirm.AddResponse("delete", i18n.T("Delete"))
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
								toasts.AddToast(adw.NewToast(i18n.T("Deleting failed: ") + err.Error()))
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
					toasts.AddToast(adw.NewToast(i18n.T("The contacts could not be loaded: ") + err.Error()))
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
	d := adw.NewAlertDialog(i18n.T("New Contact"), i18n.T("The contact is saved to the Proton address book (as a signed vCard)."))
	group := adw.NewPreferencesGroup()
	nameRow := adw.NewEntryRow()
	nameRow.SetTitle(i18n.T("Name"))
	nameRow.SetText(name)
	emailRow := adw.NewEntryRow()
	emailRow.SetTitle("Email")
	emailRow.SetText(email)
	group.Add(nameRow)
	group.Add(emailRow)
	d.SetExtraChild(group)
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("save", i18n.T("Save"))
	d.SetResponseAppearance("save", adw.ResponseSuggested)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "save" {
			return
		}
		addr, err := mail.ParseAddress(strings.TrimSpace(emailRow.Text()))
		if err != nil {
			a.toast(i18n.T("Invalid email address"))
			return
		}
		n := strings.TrimSpace(nameRow.Text())
		go func() {
			err := a.acc.AddContact(a.ctx, n, addr.Address)
			ui(func() {
				if err != nil {
					a.toast(i18n.T("The contact could not be saved: ") + err.Error())
					return
				}
				a.toast(i18n.T("Contact saved"))
				if done != nil {
					done()
				}
			})
		}()
	})
	d.Present(parent)
}
