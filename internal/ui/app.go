// Package ui is the GNOME (GTK4 + libadwaita) user interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/mailparse"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/protonmail"
	"github.com/Imbecile6197/klient/internal/relnotes"
	"github.com/Imbecile6197/klient/internal/secrets"
	"github.com/Imbecile6197/klient/internal/spam"
)

const AppID = "eu.libormacak.Klient"

// session is one logged-in Proton account. Every account gets new-mail
// events, the spam filter and notifications; one of them is shown.
type session struct {
	acc        mailbox.Account
	events     bool
	recovering bool
	unread     int

	// New messages hidden until the spam filter has judged them. Written on
	// the event goroutine, read by the UI, hence the mutex.
	mu      sync.Mutex
	checked map[string]time.Time // message ID -> when it arrived
}

// maxHold releases a message even if its check never finishes (e.g. the
// local model hangs), so no mail stays hidden for long.
const maxHold = 6 * time.Minute

func (s *session) hold(id string) {
	s.mu.Lock()
	if s.checked == nil {
		s.checked = map[string]time.Time{}
	}
	s.checked[id] = time.Now()
	s.mu.Unlock()
}

func (s *session) release(id string) {
	s.mu.Lock()
	delete(s.checked, id)
	s.mu.Unlock()
}

// held returns the IDs still waiting for the spam filter.
func (s *session) held() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for id, t := range s.checked {
		if time.Since(t) < maxHold {
			out[id] = true
		} else {
			delete(s.checked, id)
		}
	}
	return out
}

type App struct {
	app    *adw.Application
	win    *adw.ApplicationWindow
	toasts *adw.ToastOverlay

	cfg    config.Config
	mgr    *proton.Manager
	acc    mailbox.Account // the account shown in the window
	lists  *spam.Blocklists
	filter *spam.Filter
	ai     *ai.Client
	ollama *ollama.Runtime
	update pendingUpdate
	// Release notes of the last update, until "What's New" is shown.
	whatsNew []relnotes.Release

	ctx    context.Context
	cancel context.CancelFunc

	mv *mainView

	sessions      []*session
	pendingMailto []string
	notified      map[string]notifiedMsg // message ID -> account, for notification clicks
	background    bool                   // started with --background: no window at first
	quitting      bool
	tray          tray
	digest        string // last morning overview
	restart       bool   // re-exec the (updated) binary after quitting
	precompute    chan precomputeJob
	actions       map[string]*gio.SimpleAction
}

type notifiedMsg struct {
	acc  mailbox.Account
	meta protonmail.Summary
}

func Run(cfg config.Config) int {
	a := &App{cfg: cfg, notified: map[string]notifiedMsg{}, precompute: make(chan precomputeJob, 100)}
	args := os.Args[:1]
	for _, arg := range os.Args[1:] {
		if arg == "--background" {
			a.background = true
			continue
		}
		args = append(args, arg)
	}
	a.app = adw.NewApplication(AppID, gio.ApplicationHandlesOpen)
	a.app.ConnectActivate(a.activate)
	// `klient mailto:…` (and the desktop's mailto: handler) end up here.
	a.app.ConnectOpen(func(files []gio.Filer, _ string) {
		a.activate()
		for _, f := range files {
			if uri := f.URI(); strings.HasPrefix(strings.ToLower(uri), "mailto:") {
				if a.acc != nil {
					a.openMailto(uri)
				} else {
					a.pendingMailto = append(a.pendingMailto, uri)
				}
			}
		}
	})
	a.app.ConnectShutdown(func() {
		if a.cancel != nil {
			a.cancel()
		}
		if a.ollama != nil {
			a.ollama.Stop()
		}
		for _, s := range a.sessions {
			s.acc.Close()
		}
	})
	code := a.app.Run(args)
	if a.restart {
		// The D-Bus name is released now, so the new process becomes the
		// primary instance.
		if exe, err := os.Executable(); err == nil {
			_ = syscall.Exec(strings.TrimSuffix(exe, " (deleted)"), os.Args, os.Environ())
		}
	}
	return code
}

// ui runs f on the GTK main thread. Every widget access from a goroutine must
// go through it.
func ui(f func()) { glib.IdleAdd(f) }

func (a *App) activate() {
	if a.win != nil {
		a.showWindow()
		return
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.mgr = protonmail.NewManager(a.cfg)
	a.lists = spam.NewBlocklists()
	a.filter = spam.NewFilter(a.cfg, a.lists)
	a.ollama = ollama.New(config.DataDir())
	imapmail.SetKeyServer(a.cfg.KeyServerLookup)
	a.initAI()
	a.mgr.AddStatusObserver(func(st proton.Status) {
		ui(func() {
			if a.mv == nil {
				return
			}
			a.mv.offline.SetRevealed(st == proton.StatusDown)
			if st == proton.StatusUp {
				a.mv.scheduleRefresh()
			}
		})
	})

	loadAppCSS()
	a.win = adw.NewApplicationWindow(&a.app.Application)
	a.win.SetTitle("Klient")
	a.win.SetDefaultSize(1280, 800)
	a.win.SetSizeRequest(360, 480)
	a.toasts = adw.NewToastOverlay()
	a.win.SetContent(a.toasts)
	a.setupActions()
	// With background mode the window only hides: mail keeps being checked
	// and the tray icon brings it back.
	a.win.ConnectCloseRequest(func() bool {
		if a.quitting || !a.cfg.RunInBackground || a.acc == nil {
			return false
		}
		a.win.SetVisible(false)
		return true
	})
	if a.cfg.RunInBackground {
		a.startTray()
	}

	go a.lists.RunUpdater(a.ctx, a.cfg, func(err error) {
		ui(func() {
			if err != nil {
				a.toast(i18n.T("Blocklist update: ") + err.Error())
			}
			if a.mv != nil {
				a.mv.updateFilterStatus()
			}
		})
	})

	a.showStatus(i18n.T("Connecting…"))
	if !a.background {
		a.win.Present()
	}
	a.checkWhatsNew()
	go a.resumeAll()
	go a.digestLoop()
	go a.ollamaUpdateLoop()
	go a.selfUpdateLoop()
	go a.watchUpgrade()
	go a.precomputeLoop()
}

// watchUpgrade notices that the package was updated while Klient runs (the
// running binary was replaced) and offers a restart, because with background
// mode an old instance would otherwise stay running.
func (a *App) watchUpgrade() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		exe, err := os.Readlink("/proc/self/exe")
		if err != nil || !strings.HasSuffix(exe, " (deleted)") {
			continue
		}
		ui(func() {
			n := gio.NewNotification(i18n.T("Klient Has Been Updated"))
			n.SetBody(i18n.T("Restart it to use the new version."))
			n.SetDefaultAction("app.restart")
			n.AddButton(i18n.T("Restart"), "app.restart")
			a.app.SendNotification("upgrade", n)
			a.toastWithAction(i18n.T("Klient has been updated – restart it to use the new version"), i18n.T("Restart"), a.restartApp)
		})
		return
	}
}

func (a *App) restartApp() {
	a.restart = true
	a.quit()
}

// resumeAll restores every saved account; the active one is shown as soon as
// it is ready.
func (a *App) resumeAll() {
	if name, err := secrets.MigrateLegacySession(); err == nil && name != "" && !slices.Contains(a.cfg.Accounts, name) {
		ui(func() {
			a.cfg.Accounts = append(a.cfg.Accounts, name)
			if a.cfg.ActiveAccount == "" {
				a.cfg.ActiveAccount = name
			}
			a.saveConfig()
		})
	}
	names := make(chan []string)
	ui(func() { names <- append([]string{}, a.cfg.Accounts...) })
	accounts := <-names
	if len(accounts) == 0 {
		ui(func() { a.showLogin("", "") })
		return
	}
	active := a.cfg.ActiveAccount
	if !slices.Contains(accounts, active) {
		active = accounts[0]
	}
	for _, name := range accounts {
		if strings.HasPrefix(name, "imap:") {
			go a.resumeIMAP(name, name == active)
			continue
		}
		go func(name string) {
			s, err := secrets.LoadProtonSession(name)
			if err != nil {
				ui(func() { a.showLogin(i18n.T("The system keyring is not available: ")+err.Error(), name) })
				return
			}
			if s == nil {
				ui(func() {
					if name == active {
						a.showLogin("", name)
					}
				})
				return
			}
			ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
			defer cancel()
			acc, err := protonmail.Resume(ctx, a.mgr, s)
			ui(func() {
				if err != nil {
					if name == active || len(a.sessions) == 0 {
						a.showLogin(fmt.Sprintf(i18n.T("The session has expired; please log in again. (%s)"), err.Error()), name)
					} else {
						a.toast(fmt.Sprintf(i18n.T("Account %s: the session has expired; please log in again"), name))
					}
					return
				}
				box := mailbox.Proton(acc)
				a.addSession(box)
				if name == active || a.acc == nil {
					a.switchAccount(box)
				}
			})
		}(name)
	}
}

// resumeIMAP opens a saved IMAP/SMTP account (off the UI thread).
func (a *App) resumeIMAP(id string, active bool) {
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	acc, set, err := a.openIMAP(ctx, id)
	ui(func() {
		if err != nil {
			label := strings.TrimPrefix(id, "imap:")
			if errors.Is(err, imapmail.ErrAuth) {
				a.toastWithAction(fmt.Sprintf(i18n.T("Account %s: the server rejected the password"), label), i18n.T("Enter Password"), func() { a.imapLogin(set) })
			} else {
				a.toast(fmt.Sprintf(i18n.T("Account %s could not be opened: %s"), label, err.Error()))
			}
			if active && len(a.sessions) == 0 {
				a.showLogin("", "")
			}
			return
		}
		a.addSession(acc)
		if active || a.acc == nil {
			a.switchAccount(acc)
		}
	})
}

func (a *App) initAI() {
	a.ai = ai.New(a.ctx, ai.Settings{
		AssistantProvider: a.cfg.AssistantProvider, AssistantModel: a.cfg.AssistantModel,
		SpamProvider: a.cfg.SpamProvider, SpamModel: a.cfg.SpamModel,
		Local: a.ollama, LocalOnly: a.cfg.LocalOnly,
	}, func(provider string) string {
		key, err := secrets.LoadAPIKey(provider)
		if err != nil {
			log.Printf("API key %s: %v", provider, err)
		}
		return key
	})
	a.filter.SetAI(a.ai)
	// Every provider or model change goes through here: keep the status
	// line in the sidebar in sync.
	if a.mv != nil {
		a.mv.updateFilterStatus()
	}
}

func (a *App) saveConfig() {
	if err := config.Save(a.cfg); err != nil {
		a.toast(i18n.T("The settings could not be saved: ") + err.Error())
	}
	a.filter.SetConfig(a.cfg)
}

func (a *App) toast(msg string) {
	t := adw.NewToast(msg)
	t.SetTimeout(5)
	a.toasts.AddToast(t)
}

func (a *App) toastWithAction(msg, button string, action func()) {
	t := adw.NewToast(msg)
	t.SetTimeout(8)
	t.SetButtonLabel(button)
	t.ConnectButtonClicked(action)
	a.toasts.AddToast(t)
}

func (a *App) quit() {
	a.quitting = true
	a.app.Quit()
}

func (a *App) setupActions() {
	add := func(name string, accels []string, f func()) {
		act := gio.NewSimpleAction(name, nil)
		act.ConnectActivate(func(*glib.Variant) { f() })
		if a.actions == nil {
			a.actions = map[string]*gio.SimpleAction{}
		}
		a.actions[name] = act
		a.app.AddAction(act)
		if len(accels) > 0 {
			a.app.SetAccelsForAction("app."+name, accels)
		}
	}
	add("compose", []string{"<Control>n"}, func() {
		if a.acc != nil {
			a.openCompose(nil)
		}
	})
	add("preferences", []string{"<Control>comma"}, a.openPreferences)
	add("scan-inbox", nil, func() {
		if a.mv != nil {
			a.mv.scanInbox()
		}
	})
	add("logout", nil, a.confirmLogout)
	add("add-account", nil, a.showAddAccount)
	add("contacts", []string{"<Control><Shift>k"}, a.openContacts)
	add("apply-rules", nil, func() {
		if a.acc != nil {
			a.applyRulesToInbox()
		}
	})
	add("ask-mail", []string{"<Control>j"}, a.askMail)
	add("digest", nil, func() { a.showDigest(true) })
	add("autoreply", nil, a.openAutoReply)
	mv := func(f func(*mainView)) func() {
		return func() {
			if a.mv != nil {
				f(a.mv)
			}
		}
	}
	add("reply", []string{"<Control>r"}, mv(func(m *mainView) { m.reply(protonmail.ActionReply) }))
	add("reply-all", []string{"<Control><Shift>r"}, mv(func(m *mainView) { m.reply(protonmail.ActionReplyAll) }))
	add("forward", []string{"<Control>l"}, mv(func(m *mainView) { m.reply(protonmail.ActionForward) }))
	add("trash", nil, mv(func(m *mainView) { m.moveCurrent(protonmail.TrashID, i18n.T("Moved to Trash")) }))
	add("archive", []string{"<Control>e"}, mv(func(m *mainView) { m.moveCurrent(protonmail.ArchiveID, i18n.T("Archived")) }))
	add("toggle-star", []string{"<Control>d"}, mv(func(m *mainView) { m.toggleStar() }))
	add("mark-unread", []string{"<Control><Shift>u"}, mv(func(m *mainView) { m.markUnread() }))
	add("snooze", []string{"<Control>h"}, mv(func(m *mainView) { m.snoozeCustom() }))
	add("search", []string{"<Control>f"}, mv(func(m *mainView) { m.startSearch() }))
	add("refresh", []string{"F5"}, mv(func(m *mainView) { m.openFolder(m.folder) }))

	// Parameterised actions used by the "move to folder" and label menus.
	move := gio.NewSimpleAction("move-to", glib.NewVariantType("s"))
	move.ConnectActivate(func(p *glib.Variant) {
		if a.mv != nil && p != nil {
			a.mv.moveCurrent(p.String(), i18n.T("Moved"))
		}
	})
	a.app.AddAction(move)
	label := gio.NewSimpleAction("toggle-label", glib.NewVariantType("s"))
	label.ConnectActivate(func(p *glib.Variant) {
		if a.mv != nil && p != nil {
			a.mv.toggleLabel(p.String())
		}
	})
	a.app.AddAction(label)
	open := gio.NewSimpleAction("open-message", glib.NewVariantType("s"))
	open.ConnectActivate(func(p *glib.Variant) {
		if p != nil {
			a.openNotified(p.String())
		}
	})
	a.app.AddAction(open)
	add("show", nil, a.showWindow)
	add("restart", nil, a.restartApp)
	add("about", nil, a.showAbout)
	add("help", []string{"F1"}, a.showHelp)
	add("install-update", nil, a.installUpdate)
	add("update-notes", nil, a.showUpdateNotes)
	add("whats-new", nil, a.showWhatsNew)
	add("shortcuts", []string{"<Control>question", "<Control>slash"}, a.showShortcuts)
	add("quit", []string{"<Control>q"}, a.quit)
}

func (a *App) primaryMenu() *gio.Menu {
	menu := gio.NewMenu()
	s1 := gio.NewMenu()
	s1.Append(i18n.T("New Message"), "app.compose")
	s1.Append(i18n.T("Contacts"), "app.contacts")
	s1.Append(i18n.T("Ask Your Mail (AI)"), "app.ask-mail")
	s1.Append(i18n.T("Unread Mail Overview (AI)"), "app.digest")
	menu.AppendSection("", s1)
	s3 := gio.NewMenu()
	s3.Append(i18n.T("Check the Inbox with AI"), "app.scan-inbox")
	s3.Append(i18n.T("Apply Rules to the Inbox"), "app.apply-rules")
	s3.Append(i18n.T("Automatic Reply…"), "app.autoreply")
	menu.AppendSection("", s3)
	s2 := gio.NewMenu()
	s2.Append(i18n.T("Preferences"), "app.preferences")
	s2.Append(i18n.T("Help"), "app.help")
	s2.Append(i18n.T("Keyboard Shortcuts"), "app.shortcuts")
	s2.Append(i18n.T("Add Account…"), "app.add-account")
	s2.Append(i18n.T("Log Out of This Account"), "app.logout")
	s2.Append(i18n.T("What's New"), "app.whats-new")
	s2.Append(i18n.T("About Klient"), "app.about")
	s2.Append(i18n.T("Quit"), "app.quit")
	menu.AppendSection("", s2)
	return menu
}

// page wraps content in a toolbar view with a flat header bar.
func page(content gtk.Widgetter) *adw.ToolbarView {
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	hb.SetShowTitle(false)
	tv.AddTopBar(hb)
	tv.SetContent(content)
	return tv
}

func (a *App) showStatus(text string) {
	sp := adw.NewStatusPage()
	sp.SetTitle(text)
	spinner := gtk.NewSpinner()
	spinner.SetSizeRequest(32, 32)
	spinner.Start()
	sp.SetChild(spinner)
	a.toasts.SetChild(page(sp))
}

// ---- Accounts ---------------------------------------------------------------

func (a *App) sessionOf(acc mailbox.Account) *session {
	for _, s := range a.sessions {
		if s.acc == acc {
			return s
		}
	}
	return nil
}

// addSession starts background work (events, offline sync) for an account.
// Logging in to an account that is already open replaces the old session.
func (a *App) addSession(acc mailbox.Account) *session {
	for i, s := range a.sessions {
		if s.acc.UserID() == acc.UserID() && acc.UserID() != "" {
			s.acc.Close()
			a.sessions = append(a.sessions[:i], a.sessions[i+1:]...)
			break
		}
	}
	s := &session{acc: acc}
	a.sessions = append(a.sessions, s)
	// Keep the sidebar order of the config.
	slices.SortStableFunc(a.sessions, func(x, y *session) int {
		return slices.Index(a.cfg.Accounts, x.acc.Username()) - slices.Index(a.cfg.Accounts, y.acc.Username())
	})
	acc.OnDeauth(func() { ui(func() { a.recoverSession(s) }) })
	a.startEvents(s)
	go a.offlineSyncLoop(s)
	go func() { _, _ = acc.Contacts(a.ctx) }() // warm up address completion
	a.mgr.AddStatusObserver(func(st proton.Status) {
		if st == proton.StatusUp {
			ui(func() { a.startEvents(s) })
		}
	})
	a.refreshUnread(s)
	return s
}

func (a *App) onLoggedIn(acc mailbox.Account) {
	if !slices.ContainsFunc(a.cfg.Accounts, func(n string) bool { return strings.EqualFold(n, acc.Username()) }) {
		a.cfg.Accounts = append(a.cfg.Accounts, acc.Username())
	}
	a.addSession(acc)
	a.switchAccount(acc)
	if !a.ai.HasAssistant() && !a.ai.HasSpam() {
		a.toastWithAction(i18n.T("AI is not set up – choose local AI or enter an API key"), i18n.T("Set Up"), a.openPreferences)
	}
}

// switchAccount shows acc in the window.
func (a *App) switchAccount(acc mailbox.Account) {
	a.acc = acc
	if a.cfg.ActiveAccount != acc.Username() {
		a.cfg.ActiveAccount = acc.Username()
		a.saveConfig()
	}
	a.mv = newMainView(a)
	a.toasts.SetChild(a.mv.root)
	a.mv.installBreakpoints(a.win)
	a.mv.selectFolder(0)
	a.mv.offline.SetRevealed(acc.StartedOffline())
	a.win.SetTitle("Klient – " + acc.Email())
	if act := a.actions["autoreply"]; act != nil {
		act.SetEnabled(acc.Caps().AutoReply)
	}
	for _, uri := range a.pendingMailto {
		a.openMailto(uri)
	}
	a.pendingMailto = nil
}

// recoverSession handles a revoked session. Proton rotates the refresh token
// on every refresh, so if another process refreshed it (the new token is in
// the keyring) we simply continue with that one; otherwise the session really
// ended and the user has to log in again.
func (a *App) recoverSession(s *session) {
	if a.sessionOf(s.acc) != s || s.recovering {
		return
	}
	s.recovering = true
	old := s.acc
	go func() {
		var acc mailbox.Account
		err := errors.New(i18n.T("the session has ended"))
		if p := mailbox.AsProton(old); p == nil {
			// IMAP: the password was rejected (changed or revoked).
			ui(func() {
				s.recovering = false
				set, _ := a.mailServer(old.Username())
				a.toastWithAction(fmt.Sprintf(i18n.T("Account %s: the server rejected the password"), old.Email()), i18n.T("Enter Password"), func() { a.imapLogin(set) })
			})
			return
		} else {
			if st, _ := secrets.LoadProtonSession(old.Username()); st != nil && st.RefreshToken != p.RefreshToken() {
				ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
				var pa *protonmail.Account
				if pa, err = protonmail.Resume(ctx, a.mgr, st); err == nil {
					acc = mailbox.Proton(pa)
				}
				cancel()
			}
		}
		ui(func() {
			s.recovering = false
			old.Close()
			s.events = false
			if err == nil {
				s.acc = acc
				acc.OnDeauth(func() { ui(func() { a.recoverSession(s) }) })
				a.startEvents(s)
				go a.offlineSyncLoop(s)
				if a.acc == old {
					a.acc = acc
					if a.mv != nil {
						a.mv.openFolder(a.mv.folder)
					}
				}
				return
			}
			a.removeSession(s)
			msg := i18n.T("The Proton session has ended (for example after logging out on the web or in another app). Please log in again.")
			if a.acc == old || a.acc == nil {
				a.acc = nil
				a.showLogin(msg, old.Username())
			} else {
				a.toast(fmt.Sprintf(i18n.T("Account %s: the session has ended; please log in again"), old.Email()))
			}
		})
	}()
}

func (a *App) removeSession(s *session) {
	a.sessions = slices.DeleteFunc(a.sessions, func(x *session) bool { return x == s })
	a.updateTrayUnread()
}

// startEvents starts the mailbox event stream once the API is reachable.
func (a *App) startEvents(s *session) {
	if s.events || a.sessionOf(s.acc) != s {
		return
	}
	s.events = true
	acc := s.acc
	go func() {
		err := acc.Events(a.ctx, func(meta protonmail.Summary) { a.onNewMessage(s, meta) }, func() {
			ui(func() {
				if a.mv != nil && a.acc == acc {
					a.mv.scheduleRefresh()
				}
				a.refreshUnread(s)
			})
		})
		ui(func() {
			if err != nil {
				s.events = false
				if !protonmail.IsOffline(err) {
					a.toast(i18n.T("Watching for new mail could not be started: ") + err.Error())
				}
			}
		})
	}()
}

// refreshUnread updates the unread inbox count of an account (tray icon).
func (a *App) refreshUnread(s *session) {
	acc := s.acc
	go func() {
		counts, err := acc.UnreadCounts(a.ctx)
		if err != nil {
			return
		}
		ui(func() {
			s.unread = counts[protonmail.InboxID]
			a.updateTrayUnread()
			if a.mv != nil {
				a.mv.updateAccountBadges()
			}
		})
	}()
}

func (a *App) updateTrayUnread() {
	n := 0
	for _, s := range a.sessions {
		n += s.unread
	}
	a.setTrayUnread(n)
}

// offlineSyncLoop keeps the newest messages in the encrypted cache.
func (a *App) offlineSyncLoop(s *session) {
	acc := s.acc
	for {
		if s.acc != acc || a.sessionOf(acc) != s || a.ctx.Err() != nil {
			return
		}
		// Storage and profile change over time (Klient may run for days).
		if err := acc.RefreshUser(a.ctx); err == nil {
			ui(func() {
				if a.mv != nil && a.acc == acc {
					a.mv.updateStorage()
				}
			})
		}
		if n := a.cfg.OfflineMessages; n > 0 {
			if err := acc.SyncOffline(a.ctx, n, nil); err != nil && !protonmail.IsOffline(err) {
				log.Printf("offline sync: %v", err)
			}
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(30 * time.Minute):
		}
	}
}

// onNewMessage runs on the event goroutine.
func (a *App) onNewMessage(s *session, meta protonmail.Summary) {
	acc := s.acc
	// Hide the message before the list refresh that follows this event
	// (this runs on the event goroutine, before onChange).
	held := a.cfg.HoldUntilChecked && hasLabel(meta, protonmail.InboxID)
	if held {
		if _, known := a.filter.Decision(meta.ID); known {
			held = false
		}
	}
	if held {
		s.hold(meta.ID)
		ui(func() {
			if a.mv != nil && a.acc == acc {
				a.mv.updateChecking()
			}
			// Show it anyway after maxHold if the check hangs.
			glib.TimeoutSecondsAdd(uint(maxHold/time.Second)+1, func() bool {
				if a.mv != nil && a.acc == acc {
					a.mv.rebuildList()
				}
				return false
			})
		})
	}
	go func() {
		d, moved, err := a.filter.ProcessNew(a.ctx, acc, meta)
		var applied []string
		if err == nil && !moved {
			applied = a.applyRules(a.ctx, acc, meta)
			if !d.Spam {
				a.queuePrecompute(acc, meta)
			}
			if a.cfg.AutoLabel && len(applied) == 0 {
				if l := a.autoLabel(acc, meta); l != "" {
					applied = append(applied, fmt.Sprintf(i18n.T("AI label %s"), l))
				}
			}
		}
		ui(func() {
			shown := a.acc == acc
			if held {
				s.release(meta.ID)
				if a.mv != nil && shown {
					a.mv.rebuildList()
				}
			}
			if len(applied) > 0 && shown {
				a.toast(fmt.Sprintf(i18n.T("Rules: %s – %s"), strings.Join(applied, ", "), meta.Subject))
				if a.mv != nil {
					a.mv.scheduleRefresh()
				}
			}
			if err != nil {
				log.Printf("spamfilter: %v", err)
			}
			if moved {
				if shown {
					a.toastWithAction(fmt.Sprintf(i18n.T("The AI moved to spam: %s"), meta.Subject), i18n.T("Move Back"), func() {
						a.markNotSpam(d.From, meta.ID)
					})
				}
				return
			}
			if d.Spam || (a.win.IsActive() && shown) {
				return
			}
			sender := i18n.T("Unknown sender")
			if meta.Sender != nil {
				sender = mailparse.DisplayAddress(meta.Sender)
			}
			n := gio.NewNotification(sender)
			body := meta.Subject
			if len(a.sessions) > 1 {
				body += "\n" + acc.Email()
			}
			n.SetBody(body)
			a.notified[meta.ID] = notifiedMsg{acc: acc, meta: meta}
			n.SetDefaultActionAndTarget("app.open-message", glib.NewVariantString(meta.ID))
			a.app.SendNotification("new-mail-"+meta.ID, n)
		})
	}()
}

// openNotified shows the message a notification was about.
func (a *App) openNotified(id string) {
	n, ok := a.notified[id]
	a.showWindow()
	if !ok || a.sessionOf(n.acc) == nil {
		return
	}
	if a.acc != n.acc {
		a.switchAccount(n.acc)
	}
	a.app.WithdrawNotification("new-mail-" + id)
	a.mv.openThread(protonmail.Thread{ConversationID: n.meta.ConversationID, Latest: n.meta, Messages: []protonmail.Summary{n.meta}})
}

func (a *App) markNotSpam(from string, ids ...string) {
	for _, id := range ids {
		a.filter.Feedback(id, from, false)
	}
	go func() {
		err := a.acc.Move(a.ctx, protonmail.InboxID, ids...)
		ui(func() {
			if err != nil {
				a.toast(i18n.T("Moving failed: ") + err.Error())
				return
			}
			a.toast(i18n.T("Moved to the inbox; the sender has been added to the allowed senders"))
			a.mv.scheduleRefresh()
		})
	}()
}

func (a *App) markSpam(from string, ids ...string) {
	for _, id := range ids {
		a.filter.Feedback(id, from, true)
	}
	go func() {
		err := a.acc.Move(a.ctx, protonmail.SpamID, ids...)
		ui(func() {
			if err != nil {
				a.toast(i18n.T("Moving failed: ") + err.Error())
				return
			}
			a.toast(i18n.T("Moved to spam; the sender has been added to the blocked senders"))
			a.mv.scheduleRefresh()
		})
	}()
}

func (a *App) confirmLogout() {
	if a.acc == nil {
		return
	}
	d := adw.NewAlertDialog(fmt.Sprintf(i18n.T("Log Out of %s?"), a.acc.Email()), i18n.T("The session will be ended on the server and removed from the keyring, and the offline cache of this account will be deleted."))
	d.AddResponse("cancel", i18n.T("Cancel"))
	d.AddResponse("logout", i18n.T("Log Out"))
	d.SetResponseAppearance("logout", adw.ResponseDestructive)
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r != "logout" {
			return
		}
		acc := a.acc
		if s := a.sessionOf(acc); s != nil {
			a.removeSession(s)
		}
		a.cfg.Accounts = slices.DeleteFunc(a.cfg.Accounts, func(n string) bool { return n == acc.Username() })
		a.cfg.MailServers = slices.DeleteFunc(a.cfg.MailServers, func(m config.MailServer) bool { return m.ID() == acc.Username() })
		a.saveConfig()
		a.acc = nil
		a.showStatus(i18n.T("Logging out…"))
		go func() {
			err := acc.Logout(context.Background())
			ui(func() {
				if len(a.sessions) > 0 {
					a.switchAccount(a.sessions[0].acc)
				} else {
					a.showLogin("", "")
				}
				if err != nil {
					a.toast(i18n.T("Logging out on the server failed: ") + err.Error())
				}
			})
		}()
	})
	d.Present(a.win)
}

func (a *App) gtkWindow() *gtk.Window { return &a.win.ApplicationWindow.Window }
