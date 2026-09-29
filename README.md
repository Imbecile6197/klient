# Klient

**A mail client for GNOME with native Proton Mail support, PGP and an AI spam filter.**

[Česky](README.cs.md) · [Documentation](docs/index.html) · [Releases](https://github.com/Imbecile6197/klient/releases) · GPL-3.0

Klient is written in Go with GTK4 and libadwaita. It connects directly to Proton Mail (no Proton Bridge needed), to Gmail, to Seznam.cz and to any IMAP/SMTP mailbox. Every new message is judged by the spam filter first; only then is it shown and announced. The interface is available in English and Czech.

## Features

- **Proton Mail natively** – straight over the Proton API (SRP login, two-factor authentication, two-password mode).
- **Gmail, Seznam.cz and any IMAP/SMTP mailbox** – the servers are found automatically; new mail arrives instantly over IMAP IDLE.
- **Encryption** – Proton's end-to-end encryption, and PGP/MIME for the other services. Recipients' keys are found through Web Key Directory, Autocrypt, imported keys and optionally keys.openpgp.org. Messages to recipients without a key can be signed, and a warning is shown before they are sent unencrypted.
- **AI spam filter** – the AI decides using SPF/DKIM/DMARC results, Proton's markers, blocklist hits (Spamhaus DROP, URLhaus, OpenPhish – refreshed every 12 hours) and your lists of allowed and blocked senders.
- **AI assistant** – summaries, reply suggestions, editing and translating drafts, “Ask Your Mail”, sorting into labels and a morning overview. Providers: **Claude**, **ChatGPT**, **Gemini**, **Mistral** (EU servers) or a **local model** through a managed Ollama, so nothing leaves your computer. A “Local only” mode blocks every cloud provider.
- **Everyday mail** – threads, safe HTML mail with remote content blocked, a rich-text editor, attachments up to 25 MB, drafts, signature, send delay with Undo, scheduled sending, snoozing, rules, labels and folders, drag and drop, calendar invitations, automatic replies (Proton), one-click unsubscribe, printing and .eml export.
- **Multiple accounts** in one window, **background mode** with a tray icon, an **encrypted offline cache**, **automatic updates** from GitHub releases and built-in **help** (F1).

## Installation

On Fedora, install the latest release straight from GitHub:

```bash
sudo dnf install https://github.com/Imbecile6197/klient/releases/latest/download/klient.x86_64.rpm
```

After that, Klient checks for new releases once a day, downloads them in the background, verifies their SHA-256 checksum and installs them after one click and the administrator password. A What's New window shows the changes before and after each update.

For the tray icon, GNOME needs the AppIndicator extension (`gnome-shell-extension-appindicator`).

## Building from source

```bash
sudo dnf install golang gcc gtk4-devel libadwaita-devel gobject-introspection-devel webkitgtk6.0-devel
make build        # the first build takes a while (CGo bindings of GTK)
make run
make install      # into ~/.local, including the .desktop file and the icon
make rpm          # a Fedora package in build/rpm/RPMS/x86_64/
```

Publishing a release (needs `gh` and a committed tree): `git tag -a vX.Y.Z -m "Klient X.Y.Z" && make release VERSION=X.Y.Z`.

## First start

1. Log in with your Proton account, or choose **Other Service** for Gmail, Seznam.cz or another IMAP mailbox. Gmail and Seznam with two-factor authentication need an app password; the login window links to the right page.
2. In **Preferences → AI**, choose a provider for the assistant and for the spam filter and paste its API key – or install the local AI and let it run on your computer.
3. Optionally run **Check the Inbox with AI** from the menu to filter your latest 50 messages.

The complete user documentation is in [`docs/index.html`](docs/index.html) (Czech: [`docs/index.cs.html`](docs/index.cs.html)); in the application, press F1.

## Translations

The strings in the code are English and are translated through gettext catalogs in [`po/`](po/), which are embedded into the binary. The language follows the system (`LC_ALL`, `LC_MESSAGES`, `LANG`) and can be changed in Preferences → Messages. To add a language, copy `po/cs.po` to `po/<code>.po`, translate it, add the language to `internal/i18n` (its name and plural rule) and run `go test ./internal/i18n` – the test checks that every string in the code is translated with the same format verbs.

## Project structure

| Package | Purpose |
|---|---|
| `internal/protonmail` | Proton login, key unlocking, listing and decrypting messages, attachments, encrypted sending, the event stream |
| `internal/imapmail` | IMAP/SMTP accounts: server discovery, IDLE, threads, local snooze and scheduling, Gmail labels, PGP/MIME |
| `internal/mailbox` | The common account interface over Proton and IMAP |
| `internal/pgp`, `internal/pgpmime` | Keys of contacts and your own keys, signature checks, PGP/MIME, WKD and Autocrypt |
| `internal/ai` | Claude, OpenAI, Gemini, Mistral and Ollama behind one interface: spam classification with a JSON schema, summaries, replies |
| `internal/ollama` | The managed local Ollama: download, checksum, updates, start on demand |
| `internal/spam` | Blocklists and their updates, the spam filter (evidence → AI → decision), allowed and blocked senders |
| `internal/i18n`, `po` | Translations |
| `internal/update` | Updates from GitHub releases |
| `internal/ui` | The GNOME interface |
| `docs` | User documentation, also shown as Help |

Files: settings in `~/.config/klient/config.json`, data (blocklists, filter decisions, keys, cache, Ollama) in `~/.local/share/klient/`, secrets in the GNOME keyring.

## Privacy and limitations

- **With a cloud AI provider, the content of the processed messages is sent to that provider** (spam filter, summaries, suggestions). The spam filter sends at most the first 6,000 characters of the body. Requests to OpenAI use `store: false`. With local AI or in “Local only” mode, nothing leaves your computer.
- The Proton API is not officially documented for third-party clients and may change. Proton may reject a login or ask for a CAPTCHA; logging in once on the web from the same network usually helps. The app identifies itself with the `x-pm-appversion` header (`proton_app_version` in the config, `Other` by default).
- On Proton, search covers the subject, sender and recipients, not the message text (bodies are end-to-end encrypted on the server).
- With IMAP services, snoozing and scheduled sending work only while Klient is running (the background is enough).
- Outlook / Hotmail is not supported.
- `third_party/go-proton-api` is a copy of Proton's library with a few additions (conversation fields, scheduled sending, snoozing, automatic replies); `third_party/gotk4-webkitgtk` is a selection of the WebKitGTK Go bindings. See `KLIENT_PATCHES.md` in each.

## Author

Libor Macák – [libormacak.eu](https://libormacak.eu), [jsem@libormacak.eu](mailto:jsem@libormacak.eu). Bug reports and ideas are welcome in [Issues](https://github.com/Imbecile6197/klient/issues).

Licensed under the [GNU General Public License v3.0](LICENSE).
