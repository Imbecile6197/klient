package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	aipkg "github.com/Imbecile6197/klient/internal/ai"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

const pageSize = 50

type mainView struct {
	a *App

	root  *adw.OverlaySplitView
	inner *adw.NavigationSplitView

	folderList   *gtk.ListBox
	countLabels  map[string]*gtk.Label
	storageBar   *gtk.ProgressBar
	storageLabel *gtk.Label
	unreadOnly   bool
	emptyTitle   *adw.StatusPage
	folders      []protonmail.Folder // system folders followed by user folders/labels
	labels       []protonmail.UserLabel
	filterStatus *gtk.Label

	selecting   bool
	selected    map[int]bool
	checks      []*gtk.CheckButton
	lastToggled int
	selectBtn   *gtk.ToggleButton
	actionBar   *gtk.ActionBar
	selLabel    *gtk.Label
	bulkBtns    []*gtk.Button
	bulkMove    *gtk.MenuButton

	searchBar   *gtk.SearchBar
	searchEntry *gtk.SearchEntry
	searchQuery string

	listTitle *adw.WindowTitle
	listStack *gtk.Stack
	msgList   *gtk.ListBox
	moreBtn   *gtk.Button
	listPage  *adw.NavigationPage
	offline   *adw.Banner

	readerPage   *adw.NavigationPage
	readerStack  *gtk.Stack
	banner       *adw.Banner
	subject      *gtk.Label
	summaryCard  *gtk.Box
	summaryLabel *gtk.Label
	threadBox    *gtk.Box
	actionBtns   []*gtk.Button
	spamBtn      *gtk.Button
	summaryBtn   *gtk.Button
	starBtn      *gtk.Button
	moveBtn      *gtk.MenuButton
	labelBtn     *gtk.MenuButton

	folder   protonmail.Folder
	page     int
	msgs     []protonmail.Summary // raw list as loaded, newest first
	items    []protonmail.Thread  // rows of the list (threads, or single messages)
	thread   *threadState         // conversation open in the reader
	current  *protonmail.Message  // newest loaded message of that conversation
	loadSeq  int
	pending  bool
	bannerFn func()

	countdowns  []countdownLabel // "za 2 h" labels of scheduled messages
	checking    *gtk.Revealer    // "Kontroluje se N nových zpráv…"
	checkLabel  *gtk.Label
	snoozeBtn   *gtk.MenuButton
	unsnoozeBtn *gtk.Button
	accountBtn  *gtk.MenuButton
}

func newMainView(a *App) *mainView {
	m := &mainView{a: a}
	m.root = adw.NewOverlaySplitView()
	m.root.SetSidebar(m.buildSidebar())
	m.inner = adw.NewNavigationSplitView()
	m.inner.SetMinSidebarWidth(300)
	m.inner.SetMaxSidebarWidth(440)
	m.inner.SetSidebarWidthFraction(0.38)
	m.listPage = adw.NewNavigationPage(m.buildList(), "Zprávy")
	m.readerPage = adw.NewNavigationPage(m.buildReader(), "Zpráva")
	m.inner.SetSidebar(m.listPage)
	m.inner.SetContent(m.readerPage)
	m.root.SetContent(m.inner)
	m.updateFilterStatus()
	m.tickCountdowns()
	return m
}

// installBreakpoints collapses the folder sidebar on medium widths and the
// message list/reader split on narrow (phone) widths.
func (m *mainView) installBreakpoints(win *adw.ApplicationWindow) {
	medium := adw.NewBreakpoint(adw.BreakpointConditionParse("max-width: 1000sp"))
	medium.AddSetter(m.root, "collapsed", true)
	narrow := adw.NewBreakpoint(adw.BreakpointConditionParse("max-width: 640sp"))
	narrow.AddSetter(m.root, "collapsed", true)
	narrow.AddSetter(m.inner, "collapsed", true)
	win.AddBreakpoint(medium)
	win.AddBreakpoint(narrow)
}

func (m *mainView) buildSidebar() gtk.Widgetter {
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	hb.SetTitleWidget(m.accountSwitcher())
	menu := gtk.NewMenuButton()
	menu.SetIconName("open-menu-symbolic")
	menu.SetTooltipText("Hlavní nabídka")
	menu.SetMenuModel(m.a.primaryMenu())
	menu.SetPrimary(true)
	hb.PackEnd(menu)
	contacts := gtk.NewButtonFromIconName("x-office-address-book-symbolic")
	contacts.SetTooltipText("Kontakty")
	contacts.ConnectClicked(m.a.openContacts)
	contacts.SetVisible(m.a.acc.Caps().Contacts)
	hb.PackEnd(contacts)
	tv.AddTopBar(hb)

	m.folderList = gtk.NewListBox()
	m.folderList.AddCSSClass("navigation-sidebar")
	m.setFolders(nil)
	go func() {
		labels, err := m.a.acc.UserLabels(m.a.ctx)
		if err == nil {
			ui(func() { m.setFolders(labels) })
		}
	}()
	m.folderDropTarget()
	m.folderList.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row == nil {
			return
		}
		i := row.Index()
		if i < 0 || i >= len(m.folders) {
			return
		}
		m.stopSearch()
		m.openFolder(m.folders[i])
		if m.root.Collapsed() {
			m.root.SetShowSidebar(false)
		}
	})
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetChild(m.folderList)
	sw.SetVExpand(true)

	compose := gtk.NewButton()
	cc := adw.NewButtonContent()
	cc.SetIconName("mail-message-new-symbolic")
	cc.SetLabel("Nová zpráva")
	compose.SetChild(cc)
	compose.AddCSSClass("suggested-action")
	compose.AddCSSClass("compose-button")
	compose.SetMarginTop(6)
	compose.SetMarginBottom(6)
	compose.SetMarginStart(12)
	compose.SetMarginEnd(12)
	compose.SetTooltipText("Nová zpráva (Ctrl+N)")
	compose.ConnectClicked(func() { m.a.openCompose(nil) })
	side := gtk.NewBox(gtk.OrientationVertical, 0)
	side.Append(compose)
	side.Append(sw)
	tv.SetContent(side)
	tv.AddCSSClass("sidebar-pane")

	m.storageBar = gtk.NewProgressBar()
	m.storageBar.AddCSSClass("storage-bar")
	m.storageLabel = gtk.NewLabel("")
	m.storageLabel.AddCSSClass("caption")
	m.storageLabel.AddCSSClass("dim-label")
	m.storageLabel.SetXAlign(0)
	storage := gtk.NewBox(gtk.OrientationVertical, 4)
	storage.SetMarginStart(12)
	storage.SetMarginEnd(12)
	storage.SetMarginTop(8)
	storage.Append(m.storageBar)
	storage.Append(m.storageLabel)
	tv.AddBottomBar(storage)
	m.updateStorage()

	m.filterStatus = gtk.NewLabel("")
	m.filterStatus.AddCSSClass("dim-label")
	m.filterStatus.AddCSSClass("caption")
	m.filterStatus.SetWrap(true)
	m.filterStatus.SetXAlign(0)
	m.filterStatus.SetMarginStart(12)
	m.filterStatus.SetMarginEnd(12)
	m.filterStatus.SetMarginTop(8)
	m.filterStatus.SetMarginBottom(8)
	tv.AddBottomBar(m.filterStatus)
	return tv
}

// reloadFolders re-reads the folder list (e.g. after Klient created a
// folder for snoozed or scheduled mail on an IMAP server).
func (m *mainView) reloadFolders() {
	acc := m.a.acc
	go func() {
		labels, err := acc.UserLabels(m.a.ctx)
		ui(func() {
			if err == nil && m.a.mv == m && m.a.acc == acc {
				m.setFolders(labels)
			}
		})
	}()
}

// setFolders rebuilds the sidebar: system folders, then the user's own
// folders and labels from Proton.
func (m *mainView) setFolders(labels []protonmail.UserLabel) {
	selected := ""
	if row := m.folderList.SelectedRow(); row != nil && row.Index() < len(m.folders) {
		selected = m.folders[row.Index()].ID
	}
	clearListBox(m.folderList)
	m.labels = labels
	m.countLabels = map[string]*gtk.Label{}
	m.folders = nil
	caps := m.a.acc.Caps()
	for _, f := range protonmail.Folders {
		if (f.ID == protonmail.SnoozedID && !caps.CanSnooze()) || (f.ID == protonmail.ScheduledID && !caps.CanSchedule()) ||
			!m.a.acc.HasFolder(f.ID) {
			continue
		}
		m.folders = append(m.folders, f)
	}
	nSystem := len(m.folders)
	colors := make([]string, len(labels))
	for i, l := range labels {
		icon := "folder-symbolic"
		if !l.Folder {
			icon = "label-dot"
			colors[i] = l.Color
		}
		m.folders = append(m.folders, protonmail.Folder{ID: l.ID, Name: l.Name, Icon: icon})
	}
	setLabelColors(colors)
	for i, f := range m.folders {
		row := gtk.NewBox(gtk.OrientationHorizontal, 12)
		row.SetMarginTop(6)
		row.SetMarginBottom(6)
		row.SetMarginStart(6)
		row.SetMarginEnd(6)
		if f.Icon == "label-dot" {
			dot := gtk.NewBox(gtk.OrientationHorizontal, 0)
			dot.AddCSSClass("label-dot")
			dot.AddCSSClass(fmt.Sprintf("label-color-%d", i-nSystem))
			dot.SetVAlign(gtk.AlignCenter)
			row.Append(dot)
		} else {
			row.Append(gtk.NewImageFromIconName(f.Icon))
		}
		l := gtk.NewLabel(f.Name)
		l.SetXAlign(0)
		l.SetHExpand(true)
		l.SetEllipsize(pango.EllipsizeEnd)
		row.Append(l)
		count := gtk.NewLabel("")
		count.AddCSSClass("count-badge")
		count.SetVisible(false)
		count.SetVAlign(gtk.AlignCenter)
		row.Append(count)
		m.countLabels[f.ID] = count
		m.folderList.Append(row)
		if i == nSystem && len(labels) > 0 {
			// Visual gap between system and user folders.
			m.folderList.RowAtIndex(i).SetMarginTop(12)
		}
	}
	for i, f := range m.folders {
		if f.ID == selected {
			m.folderList.SelectRow(m.folderList.RowAtIndex(i))
		}
	}
	m.rebuildOrganizeMenus()
	m.updateCounts()
}

// updateCounts refreshes the unread badges in the sidebar.
func (m *mainView) updateCounts() {
	go func() {
		counts, err := m.a.acc.UnreadCounts(m.a.ctx)
		if err != nil {
			return
		}
		ui(func() {
			for id, l := range m.countLabels {
				n := counts[id]
				// "All mail" and "Sent" counts are not useful as badges.
				if id == protonmail.AllMailID || id == protonmail.SentID || id == protonmail.DraftsID {
					n = 0
				}
				l.SetText(fmt.Sprint(n))
				l.SetVisible(n > 0)
			}
		})
	}()
}

func (m *mainView) updateStorage() {
	st := m.a.acc.Storage()
	if st.Max == 0 {
		m.storageBar.SetVisible(false)
		m.storageLabel.SetVisible(st.Used > 0)
		if st.Used > 0 {
			// The server does not report a limit (e.g. Seznam).
			m.storageLabel.SetText("Obsazeno " + humanSize(int64(st.Used)))
			m.storageLabel.SetTooltipText("Součet velikosti zpráv ve složkách; server neuvádí limit schránky")
		}
		return
	}
	m.storageBar.SetVisible(true)
	m.storageLabel.SetVisible(true)
	frac := float64(st.Used) / float64(st.Max)
	m.storageBar.SetFraction(min(frac, 1))
	m.storageLabel.SetText(fmt.Sprintf("%s z %s (%.0f %%)", humanSize(int64(st.Used)), humanSize(int64(st.Max)), frac*100))
	for _, c := range []string{"storage-warning", "storage-full"} {
		m.storageBar.RemoveCSSClass(c)
	}
	switch {
	case frac >= 0.9:
		m.storageBar.AddCSSClass("storage-full")
	case frac >= 0.75:
		m.storageBar.AddCSSClass("storage-warning")
	}
	tip := fmt.Sprintf("Pošta zabírá %s", humanSize(int64(st.Mail)))
	if st.Split {
		tip = fmt.Sprintf("Místo pro poštu, kalendář a kontakty (bez Drive).\nPošta: %s\nDrive má vlastní místo: %s z %s",
			humanSize(int64(st.Mail)), humanSize(int64(st.Drive)), humanSize(int64(st.DriveMax)))
	} else if st.Drive > 0 {
		tip = fmt.Sprintf("Místo sdílené všemi službami Protonu.\nPošta: %s\nDrive: %s", humanSize(int64(st.Mail)), humanSize(int64(st.Drive)))
	}
	if !st.Updated.IsZero() {
		tip += "\nAktualizováno " + st.Updated.Format("2. 1. 15:04")
	}
	m.storageBar.SetTooltipText(tip)
	m.storageLabel.SetTooltipText(tip)
}

// rebuildOrganizeMenus fills the reader's "move to" and "labels" menus.
func (m *mainView) rebuildOrganizeMenus() {
	if m.moveBtn == nil {
		return
	}
	move := gio.NewMenu()
	sys := gio.NewMenu()
	for _, f := range []struct{ id, name string }{
		{protonmail.InboxID, "Doručená pošta"}, {protonmail.ArchiveID, "Archiv"},
		{protonmail.SpamID, "Spam"}, {protonmail.TrashID, "Koš"},
	} {
		sys.AppendItem(gio.NewMenuItem(f.name, "app.move-to::"+f.id))
	}
	move.AppendSection("", sys)
	user := gio.NewMenu()
	labels := gio.NewMenu()
	for _, l := range m.labels {
		if l.Folder {
			user.AppendItem(gio.NewMenuItem(l.Name, "app.move-to::"+l.ID))
		} else {
			labels.AppendItem(gio.NewMenuItem(l.Name, "app.toggle-label::"+l.ID))
		}
	}
	move.AppendSection("Moje složky", user)
	m.moveBtn.SetMenuModel(move)
	m.labelBtn.SetMenuModel(labels)
	m.labelBtn.SetVisible(labels.NItems() > 0)
}

func (m *mainView) updateFilterStatus() {
	if m.filterStatus == nil {
		return
	}
	nets, domains := m.a.lists.Stats()
	ai := "AI vypnuta"
	if m.a.ai.HasSpam() && m.a.cfg.SpamFilterEnabled {
		name := m.a.cfg.SpamProvider
		if p, ok := aipkg.ProviderByID(name); ok {
			name = p.Name
		}
		ai = name + ", " + m.a.cfg.SpamModel
		if aipkg.IsLocal(m.a.cfg.SpamProvider) {
			ai = "lokální AI (" + m.a.cfg.SpamModel + ", nic neodchází)"
		}
	} else if m.a.cfg.SpamFilterEnabled {
		ai = "jen blocklisty (AI není nastavená)"
	}
	upd := "nikdy"
	if !m.a.lists.LastUpdate.IsZero() {
		upd = m.a.lists.LastUpdate.Format("2. 1. 15:04")
	}
	m.filterStatus.SetText(fmt.Sprintf("Spamfiltr: %s\nBlocklisty: %d sítí, %d domén · aktualizace %s", ai, nets, domains, upd))
}

func (m *mainView) buildList() gtk.Widgetter {
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	m.listTitle = adw.NewWindowTitle("", "")
	hb.SetTitleWidget(m.listTitle)

	sidebarBtn := gtk.NewToggleButton()
	sidebarBtn.SetIconName("sidebar-show-symbolic")
	sidebarBtn.SetTooltipText("Složky")
	sidebarBtn.SetActive(m.root.ShowSidebar())
	sidebarBtn.SetVisible(m.root.Collapsed())
	sidebarBtn.ConnectToggled(func() {
		if m.root.ShowSidebar() != sidebarBtn.Active() {
			m.root.SetShowSidebar(sidebarBtn.Active())
		}
	})
	m.root.NotifyProperty("show-sidebar", func() {
		if sidebarBtn.Active() != m.root.ShowSidebar() {
			sidebarBtn.SetActive(m.root.ShowSidebar())
		}
	})
	m.root.NotifyProperty("collapsed", func() { sidebarBtn.SetVisible(m.root.Collapsed()) })
	hb.PackStart(sidebarBtn)

	compose := gtk.NewButtonFromIconName("mail-message-new-symbolic")
	compose.SetTooltipText("Nová zpráva (Ctrl+N)")
	compose.ConnectClicked(func() { m.a.openCompose(nil) })
	hb.PackStart(compose)

	refresh := gtk.NewButtonFromIconName("view-refresh-symbolic")
	refresh.SetTooltipText("Obnovit (F5)")
	refresh.ConnectClicked(func() { m.openFolder(m.folder) })
	hb.PackEnd(refresh)
	unreadBtn := gtk.NewToggleButton()
	unreadBtn.SetIconName("mail-unread-symbolic")
	unreadBtn.SetTooltipText("Jen nepřečtené")
	unreadBtn.ConnectToggled(func() {
		m.unreadOnly = unreadBtn.Active()
		m.rebuildList()
	})
	hb.PackEnd(unreadBtn)
	searchBtn := gtk.NewToggleButton()
	searchBtn.SetIconName("system-search-symbolic")
	searchBtn.SetTooltipText("Hledat (Ctrl+F)")
	hb.PackEnd(searchBtn)
	tv.AddTopBar(hb)

	m.searchEntry = gtk.NewSearchEntry()
	m.searchEntry.SetPlaceholderText("Hledat v předmětu, odesílateli a příjemcích")
	m.searchEntry.SetHExpand(true)
	m.searchBar = gtk.NewSearchBar()
	clampS := adw.NewClamp()
	clampS.SetChild(m.searchEntry)
	m.searchBar.SetChild(clampS)
	m.searchBar.ConnectEntry(m.searchEntry)
	m.searchBar.NotifyProperty("search-mode-enabled", func() {
		on := m.searchBar.SearchMode()
		if searchBtn.Active() != on {
			searchBtn.SetActive(on)
		}
		if !on && m.searchQuery != "" {
			m.searchQuery = ""
			m.openFolder(m.folder)
		}
	})
	searchBtn.ConnectToggled(func() { m.searchBar.SetSearchMode(searchBtn.Active()) })
	m.searchEntry.ConnectActivate(func() { m.runSearch(m.searchEntry.Text()) })
	m.searchEntry.ConnectStopSearch(func() { m.stopSearch() })
	tv.AddTopBar(m.searchBar)
	m.offline = adw.NewBanner("Offline – zobrazuje se pošta uložená v šifrované cache")
	tv.AddTopBar(m.offline)
	cbox := gtk.NewBox(gtk.OrientationHorizontal, 8)
	cbox.AddCSSClass("checking-bar")
	sp := gtk.NewSpinner()
	sp.Start()
	m.checkLabel = gtk.NewLabel("")
	m.checkLabel.SetXAlign(0)
	m.checkLabel.AddCSSClass("caption")
	cbox.Append(sp)
	cbox.Append(m.checkLabel)
	m.checking = gtk.NewRevealer()
	m.checking.SetChild(cbox)
	m.checking.SetTooltipText("Nová pošta se zobrazí, až ji posoudí spamfiltr (lze vypnout v Předvolbách → Spamfiltr)")
	tv.AddTopBar(m.checking)

	m.msgList = gtk.NewListBox()
	m.msgList.AddCSSClass("navigation-sidebar")
	m.msgList.ConnectRowActivated(func(row *gtk.ListBoxRow) {
		i := row.Index()
		if i < 0 || i >= len(m.items) {
			return
		}
		if m.selecting {
			m.toggleRow(i, false)
			return
		}
		m.openThread(m.items[i])
	})
	m.msgList.SetSelectionMode(gtk.SelectionBrowse)
	m.selected = map[int]bool{}
	m.lastToggled = -1

	m.moreBtn = gtk.NewButtonWithLabel("Načíst starší zprávy")
	m.moreBtn.AddCSSClass("flat")
	m.moreBtn.SetMarginTop(6)
	m.moreBtn.SetMarginBottom(12)
	m.moreBtn.SetHAlign(gtk.AlignCenter)
	m.moreBtn.ConnectClicked(func() { m.loadPage(m.page + 1) })

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(m.msgList)
	box.Append(m.moreBtn)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetChild(box)

	empty := adw.NewStatusPage()
	empty.SetIconName("mail-read-symbolic")
	empty.SetTitle("Žádné zprávy")

	m.listStack = gtk.NewStack()
	m.listStack.AddNamed(spinnerBox(), "loading")
	m.listStack.AddNamed(empty, "empty")
	m.listStack.AddNamed(sw, "list")
	tv.SetContent(m.listStack)
	m.buildSelection(tv, hb)
	// Local shortcuts: only while the list has focus, so text fields keep
	// their own Ctrl+A / Escape / Delete.
	localShortcuts(tv, map[string]func(){
		"<Control>a": func() {
			if !m.selecting {
				m.selectBtn.SetActive(true)
			}
			m.selectAll()
		},
		"Escape": m.leaveSelection,
		"Delete": func() { m.moveCurrent(protonmail.TrashID, "Přesunuto do koše") },
	})
	return tv
}

func spinnerBox() gtk.Widgetter {
	s := gtk.NewSpinner()
	s.SetSizeRequest(32, 32)
	s.SetHAlign(gtk.AlignCenter)
	s.SetVAlign(gtk.AlignCenter)
	s.Start()
	return s
}

func (m *mainView) buildReader() gtk.Widgetter {
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	hb.SetShowTitle(false)

	reply := gtk.NewButtonFromIconName("mail-reply-sender-symbolic")
	reply.SetTooltipText("Odpovědět (Ctrl+R)")
	reply.ConnectClicked(func() { m.reply(protonmail.ActionReply) })
	replyAll := gtk.NewButtonFromIconName("mail-reply-all-symbolic")
	replyAll.SetTooltipText("Odpovědět všem (Ctrl+Shift+R)")
	replyAll.ConnectClicked(func() { m.reply(protonmail.ActionReplyAll) })
	forward := gtk.NewButtonFromIconName("mail-forward-symbolic")
	forward.SetTooltipText("Přeposlat (Ctrl+L)")
	forward.ConnectClicked(func() { m.reply(protonmail.ActionForward) })
	m.starBtn = gtk.NewButtonFromIconName("non-starred-symbolic")
	m.starBtn.SetTooltipText("Hvězdička (Ctrl+D)")
	m.starBtn.ConnectClicked(m.toggleStar)
	unread := gtk.NewButtonFromIconName("mail-unread-symbolic")
	unread.SetTooltipText("Označit jako nepřečtené (Ctrl+Shift+U)")
	unread.ConnectClicked(m.markUnread)
	archive := gtk.NewButtonFromIconName("folder-documents-symbolic")
	archive.SetTooltipText("Archivovat (Ctrl+E)")
	archive.ConnectClicked(func() { m.moveCurrent(protonmail.ArchiveID, "Archivováno") })
	m.moveBtn = gtk.NewMenuButton()
	m.moveBtn.SetIconName("folder-open-symbolic")
	m.moveBtn.SetTooltipText("Přesunout do složky")
	m.labelBtn = gtk.NewMenuButton()
	m.labelBtn.SetIconName("bookmark-new-symbolic")
	m.labelBtn.SetTooltipText("Štítky")
	m.summaryBtn = gtk.NewButtonWithLabel("Shrnout")
	m.summaryBtn.SetTooltipText("Shrnutí zprávy nebo celého vlákna pomocí AI")
	m.summaryBtn.ConnectClicked(m.summarize)

	m.snoozeBtn = m.snoozeButton()
	m.unsnoozeBtn = gtk.NewButtonFromIconName("mail-send-receive-symbolic")
	m.unsnoozeBtn.SetTooltipText("Vrátit do doručené pošty hned")
	m.unsnoozeBtn.ConnectClicked(m.unsnooze)
	m.unsnoozeBtn.SetVisible(false)

	m.spamBtn = gtk.NewButtonFromIconName("mail-mark-junk-symbolic")
	m.spamBtn.ConnectClicked(m.toggleSpam)

	trash := gtk.NewButtonFromIconName("user-trash-symbolic")
	trash.SetTooltipText("Přesunout do koše")
	trash.ConnectClicked(func() { m.moveCurrent(protonmail.TrashID, "Přesunuto do koše") })

	hb.PackStart(reply)
	hb.PackStart(replyAll)
	hb.PackStart(forward)
	hb.PackStart(m.summaryBtn)
	hb.PackEnd(trash)
	hb.PackEnd(m.spamBtn)
	hb.PackEnd(m.labelBtn)
	hb.PackEnd(m.moveBtn)
	hb.PackEnd(archive)
	hb.PackEnd(m.unsnoozeBtn)
	hb.PackEnd(m.snoozeBtn)
	hb.PackEnd(unread)
	hb.PackEnd(m.starBtn)
	m.actionBtns = []*gtk.Button{reply, replyAll, forward, m.summaryBtn, m.spamBtn, trash, m.starBtn, unread, archive}
	tv.AddTopBar(hb)

	m.banner = adw.NewBanner("")
	m.banner.ConnectButtonClicked(func() {
		if m.bannerFn != nil {
			m.bannerFn()
		}
	})
	tv.AddTopBar(m.banner)

	content := gtk.NewBox(gtk.OrientationVertical, 12)
	content.SetMarginTop(18)
	content.SetMarginBottom(24)
	content.SetMarginStart(18)
	content.SetMarginEnd(18)

	label := func(classes ...string) *gtk.Label {
		l := gtk.NewLabel("")
		l.SetXAlign(0)
		l.SetWrap(true)
		l.SetWrapMode(pango.WrapWordChar)
		l.SetSelectable(true)
		for _, c := range classes {
			l.AddCSSClass(c)
		}
		return l
	}
	m.subject = label("title-2")
	content.Append(m.subject)

	m.summaryCard = gtk.NewBox(gtk.OrientationVertical, 6)
	m.summaryCard.AddCSSClass("card")
	m.summaryCard.AddCSSClass("summary-card")
	title := gtk.NewLabel("Shrnutí (AI)")
	title.AddCSSClass("heading")
	title.SetXAlign(0)
	m.summaryLabel = label()
	for _, w := range []*gtk.Label{title, m.summaryLabel} {
		w.SetMarginStart(12)
		w.SetMarginEnd(12)
	}
	title.SetMarginTop(12)
	m.summaryLabel.SetMarginBottom(12)
	m.summaryCard.Append(title)
	m.summaryCard.Append(m.summaryLabel)
	m.summaryCard.SetVisible(false)
	content.Append(m.summaryCard)

	m.threadBox = gtk.NewBox(gtk.OrientationVertical, 12)
	content.Append(m.threadBox)

	clamp := adw.NewClamp()
	clamp.SetMaximumSize(960)
	clamp.SetChild(content)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	// Do not jump to a widget that gets keyboard focus (e.g. after the
	// message list refreshes): that scrolled the reader back to the top.
	vp := gtk.NewViewport(nil, nil)
	vp.SetScrollToFocus(false)
	vp.SetChild(clamp)
	sw.SetChild(vp)

	empty := adw.NewStatusPage()
	empty.SetIconName("mail-read-symbolic")
	empty.SetTitle("Není vybrána žádná zpráva")
	m.emptyTitle = empty

	m.readerStack = gtk.NewStack()
	m.readerStack.AddNamed(empty, "empty")
	m.readerStack.AddNamed(spinnerBox(), "loading")
	m.readerStack.AddNamed(sw, "message")
	tv.SetContent(m.readerStack)
	m.setReaderActions(false)
	m.rebuildOrganizeMenus()
	localShortcuts(tv, map[string]func(){
		"Delete": func() { m.moveCurrent(protonmail.TrashID, "Přesunuto do koše") },
	})
	return tv
}

func localShortcuts(w gtk.Widgetter, keys map[string]func()) {
	sc := gtk.NewShortcutController()
	for accel, f := range keys {
		f := f
		sc.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString(accel),
			gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { f(); return true })))
	}
	gtk.BaseWidget(w).AddController(sc)
}

func (m *mainView) setReaderActions(on bool) {
	for _, b := range m.actionBtns {
		b.SetSensitive(on)
	}
	if m.moveBtn != nil {
		m.moveBtn.SetSensitive(on)
		m.labelBtn.SetSensitive(on)
		m.snoozeBtn.SetSensitive(on)
		m.unsnoozeBtn.SetSensitive(on)
	}
}

func (m *mainView) selectFolder(i int) {
	m.folderList.SelectRow(m.folderList.RowAtIndex(i))
}

func (m *mainView) openFolder(f protonmail.Folder) {
	m.folder = f
	if m.snoozeBtn != nil {
		m.unsnoozeBtn.SetVisible(f.ID == protonmail.SnoozedID)
		m.snoozeBtn.SetVisible(m.a.acc.Caps().CanSnooze() && f.ID != protonmail.SnoozedID && f.ID != protonmail.ScheduledID &&
			f.ID != protonmail.DraftsID && f.ID != protonmail.SentID)
	}
	m.listTitle.SetTitle(f.Name)
	m.listTitle.SetSubtitle("")
	m.listPage.SetTitle(f.Name)
	m.loadPage(0)
}

func clearListBox(lb *gtk.ListBox) {
	for {
		child := lb.FirstChild()
		if child == nil {
			return
		}
		lb.Remove(child)
	}
}

func (m *mainView) loadPage(page int) {
	m.loadSeq++
	seq, folder := m.loadSeq, m.folder
	if page == 0 {
		m.listStack.SetVisibleChildName("loading")
	}
	m.moreBtn.SetSensitive(false)
	go func() {
		ctx, cancel := context.WithTimeout(m.a.ctx, 60*time.Second)
		defer cancel()
		msgs, err := m.a.acc.List(ctx, folder.ID, page, pageSize)
		ui(func() {
			if seq != m.loadSeq {
				return // a newer load superseded this one
			}
			m.moreBtn.SetSensitive(true)
			if err != nil {
				m.a.toast("Načtení zpráv selhalo: " + err.Error())
				if page == 0 {
					m.listStack.SetVisibleChildName("empty")
				}
				return
			}
			if page == 0 {
				m.msgs = nil
			}
			m.page = page
			m.msgs = append(m.msgs, msgs...)
			m.rebuildList()
			m.moreBtn.SetVisible(len(msgs) == pageSize)
			unread := 0
			for _, t := range m.items {
				if t.Unread() {
					unread++
				}
			}
			if unread > 0 {
				m.listTitle.SetSubtitle(fmt.Sprintf("%d nepřečtených", unread))
			} else {
				m.listTitle.SetSubtitle("")
			}
		})
	}()
}

// scheduleRefresh coalesces bursts of mailbox events into one reload.
func (m *mainView) scheduleRefresh() {
	if m.pending {
		return
	}
	m.pending = true
	glib.TimeoutSecondsAdd(2, func() bool {
		m.pending = false
		if m.page == 0 && m.searchQuery == "" {
			m.loadPage(0)
		}
		m.updateCounts()
		return false
	})
}

func formatTime(unix int64) string {
	t := time.Unix(unix, 0)
	now := time.Now()
	switch {
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("2. 1.")
	default:
		return t.Format("2. 1. 2006")
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func categoryName(c string) string {
	switch c {
	case "ham":
		return "legitimní"
	case "newsletter":
		return "newsletter"
	case "spam":
		return "spam"
	case "phishing":
		return "phishing"
	case "scam":
		return "podvod"
	case "malware":
		return "malware"
	}
	return c
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (m *mainView) saveAttachment(att protonmail.Attachment) {
	dlg := gtk.NewFileDialog()
	dlg.SetInitialName(orDefault(att.Name, "priloha"))
	dlg.Save(context.Background(), m.a.gtkWindow(), func(res gio.AsyncResulter) {
		file, err := dlg.SaveFinish(res)
		if err != nil || file == nil {
			return
		}
		path := file.Path()
		go func() {
			data, err := m.a.acc.AttachmentData(m.a.ctx, att)
			if err == nil {
				err = os.WriteFile(path, data, 0o600)
			}
			ui(func() {
				if err != nil {
					m.a.toast("Uložení přílohy selhalo: " + err.Error())
					return
				}
				m.a.toast("Příloha uložena")
			})
		}()
	})
}

// scanInbox runs the spam filter over the first page of the inbox for
// messages that have no decision yet.
func (m *mainView) scanInbox() {
	if !m.a.ai.HasSpam() {
		m.a.toastWithAction("AI spamfiltr není nastavený", "Nastavit", m.a.openPreferences)
		return
	}
	m.a.toast("Prověřuji doručenou poštu…")
	go func() {
		msgs, err := m.a.acc.List(m.a.ctx, protonmail.InboxID, 0, pageSize)
		moved := 0
		if err == nil {
			for _, s := range msgs {
				if _, mv, e := m.a.filter.ProcessNew(m.a.ctx, m.a.acc, s); e == nil && mv {
					moved++
				}
			}
		}
		ui(func() {
			if err != nil {
				m.a.toast("Prověření selhalo: " + err.Error())
				return
			}
			m.a.toast(fmt.Sprintf("Hotovo, do spamu přesunuto: %d", moved))
			m.scheduleRefresh()
		})
	}()
}

func hasLabel(meta protonmail.Summary, id string) bool {
	for _, l := range meta.LabelIDs {
		if l == id {
			return true
		}
	}
	return false
}

func (m *mainView) startSearch() {
	m.searchBar.SetSearchMode(true)
	m.searchEntry.GrabFocus()
}

func (m *mainView) stopSearch() {
	if m.searchBar != nil && m.searchBar.SearchMode() {
		m.searchQuery = ""
		m.searchEntry.SetText("")
		m.searchBar.SetSearchMode(false)
	}
}

// runSearch searches the current folder (the whole mailbox for the Inbox,
// since people usually mean "find that e-mail").
func (m *mainView) runSearch(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	m.searchQuery = query
	scope := m.folder
	if scope.ID == protonmail.InboxID {
		scope = protonmail.FolderByID(protonmail.AllMailID)
	}
	m.loadSeq++
	seq := m.loadSeq
	m.listStack.SetVisibleChildName("loading")
	m.listTitle.SetTitle("Hledání: " + query)
	m.listTitle.SetSubtitle("v " + scope.Name)
	m.moreBtn.SetVisible(false)
	go func() {
		ctx, cancel := context.WithTimeout(m.a.ctx, 2*time.Minute)
		defer cancel()
		res, err := m.a.acc.Search(ctx, scope.ID, query, 3000)
		ui(func() {
			if seq != m.loadSeq {
				return
			}
			if err != nil && len(res) == 0 {
				m.a.toast("Hledání selhalo: " + err.Error())
			}
			m.msgs = res
			m.rebuildList()
			m.listTitle.SetSubtitle(fmt.Sprintf("%d výsledků v %s", len(res), scope.Name))
		})
	}()
}
