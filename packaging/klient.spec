# Packages the locally built binary (make rpm). Building inside rpmbuild would
# need network access for Go modules and a very long cgo build of the GTK
# bindings, so `make rpm` compiles first and this spec only installs.
%global debug_package %{nil}
%global app_id eu.libormacak.Klient

Name:           klient
Version:        %{klient_version}
Release:        1%{?dist}
Summary:        Mail client for Proton Mail with PGP, AI assistant and AI spam filter
Summary(cs):    Pošta Proton s PGP, AI asistentem a AI spamfiltrem

License:        GPL-3.0-or-later AND MIT AND MPL-2.0 AND Apache-2.0 AND BSD-3-Clause
URL:            https://libormacak.eu
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

%description
A GNOME mail client that talks to Proton Mail directly over its API, without
Proton Bridge: end-to-end encryption and native PGP, conversations, safe HTML
mail, a rich-text editor, an AI assistant and an AI-driven spam filter using
block lists, an encrypted offline cache, rules and undo send.

%description -l cs
E-mailový klient pro GNOME, který komunikuje s Proton Mail přímo přes jeho API,
bez Proton Bridge: end-to-end šifrování a nativní PGP, vlákna, bezpečné HTML
zprávy, formátovaný editor, AI asistent a spamfiltr řízený AI s blocklisty,
šifrovaná offline cache, pravidla a zpoždění odeslání.

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
* Tue Sep 29 2026 Libor Macák <jsem@libormacak.eu> - 0.6.6-1
- Help (F1, main menu) opens the built-in user documentation

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.6.5-1
- Sidebar spam-filter status follows model changes and names the provider

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.6.4-1
- Monochrome panel icon; only the new-mail dot is red
- User documentation in docs/index.html

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.6.3-1
- Mistral AI (EU servers) as a cloud AI provider with strict JSON schema
- Ministral 3 (3B, 8B) offered for local AI and model comparison

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.6.2-1
- Gmail: labels as coloured tags (add keeps the message in place, remove
  drops only the label), Gmail-style archive to All Mail, Starred folder,
  "Important" hidden

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.6.1-1
- Attachments shown as chips at the top of a message (click opens, arrow
  saves)
- The reader no longer jumps back to the top while scrolling; HTML mail
  height is measured from its content

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.6.0-1
- PGP for Seznam, Gmail and IMAP accounts: own key (create, import, back
  up), PGP/MIME encryption and signatures, WKD, Autocrypt and optional
  keys.openpgp.org lookup; drafts and sent copies encrypted to self
- Used space shown for servers without a quota (Seznam)
- Preferences pages in the same order for every account

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.5.0-1
- IMAP accounts: conversation threads, snooze and scheduled sending run by
  Klient (folders "Odložené" and "Naplánované"), full References in replies
- The UI no longer waits for IMAP network operations when drawing folders

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.4.1-1
- Seznam login: guidance for the application password required with
  two-factor authentication; login errors shown in full

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.4.0-1
- Seznam.cz, Gmail (app password) and any IMAP/SMTP account: automatic
  server discovery, IMAP IDLE, full-text search, drafts, sending with a copy
  in Sent, offline cache, spam filter and AI like for Proton
- Account wizard with service tiles and logos

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.3.6-1
- Internal: common mail account interface (preparation for Seznam, Gmail
  and IMAP); features the service lacks are hidden
- Service logo next to accounts in the account switcher

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.3.5-1
- About dialog: author's website and e-mail, updated feature list and
  licences of the new components

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.3.4-1
- New inbox mail is shown only after the spam filter has judged it
  (switchable; released after 6 minutes at the latest)

* Mon Sep 28 2026 Libor Macák <jsem@libormacak.eu> - 0.3.3-1
- Storage indicator shows the mail quota like Proton's web (Drive counted
  separately on split-storage plans), refreshes every 30 minutes and warns
  in colour when nearly full

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.3.2-1
- AI summaries and label suggestions are cached (encrypted) per message
- Summaries of new mail are prepared in advance by the local model, only
  on mains power and when the model is idle
- The local model stays loaded for 30 minutes on mains power

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.3.1-1
- Offer a restart when the package is updated while Klient runs in the
  background

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.3.0-1
- Local AI via a managed Ollama with an auto-updater (Ollama and models)
- Model comparison on the user's own mail; local-only mode

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.2.0-1
- Multiple Proton accounts with an account switcher
- Background mode with a tray icon and autostart
- Scheduled send with a countdown; countdown in the undo-send toast
- Snooze, calendar invitations, vacation auto-reply
- Drag and drop of attachments and of messages onto folders
- Attach own public key, import keys from attachments
- AI: ask your mail, automatic labels, morning overview
- Keyboard shortcuts window

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.1.4-1
- Headers read from the raw header block: unsubscribe works for cached
  messages too; unsubscribe button moved up and highlighted
- Revoked sessions recover automatically from the keyring, otherwise the
  login screen is shown instead of an error

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.1.3-1
- Own messages show "Odesláno z vašeho účtu" instead of a missing sender key
- Correct Czech plural of attachment counts

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.1.2-1
- Messages with an attachment and an empty body can be saved and sent
  (draft update sent an unencrypted empty body, rejected by the API)
- Shorter, readable error messages from the Proton API

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.1.1-1
- Proton contacts are loaded again (empty filter bug); address suggestions
  from contacts and recent correspondents, diacritics-insensitive
- Contacts window: search, write, add, delete; add sender from a message

* Sun Sep 27 2026 Libor Macák <jsem@libormacak.eu> - 0.1.0-1
- First test release
