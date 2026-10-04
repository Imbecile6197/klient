# Packages the locally built binary (make rpm). Building inside rpmbuild would
# need network access for Go modules and a very long cgo build of the GTK
# bindings, so `make rpm` compiles first and this spec only installs.
%global debug_package %{nil}
%global app_id eu.libormacak.Klient

Name:           klient
Version:        %{klient_version}
Release:        1%{?dist}
Summary:        Mail client with PGP, an AI assistant and an AI spam filter
Summary(cs):    E-mailový klient s PGP, AI asistentem a AI spamfiltrem

License:        GPL-3.0-or-later AND MIT AND MPL-2.0 AND Apache-2.0 AND BSD-3-Clause
URL:            https://github.com/Imbecile6197/klient
Source0:        %{name}-%{version}.tar.gz
ExclusiveArch:  x86_64

BuildRequires:  desktop-file-utils
BuildRequires:  libappstream-glib

# Shared libraries (GTK 4, libadwaita, WebKitGTK) are detected automatically.
# The keyring holds the session and API keys.
Requires:       (gnome-keyring or kwallet or keepassxc)
Requires:       /usr/bin/gio
Requires:       hicolor-icon-theme
# The tray icon is shown by GNOME only with the AppIndicator extension.
Recommends:     gnome-shell-extension-appindicator
# Spell checking while writing (loaded at run time; off without them).
Recommends:     enchant2
Recommends:     hunspell-cs
Recommends:     hunspell-en-US

%description
A GNOME mail client for Proton Mail (directly over its API, without Proton
Bridge), Gmail, Seznam.cz and any IMAP/SMTP mailbox: end-to-end encryption
and PGP/MIME, conversations, safe HTML mail, a rich-text editor, an AI
assistant and an AI-driven spam filter with block lists (cloud providers or a
local model), an encrypted offline cache, rules, undo send and scheduled
sending. In English and Czech.

%description -l cs
E-mailový klient pro GNOME pro Proton Mail (přímo přes jeho API, bez Proton
Bridge), Gmail, Seznam.cz a libovolnou schránku IMAP/SMTP: end-to-end
šifrování a PGP/MIME, vlákna, bezpečné HTML zprávy, formátovaný editor, AI
asistent a spamfiltr řízený AI s blocklisty (cloudoví poskytovatelé nebo
lokální model), šifrovaná offline cache, pravidla, zpoždění odeslání
a naplánované odeslání. Česky a anglicky.

%prep
%setup -q

%install
install -Dm755 klient %{buildroot}%{_bindir}/klient
install -Dm644 %{app_id}.desktop %{buildroot}%{_datadir}/applications/%{app_id}.desktop
install -Dm644 %{app_id}.svg %{buildroot}%{_datadir}/icons/hicolor/scalable/apps/%{app_id}.svg
install -Dm644 %{app_id}-symbolic.svg %{buildroot}%{_datadir}/icons/hicolor/symbolic/apps/%{app_id}-symbolic.svg
install -Dm644 %{app_id}.metainfo.xml %{buildroot}%{_metainfodir}/%{app_id}.metainfo.xml

%check
desktop-file-validate %{buildroot}%{_datadir}/applications/%{app_id}.desktop
appstream-util validate-relax --nonet %{buildroot}%{_metainfodir}/%{app_id}.metainfo.xml

%files
%license LICENSE
%doc README.md
%{_bindir}/klient
%{_datadir}/applications/%{app_id}.desktop
%{_datadir}/icons/hicolor/scalable/apps/%{app_id}.svg
%{_datadir}/icons/hicolor/symbolic/apps/%{app_id}-symbolic.svg
%{_metainfodir}/%{app_id}.metainfo.xml

%changelog
* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.10.0-1
- Proton: PGP/MIME (and signed clear MIME) for external recipients, keys
  and settings from Proton contacts
- Inline PGP decryption; protected headers (send optional, receive)
- Contact key store with source, verification, pending key changes,
  weekly WKD refresh; key details dialog and warning in the message
- Send only encrypted option; own key validity, extension, publishing on
  keys.openpgp.org, revocation certificate

* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.9.1-1
- HTML signature: format choice, HTML source with live preview, template,
  opening an .html file; shown as a preview in the composer, sent as HTML
  with a plain-text version

* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.9.0-1
- Spell checking while writing (Enchant loaded at run time, Czech and
  English dictionaries, corrections in the right-click menu)
- OpenPhish matched by whole links, not domains; a link listed on
  OpenPhish or URLhaus always means spam, whatever the AI says
- The status in the sidebar shows the assistant's model too

* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.8.3-1
- IMAP accounts started offline go online when the connection returns
  (banner, folders and list refresh)
- No "no Drafts folder" warning for the safety copy before sending
- Auto-empty no longer uses SEARCH BEFORE (rejected by Seznam)

* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.8.2-1
- Progress while emptying Trash and Spam; Proton empties a whole folder in
  one request (falls back to batches)

* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.8.1-1
- Automatic emptying of Trash and Spam moved to Preferences → General

* Sun Oct 04 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.8.0-1
- All accounts together (combined Inbox, Starred, Sent, Archive, Spam, Trash)
- Outbox: unsent mail is kept and retried after connection failures
- Folder management (create, rename, delete, hide), emptying Trash and Spam
  by hand or automatically after N days
- Buttons in the new-mail notification (read, archive, delete, spam)

* Sat Oct 03 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.9-1
- The key test no longer uses the provider's expensive default model;
  switching a role's provider restores the model chosen before
- Every AI request is logged (provider, model, size, time, result)

* Thu Oct 01 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.8-1
- The spam filter's reason is always in the interface language (stated
  again after the message for small local models)

* Thu Oct 01 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.7-1
- AI summaries, the morning overview and answers about mail are always in
  the interface language (stated in the request after the messages too);
  cached summaries in another language are regenerated

* Wed Sep 30 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.6-1
- IMAP/SMTP servers with an untrusted certificate: show it and let the user
  trust that one certificate (pinned by SHA-256 per server); certificate
  errors are no longer reported as the server being unavailable (#1)

* Wed Sep 30 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.5-1
- Help opens in its own WebKit window instead of the web browser (a
  sandboxed browser got only one file through the document portal, so the
  language switch and the screenshot failed on Ubuntu)

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.4-1
- New General page in Preferences (language, default email application,
  background mode, autostart, updates), moved out of Messages

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.3-1
- Ubuntu package (.deb for Ubuntu 26.04 LTS and newer, built in a container
  by packaging/deb.sh); the updater installs the package format Klient was
  installed from (dnf or apt)

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.2-1
- Fix messages with a one-line plain-text body shown empty
- Fix Czech date formats in the English interface
- Demo mode (KLIENT_DEMO=1) with made-up mail; screenshots generated from
  it (packaging/screenshots.sh) in README, docs and metainfo

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.1-1
- "What's New" after an update (changes since the last used version), before
  installing an update and in the main menu; release notes are embedded
  from packaging/release-notes

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.7.0-1
- English and Czech user interface (gettext catalogs in po/, embedded);
  the language follows the system or the Preferences
- AI answers in the interface language
- Documentation and README in English and Czech
- Localized dates, times and plural forms

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.8-1
- Automatic updates from GitHub releases: a new version is downloaded in
  the background, verified by SHA-256 and installed after one click and the
  administrator password (Preferences → Messages → Updates)

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.7-1
- Source code published on GitHub; link in About

* Tue Sep 29 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.6-1
- Help (F1, main menu) opens the built-in user documentation

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.5-1
- Sidebar spam-filter status follows model changes and names the provider

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.4-1
- Monochrome panel icon; only the new-mail dot is red
- User documentation in docs/index.html

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.3-1
- Mistral AI (EU servers) as a cloud AI provider with strict JSON schema
- Ministral 3 (3B, 8B) offered for local AI and model comparison

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.2-1
- Gmail: labels as coloured tags (add keeps the message in place, remove
  drops only the label), Gmail-style archive to All Mail, Starred folder,
  "Important" hidden

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.1-1
- Attachments shown as chips at the top of a message (click opens, arrow
  saves)
- The reader no longer jumps back to the top while scrolling; HTML mail
  height is measured from its content

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.6.0-1
- PGP for Seznam, Gmail and IMAP accounts: own key (create, import, back
  up), PGP/MIME encryption and signatures, WKD, Autocrypt and optional
  keys.openpgp.org lookup; drafts and sent copies encrypted to self
- Used space shown for servers without a quota (Seznam)
- Preferences pages in the same order for every account

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.5.0-1
- IMAP accounts: conversation threads, snooze and scheduled sending run by
  Klient (folders "Odložené" and "Naplánované"), full References in replies
- The UI no longer waits for IMAP network operations when drawing folders

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.4.1-1
- Seznam login: guidance for the application password required with
  two-factor authentication; login errors shown in full

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.4.0-1
- Seznam.cz, Gmail (app password) and any IMAP/SMTP account: automatic
  server discovery, IMAP IDLE, full-text search, drafts, sending with a copy
  in Sent, offline cache, spam filter and AI like for Proton
- Account wizard with service tiles and logos

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.6-1
- Internal: common mail account interface (preparation for Seznam, Gmail
  and IMAP); features the service lacks are hidden
- Service logo next to accounts in the account switcher

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.5-1
- About dialog: author's website and e-mail, updated feature list and
  licences of the new components

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.4-1
- New inbox mail is shown only after the spam filter has judged it
  (switchable; released after 6 minutes at the latest)

* Mon Sep 28 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.3-1
- Storage indicator shows the mail quota like Proton's web (Drive counted
  separately on split-storage plans), refreshes every 30 minutes and warns
  in colour when nearly full

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.2-1
- AI summaries and label suggestions are cached (encrypted) per message
- Summaries of new mail are prepared in advance by the local model, only
  on mains power and when the model is idle
- The local model stays loaded for 30 minutes on mains power

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.1-1
- Offer a restart when the package is updated while Klient runs in the
  background

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.3.0-1
- Local AI via a managed Ollama with an auto-updater (Ollama and models)
- Model comparison on the user's own mail; local-only mode

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.2.0-1
- Multiple Proton accounts with an account switcher
- Background mode with a tray icon and autostart
- Scheduled send with a countdown; countdown in the undo-send toast
- Snooze, calendar invitations, vacation auto-reply
- Drag and drop of attachments and of messages onto folders
- Attach own public key, import keys from attachments
- AI: ask your mail, automatic labels, morning overview
- Keyboard shortcuts window

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.1.4-1
- Headers read from the raw header block: unsubscribe works for cached
  messages too; unsubscribe button moved up and highlighted
- Revoked sessions recover automatically from the keyring, otherwise the
  login screen is shown instead of an error

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.1.3-1
- Own messages show "Odesláno z vašeho účtu" instead of a missing sender key
- Correct Czech plural of attachment counts

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.1.2-1
- Messages with an attachment and an empty body can be saved and sent
  (draft update sent an unencrypted empty body, rejected by the API)
- Shorter, readable error messages from the Proton API

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.1.1-1
- Proton contacts are loaded again (empty filter bug); address suggestions
  from contacts and recent correspondents, diacritics-insensitive
- Contacts window: search, write, add, delete; add sender from a message

* Sun Sep 27 2026 Imbecile6197 <Imbecile6197@users.noreply.github.com> - 0.1.0-1
- First test release
