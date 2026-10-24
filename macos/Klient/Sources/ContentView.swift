import SwiftUI

struct ContentView: View {
    @EnvironmentObject var state: AppState
    @State private var columnVisibility = NavigationSplitViewVisibility.all
    @State private var printToken = 0
    @State private var confirmDeleteFolder: AppState.FolderRow?

    var body: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
            sidebar
        } content: {
            messageList
        } detail: {
            detail
        }
        .navigationSplitViewStyle(.balanced)
        .background(Color(nsColor: .windowBackgroundColor))
        .safeAreaInset(edge: .top, spacing: 0) {
            if state.offlineBanner {
                HStack(spacing: 8) {
                    Image(systemName: "icloud.slash")
                    Text(String(localized: "Offline – showing the mail saved in the encrypted cache"))
                        .font(.caption.weight(.medium))
                    Spacer(minLength: 0)
                }
                .foregroundStyle(.primary)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity)
                .background(Color.orange.opacity(0.18))
                .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .animation(.snappy, value: state.offlineBanner)
        .searchable(text: $state.searchQuery, placement: .toolbar, prompt: String(localized: "Search Mail"))
        .onSubmit(of: .search) { state.runSearch() }
        .onChange(of: state.searchQuery) { _, q in
            if q.isEmpty { state.reloadMessages() }
        }
        .onChange(of: state.selectedAccount) { _, _ in
            withAnimation(.snappy(duration: 0.25)) {
                state.loadFoldersAndMail()
            }
        }
        .onChange(of: state.selectedMessageIDs) { _, ids in
            if ids.count == 1, let id = ids.first {
                if state.selectedMessageID != id {
                    state.open(id)
                }
            } else if ids.count > 1 {
                state.openMessage = nil
                state.selectedMessageID = nil
            }
        }
        .onChange(of: state.pendingPrint) { _, pending in
            if pending {
                printToken += 1
                state.pendingPrint = false
            }
        }
        .alert(state.folderPromptAsLabel
               ? String(localized: "New Label")
               : String(localized: "New Folder"), isPresented: Binding(
            get: { state.showFolderNamePrompt && state.folderPromptMode == .create },
            set: { if !$0 { state.showFolderNamePrompt = false; state.folderPromptAsLabel = false } }
        )) {
            TextField(String(localized: "Name"), text: $state.folderPromptName)
            Button(String(localized: "Cancel"), role: .cancel) {
                state.showFolderNamePrompt = false
                state.folderPromptAsLabel = false
            }
            Button(String(localized: "Create")) {
                state.createFolder(name: state.folderPromptName, asLabel: state.folderPromptAsLabel)
                state.showFolderNamePrompt = false
            }
        }
        .alert(
            (state.folders.first(where: { $0.id == state.folderPromptTargetID })?.isLabel == true)
                ? String(localized: "Rename Label")
                : String(localized: "Rename Folder"),
            isPresented: Binding(
                get: { state.showFolderNamePrompt && state.folderPromptMode == .rename },
                set: { if !$0 { state.showFolderNamePrompt = false } }
            )
        ) {
            TextField(String(localized: "Name"), text: $state.folderPromptName)
            Button(String(localized: "Cancel"), role: .cancel) { state.showFolderNamePrompt = false }
            Button(String(localized: "Rename")) {
                state.renameFolder(id: state.folderPromptTargetID, name: state.folderPromptName)
                state.showFolderNamePrompt = false
            }
        }
        .confirmationDialog(
            confirmDeleteFolder?.isLabel == true
                ? String(localized: "Delete label?")
                : String(localized: "Delete folder?"),
            isPresented: Binding(
                get: { confirmDeleteFolder != nil },
                set: { if !$0 { confirmDeleteFolder = nil } }
            ),
            titleVisibility: .visible
        ) {
            if let f = confirmDeleteFolder {
                Button(String(localized: "Delete"), role: .destructive) {
                    state.deleteFolder(id: f.id, name: f.name)
                    confirmDeleteFolder = nil
                }
            }
            Button(String(localized: "Cancel"), role: .cancel) { confirmDeleteFolder = nil }
        } message: {
            if confirmDeleteFolder?.isLabel == true {
                Text(String(localized: "Only the label is removed from messages; the messages stay."))
            } else {
                Text(String(localized: "Messages in the folder go back to the inbox; with a label, only the label is removed from the messages."))
            }
        }
        .confirmationDialog(
            String(format: String(localized: "Unsubscribe from %@?"),
                   state.openMessage?.sender ?? ""),
            isPresented: $state.showUnsubscribeConfirm,
            titleVisibility: .visible
        ) {
            Button(String(localized: "Unsubscribe"), role: .destructive) {
                state.confirmUnsubscribe()
            }
            Button(String(localized: "Cancel"), role: .cancel) {
                state.showUnsubscribeConfirm = false
            }
        } message: {
            Text(state.unsubscribeConfirmBody)
        }
        .overlay(alignment: .top) {
            StatusBanner(
                text: state.status,
                actionTitle: state.undoSendActive ? String(localized: "Undo") : nil,
                action: state.undoSendActive ? { state.undoPendingSend() } : nil
            )
            .padding(.top, 8)
            .animation(.snappy, value: state.status)
            .animation(.snappy, value: state.undoSendActive)
            .allowsHitTesting(state.undoSendActive)
        }
        .sheet(isPresented: $state.showLogin) {
            LoginSheet().environmentObject(state)
        }
        .sheet(isPresented: $state.showCompose) {
            ComposeView(replyTo: state.composeReply, action: state.composeAction)
                .environmentObject(state)
                .frame(minWidth: 640, minHeight: 520)
        }
        .background(SettingsOpener())
        .sheet(isPresented: $state.showContacts) {
            NavigationStack {
                ContactsView().environmentObject(state)
            }
            .frame(minWidth: 480, minHeight: 420)
        }
        .sheet(isPresented: $state.showAddAccount) {
            AddAccountView().environmentObject(state)
        }
        .sheet(isPresented: $state.showAskAI) {
            AskAISheet().environmentObject(state)
        }
        .sheet(isPresented: $state.showAbout) {
            AboutView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
        }
        .sheet(isPresented: $state.showAutoReply) {
            AutoReplyView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
        }
        .sheet(isPresented: $state.showUpdate) {
            UpdateView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
                .id(state.localeEpoch)
        }
        .sheet(isPresented: $state.showWhatsNew) {
            WhatsNewView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
                .id(state.localeEpoch)
        }
        .sheet(isPresented: $state.showSnoozePicker) {
            SnoozePickerSheet()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
        }
        .sheet(isPresented: $state.showHelp) {
            HelpView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
                .id(state.localeEpoch)
        }
        .sheet(isPresented: $state.showShortcuts) {
            ShortcutsHelpView()
        }
        .sheet(isPresented: $state.showOutbox) {
            OutboxView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
        }
    }

    // MARK: - Sidebar

    private var sidebar: some View {
        List(selection: $state.selectedAccount) {
            Section {
                ForEach(state.accounts) { acc in
                    let kind = acc.username == "unified" ? "unified" : acc.kind
                    Label {
                        Text(KlientLocale.accountDisplayName(
                            username: acc.username,
                            displayName: acc.displayName,
                            email: acc.email
                        ))
                        .lineLimit(1)
                    } icon: {
                        Image(systemName: KlientTheme.accountIcon(kind))
                            .symbolRenderingMode(.hierarchical)
                            .foregroundStyle(KlientTheme.accountTint(kind))
                    }
                    .tag(Optional(acc.username))
                }
            } header: {
                Text(String(localized: "Accounts"))
            }

            Section {
                ForEach(state.mailboxFolders.filter { !state.hiddenFolderIDs.contains($0.id) }) { f in
                    sidebarFolderRow(f)
                }
            } header: {
                HStack {
                    Text(String(localized: "Mailboxes"))
                    Spacer()
                    if state.selectedAccount != nil && state.selectedAccount != "unified" {
                        Button {
                            state.folderPromptMode = .create
                            state.folderPromptAsLabel = false
                            state.folderPromptName = ""
                            state.showFolderNamePrompt = true
                        } label: {
                            Image(systemName: "folder.badge.plus")
                        }
                        .buttonStyle(.borderless)
                        .help(String(localized: "New Folder…"))
                    }
                }
            }

            if state.selectedAccount != nil && state.selectedAccount != "unified" {
                Section {
                    ForEach(state.tagLabels.filter { !state.hiddenFolderIDs.contains($0.id) }) { f in
                        sidebarFolderRow(f)
                    }
                } header: {
                    HStack {
                        Text(String(localized: "Labels"))
                        Spacer()
                        Button {
                            state.folderPromptMode = .create
                            state.folderPromptAsLabel = true
                            state.folderPromptName = ""
                            state.showFolderNamePrompt = true
                        } label: {
                            Image(systemName: "tag")
                        }
                        .buttonStyle(.borderless)
                        .help(String(localized: "New Label…"))
                    }
                }
            }

            if !state.hiddenFolderIDs.isEmpty {
                Section {
                    ForEach(state.folders.filter { state.hiddenFolderIDs.contains($0.id) }) { f in
                        Button {
                            state.toggleFolderHidden(f.id)
                        } label: {
                            Label(
                                KlientLocale.folderDisplayName(id: f.id, name: f.name),
                                systemImage: "eye.slash"
                            )
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .help(String(localized: "Show in Sidebar"))
                    }
                } header: {
                    Text(String(localized: "Hidden"))
                }
            }

            if state.outboxCount > 0 {
                Section {
                    Button {
                        state.showOutbox = true
                    } label: {
                        Label {
                            Text(String(format: String(localized: "Outbox (%lld)"), state.outboxCount))
                                .foregroundStyle(KlientTheme.brand)
                        } icon: {
                            Image(systemName: "paperplane.fill")
                                .foregroundStyle(KlientTheme.brand)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(String(localized: "Messages that could not be sent"))
                }
            }
        }
        .listStyle(.sidebar)
        .navigationSplitViewColumnWidth(
            min: KlientTheme.sidebarMin,
            ideal: KlientTheme.sidebarIdeal,
            max: 280
        )
        .navigationTitle("Klient")
        .safeAreaInset(edge: .bottom, spacing: 0) {
            // Do not apply .fixedSize here — it fights NavigationSplitView's
            // column height and can crush the three-column layout (or crash layout).
            FilterStatusStrip(status: state.filterStatus)
        }
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button {
                    state.showAddAccount = true
                } label: {
                    Image(systemName: "person.badge.plus")
                }
                .help(String(localized: "Add Account"))

                Menu {
                    Button(String(localized: "Proton Sign In…")) { state.showLogin = true }
                    Button(String(localized: "Settings…")) { KlientSettings.open() }
                    Divider()
                    Button(String(localized: "Log Out")) { state.logout() }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
            }
        }
        .task {
            state.refreshFilterStatus()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                state.refreshFilterStatus()
            }
        }
    }

    @ViewBuilder
    private func sidebarFolderRow(_ f: AppState.FolderRow) -> some View {
        let title = KlientLocale.folderDisplayName(id: f.id, name: f.name)
        let style = KlientTheme.folderStyle(f.name, isLabel: f.isLabel, colorHex: f.color)
        Button {
            withAnimation(.snappy(duration: 0.2)) {
                state.selectedFolder = f.id
                state.searchQuery = ""
                state.selectedMessageIDs = []
                state.selectedMessageID = nil
                state.openMessage = nil
                state.reloadMessages()
            }
        } label: {
            Label {
                Text(title)
                    .fontWeight(state.selectedFolder == f.id ? .semibold : .regular)
            } icon: {
                Image(systemName: style.symbol)
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(style.tint)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .listRowBackground(
            RoundedRectangle(cornerRadius: 7, style: .continuous)
                .fill(state.selectedFolder == f.id
                      ? Color.accentColor.opacity(0.14)
                      : Color.clear)
                .padding(.horizontal, 2)
        )
        .dropDestination(for: String.self) { ids, _ in
            guard !ids.isEmpty else { return false }
            state.selectedMessageIDs = Set(ids)
            state.bulkMove(to: f.id)
            return true
        }
        .contextMenu {
            if state.selectedAccount != "unified" {
                if f.isLabel {
                    Button(String(localized: "New Label…")) {
                        state.folderPromptMode = .create
                        state.folderPromptAsLabel = true
                        state.folderPromptName = ""
                        state.showFolderNamePrompt = true
                    }
                } else {
                    Button(String(localized: "New Folder…")) {
                        state.folderPromptMode = .create
                        state.folderPromptAsLabel = false
                        state.folderPromptName = ""
                        state.showFolderNamePrompt = true
                    }
                }
                Button(state.hiddenFolderIDs.contains(f.id)
                       ? String(localized: "Show in Sidebar")
                       : String(localized: "Hide from Sidebar")) {
                    state.toggleFolderHidden(f.id)
                }
                if f.custom {
                    Button(String(localized: "Rename…")) {
                        state.folderPromptMode = .rename
                        state.folderPromptTargetID = f.id
                        state.folderPromptName = f.name
                        state.showFolderNamePrompt = true
                    }
                    Button(
                        f.isLabel
                            ? String(localized: "Delete Label…")
                            : String(localized: "Delete Folder…"),
                        role: .destructive
                    ) {
                        confirmDeleteFolder = f
                    }
                }
                if f.id == state.labels.trash || f.id == state.labels.spam {
                    Divider()
                    Button(String(localized: "Empty…"), role: .destructive) {
                        state.emptyFolder(f.id)
                    }
                }
            }
        }
    }

    // MARK: - Message list

    private var messageList: some View {
        Group {
            if state.messages.isEmpty {
                ContentUnavailableView {
                    Label(String(localized: "No Messages"), systemImage: "tray")
                } description: {
                    Text(state.searchQuery.isEmpty
                          ? String(localized: "This mailbox is empty.")
                          : String(localized: "Nothing matched your search."))
                }
            } else {
                List(selection: $state.selectedMessageIDs) {
                    ForEach(state.messages) { m in
                        MessageRowView(
                            message: m,
                            selected: state.selectedMessageIDs.contains(m.id),
                            countdownMode: state.isScheduledFolder || state.isSnoozedFolder
                        )
                        .equatable()
                        .tag(m.id)
                        .listRowInsets(EdgeInsets(top: 4, leading: 10, bottom: 4, trailing: 12))
                        .draggable(m.id)
                        .contextMenu {
                            messageContextMenu(for: m)
                        }
                    }
                }
                .listStyle(.inset(alternatesRowBackgrounds: true))
            }
        }
        .navigationTitle(currentFolderName)
        .navigationSubtitle(folderSubtitle)
        .navigationSplitViewColumnWidth(
            min: KlientTheme.listMin,
            ideal: KlientTheme.listIdeal,
            max: 440
        )
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if state.selectedMessageIDs.count > 1 {
                    Button { state.bulkMove(to: state.labels.archive) } label: {
                        Image(systemName: "archivebox")
                    }
                    .help(String(localized: "Archive"))
                    Button { state.bulkMove(to: state.labels.trash) } label: {
                        Image(systemName: "trash")
                    }
                    .help(String(localized: "Trash"))
                    Button { state.bulkMarkSpam(true) } label: {
                        Image(systemName: "flame")
                    }
                    .help(String(localized: "Spam"))
                    Button { state.bulkToggleStar() } label: {
                        Image(systemName: "star")
                    }
                    .help(String(localized: "Star"))
                    Menu {
                        ForEach(state.mailboxFolders) { f in
                            Button(KlientLocale.folderDisplayName(id: f.id, name: f.name)) {
                                state.bulkMove(to: f.id)
                            }
                        }
                        if !state.tagLabels.isEmpty {
                            Divider()
                            ForEach(state.tagLabels) { f in
                                Button(f.name) { state.toggleLabel(f.id) }
                            }
                        }
                    } label: {
                        Image(systemName: "folder")
                    }
                    .help(String(localized: "Move"))
                    Button { state.bulkMarkRead(true) } label: {
                        Image(systemName: "envelope.open")
                    }
                    .help(String(localized: "Mark as read"))
                    Button { state.bulkMarkRead(false) } label: {
                        Image(systemName: "envelope.badge")
                    }
                    .help(String(localized: "Mark Unread"))
                }
                Button { state.newMessage() } label: {
                    Label(String(localized: "Compose"), systemImage: "square.and.pencil")
                }
                .help(String(localized: "New Message"))

                Button { state.reloadMessages() } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .help(String(localized: "Refresh"))

                Button { state.scanSpam() } label: {
                    Image(systemName: "flame.fill")
                }
                .help(String(localized: "Scan Inbox for Spam"))
            }
        }
    }

    @ViewBuilder
    private func messageContextMenu(for m: AppState.MessageRow) -> some View {
        Button(String(localized: "Archive")) {
            state.selectedMessageIDs = [m.id]
            state.bulkMove(to: state.labels.archive)
        }
        Button(String(localized: "Trash")) {
            state.selectedMessageIDs = [m.id]
            state.bulkMove(to: state.labels.trash)
        }
        Button(String(localized: "Spam")) {
            state.selectedMessageIDs = [m.id]
            state.bulkMove(to: state.labels.spam)
        }
        Divider()
        Button(String(localized: "Mark as read")) {
            state.selectedMessageIDs = [m.id]
            state.bulkMarkRead(true)
        }
        Button(String(localized: "Mark Unread")) {
            state.selectedMessageIDs = [m.id]
            state.bulkMarkRead(false)
        }
        if state.isDraftsFolder {
            Divider()
            Button(String(localized: "Edit Draft")) { state.openDraft(m.id) }
            Button(String(localized: "Delete Draft"), role: .destructive) { state.deleteDraft(m.id) }
        }
        Menu(String(localized: "Move to")) {
            ForEach(state.mailboxFolders) { f in
                Button(KlientLocale.folderDisplayName(id: f.id, name: f.name)) {
                    state.selectedMessageIDs = [m.id]
                    state.bulkMove(to: f.id)
                }
            }
        }
        if !state.tagLabels.isEmpty {
            Menu(String(localized: "Labels")) {
                ForEach(state.tagLabels) { lab in
                    Button {
                        state.selectedMessageIDs = [m.id]
                        state.toggleLabel(lab.id)
                    } label: {
                        if m.labels.contains(lab.id) {
                            Label(lab.name, systemImage: "checkmark")
                        } else {
                            Text(lab.name)
                        }
                    }
                }
            }
        }
        if !m.senderEmail.isEmpty {
            Divider()
            Button(String(localized: "Create a Rule for the Sender…")) {
                state.createRuleFromSender(m.senderEmail)
            }
        }
    }

    private var currentFolderName: String {
        if let f = state.folders.first(where: { $0.id == state.selectedFolder }) {
            return KlientLocale.folderDisplayName(id: f.id, name: f.name)
        }
        return String(localized: "Mail")
    }

    private var folderSubtitle: String {
        let sel = state.selectedMessageIDs.count
        if sel > 1 {
            return String(format: String(localized: "%lld selected"), sel)
        }
        let n = state.messages.count
        if n == 0 { return "" }
        return String(format: String(localized: "%lld messages"), n)
    }

    // MARK: - Detail

    private var detail: some View {
        Group {
            if let msg = state.openMessage {
                messageDetail(msg)
                    .transition(.opacity.combined(with: .move(edge: .trailing)))
            } else {
                ContentUnavailableView {
                    Label("Klient", systemImage: "envelope.open.fill")
                } description: {
                    Text(String(localized: "Select a message to read."))
                }
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
            }
        }
        .animation(.snappy(duration: 0.28), value: state.openMessage?.id)
        .toolbar {
            if state.openMessage != nil {
                ToolbarItemGroup(placement: .primaryAction) {
                    detailToolbar
                }
            }
        }
    }

    private func messageDetail(_ msg: AppState.MessageDetail) -> some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 12) {
                Text(msg.subject.isEmpty ? String(localized: "(no subject)") : msg.subject)
                    .font(.title2.weight(.semibold))
                    .textSelection(.enabled)

                HStack(alignment: .top, spacing: 12) {
                    ZStack {
                        Circle()
                            .fill(
                                LinearGradient(
                                    colors: [KlientTheme.brand, KlientTheme.brand.opacity(0.7)],
                                    startPoint: .topLeading,
                                    endPoint: .bottomTrailing
                                )
                            )
                            .frame(width: 42, height: 42)
                        Text(String(msg.sender.prefix(1)).uppercased())
                            .font(.headline.weight(.semibold))
                            .foregroundStyle(.white)
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        Text(msg.sender)
                            .font(.body.weight(.medium))
                            .textSelection(.enabled)
                        if !msg.to.isEmpty {
                            Text(String(format: String(localized: "To: %@"), msg.to))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                                .lineLimit(2)
                        }
                    }
                    Spacer(minLength: 0)
                }

                if state.openMessageIsScheduled {
                    HStack(spacing: 10) {
                        Image(systemName: "clock.badge.exclamationmark")
                            .foregroundStyle(KlientTheme.brand)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(String(format: String(localized: "Scheduled: will be sent %@"),
                                         KlientTheme.countdown(until: msg.time)))
                                .font(.caption.weight(.medium))
                            Text(KlientTheme.absoluteDate(msg.time))
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        Spacer(minLength: 8)
                        Button(String(localized: "Cancel Sending")) {
                            state.cancelScheduled()
                        }
                        .controlSize(.small)
                        .buttonStyle(.bordered)
                    }
                    .padding(10)
                    .background(KlientTheme.brand.opacity(0.12), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                }

                if state.openMessageIsSnoozed {
                    HStack(spacing: 10) {
                        Image(systemName: "moon.zzz")
                            .foregroundStyle(.secondary)
                        Text(String(format: String(localized: "Snoozed until %@"),
                                     KlientTheme.absoluteDate(msg.time)))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer(minLength: 8)
                        Button(String(localized: "Unsnooze")) {
                            state.unsnooze()
                        }
                        .controlSize(.small)
                        .buttonStyle(.bordered)
                    }
                    .padding(10)
                    .background(Color.secondary.opacity(0.1), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                }

                if state.spamDecisionBusy || state.spamDecision != nil {
                    if let decision = state.spamDecision {
                        SpamFilterBanner(decision: decision) {
                            state.markSpam(false)
                        }
                    } else {
                        SpamFilterBannerBusy()
                    }
                }

                if state.messageSummaryBusy || !state.messageSummary.isEmpty {
                    MessageSummaryBanner(
                        summary: state.messageSummary,
                        busy: state.messageSummaryBusy,
                        onDismiss: { state.dismissMessageSummary() }
                    )
                }

                if msg.hasRemoteContent && !msg.remoteAllowed {
                    HStack(spacing: 10) {
                        Image(systemName: "photo.on.rectangle.angled")
                            .foregroundStyle(.secondary)
                        Text(String(localized: "Remote images are blocked to protect your privacy."))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer(minLength: 8)
                        Button(String(localized: "Show Remote Content")) {
                            state.allowRemoteContent()
                        }
                        .controlSize(.small)
                        .buttonStyle(.bordered)
                    }
                    .padding(10)
                    .background(Color.orange.opacity(0.12), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                }

                if msg.unsubscribe != nil {
                    HStack(spacing: 10) {
                        Image(systemName: "envelope.badge.shield.half.filled")
                            .foregroundStyle(.secondary)
                        Text(String(localized: "This message comes from a mailing list – you can unsubscribe from it"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer(minLength: 8)
                        Button(String(localized: "Unsubscribe")) {
                            state.unsubscribeOpen()
                        }
                        .controlSize(.small)
                        .buttonStyle(.bordered)
                    }
                    .padding(10)
                    .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                }

                if state.threadMessages.count > 1 {
                    ThreadCardsStack(openID: msg.id)
                        .environmentObject(state)
                }

                if let notice = state.senderKeyNotice {
                    HStack(alignment: .top, spacing: 10) {
                        Image(systemName: notice.warn ? "exclamationmark.triangle.fill" : "info.circle")
                            .foregroundStyle(notice.warn ? Color.orange : Color.secondary)
                        Text(notice.text)
                            .font(.caption)
                            .foregroundStyle(notice.warn ? Color.primary : Color.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if notice.pending {
                            Button(String(localized: "Accept")) { state.acceptSenderPendingKey() }
                                .controlSize(.small)
                                .buttonStyle(.borderedProminent)
                            Button(String(localized: "Reject")) { state.rejectSenderPendingKey() }
                                .controlSize(.small)
                                .buttonStyle(.bordered)
                        }
                    }
                    .padding(10)
                    .background(
                        (notice.warn ? Color.orange : Color.secondary).opacity(0.12),
                        in: RoundedRectangle(cornerRadius: 8, style: .continuous)
                    )
                }

                if !state.invites.isEmpty {
                    ForEach(Array(state.invites.enumerated()), id: \.offset) { _, inv in
                        InviteBanner(invite: inv) { status in
                            state.replyInvite(status: status, invite: inv)
                        } openCalendar: {
                            state.openInviteInCalendar(inv)
                        }
                    }
                }

                if !msg.attachments.isEmpty {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(Array(msg.attachments.enumerated()), id: \.offset) { _, att in
                                Menu {
                                    Button(String(localized: "Open")) {
                                        state.openAttachment(att)
                                    }
                                    Button(String(localized: "Save…")) {
                                        state.saveAttachment(att)
                                    }
                                    if state.isPGPKeyAttachment(att) {
                                        Divider()
                                        Button(String(localized: "Import the public key")) {
                                            state.importPGPAttachment(att)
                                        }
                                    }
                                } label: {
                                    Label(
                                        att["name"] as? String ?? "file",
                                        systemImage: state.isPGPKeyAttachment(att) ? "key.fill" : "paperclip"
                                    )
                                    .font(.caption)
                                }
                                .menuStyle(.borderlessButton)
                                .buttonStyle(.bordered)
                                .controlSize(.small)
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, 22)
            .padding(.vertical, 16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.bar)

            Divider()

            if state.readerPlainText {
                ScrollView {
                    Text(msg.text.isEmpty ? String(localized: "(no plain text)") : msg.text)
                        .font(.body.monospaced())
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(20)
                }
            } else if msg.bodyHTML.isEmpty && msg.html.isEmpty && msg.text.isEmpty {
                // List-row stub while getFull / prefetch fills the body.
                ProgressView()
                    .controlSize(.regular)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                // Equatable: spam/summary/@Published churn must not rebuild WKWebView HTML.
                HTMLView(bodyHTML: msg.bodyHTML, html: msg.html, textFallback: msg.text, printToken: printToken)
                    .equatable()
                    .id(msg.id)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    @ViewBuilder
    private var detailToolbar: some View {
        ControlGroup {
            Button { state.reply(all: false) } label: {
                Image(systemName: "arrowshape.turn.up.left")
            }
            .help(String(localized: "Reply"))

            Button { state.reply(all: true) } label: {
                Image(systemName: "arrowshape.turn.up.left.2")
            }
            .help(String(localized: "Reply All"))

            Button { state.forward() } label: {
                Image(systemName: "arrowshape.turn.up.forward")
            }
            .help(String(localized: "Forward"))
        }

        ControlGroup {
            Button {
                state.summarizeOpen()
            } label: {
                Label(String(localized: "Summarize"), systemImage: "sparkles")
            }
            .help(String(localized: "Summarize with AI"))
            .disabled(state.messageSummaryBusy)

            Menu {
                Button {
                    state.summarizeOpen()
                } label: {
                    Label(String(localized: "Summarize"), systemImage: "sparkles")
                }
                .disabled(state.messageSummaryBusy)

                Button {
                    state.draftReplyWithAI()
                } label: {
                    Label(String(localized: "Draft Reply with AI"), systemImage: "pencil.and.outline")
                }

                Button {
                    state.askAboutOpenMail()
                } label: {
                    Label(String(localized: "Ask about this mail"), systemImage: "text.bubble")
                }

                Button {
                    state.suggestLabelForOpen()
                } label: {
                    Label(String(localized: "Suggest Label (AI)"), systemImage: "tag")
                }
            } label: {
                Label(String(localized: "AI"), systemImage: "sparkles.rectangle.stack")
            }
            .help(String(localized: "AI tools for this message"))
        }

        Button { state.moveSelected(to: state.labels.archive) } label: {
            Image(systemName: "archivebox")
        }
        .help(String(localized: "Archive"))

        Button { state.moveSelected(to: state.labels.trash) } label: {
            Image(systemName: "trash")
        }
        .help(String(localized: "Trash"))

        if !state.tagLabels.isEmpty {
            Menu {
                ForEach(state.tagLabels) { lab in
                    Button {
                        state.toggleLabel(lab.id)
                    } label: {
                        if state.messageHasLabel(lab.id) {
                            Label(lab.name, systemImage: "checkmark")
                        } else {
                            Text(lab.name)
                        }
                    }
                }
            } label: {
                Image(systemName: "tag")
            }
            .help(String(localized: "Labels"))
        }

        Menu {
            Menu(String(localized: "Move to")) {
                ForEach(state.mailboxFolders) { f in
                    Button(KlientLocale.folderDisplayName(id: f.id, name: f.name)) {
                        state.moveSelected(to: f.id)
                    }
                }
            }
            if !state.tagLabels.isEmpty {
                Menu(String(localized: "Labels")) {
                    ForEach(state.tagLabels) { lab in
                        Button {
                            state.toggleLabel(lab.id)
                        } label: {
                            if state.messageHasLabel(lab.id) {
                                Label(lab.name, systemImage: "checkmark")
                            } else {
                                Text(lab.name)
                            }
                        }
                    }
                }
            }
            Divider()
            Button {
                state.bulkMarkSpam(true)
            } label: {
                Label(String(localized: "Spam"), systemImage: "xmark.bin")
            }
            Button {
                state.bulkMarkSpam(false)
            } label: {
                Label(String(localized: "Not Spam"), systemImage: "checkmark.circle")
            }
            Button {
                state.suggestLabelForOpen()
            } label: {
                Label(String(localized: "Suggest Label (AI)"), systemImage: "tag")
            }
            Button {
                state.readerPlainText.toggle()
            } label: {
                Label(
                    state.readerPlainText
                        ? String(localized: "Show HTML")
                        : String(localized: "Show Plain Text"),
                    systemImage: "doc.plaintext"
                )
            }
            Divider()
            Button {
                state.markUnread()
            } label: {
                Label(String(localized: "Mark Unread"), systemImage: "envelope.badge")
            }
            Button {
                state.toggleStar()
            } label: {
                Label(String(localized: "Star"), systemImage: "star")
            }
            Divider()
            Button {
                state.snoozeHours(1)
            } label: {
                Label(String(localized: "Snooze 1 Hour"), systemImage: "moon.zzz")
            }
            Button {
                state.snoozeHours(24)
            } label: {
                Label(String(localized: "Snooze Tomorrow"), systemImage: "clock")
            }
            Button {
                state.snoozePickerDate = Calendar.current.date(
                    bySettingHour: 8, minute: 0, second: 0,
                    of: Date().addingTimeInterval(86400)
                ) ?? Date().addingTimeInterval(86400)
                state.showSnoozePicker = true
            } label: {
                Label(String(localized: "Custom date and time…"), systemImage: "calendar")
            }
            if state.openMessageIsSnoozed {
                Button {
                    state.unsnooze()
                } label: {
                    Label(String(localized: "Unsnooze"), systemImage: "sun.max")
                }
            }
            if state.openMessageIsScheduled {
                Button {
                    state.cancelScheduled()
                } label: {
                    Label(String(localized: "Cancel Sending"), systemImage: "xmark.circle")
                }
            }
            Divider()
            Button {
                state.summarizeOpen()
            } label: {
                Label(String(localized: "Summarize with AI"), systemImage: "sparkles")
            }
            Button {
                state.exportEML()
            } label: {
                Label(String(localized: "Save as .eml"), systemImage: "square.and.arrow.down")
            }
            Button {
                state.requestPrint()
            } label: {
                Label(String(localized: "Print…"), systemImage: "printer")
            }
            if state.openMessage?.unsubscribe != nil {
                Button {
                    state.unsubscribeOpen()
                } label: {
                    Label(String(localized: "Unsubscribe"), systemImage: "hand.raised")
                }
            }
            if state.isDraftsFolder {
                Divider()
                Button {
                    if let id = state.openMessage?.id { state.openDraft(id) }
                } label: {
                    Label(String(localized: "Edit Draft"), systemImage: "doc.badge.ellipsis")
                }
                Button(role: .destructive) {
                    state.deleteDraft()
                } label: {
                    Label(String(localized: "Delete Draft"), systemImage: "trash")
                }
            }
            if let email = state.openMessage?.senderEmail, !email.isEmpty {
                Divider()
                Button {
                    state.createRuleFromSender(email)
                } label: {
                    Label(String(localized: "Create a Rule for the Sender…"), systemImage: "list.bullet.rectangle")
                }
            }
            Divider()
            Button {
                state.emptyFolder(state.labels.trash)
            } label: {
                Label(String(localized: "Empty Trash"), systemImage: "trash")
            }
            Button {
                state.emptyFolder(state.labels.spam)
            } label: {
                Label(String(localized: "Empty Spam"), systemImage: "xmark.bin")
            }
        } label: {
            Image(systemName: "ellipsis.circle")
        }
    }
}

// MARK: - Spam filter score

/// Linux-style spam verdict banner under the reader header (`updateSpamUI`).
struct SpamFilterBanner: View {
    let decision: AppState.SpamDecision
    var onNotSpam: () -> Void

    /// Same soft cap as MessageSummaryBanner — long AI reasons must not crush the split view.
    private static let ratingMaxHeight: CGFloat = 160

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: decision.spam ? "xmark.bin.fill" : "checkmark.shield.fill")
                .font(.title3)
                .foregroundStyle(decision.spam ? Color.orange : KlientTheme.brand)
                .padding(.top, 2)

            VStack(alignment: .leading, spacing: 6) {
                Text(String(localized: "Spam filter rating"))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(decision.spam ? Color.orange : KlientTheme.brand)

                // Scroll + height cap (not unbounded fixedSize): long category/reason/feeds
                // text was truncated in the reader; mirror MessageSummaryBanner.
                ScrollView(.vertical, showsIndicators: true) {
                    Text(decision.bannerText)
                        .font(.subheadline)
                        .lineLimit(nil)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: Self.ratingMaxHeight)
                .fixedSize(horizontal: false, vertical: true)
            }

            if decision.showsNotSpamAction {
                Button(String(localized: "Not Spam")) {
                    onNotSpam()
                }
                .controlSize(.small)
                .buttonStyle(.bordered)
            }
        }
        .padding(12)
        .background(
            (decision.spam ? Color.orange : KlientTheme.brand).opacity(0.1),
            in: RoundedRectangle(cornerRadius: 10, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(
                    (decision.spam ? Color.orange : KlientTheme.brand).opacity(0.22),
                    lineWidth: 1
                )
        )
        .accessibilityElement(children: .contain)
        .accessibilityLabel(String(localized: "Spam filter rating"))
        .accessibilityValue(decision.bannerText)
    }
}

struct SpamFilterBannerBusy: View {
    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            ProgressView()
                .controlSize(.small)
                .padding(.top, 2)
            VStack(alignment: .leading, spacing: 6) {
                Text(String(localized: "Spam filter rating"))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(KlientTheme.brand)
                Text(String(localized: "Checking spam filter…"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(KlientTheme.brand.opacity(0.08), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(String(localized: "Spam filter rating"))
        .accessibilityValue(String(localized: "Checking spam filter…"))
    }
}

// MARK: - AI summary

struct MessageSummaryBanner: View {
    let summary: String
    let busy: Bool
    var onDismiss: () -> Void

    /// Soft Mail-like cap so the reader header cannot displace the split view.
    private static let summaryMaxHeight: CGFloat = 160
    /// Banner chrome (padding + title row) above the scroll area — used for DEBUG assert.
    private static let bannerChromeHeight: CGFloat = 56

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "sparkles")
                .font(.title3)
                .foregroundStyle(KlientTheme.brand)
                .padding(.top, 2)

            VStack(alignment: .leading, spacing: 6) {
                Text(String(localized: "AI Summary"))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(KlientTheme.brand)

                if busy && summary.isEmpty {
                    HStack(spacing: 8) {
                        ProgressView()
                            .controlSize(.small)
                        Text(String(localized: "Summarizing…"))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                } else {
                    // Scroll + height cap: never unconstrained .fixedSize(vertical:)
                    // on this text — it fights NavigationSplitView + greedy HTMLView
                    // and can crush the three-column layout (same class of bug as
                    // FilterStatusStrip). fixedSize on the *capped* ScrollView only
                    // shrink-wraps short summaries; long ones scroll within maxHeight.
                    ScrollView(.vertical, showsIndicators: true) {
                        Text(summary)
                            .font(.subheadline)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .frame(maxHeight: Self.summaryMaxHeight)
                    .fixedSize(horizontal: false, vertical: true)
                }
            }

            Button {
                onDismiss()
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .help(String(localized: "Dismiss summary"))
            .disabled(busy && summary.isEmpty)
        }
        .padding(12)
        .background(KlientTheme.brand.opacity(0.1), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(KlientTheme.brand.opacity(0.22), lineWidth: 1)
        )
        .background(
            GeometryReader { geo in
                Color.clear.preference(key: SummaryBannerHeightKey.self, value: geo.size.height)
            }
        )
        .onPreferenceChange(SummaryBannerHeightKey.self) { height in
            guard ProcessInfo.processInfo.environment["KLIENT_LAYOUT_DEBUG"] != nil else { return }
            let limit = Self.summaryMaxHeight + Self.bannerChromeHeight + 24
            if height > limit + 1 {
                print("[Klient][layout] MessageSummaryBanner height \(Int(height)) exceeds cap \(Int(limit)) — split view at risk")
            } else if !summary.isEmpty || busy {
                print("[Klient][layout] MessageSummaryBanner height=\(Int(height)) chars=\(summary.count) busy=\(busy) ok")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(String(localized: "AI Summary"))
    }
}

private struct SummaryBannerHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}

// MARK: - Invite

struct InviteBanner: View {
    let invite: [String: Any]
    var onReply: (String) -> Void
    var openCalendar: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label(invite["summary"] as? String ?? String(localized: "Event"), systemImage: "calendar")
                .font(.headline)
            if let when = invite["when"] as? String, !when.isEmpty {
                Text(when).font(.subheadline).foregroundStyle(.secondary)
            }
            if let loc = invite["location"] as? String, !loc.isEmpty {
                Text(loc).font(.caption).foregroundStyle(.tertiary)
            }
            if (invite["method"] as? String) == "REQUEST" {
                HStack(spacing: 8) {
                    Button(String(localized: "Accept")) { onReply("ACCEPTED") }
                        .buttonStyle(.borderedProminent)
                    Button(String(localized: "Maybe")) { onReply("TENTATIVE") }
                    Button(String(localized: "Decline"), role: .destructive) { onReply("DECLINED") }
                    Button(String(localized: "Calendar")) { openCalendar() }
                }
                .controlSize(.small)
            } else if (invite["method"] as? String) == "CANCEL" {
                Text(String(localized: "This event was cancelled"))
                    .foregroundStyle(.secondary)
                Button(String(localized: "Open in Calendar")) { openCalendar() }
                    .controlSize(.small)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }
}

// MARK: - Login

struct LoginSheet: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            VStack(spacing: 14) {
                Image(systemName: "envelope.badge.shield.half.filled")
                    .font(.system(size: 44, weight: .light))
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(KlientTheme.brand)
                    .padding(.top, 8)

                Text("Klient")
                    .font(.largeTitle.weight(.bold))

                Text(String(localized: "Sign in to Proton Mail"))
                    .font(.title3)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 28)
            .background(
                LinearGradient(
                    colors: [
                        KlientTheme.brand.opacity(0.18),
                        Color(nsColor: .windowBackgroundColor),
                    ],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )

            Form {
                if state.needsHV {
                    Section {
                        Text(String(localized: "Proton wants to verify that a human is logging in. Complete the check in your browser, then come back and continue."))
                            .font(.body)
                            .foregroundStyle(.secondary)
                        if !state.hvURL.isEmpty {
                            Text(state.hvURL)
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                                .textSelection(.enabled)
                        }
                    }
                    Section {
                        Button(String(localized: "Open Verification")) { state.openHumanVerification() }
                        Button(String(localized: "Continue")) { state.continueAfterHV() }
                            .disabled(state.loggingIn)
                            .keyboardShortcut(.defaultAction)
                    }
                } else {
                    Section {
                        TextField(String(localized: "Username"), text: $state.loginUsername)
                            .textContentType(.username)
                        SecureField(String(localized: "Password"), text: $state.loginPassword)
                            .textContentType(.password)
                        if state.needsTOTP {
                            TextField(String(localized: "Authentication code"), text: $state.loginTOTP)
                                .textContentType(.oneTimeCode)
                        }
                        if state.needsMailboxPassword {
                            SecureField(String(localized: "Mailbox password"), text: $state.mailboxPassword)
                        }
                    }
                }
                if !state.status.isEmpty {
                    Section {
                        Text(state.status)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
            }
            .formStyle(.grouped)
            .padding(.top, 4)

            HStack {
                Button(String(localized: "Cancel")) {
                    state.needsHV = false
                    state.loggingIn = false
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)
                Spacer()
                if !state.needsHV {
                    if state.needsTOTP {
                        Button(String(localized: "Continue")) { state.submitTOTP() }
                            .buttonStyle(.borderedProminent)
                            .disabled(state.loggingIn)
                            .keyboardShortcut(.defaultAction)
                    } else if state.needsMailboxPassword {
                        Button(String(localized: "Unlock")) { state.loginFinish() }
                            .buttonStyle(.borderedProminent)
                            .disabled(state.loggingIn)
                            .keyboardShortcut(.defaultAction)
                    } else {
                        Button(state.loggingIn ? String(localized: "Signing In…") : String(localized: "Sign In")) {
                            state.loginStart()
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(state.loggingIn || state.loginUsername.isEmpty || state.loginPassword.isEmpty)
                        .keyboardShortcut(.defaultAction)
                    }
                }
            }
            .padding(20)
        }
        .frame(width: 440)
    }
}

/// Stacked conversation cards (Linux threadview) — expand/collapse per message.
struct ThreadCardsStack: View {
    @EnvironmentObject var state: AppState
    let openID: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(String(format: String(localized: "%lld messages in the thread"), state.threadMessages.count))
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            ForEach(state.threadMessages) { tm in
                let expanded = state.expandedThreadIDs.contains(tm.id)
                VStack(alignment: .leading, spacing: 0) {
                    Button {
                        state.toggleThreadCard(tm.id)
                    } label: {
                        HStack(spacing: 8) {
                            Image(systemName: expanded ? "chevron.down" : "chevron.right")
                                .font(.caption2.weight(.semibold))
                                .foregroundStyle(.secondary)
                                .frame(width: 10)
                            VStack(alignment: .leading, spacing: 2) {
                                HStack {
                                    Text(tm.sender.isEmpty ? String(localized: "Unknown sender") : tm.sender)
                                        .font(.caption.weight(tm.unread ? .semibold : .medium))
                                        .lineLimit(1)
                                    Spacer(minLength: 4)
                                    Text(KlientTheme.relativeDate(tm.time))
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }
                                Text(tm.subject)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                            }
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 8)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    if expanded && tm.id == openID {
                        Text(String(localized: "Showing below"))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 28)
                            .padding(.bottom, 8)
                    } else if expanded {
                        Text(String(localized: "Tap to load this message"))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 28)
                            .padding(.bottom, 8)
                    }
                }
                .background(
                    tm.id == openID
                    ? Color.accentColor.opacity(0.12)
                    : Color.secondary.opacity(0.08),
                    in: RoundedRectangle(cornerRadius: 8, style: .continuous)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .strokeBorder(tm.id == openID ? Color.accentColor.opacity(0.35) : Color.clear, lineWidth: 1)
                )
            }
        }
    }
}

struct AskAISheet: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Label(String(localized: "Ask Your Mail"), systemImage: "sparkles")
                .font(.title2.weight(.semibold))
            TextField(String(localized: "Ask a question about your mailbox"), text: $state.askQuestion)
                .textFieldStyle(.plain)
                .padding(10)
                .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            Button(String(localized: "Ask")) { state.runAskAI() }
                .buttonStyle(.borderedProminent)
                .disabled(state.askQuestion.trimmingCharacters(in: .whitespaces).isEmpty)
            // Safe here: modal sheet with fixed frame; fixedSize only expands
            // text inside a height-capped ScrollView (unlike MessageSummaryBanner
            // which lived unconstrained inside NavigationSplitView).
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text(state.askAnswer.isEmpty
                         ? String(localized: "Answers stay on your machine when using local AI.")
                         : state.askAnswer)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .fixedSize(horizontal: false, vertical: true)
                        .foregroundStyle(state.askAnswer.isEmpty ? .secondary : .primary)
                        .textSelection(.enabled)

                    // Linux-parity Sources: [n] maps to docs sent to AnswerFromMail.
                    if !state.askHits.isEmpty {
                        Text(String(localized: "Sources"))
                            .font(.headline)
                        ForEach(state.askHits) { hit in
                            Button {
                                state.openAskHit(hit)
                            } label: {
                                HStack(spacing: 8) {
                                    Text("[\(hit.index)] \(hit.subject)")
                                        .font(.callout)
                                        .multilineTextAlignment(.leading)
                                        .lineLimit(2)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                    Image(systemName: "chevron.right")
                                        .font(.caption.weight(.semibold))
                                        .foregroundStyle(.tertiary)
                                }
                                .padding(.vertical, 6)
                                .padding(.horizontal, 8)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                        }
                    }
                }
            }
            .frame(maxHeight: 320)
            HStack {
                Spacer()
                Button(String(localized: "Done")) { dismiss() }
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(24)
        .frame(width: 520, height: 460)
    }
}
