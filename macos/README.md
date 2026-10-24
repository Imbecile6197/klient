# Klient for macOS

Native SwiftUI shell over the shared Go mail core (`cmd/klientcore` → `libklient.a`).

## Build

```bash
./macos/scripts/build.sh
open macos/build/Klient.app
```

Demo: `open --env KLIENT_DEMO=1 macos/build/Klient.app`

## Parity with Linux

Honest status vs `internal/ui/*` (Swift + `macbridge`, not README wishful thinking).
Focus: Linux → macOS gaps only; macOS-only extras (System Contacts, …) are fine.

**Daily-mail parity ≈ 95%.** Core compose/read/folders/AI/spam/offline loops/Proton reconnect/thread cards/bulk bar match Linux. Remaining gaps are thinner UX (hold “checking N…” bar, shortcuts sheet).

| Area | Status |
|------|--------|
| Proton login (HV, TOTP, mailbox password) | done |
| IMAP / Gmail / Seznam (discover + login + cert trust) | done |
| Unified “All Accounts” | done |
| List / search / read (WKWebView) / attachments open+save / plain-text | done |
| Threads (cfg.Threads) + conversation UI | done — stacked expand/collapse cards (Linux-style) |
| Remote content bar + allow sender | done |
| One-click / mailto / HTTPS unsubscribe | done |
| Multi-select bulk archive/trash/read/spam/star/move | done |
| Folder create / rename / delete / hide + drag-to-folder + Proton labels | done |
| Drafts open / delete | done |
| Print (WKWebView) + `.eml` export | done |
| Compose Bcc + rich-text toolbar + AI draft/improve | done (WebKit spellcheck vs Linux hunspell — platform) |
| Compose / reply / forward / draft / schedule / undo-send | done |
| Compose encryption UX (sign / attach key / require + block/warn) | done |
| Archive / trash / spam / star / snooze / unsnooze | done |
| Scheduled banner + cancel; list countdown | done |
| Rules (folder picker + apply on new mail) | done |
| Contacts (+ macOS Contacts suggest/import) | done — macOS-extra: System Contacts |
| Calendar invites (accept / maybe / decline / Calendar.app) | done |
| Automatic reply (vacation) | done |
| AI (summarize, ask mail, digest, improve, draft reply, spam scan, auto-label, compare models, usage strip + details) | done (no background summary precompute) |
| Ollama install / start / catalog pull + Grok provider | done |
| Spam rating banner (reader) + feeds prefs / status strip | done |
| Spam catch-up / sweep (mail while app was closed) | done — `catchUp` on session start + `sweepLoop` |
| Offline cache size pref + SyncOffline + clear/stats + banner | done |
| Auto-empty Trash/Spam after N days | done — `autoEmptyLoop` |
| Protect subject of encrypted IMAP mail | done — `imapmail.SetProtectSubject` on start/config save |
| PGP prefs + IMAP own-key + import from attachment + sender key-change notice | done |
| Hold-until-checked (hide unjudged inbox mail) | partial — hold works in list; no “checking N…” bar |
| Proton event reconnect / deauth recover | done — StatusUp restart + keyring refresh resume |
| Menu bar icon + Dock badge | done (menubar refreshes on unread / activate) |
| mailto: handler | done |
| Locale EN/CS (prefs + bridge) | done |
| Check for updates (GitHub → DMG link) | done |
| About / What's New (embedded release notes) | done |
| Help (embedded docs, EN/CS, browse + search) | done |
| Outbox (retry / edit / delete + auto-retry) | done |
| Create rule from sender (⋯ / context menu) | done |
| Notifications (Mark read / Archive / Trash / Spam actions) | done |
| Keyboard shortcuts window | partial — short sheet; Linux lists many more bindings |

## Release helpers

```bash
VERSION=0.11.7 ./macos/scripts/build.sh
VERSION=0.11.7 ./macos/scripts/package-dmg.sh
# SIGN_IDENTITY=… ./macos/scripts/sign-notarize.sh
```

Never upload or tag without the author’s explicit yes for that version.
