package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/imapmail"
)

// certError returns the untrusted-certificate error inside err, if any.
func certError(err error) *imapmail.CertError {
	var ce *imapmail.CertError
	if errors.As(err, &ce) {
		return ce
	}
	return nil
}

// askTrustCert shows a certificate that failed verification and calls trust
// when the user decides to trust it anyway.
func (a *App) askTrustCert(parent gtk.Widgetter, ce *imapmail.CertError, trust func()) {
	d := adw.NewAlertDialog(i18n.T("Untrusted Certificate"), fmt.Sprintf(i18n.T("The certificate of the server %s cannot be verified: %s.\n\nThis happens when a certificate has expired or is self-signed, but also when someone intercepts the connection. Trust it only if you know the server and expect this certificate. Klient will accept this one certificate for the server; if it changes, you will be asked again."), ce.Key(), ce.Reason()))

	d.SetPreferWideLayout(true)
	grid := gtk.NewGrid()
	grid.SetColumnSpacing(12)
	grid.SetRowSpacing(6)
	row := 0
	add := func(title, value string, mono bool) {
		t := gtk.NewLabel(title)
		t.SetXAlign(1)
		t.SetYAlign(0)
		t.AddCSSClass("dim-label")
		v := gtk.NewLabel(value)
		v.SetXAlign(0)
		v.SetWrap(true)
		v.SetWrapMode(pango.WrapWordChar)
		v.SetSelectable(true)
		if mono {
			v.AddCSSClass("monospace")
		}
		grid.Attach(t, 0, row, 1, 1)
		grid.Attach(v, 1, row, 1, 1)
		row++
	}
	c := ce.Cert
	names := c.DNSNames
	if len(names) == 0 {
		names = []string{c.Subject.CommonName}
	}
	date := i18n.T("Jan 2, 2006 15:04")
	add(i18n.T("Issued to"), strings.Join(names, ", "), false)
	add(i18n.T("Issued by"), orDefault(c.Issuer.CommonName, c.Issuer.String()), false)
	add(i18n.T("Valid"), c.NotBefore.Local().Format(date)+" – "+c.NotAfter.Local().Format(date), false)
	add("SHA-256", ce.FingerprintText(), true)
	d.SetExtraChild(grid)

	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("trust", i18n.T("Trust This Certificate"))
	d.SetResponseAppearance("trust", adw.ResponseDestructive)
	d.SetDefaultResponse("cancel")
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "trust" {
			trust()
		}
	})
	d.Present(parent)
}

// trustCert stores the certificate as trusted for a saved account and its
// open session.
func (a *App) trustCert(id string, ce *imapmail.CertError) {
	for i, m := range a.cfg.MailServers {
		if m.ID() != id {
			continue
		}
		t := make(map[string]string, len(m.TrustedCerts)+1)
		for k, v := range m.TrustedCerts {
			t[k] = v
		}
		t[ce.Key()] = ce.Fingerprint
		a.cfg.MailServers[i].TrustedCerts = t
		a.saveConfig()
	}
	for _, s := range a.sessions {
		if acc, ok := s.acc.(*imapmail.Account); ok && acc.Settings().ID() == id {
			acc.TrustCert(ce)
		}
	}
}
