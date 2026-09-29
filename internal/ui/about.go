package ui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
)

// Version is set at build time (-ldflags "-X .../internal/ui.Version=…").
var Version = "0.1.0-dev"

func (a *App) showAbout() {
	d := adw.NewAboutDialog()
	d.SetApplicationName("Klient")
	d.SetApplicationIcon(AppID)
	d.SetVersion(Version)
	d.SetDeveloperName("Libor Macák")
	d.SetDevelopers([]string{"Libor Macák https://libormacak.eu"})
	d.SetWebsite("https://libormacak.eu")
	d.SetCopyright("© 2026 Libor Macák")
	d.SetComments(i18n.T("An email client for GNOME and Proton Mail with native PGP, an AI assistant and a local AI-driven spam filter."))
	d.SetSupportURL("mailto:jsem@libormacak.eu?subject=Klient%20" + Version)
	d.SetIssueURL("https://github.com/Imbecile6197/klient/issues")
	d.SetLicenseType(gtk.LicenseGPL30)
	d.AddLink(i18n.T("Source Code on GitHub"), "https://github.com/Imbecile6197/klient")
	if notes := releaseNotes(); notes != "" {
		d.SetReleaseNotesVersion(Version)
		d.SetReleaseNotes(notes)
	}

	d.AddLink(i18n.T("Write to the author: jsem@libormacak.eu"), "mailto:jsem@libormacak.eu?subject=Klient%20"+Version)
	d.AddLink("Proton Mail", "https://proton.me/mail")
	d.AddLink(i18n.T("Google Gemini API Privacy Terms"), "https://ai.google.dev/gemini-api/terms")

	d.AddCreditSection(i18n.T("Built With"), []string{
		i18n.T("GTK 4 and libadwaita https://gnome.org"),
		"WebKitGTK https://webkitgtk.org",
		i18n.T("go-proton-api and GopenPGP (Proton AG) https://github.com/ProtonMail"),
	})
	d.AddLegalSection("go-proton-api, GopenPGP", "© Proton AG", gtk.LicenseMITX11, "")
	d.AddLegalSection("gotk4, gotk4-adwaita, gotk4-webkitgtk", "© diamondburned", gtk.LicenseMPL20, "")
	d.AddLegalSection("Anthropic Go SDK", "© Anthropic", gtk.LicenseMITX11, "")
	d.AddLegalSection("OpenAI Go SDK, Google GenAI SDK", "© OpenAI, © Google", gtk.LicenseApache20, "")
	d.AddLegalSection("Ollama", "© Ollama", gtk.LicenseMITX11, "")
	d.AddLegalSection("fyne.io/systray", "© Fyne.io", gtk.LicenseApache20, "")
	d.AddLegalSection("klauspost/compress", "© Klaus Post", gtk.LicenseBSD3, "")
	d.AddLegalSection("modernc.org/sqlite", "© The Sqlite Authors", gtk.LicenseBSD3, "")
	d.AddLegalSection(i18n.T("Blocklists"), "Spamhaus DROP, URLhaus (abuse.ch), OpenPhish", gtk.LicenseCustom,
		i18n.T("The blocklist data is subject to the terms of its providers and is downloaded for personal, non-commercial use."))

	d.SetDebugInfo(a.debugInfo())
	d.Present(a.win)
}

// debugInfo is shown under "Troubleshooting" and can be copied into a bug report.
func (a *App) debugInfo() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Klient %s\n", Version)
	fmt.Fprintf(&sb, "Go %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&sb, "GTK %d.%d.%d\n", gtk.GetMajorVersion(), gtk.GetMinorVersion(), gtk.GetMicroVersion())
	fmt.Fprintf(&sb, "libadwaita %d.%d.%d\n", adw.GetMajorVersion(), adw.GetMinorVersion(), adw.GetMicroVersion())
	fmt.Fprintf(&sb, "WebKitGTK %d.%d.%d\n", webkit.GetMajorVersion(), webkit.GetMinorVersion(), webkit.GetMicroVersion())
	fmt.Fprintf(&sb, "Configuration: %s\nData: %s\n", config.ConfigDir(), config.DataDir())
	fmt.Fprintf(&sb, "AI assistant: %s / %s\nAI spam filter: %s / %s (enabled: %v)\n",
		a.cfg.AssistantProvider, a.cfg.AssistantModel, a.cfg.SpamProvider, a.cfg.SpamModel, a.cfg.SpamFilterEnabled)
	fmt.Fprintf(&sb, "Threads: %v, offline messages: %d, send delay: %d s\n", a.cfg.Threads, a.cfg.OfflineMessages, a.cfg.SendDelay)
	if a.lists != nil {
		nets, domains := a.lists.Stats()
		fmt.Fprintf(&sb, "Blocklists: %d networks, %d domains, updated %s\n", nets, domains, a.lists.LastUpdate.Format("2006-01-02 15:04"))
	}
	if a.acc != nil && a.acc.Cache() != nil {
		m, b := a.acc.Cache().Stats()
		fmt.Fprintf(&sb, "Cache: %d messages, %d bodies\n", m, b)
	}
	return sb.String()
}
