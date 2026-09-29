package ui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/config"
)

// Version is set at build time (-ldflags "-X .../internal/ui.Version=…").
var Version = "0.1.0-dev"

const releaseNotes = `<p>První testovací verze.</p>
<ul>
<li>Proton Mail přes nativní API (SRP, 2FA, ověření člověka), bez Proton Bridge</li>
<li>PGP: ověřování podpisů, šifrování pro externí příjemce, správa klíčů</li>
<li>Vlákna, HTML zprávy s blokováním vzdáleného obsahu, formátovaný editor</li>
<li>AI asistent a spamfiltr (Claude, ChatGPT, Gemini), blocklisty každých 12 hodin</li>
<li>Lokální AI přes Ollamu s automatickými aktualizacemi – obsah zpráv neopouští počítač</li>
<li>Více účtů, běh na pozadí s ikonou v liště, naplánované odeslání, odložení zpráv</li>
<li>Pozvánky do kalendáře, automatická odpověď, nová pošta až po kontrole spamfiltrem</li>
<li>Šifrovaná offline cache, pravidla, zpoždění odeslání, odhlášení odběru, tisk a export .eml</li>
</ul>`

func (a *App) showAbout() {
	d := adw.NewAboutDialog()
	d.SetApplicationName("Klient")
	d.SetApplicationIcon(AppID)
	d.SetVersion(Version)
	d.SetDeveloperName("Libor Macák")
	d.SetDevelopers([]string{"Libor Macák https://libormacak.eu"})
	d.SetWebsite("https://libormacak.eu")
	d.SetCopyright("© 2026 Libor Macák")
	d.SetComments("E-mailový klient pro GNOME a Proton Mail s nativním PGP, AI asistentem a lokálním spamfiltrem řízeným AI.")
	d.SetSupportURL("mailto:jsem@libormacak.eu?subject=Klient%20" + Version)
	d.SetIssueURL("mailto:jsem@libormacak.eu?subject=Chyba%20v%20Klientovi%20" + Version)
	d.SetLicenseType(gtk.LicenseGPL30)
	d.SetReleaseNotesVersion(Version)
	d.SetReleaseNotes(releaseNotes)

	d.AddLink("Napsat autorovi: jsem@libormacak.eu", "mailto:jsem@libormacak.eu?subject=Klient%20"+Version)
	d.AddLink("Proton Mail", "https://proton.me/mail")
	d.AddLink("Zásady ochrany soukromí Google Gemini API", "https://ai.google.dev/gemini-api/terms")

	d.AddCreditSection("Postaveno na", []string{
		"GTK 4 a libadwaita https://gnome.org",
		"WebKitGTK https://webkitgtk.org",
		"go-proton-api a GopenPGP (Proton AG) https://github.com/ProtonMail",
	})
	d.AddLegalSection("go-proton-api, GopenPGP", "© Proton AG", gtk.LicenseMITX11, "")
	d.AddLegalSection("gotk4, gotk4-adwaita, gotk4-webkitgtk", "© diamondburned", gtk.LicenseMPL20, "")
	d.AddLegalSection("Anthropic Go SDK", "© Anthropic", gtk.LicenseMITX11, "")
	d.AddLegalSection("OpenAI Go SDK, Google GenAI SDK", "© OpenAI, © Google", gtk.LicenseApache20, "")
	d.AddLegalSection("Ollama", "© Ollama", gtk.LicenseMITX11, "")
	d.AddLegalSection("fyne.io/systray", "© Fyne.io", gtk.LicenseApache20, "")
	d.AddLegalSection("klauspost/compress", "© Klaus Post", gtk.LicenseBSD3, "")
	d.AddLegalSection("modernc.org/sqlite", "© The Sqlite Authors", gtk.LicenseBSD3, "")
	d.AddLegalSection("Blocklisty", "Spamhaus DROP, URLhaus (abuse.ch), OpenPhish", gtk.LicenseCustom,
		"Data blocklistů podléhají podmínkám jejich poskytovatelů a jsou stahována pro osobní, nekomerční použití.")

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
	fmt.Fprintf(&sb, "Konfigurace: %s\nData: %s\n", config.ConfigDir(), config.DataDir())
	fmt.Fprintf(&sb, "AI asistent: %s / %s\nAI spamfiltr: %s / %s (zapnutý: %v)\n",
		a.cfg.AssistantProvider, a.cfg.AssistantModel, a.cfg.SpamProvider, a.cfg.SpamModel, a.cfg.SpamFilterEnabled)
	fmt.Fprintf(&sb, "Vlákna: %v, offline zpráv: %d, zpoždění odeslání: %d s\n", a.cfg.Threads, a.cfg.OfflineMessages, a.cfg.SendDelay)
	if a.lists != nil {
		nets, domains := a.lists.Stats()
		fmt.Fprintf(&sb, "Blocklisty: %d sítí, %d domén, aktualizace %s\n", nets, domains, a.lists.LastUpdate.Format("2006-01-02 15:04"))
	}
	if a.acc != nil && a.acc.Cache() != nil {
		m, b := a.acc.Cache().Stats()
		fmt.Fprintf(&sb, "Cache: %d zpráv, %d těl\n", m, b)
	}
	return sb.String()
}
