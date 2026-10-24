import SwiftUI
import AppKit
import UniformTypeIdentifiers

/// Native macOS Settings window (System Settings / Mail prefs aesthetic).
/// Top icon+title row for sections — never `NavigationSplitView` (crashes in sheets / some window hosts).
struct PreferencesView: View {
    @EnvironmentObject var state: AppState

    @State private var language = ""
    @State private var holdUntilChecked = true
    @State private var spamEnabled = true
    @State private var spamThreshold = 0.8
    @State private var threads = true
    @State private var sendDelay = 5
    @State private var autoUpdate = true
    @State private var launchAtLogin = false
    @State private var launchAtLoginNeedsApproval = false
    @State private var suppressLaunchAtLoginChange = false
    @State private var assistantProvider = "claude"
    @State private var assistantModel = ""
    @State private var spamProvider = "claude"
    @State private var spamModel = ""
    @State private var localOnly = false
    @State private var apiKeyProvider = "claude"
    @State private var apiKey = ""
    @State private var signature = ""
    @State private var signatureHTML = ""
    @State private var signatureUseHTML = false
    @State private var status = ""
    @State private var rules: [PrefRule] = []
    @State private var editingRule: PrefRule?
    @State private var ollamaStatus = ""
    @State private var ollamaInstalled = false
    @State private var ollamaModels: [OllamaInstalledModel] = []
    @State private var ollamaBusy = false
    @State private var customPullName = ""
    @State private var cloudModels: [String] = []
    @State private var cloudModelsProvider = ""
    @State private var modelPickerRole: ModelPickRole?
    @State private var modelPickerBusy = false
    @State private var roleModels: [String: String] = [:]
    @State private var aiReady = ""
    @State private var usageLine = ""
    @State private var usageDetailLines: [[String: Any]] = []
    @State private var showUsageDetails = false
    @State private var showCompareModels = false
    @State private var hasAssistant = false
    @State private var hasSpamAI = false
    @State private var aiTestOK: Bool?
    @State private var aiTestDetail = ""
    @State private var aiTesting = false
    @State private var selectedTab: Tab = .general
    @State private var loaded = false
    @State private var saveWork: DispatchWorkItem?
    @State private var pgpKeys: [String] = []
    @State private var pgpImportText = ""
    @State private var ownPublicKey = ""
    @State private var ownKeyManaged = false
    @State private var ownKeyHasKey = false
    @State private var ownKeyFingerprint = ""
    @State private var ownKeyExpiresText = ""
    @State private var ownKeyYears = 2
    @State private var ownKeyBusy = false
    @State private var showImportOwnKey = false
    @State private var importOwnKeyText = ""
    @State private var importOwnKeyPass = ""
    @State private var showBackupPass = false
    @State private var backupPass = ""
    @State private var keyServerLookup = true
    @State private var attachPublicKey = false
    @State private var requireEncryption = false
    @State private var offlineMessages = 500
    @State private var autoEmptyDays = 0
    @State private var protectSubject = false
    @State private var cacheAvailable = false
    @State private var cacheMessages = 0
    @State private var cacheBodies = 0
    @State private var autoLabel = false
    @State private var digestEnabled = false
    @State private var digestHour = 7
    @State private var spellCheck = true
    @State private var notifyButtons: [String] = ["read", "archive", "trash", "spam"]
    @State private var spamFeeds: [PrefFeed] = []

    private let validityChoices: [(years: Int, label: String)] = [
        (1, String(localized: "1 year")),
        (2, String(localized: "2 years")),
        (5, String(localized: "5 years")),
        (0, String(localized: "No expiry")),
    ]

    private let providers: [(id: String, name: String)] = [
        ("claude", "Claude"),
        ("openai", "OpenAI"),
        ("gemini", "Google Gemini"),
        ("mistral", "Mistral"),
        ("grok", "Grok"),
        ("ollama", "Ollama"),
    ]

    /// Default assistant / spam models per provider (mirrors internal/ai.Providers).
    private let providerDefaults: [String: (assistant: String, spam: String)] = [
        "claude": ("claude-opus-5", "claude-opus-5"),
        "openai": ("gpt-5.5", "gpt-5.4-mini"),
        "gemini": ("gemini-3.1-pro-preview", "gemini-3-flash-preview"),
        "mistral": ("mistral-medium-latest", "mistral-small-latest"),
        "grok": ("grok-4.7", "grok-4.7"),
        "ollama": ("qwen3.5:4b", "qwen3.5:4b"),
    ]

    private enum ModelPickRole: String, Identifiable {
        case assistant, spam
        var id: String { rawValue }
        var title: String {
            switch self {
            case .assistant: return String(localized: "Assistant")
            case .spam: return String(localized: "Spam Filter")
            }
        }
    }

    private struct PrefFeed: Identifiable, Equatable {
        var id: String { url }
        var name: String
        var url: String
        var kind: String
        var enabled: Bool
    }

    private var ollamaCatalog: [OllamaCatalogEntry] {
        [
            OllamaCatalogEntry(name: "qwen3.5:4b", label: "Qwen 3.5 4B (Alibaba)", size: "3.4 GB", category: "Suggested"),
            OllamaCatalogEntry(name: "gemma4:e2b-it-qat", label: "Gemma 4 E2B (Google)", size: "4.3 GB", category: "Suggested"),
            OllamaCatalogEntry(name: "ministral-3:3b", label: "Ministral 3 3B (Mistral AI)", size: "3.0 GB", category: "Suggested"),
            OllamaCatalogEntry(name: "gemma4:e2b-it-qat", label: "Gemma 4 E2B (Google)", size: "4.3 GB", category: "Gemma"),
            OllamaCatalogEntry(name: "gemma4:e4b-it-qat", label: String(localized: "Gemma 4 E4B (Google) – better at Czech, needs more memory"), size: "6.1 GB", category: "Gemma"),
            OllamaCatalogEntry(name: "llama3.2:3b", label: "Llama 3.2 3B (Meta)", size: "2.0 GB", category: "Llama"),
            OllamaCatalogEntry(name: "llama3.2:1b", label: "Llama 3.2 1B (Meta)", size: "1.3 GB", category: "Llama"),
            OllamaCatalogEntry(name: "ministral-3:3b", label: "Ministral 3 3B (Mistral AI)", size: "3.0 GB", category: "Mistral"),
            OllamaCatalogEntry(name: "ministral-3:8b", label: String(localized: "Ministral 3 8B (Mistral AI) – smarter, slower"), size: "6.0 GB", category: "Mistral"),
            OllamaCatalogEntry(name: "phi4-mini", label: "Phi 4 Mini (Microsoft)", size: "2.5 GB", category: "Phi"),
            OllamaCatalogEntry(name: "phi3:mini", label: "Phi 3 Mini (Microsoft)", size: "2.2 GB", category: "Phi"),
            OllamaCatalogEntry(name: "hf.co/google/gemma-3-4b-it-GGUF", label: "Gemma 3 4B (Hugging Face)", size: String(localized: "varies"), category: "Hugging Face"),
            OllamaCatalogEntry(name: "hf.co/bartowski/Llama-3.2-3B-Instruct-GGUF:Q4_K_M", label: "Llama 3.2 3B Instruct Q4 (HF)", size: "~2 GB", category: "Hugging Face"),
        ]
    }

    enum Tab: String, CaseIterable, Identifiable, Hashable {
        case general, spam, ai, rules, pgp
        var id: String { rawValue }
        var title: String {
            switch self {
            case .general: return String(localized: "General")
            case .spam: return String(localized: "Spam Filter")
            case .ai: return String(localized: "AI")
            case .rules: return String(localized: "Rules")
            case .pgp: return "PGP"
            }
        }
        var icon: String {
            switch self {
            case .general: return "gearshape"
            case .spam: return "flame"
            case .ai: return "sparkles"
            case .rules: return "list.bullet.rectangle"
            case .pgp: return "lock.rectangle.stack"
            }
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            topTabs
            Divider()
            detailHeader
            Divider()
            Group {
                switch selectedTab {
                case .general: generalForm
                case .spam: spamForm
                case .ai: aiForm
                case .rules: rulesForm
                case .pgp: pgpForm
                }
            }
            .formStyle(.grouped)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            if !status.isEmpty {
                Divider()
                Text(status)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 10)
            }
        }
        .frame(width: 640, height: 560)
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear {
            // Restore tab after locale remount (language change); pendingTab wins.
            if KlientSettings.pendingTab == nil, let last = KlientSettings.lastTab {
                selectedTab = last
            }
            applyPendingTab()
            load()
            applyPendingRule()
        }
        .onReceive(NotificationCenter.default.publisher(for: .klientOpenSettings)) { note in
            if let raw = note.userInfo?["tab"] as? String, let tab = Tab(rawValue: raw) {
                selectedTab = tab
            } else {
                applyPendingTab()
            }
            applyPendingRule()
        }
        .background(prefsAutoSaveTriggers)
        .sheet(item: $editingRule) { rule in
            RuleEditorSheet(rule: rule, folders: state.folders) { updated in
                if let i = rules.firstIndex(where: { $0.id == updated.id }) {
                    rules[i] = updated
                } else {
                    rules.append(updated)
                }
                editingRule = nil
                scheduleSave()
            } onCancel: {
                editingRule = nil
            }
        }
        .sheet(isPresented: $showImportOwnKey) {
            VStack(alignment: .leading, spacing: 12) {
                Text(String(localized: "Import a Private Key…"))
                    .font(.headline)
                TextEditor(text: $importOwnKeyText)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 140)
                SecureField(String(localized: "Key Passphrase"), text: $importOwnKeyPass)
                Text(String(localized: "If the key has no passphrase, leave the field empty."))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack {
                    Button(String(localized: "Cancel")) { showImportOwnKey = false }
                    Spacer()
                    Button(String(localized: "Import")) { importOwnKey() }
                        .buttonStyle(.borderedProminent)
                        .disabled(importOwnKeyText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .padding(20)
            .frame(width: 480, height: 340)
        }
        .alert(String(localized: "Backup Password"), isPresented: $showBackupPass) {
            SecureField(String(localized: "Password"), text: $backupPass)
            Button(String(localized: "Cancel"), role: .cancel) { backupPass = "" }
            Button(String(localized: "Save…")) { exportOwnPrivate() }
                .disabled(backupPass.isEmpty)
        } message: {
            Text(String(localized: "The backup is encrypted with this password. It cannot be restored without it."))
        }
        .sheet(item: $modelPickerRole) { role in
            modelPickerSheet(role: role)
        }
        .sheet(isPresented: $showUsageDetails) {
            usageDetailsSheet
        }
        .sheet(isPresented: $showCompareModels) {
            CompareModelsSheet(ollamaModels: ollamaModels.map(\.name)) { chosen in
                useLocalModel(chosen)
                showCompareModels = false
            }
        }
    }

    private func modelPickerSheet(role: ModelPickRole) -> some View {
        NavigationStack {
            List {
                if modelPickerModels.isEmpty {
                    Text(String(localized: "No models returned"))
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(modelPickerModels, id: \.self) { name in
                        Button {
                            applyPickedModel(name, role: role)
                        } label: {
                            HStack {
                                Text(name)
                                Spacer()
                                let current = role == .assistant ? assistantModel : spamModel
                                if name == current {
                                    Image(systemName: "checkmark")
                                        .foregroundStyle(Color.accentColor)
                                }
                            }
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .navigationTitle(String(format: String(localized: "Choose a model – %@"), role.title))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "Cancel")) { modelPickerRole = nil }
                }
            }
        }
        .frame(width: 420, height: 480)
    }

    private var modelPickerModels: [String] { cloudModels }

    private var usageDetailsSheet: some View {
        NavigationStack {
            List {
                if usageDetailLines.isEmpty {
                    Text(String(localized: "None yet"))
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(Array(usageDetailLines.enumerated()), id: \.offset) { _, row in
                        VStack(alignment: .leading, spacing: 2) {
                            Text([
                                row["Provider"] as? String,
                                row["Model"] as? String,
                                row["Task"] as? String,
                            ].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.body.weight(.medium))
                            let reqs = (row["Requests"] as? NSNumber)?.intValue
                                ?? (row["Requests"] as? Int) ?? 0
                            let cost = (row["Cost"] as? NSNumber)?.doubleValue
                                ?? (row["Cost"] as? Double) ?? 0
                            Text(String(format: String(localized: "%lld requests · $%.4f"), Int64(reqs), cost))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.vertical, 2)
                    }
                }
            }
            .navigationTitle(String(localized: "AI Usage Details"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "Close")) { showUsageDetails = false }
                }
            }
        }
        .frame(width: 480, height: 420)
    }

    private func applyPickedModel(_ name: String, role: ModelPickRole) {
        switch role {
        case .assistant:
            assistantModel = name
            roleModels["assistant:\(assistantProvider)"] = name
        case .spam:
            spamModel = name
            roleModels["spam:\(spamProvider)"] = name
        }
        modelPickerRole = nil
        scheduleSave()
        status = String(format: String(localized: "Model %@ saved"), name)
        refreshAIStatus()
    }

    /// Split out of `body` so the type checker does not time out on the onChange chain.
    @ViewBuilder
    private var prefsAutoSaveTriggers: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .onChange(of: language) { _, _ in scheduleSave() }
            .onChange(of: threads) { _, _ in scheduleSave() }
            .onChange(of: sendDelay) { _, _ in scheduleSave() }
            .onChange(of: autoUpdate) { _, _ in scheduleSave() }
            .onChange(of: launchAtLogin) { _, on in applyLaunchAtLogin(on) }
            .onChange(of: signature) { _, _ in scheduleSave() }
            .onChange(of: signatureHTML) { _, _ in scheduleSave() }
            .onChange(of: signatureUseHTML) { _, _ in scheduleSave() }
            .onChange(of: spamEnabled) { _, _ in scheduleSave() }
            .onChange(of: holdUntilChecked) { _, _ in scheduleSave() }
            .onChange(of: spamThreshold) { _, _ in scheduleSave() }
            .onChange(of: localOnly) { _, on in
                if on {
                    if assistantProvider != "ollama" { assistantProvider = "ollama" }
                    if spamProvider != "ollama" { spamProvider = "ollama" }
                }
                scheduleSave()
            }
            .onChange(of: assistantProvider) { old, new in
                switchProviderRole(role: "assistant", from: old, to: new, model: $assistantModel)
                scheduleSave()
            }
            .onChange(of: assistantModel) { _, _ in scheduleSave() }
            .onChange(of: spamProvider) { old, new in
                switchProviderRole(role: "spam", from: old, to: new, model: $spamModel)
                scheduleSave()
            }
            .onChange(of: spamModel) { _, _ in scheduleSave() }
            .onChange(of: rules) { _, _ in scheduleSave() }
            .onChange(of: keyServerLookup) { _, _ in scheduleSave() }
            .onChange(of: attachPublicKey) { _, _ in scheduleSave() }
            .onChange(of: requireEncryption) { _, _ in scheduleSave() }
            .onChange(of: offlineMessages) { _, _ in scheduleSave() }
            .onChange(of: autoEmptyDays) { _, _ in scheduleSave() }
            .onChange(of: protectSubject) { _, _ in scheduleSave() }
            .onChange(of: autoLabel) { _, _ in scheduleSave() }
            .onChange(of: digestEnabled) { _, _ in scheduleSave() }
            .onChange(of: digestHour) { _, _ in scheduleSave() }
            .onChange(of: spellCheck) { _, _ in scheduleSave() }
            .onChange(of: notifyButtons) { _, _ in scheduleSave() }
            .onChange(of: spamFeeds) { _, _ in scheduleSave() }
            .onChange(of: selectedTab) { _, tab in
                KlientSettings.lastTab = tab
                if tab == .pgp { refreshPGP() }
                if tab == .ai {
                    refreshOllama()
                    refreshOllamaModels()
                }
            }
            .onChange(of: state.ollamaPullFrac) { _, _ in
                if !state.ollamaPullModel.isEmpty { ollamaBusy = true }
            }
            .onChange(of: state.ollamaPullStatus) { _, s in
                if s == "success" {
                    ollamaBusy = false
                    refreshOllama()
                    refreshOllamaModels()
                    refreshAIStatus()
                }
            }
    }

    // MARK: - Chrome

    private var topTabs: some View {
        HStack(spacing: 8) {
            ForEach(Tab.allCases) { tab in
                Button {
                    selectedTab = tab
                } label: {
                    VStack(spacing: 6) {
                        Image(systemName: tab.icon)
                            .font(.system(size: 18, weight: .medium))
                            .frame(width: 44, height: 28)
                            .background(
                                RoundedRectangle(cornerRadius: 7, style: .continuous)
                                    .fill(selectedTab == tab
                                          ? Color.accentColor.opacity(0.18)
                                          : Color(nsColor: .controlBackgroundColor))
                            )
                        Text(tab.title)
                            .font(.caption)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 10)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .foregroundStyle(selectedTab == tab ? Color.accentColor : Color.primary)
                .accessibilityAddTraits(selectedTab == tab ? .isSelected : [])
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 10)
        .padding(.bottom, 4)
        .background(.bar)
    }

    private var detailHeader: some View {
        HStack {
            Text(selectedTab.title)
                .font(.title2.weight(.semibold))
            Spacer()
            Button(String(localized: "Save")) { save(immediate: true) }
                .keyboardShortcut(.defaultAction)
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
    }

    // MARK: - Sections

    private var generalForm: some View {
        Form {
            Section {
                Picker(String(localized: "Language"), selection: $language) {
                    Text(String(localized: "System")).tag("")
                    Text("English").tag("en")
                    Text("Česky").tag("cs")
                }
                .pickerStyle(.menu)
            } footer: {
                Text(String(localized: "What's New, Help and AI follow this language immediately. Restart Klient if some menus stay in the old language."))
            }

            Section {
                Toggle(String(localized: "Group messages into threads"), isOn: $threads)
                Toggle(String(localized: "Underline misspelled words while writing"), isOn: $spellCheck)
            } header: {
                Text(String(localized: "Message List"))
            } footer: {
                Text(String(localized: "Replies are shown together as a conversation. Spell-check uses the system dictionaries."))
            }

            Section {
                LabeledContent(String(localized: "Send delay")) {
                    Stepper(value: $sendDelay, in: 0...60, step: 5) {
                        Text(String(format: String(localized: "%lld seconds"), sendDelay))
                            .monospacedDigit()
                    }
                }
                Toggle(String(localized: "Protect subject of encrypted mail"), isOn: $protectSubject)
            } header: {
                Text(String(localized: "Sending"))
            } footer: {
                Text(String(localized: "During this time sending can be undone; 0 sends immediately. Protected subjects read “…” outside the encryption."))
            }

            Section {
                LabeledContent(String(localized: "Offline cache")) {
                    Stepper(value: $offlineMessages, in: 0...5000, step: 100) {
                        Text(offlineMessages == 0
                             ? String(localized: "Off")
                             : String(format: String(localized: "%lld messages"), offlineMessages))
                            .monospacedDigit()
                    }
                }
                LabeledContent(String(localized: "Stored")) {
                    Text(cacheAvailable
                         ? String(format: String(localized: "%lld messages, %lld of them with content"),
                                  cacheMessages, cacheBodies)
                         : String(localized: "the cache is not available"))
                        .foregroundStyle(.secondary)
                        .font(.caption)
                }
                Button(String(localized: "Clear"), role: .destructive) {
                    if state.clearOfflineCache() {
                        refreshCacheStats()
                    }
                }
                .disabled(!cacheAvailable)
                LabeledContent(String(localized: "Auto-empty Trash and Spam")) {
                    Stepper(value: $autoEmptyDays, in: 0...365, step: 1) {
                        Text(autoEmptyDays == 0
                             ? String(localized: "Never")
                             : String(format: String(localized: "After %lld days"), autoEmptyDays))
                            .monospacedDigit()
                    }
                }
            } header: {
                Text(String(localized: "Storage"))
            } footer: {
                Text(String(localized: "Newest messages kept with bodies in the encrypted offline cache; 0 turns the cache off."))
            }

            Section {
                Toggle(String(localized: "Mark as read"), isOn: notifyBinding("read"))
                Toggle(String(localized: "Archive"), isOn: notifyBinding("archive"))
                Toggle(String(localized: "Trash"), isOn: notifyBinding("trash"))
                Toggle(String(localized: "Spam"), isOn: notifyBinding("spam"))
            } header: {
                Text(String(localized: "Notification Actions"))
            } footer: {
                Text(String(localized: "Buttons shown on new-mail notifications."))
            }

            Section {
                Toggle(String(localized: "Open at Login"), isOn: $launchAtLogin)
                if launchAtLoginNeedsApproval {
                    Text(String(localized: "Klient needs approval in System Settings → General → Login Items."))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Button(String(localized: "Open Login Items Settings…")) {
                        LoginItem.openSystemSettingsLoginItems()
                    }
                }
            } header: {
                Text(String(localized: "Startup"))
            } footer: {
                Text(String(localized: "Launch Klient when you log in to this Mac."))
            }

            Section {
                Toggle(String(localized: "Check for updates automatically"), isOn: $autoUpdate)
                Button(String(localized: "Check for Updates…")) {
                    state.checkForUpdates()
                }
            } header: {
                Text(String(localized: "Updates"))
            } footer: {
                Text(String(localized: "Once a day from GitHub. A new version can be downloaded as a DMG; drag Klient to Applications to install."))
            }

            Section {
                Toggle(String(localized: "Use HTML signature"), isOn: $signatureUseHTML)
                TextField(String(localized: "Appended to new messages"), text: $signature, axis: .vertical)
                    .lineLimit(3...8)
                if signatureUseHTML {
                    TextField(String(localized: "HTML signature"), text: $signatureHTML, axis: .vertical)
                        .font(.system(.body, design: .monospaced))
                        .lineLimit(4...12)
                }
            } header: {
                Text(String(localized: "Signature"))
            } footer: {
                Text(signatureUseHTML
                     ? String(localized: "Added below new messages as HTML (plain text is kept as a fallback).")
                     : String(localized: "Added below new messages."))
            }
        }
    }

    private func notifyBinding(_ id: String) -> Binding<Bool> {
        Binding(
            get: { notifyButtons.contains(id) },
            set: { on in
                if on {
                    if !notifyButtons.contains(id) { notifyButtons.append(id) }
                } else {
                    notifyButtons.removeAll { $0 == id }
                }
            }
        )
    }

    private var spamForm: some View {
        Form {
            Section {
                Toggle(String(localized: "Filter incoming mail with AI"), isOn: $spamEnabled)
                Toggle(String(localized: "Show new mail only after it is checked"), isOn: $holdUntilChecked)
            } footer: {
                Text(String(localized: "When holding is on, messages stay quiet until the filter finishes so spam never flashes up."))
            }

            Section {
                LabeledContent(String(localized: "Threshold")) {
                    Text(String(format: "%.2f", spamThreshold))
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
                Slider(value: $spamThreshold, in: 0.50...0.99, step: 0.01)
            } header: {
                Text(String(localized: "Sensitivity"))
            } footer: {
                Text(String(localized: "Spam probability at which a message is moved to Spam."))
            }

            Section {
                ForEach($spamFeeds) { $feed in
                    Toggle(feed.name, isOn: $feed.enabled)
                }
            } header: {
                Text(String(localized: "Blocklist Feeds"))
            } footer: {
                Text(String(localized: "Downloaded automatically and used together with the AI filter."))
            }
        }
    }

    private var aiForm: some View {
        Form {
            Section {
                HStack(spacing: 10) {
                    aiStatusIcon(ok: aiOverallOK)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(aiOverallOK
                             ? String(localized: "AI is ready")
                             : String(localized: "AI is not fully set up"))
                            .font(.body.weight(.medium))
                        Text(aiReady.isEmpty ? String(localized: "Checking…") : aiReady)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 8)
                    Button {
                        testAI()
                    } label: {
                        if aiTesting {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Text(String(localized: "Test"))
                        }
                    }
                    .disabled(aiTesting)
                    .help(String(localized: "Sends a short test request"))
                }
                if let aiTestOK {
                    HStack(alignment: .top, spacing: 8) {
                        aiStatusIcon(ok: aiTestOK)
                        VStack(alignment: .leading, spacing: 2) {
                            if aiTestOK {
                                Text(String(localized: "Connection test succeeded"))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            } else if !aiTestDetail.isEmpty {
                                // Show the bridge/Explain reason as the primary
                                // line — not only the generic "test failed" title.
                                Text(aiTestDetail)
                                    .font(.caption)
                                    .foregroundStyle(.primary)
                                    .fixedSize(horizontal: false, vertical: true)
                                    .textSelection(.enabled)
                            } else {
                                Text(String(localized: "Connection test failed"))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            } header: {
                Text(String(localized: "Status"))
            } footer: {
                if !usageLine.isEmpty {
                    Text(String(localized: "Usage this month") + ": " + usageLine)
                }
            }

            Section {
                Button(String(localized: "Usage Details…")) {
                    refreshAIStatus()
                    showUsageDetails = true
                }
            }

            Section {
                Toggle(String(localized: "Local AI only"), isOn: $localOnly)
            } footer: {
                Text(String(localized: "When on, only Ollama on this Mac can be used — message content stays on this computer."))
            }

            Section {
                Toggle(String(localized: "Sort new mail into labels"), isOn: $autoLabel)
                Toggle(String(localized: "Morning mail overview"), isOn: $digestEnabled)
                if digestEnabled {
                    LabeledContent(String(localized: "Hour")) {
                        Stepper(value: $digestHour, in: 0...23) {
                            Text(String(format: String(localized: "%lld:00"), digestHour))
                                .monospacedDigit()
                        }
                    }
                }
            } header: {
                Text(String(localized: "Automatic Features"))
            } footer: {
                Text(String(localized: "They use the assistant model. With local AI the content stays on this Mac; with cloud AI it is sent to the provider. The overview arrives after this hour while Klient is running."))
            }

            Section {
                HStack {
                    Text(String(localized: "Assistant"))
                    Spacer()
                    aiStatusIcon(ok: hasAssistant)
                }
                providerPicker(String(localized: "Provider"), selection: $assistantProvider)
                modelField(provider: assistantProvider, selection: $assistantModel, role: .assistant)
            } header: {
                Text(String(localized: "Assistant"))
            } footer: {
                Text(String(localized: "Summaries, reply suggestions, and text editing. Use Choose… to list models for the saved API key."))
            }

            Section {
                HStack {
                    Text(String(localized: "Spam Filter"))
                    Spacer()
                    aiStatusIcon(ok: hasSpamAI)
                }
                providerPicker(String(localized: "Provider"), selection: $spamProvider)
                modelField(provider: spamProvider, selection: $spamModel, role: .spam)
            } header: {
                Text(String(localized: "Spam Filter Model"))
            } footer: {
                Text(String(localized: "A cheaper, faster model is usually enough here. Choose… loads the model list from the provider when the API key is saved."))
            }

            Section {
                Picker(String(localized: "Provider"), selection: $apiKeyProvider) {
                    ForEach(providers.filter { $0.id != "ollama" }, id: \.id) { p in
                        Text(String(localized: String.LocalizationValue(p.name))).tag(p.id)
                    }
                }
                SecureField(String(localized: "API key"), text: $apiKey)
                Button(String(localized: "Save API Key")) { saveAPIKey() }
            } header: {
                Text(String(localized: "API Keys"))
            } footer: {
                Text(String(localized: "Keys are stored in the macOS Keychain. Google Gemini uses a key from Google AI Studio; Grok uses an xAI key from console.x.ai. After saving a key, use Choose… next to Assistant or Spam Filter Model."))
            }

            ollamaSection
        }
    }

    @ViewBuilder
    private var ollamaSection: some View {
        Section {
            HStack(spacing: 8) {
                Image(systemName: ollamaInstalled ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                    .foregroundStyle(ollamaInstalled ? Color.green : Color.orange)
                Text(ollamaStatus.isEmpty ? String(localized: "Checking…") : ollamaStatus)
                    .foregroundStyle(.secondary)
                Spacer()
            }
            Button(String(localized: "Install / Start Ollama")) {
                ollamaBusy = true
                state.installOllama()
                DispatchQueue.main.asyncAfter(deadline: .now() + 2.0) {
                    ollamaBusy = false
                    refreshOllama()
                    refreshOllamaModels()
                    refreshAIStatus()
                }
            }
            .disabled(ollamaBusy)
            if ollamaBusy || !state.ollamaPullModel.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    if !state.ollamaPullStatus.isEmpty || !state.ollamaPullModel.isEmpty {
                        Text(pullProgressLabel)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if state.ollamaPullFrac >= 0 {
                        ProgressView(value: min(max(state.ollamaPullFrac, 0), 1))
                    } else {
                        ProgressView()
                    }
                }
            }
        } header: {
            Text(String(localized: "Local AI (Ollama)"))
        } footer: {
            Text(String(localized: "The model runs on this Mac; message content stays here. Without a GPU it is slower."))
        }

        if ollamaInstalled {
            Section {
                Button(String(localized: "Compare Models on My Mail…")) {
                    showCompareModels = true
                }
                .disabled(ollamaModels.count < 2)
            } footer: {
                Text(String(localized: "Run the same spam-filter sample on each installed local model and pick a winner."))
            }

            Section {
                if ollamaModels.isEmpty {
                    Text(String(localized: "None yet – download a model from the catalog below"))
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(ollamaModels) { m in
                        HStack(spacing: 10) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(Color.green)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(m.name)
                                Text(byteSizeLabel(m.size) + inUseSuffix(m.name))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button(String(localized: "Use")) {
                                useLocalModel(m.name)
                            }
                            .buttonStyle(.borderless)
                            Button(role: .destructive) {
                                deleteOllamaModel(m.name)
                            } label: {
                                Image(systemName: "trash")
                            }
                            .buttonStyle(.borderless)
                            .help(String(localized: "Delete Model"))
                        }
                    }
                }
            } header: {
                Text(String(localized: "Downloaded models"))
            }

            ForEach(catalogCategories, id: \.self) { category in
                Section {
                    ForEach(ollamaCatalog.filter { $0.category == category }) { entry in
                        HStack(spacing: 10) {
                            Image(systemName: isModelInstalled(entry.name) ? "checkmark.circle.fill" : "arrow.down.circle")
                                .foregroundStyle(isModelInstalled(entry.name) ? Color.green : Color.accentColor)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(entry.label)
                                Text("\(entry.name) · \(entry.size)")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            if isModelInstalled(entry.name) {
                                Button(String(localized: "Use")) { useLocalModel(entry.name) }
                                    .buttonStyle(.borderless)
                            } else {
                                Button(String(localized: "Install")) {
                                    pullOllamaModel(entry.name)
                                }
                                .buttonStyle(.borderless)
                                .disabled(ollamaBusy)
                            }
                        }
                    }
                } header: {
                    Text(catalogCategoryTitle(category))
                }
            }

            Section {
                TextField(String(localized: "Model name or HF tag"), text: $customPullName)
                    .textFieldStyle(.roundedBorder)
                Button(String(localized: "Install custom model")) {
                    let name = customPullName.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !name.isEmpty else { return }
                    pullOllamaModel(name)
                }
                .disabled(ollamaBusy || customPullName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            } header: {
                Text(String(localized: "Custom / Hugging Face"))
            } footer: {
                Text(String(localized: "Any name Ollama accepts, e.g. gemma2:2b or hf.co/org/repo:Q4_K_M."))
            }
        }
    }

    private var catalogCategories: [String] {
        var seen = Set<String>()
        var out: [String] = []
        for e in ollamaCatalog where seen.insert(e.category).inserted {
            out.append(e.category)
        }
        return out
    }

    private var pullProgressLabel: String {
        let model = state.ollamaPullModel
        let st = state.ollamaPullStatus
        if state.ollamaPullFrac >= 0, state.ollamaPullTotal > 0 {
            return String(format: String(localized: "Downloading %@: %@ of %@"),
                          model, byteSizeLabel(state.ollamaPullDone), byteSizeLabel(state.ollamaPullTotal))
        }
        if !st.isEmpty && st != "success" {
            return String(format: String(localized: "Downloading %@: %@"), model, st)
        }
        return model.isEmpty ? String(localized: "Working…") : model
    }

    private var aiOverallOK: Bool {
        if let aiTestOK { return aiTestOK }
        return hasAssistant && hasSpamAI
    }

    @ViewBuilder
    private func aiStatusIcon(ok: Bool) -> some View {
        Image(systemName: ok ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
            .foregroundStyle(ok ? Color.green : Color.orange)
            .font(.title3)
            .accessibilityLabel(ok
                ? String(localized: "Ready")
                : String(localized: "Not configured"))
    }

    private var pgpForm: some View {
        Form {
            Section {
                Toggle(String(localized: "Look up keys on keys.openpgp.org"), isOn: $keyServerLookup)
                Toggle(String(localized: "Attach my public key to outgoing mail"), isOn: $attachPublicKey)
                Toggle(String(localized: "Require encryption for new messages"), isOn: $requireEncryption)
            } header: {
                Text(String(localized: "Encryption"))
            } footer: {
                Text(String(localized: "Contact public keys are stored locally for OpenPGP mail."))
            }

            Section {
                if pgpKeys.isEmpty {
                    Text(String(localized: "No contact keys stored yet."))
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(pgpKeys, id: \.self) { email in
                        HStack {
                            Text(email)
                            Spacer()
                            Button(role: .destructive) {
                                deletePGPKey(email)
                            } label: {
                                Image(systemName: "trash")
                            }
                            .buttonStyle(.borderless)
                            .help(String(localized: "Delete"))
                        }
                    }
                }
            } header: {
                HStack {
                    Text(String(localized: "Contact Keys"))
                    Spacer()
                    Button {
                        refreshPGP()
                    } label: {
                        Image(systemName: "arrow.clockwise")
                    }
                    .buttonStyle(.borderless)
                }
            }

            Section {
                TextEditor(text: $pgpImportText)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 100)
                Button(String(localized: "Import Public Key")) {
                    importPGPKey()
                }
                .disabled(pgpImportText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            } header: {
                Text(String(localized: "Import"))
            } footer: {
                Text(String(localized: "Paste an ASCII-armored public key block."))
            }

            Section {
                ownKeySection
            } header: {
                Text(String(localized: "My Key"))
            } footer: {
                if ownKeyManaged {
                    Text(String(localized: "Without a key, mail from this account goes out unencrypted and unsigned. With a key, Klient encrypts messages to everyone whose key it knows and signs them for everyone else."))
                }
            }
        }
    }

    @ViewBuilder
    private var ownKeySection: some View {
        if state.selectedAccount == nil || state.selectedAccount == "unified" {
            Text(String(localized: "Sign in to an account to show your public key."))
                .foregroundStyle(.secondary)
        } else if !ownKeyManaged {
            // Proton: show public key only (account keys).
            if ownPublicKey.isEmpty {
                Text(String(localized: "Sign in to an account to show your public key."))
                    .foregroundStyle(.secondary)
            } else {
                Text(ownPublicKey)
                    .font(.system(.caption, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Button(String(localized: "Copy Public Key")) { copyOwnPublicKey() }
            }
            Button(String(localized: "Load My Public Key")) { loadOwnKey() }
        } else if !ownKeyHasKey {
            Picker(String(localized: "Validity of a new key"), selection: $ownKeyYears) {
                ForEach(validityChoices, id: \.years) { c in
                    Text(c.label).tag(c.years)
                }
            }
            Button(String(localized: "Create a New Key")) { generateOwnKey() }
                .disabled(ownKeyBusy)
            Button(String(localized: "Import a Private Key…")) {
                importOwnKeyText = ""
                importOwnKeyPass = ""
                showImportOwnKey = true
            }
            .disabled(ownKeyBusy)
        } else {
            if !ownKeyFingerprint.isEmpty {
                LabeledContent(String(localized: "Key fingerprint")) {
                    Text(ownKeyFingerprint)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                }
            }
            if !ownKeyExpiresText.isEmpty {
                LabeledContent(String(localized: "Valid until")) {
                    Text(ownKeyExpiresText).foregroundStyle(.secondary)
                }
            }
            if !ownPublicKey.isEmpty {
                Text(ownPublicKey)
                    .font(.system(.caption2, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .lineLimit(6)
            }
            HStack {
                Button(String(localized: "Copy Public Key")) { copyOwnPublicKey() }
                Button(String(localized: "Save the Public Key")) { saveOwnPublicKeyFile() }
            }
            Menu(String(localized: "Extend…")) {
                ForEach(validityChoices, id: \.years) { c in
                    Button(c.label) { setOwnExpiry(c.years) }
                }
            }
            .disabled(ownKeyBusy)
            Button(String(localized: "Publish on keys.openpgp.org…")) { publishOwnKey() }
                .disabled(ownKeyBusy)
            Button(String(localized: "Save a Revocation Certificate…")) { saveRevocationCert() }
                .disabled(ownKeyBusy)
            Button(String(localized: "Back Up the Private Key…")) {
                backupPass = ""
                showBackupPass = true
            }
            .disabled(ownKeyBusy)
            Button(String(localized: "Remove Key"), role: .destructive) { deleteOwnKey() }
                .disabled(ownKeyBusy)
            Button(String(localized: "Load My Public Key")) { loadOwnKey() }
        }
    }

    private var rulesForm: some View {
        Form {
            Section {
                if rules.isEmpty {
                    ContentUnavailableView {
                        Label(String(localized: "No rules yet"), systemImage: "list.bullet.rectangle")
                    } description: {
                        Text(String(localized: "They apply to every new message in the inbox (after the spam filter). You can also create a rule for a sender from the ⋯ menu of a message."))
                    }
                    .frame(minHeight: 120)
                } else {
                    ForEach($rules) { $rule in
                        HStack(spacing: 10) {
                            Toggle("", isOn: $rule.enabled)
                                .labelsHidden()
                                .help(String(localized: "Enabled"))
                            VStack(alignment: .leading, spacing: 2) {
                                Text(rule.name.isEmpty ? String(localized: "Rule") : rule.name)
                                    .font(.body.weight(.medium))
                                Text(rule.summary)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(2)
                            }
                            Spacer(minLength: 8)
                            Button {
                                editingRule = rule
                            } label: {
                                Image(systemName: "pencil")
                            }
                            .buttonStyle(.borderless)
                            .help(String(localized: "Edit"))
                            Button(role: .destructive) {
                                rules.removeAll { $0.id == rule.id }
                            } label: {
                                Image(systemName: "trash")
                            }
                            .buttonStyle(.borderless)
                            .help(String(localized: "Delete"))
                        }
                        .padding(.vertical, 2)
                    }
                }
            } header: {
                HStack {
                    Text(String(localized: "Rules for Incoming Mail"))
                    Spacer()
                    Button {
                        editingRule = PrefRule.new()
                    } label: {
                        Image(systemName: "plus")
                    }
                    .buttonStyle(.borderless)
                    .help(String(localized: "Add Rule"))
                }
            } footer: {
                Text(String(localized: "They apply to every new message in the inbox (after the spam filter). You can also create a rule for a sender from the ⋯ menu of a message."))
            }

            Section {
                Button(String(localized: "Apply Rules to Inbox")) {
                    state.applyRulesToInbox()
                    status = String(localized: "Applying rules…")
                }
            }
        }
    }

    @ViewBuilder
    private func providerPicker(_ title: String, selection: Binding<String>) -> some View {
        Picker(title, selection: selection) {
            ForEach(availableProviders, id: \.id) { p in
                Text(String(localized: String.LocalizationValue(p.name))).tag(p.id)
            }
        }
    }

    private var availableProviders: [(id: String, name: String)] {
        if localOnly {
            return providers.filter { $0.id == "ollama" }
        }
        return providers
    }

    // MARK: - Load / save

    private func applyPendingTab() {
        if let tab = KlientSettings.pendingTab {
            selectedTab = tab
            KlientSettings.pendingTab = nil
        }
    }

    private func applyPendingRule() {
        guard let rule = KlientSettings.pendingRule else { return }
        KlientSettings.pendingRule = nil
        selectedTab = .rules
        // Defer so the Rules tab is visible before the editor sheet.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
            editingRule = rule
        }
    }

    private func load() {
        guard let cfg = try? Bridge.call("config.get") as? [String: Any] else { return }
        language = cfg["language"] as? String ?? ""
        holdUntilChecked = cfgBool(cfg["hold_until_checked"], true)
        spamEnabled = cfgBool(cfg["spam_filter_enabled"], true)
        spamThreshold = cfgDouble(cfg["spam_threshold"], 0.8)
        threads = cfgBool(cfg["threads"], true)
        sendDelay = cfgInt(cfg["send_delay"], 5)
        autoUpdate = cfgBool(cfg["auto_update"], true)
        launchAtLogin = LoginItem.isEnabled
        launchAtLoginNeedsApproval = LoginItem.requiresApproval
        LoginItem.desired = launchAtLogin
        assistantProvider = cfg["assistant_provider"] as? String ?? "claude"
        assistantModel = cfg["assistant_model"] as? String ?? ""
        spamProvider = cfg["spam_provider"] as? String ?? "claude"
        spamModel = cfg["spam_model"] as? String ?? ""
        if let rm = cfg["role_models"] as? [String: String] {
            roleModels = rm
        } else {
            roleModels = [:]
        }
        localOnly = cfgBool(cfg["local_only"], false)
        signature = cfg["signature"] as? String ?? ""
        signatureHTML = cfg["signature_html"] as? String ?? ""
        signatureUseHTML = cfgBool(cfg["signature_use_html"], false)
        if let raw = cfg["rules"] as? [[String: Any]] {
            rules = raw.map { PrefRule(dict: $0) }
        } else {
            rules = []
        }
        keyServerLookup = cfgBool(cfg["key_server_lookup"], true)
        attachPublicKey = cfgBool(cfg["attach_public_key"], false)
        requireEncryption = cfgBool(cfg["require_encryption"], false)
        offlineMessages = cfgInt(cfg["offline_messages"], 500)
        autoEmptyDays = cfgInt(cfg["auto_empty_days"], 0)
        protectSubject = cfgBool(cfg["protect_subject"], false)
        autoLabel = cfgBool(cfg["auto_label"], false)
        digestEnabled = cfgBool(cfg["digest"], false)
        digestHour = cfgInt(cfg["digest_hour"], 7)
        spellCheck = cfgBool(cfg["spell_check"], true)
        if let nb = cfg["notify_buttons"] as? [String] {
            notifyButtons = nb
        } else {
            notifyButtons = ["read", "archive", "trash", "spam"]
        }
        if let feeds = cfg["feeds"] as? [[String: Any]] {
            spamFeeds = feeds.compactMap { row in
                guard let url = row["url"] as? String, !url.isEmpty else { return nil }
                return PrefFeed(
                    name: row["name"] as? String ?? url,
                    url: url,
                    kind: row["kind"] as? String ?? "",
                    enabled: cfgBool(row["enabled"], true)
                )
            }
        } else {
            spamFeeds = []
        }
        refreshOllama()
        refreshOllamaModels()
        refreshAIStatus()
        refreshCacheStats()
        if selectedTab == .pgp { refreshPGP() }
        DispatchQueue.main.async { loaded = true }
    }

    private func refreshCacheStats() {
        let s = state.offlineCacheStats()
        cacheAvailable = s.available
        cacheMessages = s.messages
        cacheBodies = s.bodies
    }

    private func refreshPGP() {
        pgpKeys = (try? Bridge.call("pgp.listKeys") as? [String]) ?? []
        loadOwnKey()
    }

    private func loadOwnKey() {
        guard let acc = state.selectedAccount, acc != "unified" else {
            ownPublicKey = ""
            ownKeyManaged = false
            ownKeyHasKey = false
            ownKeyFingerprint = ""
            ownKeyExpiresText = ""
            return
        }
        guard let r = try? Bridge.call("pgp.ownKeyStatus", params: ["account": acc]) as? [String: Any] else {
            // Fallback for older bridge.
            if let r = try? Bridge.call("pgp.ownPublicKey", params: ["account": acc]) as? [String: String] {
                ownPublicKey = r["publicKey"] ?? ""
            } else {
                ownPublicKey = ""
            }
            ownKeyManaged = false
            return
        }
        ownKeyManaged = boolAny(r["managed"], true)
        ownKeyHasKey = boolAny(r["hasKey"], false)
        ownPublicKey = r["publicKey"] as? String ?? ""
        ownKeyFingerprint = r["fingerprint"] as? String ?? ""
        if let exp = r["expires"] as? NSNumber {
            let d = Date(timeIntervalSince1970: exp.doubleValue)
            var t = d.formatted(date: .abbreviated, time: .omitted)
            if boolAny(r["expired"], false) {
                t += " – " + String(localized: "expired")
            } else if boolAny(r["expiresSoon"], false) {
                t += " – " + String(localized: "expires soon, extend it")
            }
            ownKeyExpiresText = t
        } else if ownKeyHasKey {
            ownKeyExpiresText = String(localized: "no expiry")
        } else {
            ownKeyExpiresText = ""
        }
    }

    private func boolAny(_ v: Any?, _ d: Bool) -> Bool {
        if let b = v as? Bool { return b }
        if let n = v as? NSNumber { return n.boolValue }
        return d
    }

    private func copyOwnPublicKey() {
        guard !ownPublicKey.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(ownPublicKey, forType: .string)
        status = String(localized: "Copied")
    }

    private func generateOwnKey() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("pgp.generateOwnKey", params: [
                    "account": acc, "years": ownKeyYears,
                ])
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = String(localized: "Key created – from now on your mail is encrypted and signed")
                    loadOwnKey()
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func importOwnKey() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        let armored = importOwnKeyText
        let pass = importOwnKeyPass
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("pgp.importOwnKey", params: [
                    "account": acc, "armored": armored, "passphrase": pass,
                ])
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    showImportOwnKey = false
                    status = String(localized: "Key imported")
                    loadOwnKey()
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func setOwnExpiry(_ years: Int) {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("pgp.setOwnExpiry", params: ["account": acc, "years": years])
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = String(localized: "The key validity was changed")
                    loadOwnKey()
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func publishOwnKey() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("pgp.publishOwnKey", params: ["account": acc]) as? [String: Any]
                let st = r?["status"] as? String ?? ""
                let email = r?["email"] as? String ?? ""
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    switch st {
                    case "published":
                        status = String(localized: "The key is already published for this address")
                    case "pending":
                        status = String(format: String(localized: "Check your mail: keys.openpgp.org sent a confirmation link to %@"), email)
                    default:
                        status = String(localized: "The key was uploaded, but the address could not be confirmed")
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func saveOwnPublicKeyFile() {
        guard !ownPublicKey.isEmpty else { return }
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "public-key.asc"
        panel.allowedContentTypes = [.text]
        guard panel.runModal() == .OK, let url = panel.url else { return }
        try? ownPublicKey.write(to: url, atomically: true, encoding: .utf8)
        status = String(localized: "Saved")
    }

    private func saveRevocationCert() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("pgp.revocationCert", params: ["account": acc]) as? [String: String]
                let cert = r?["certificate"] ?? ""
                let email = r?["email"] ?? "key"
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    let panel = NSSavePanel()
                    let day = Date().formatted(.iso8601.year().month().day())
                    panel.nameFieldStringValue = "\(email)-revocation-\(day).asc"
                    panel.allowedContentTypes = [.text]
                    guard panel.runModal() == .OK, let url = panel.url else { return }
                    try? cert.write(to: url, atomically: true, encoding: .utf8)
                    status = String(localized: "Saved")
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func exportOwnPrivate() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        let pass = backupPass
        backupPass = ""
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("pgp.exportOwnPrivate", params: [
                    "account": acc, "passphrase": pass,
                ]) as? [String: String]
                let armored = r?["privateKey"] ?? ""
                let email = r?["email"] ?? "key"
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    let panel = NSSavePanel()
                    let day = Date().formatted(.iso8601.year().month().day())
                    panel.nameFieldStringValue = "\(email)-private-\(day).asc"
                    panel.allowedContentTypes = [.text]
                    guard panel.runModal() == .OK, let url = panel.url else { return }
                    try? armored.write(to: url, atomically: true, encoding: .utf8)
                    status = String(localized: "Saved")
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func deleteOwnKey() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        ownKeyBusy = true
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("pgp.deleteOwnKey", params: ["account": acc])
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = String(localized: "Key deleted")
                    loadOwnKey()
                }
            } catch {
                DispatchQueue.main.async {
                    ownKeyBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func importPGPKey() {
        let armored = pgpImportText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !armored.isEmpty else { return }
        do {
            let r = try Bridge.call("pgp.importKey", params: ["armored": armored]) as? [String: Any]
            let emails = r?["emails"] as? [String] ?? []
            status = emails.isEmpty
                ? String(localized: "Key imported")
                : String(format: String(localized: "Imported key for %@"), emails.joined(separator: ", "))
            pgpImportText = ""
            refreshPGP()
        } catch {
            status = error.localizedDescription
        }
    }

    private func deletePGPKey(_ email: String) {
        do {
            _ = try Bridge.call("pgp.deleteKey", params: ["email": email])
            refreshPGP()
            status = String(localized: "Key deleted")
        } catch {
            status = error.localizedDescription
        }
    }

    private func scheduleSave() {
        guard loaded else { return }
        saveWork?.cancel()
        let work = DispatchWorkItem { save(immediate: false) }
        saveWork = work
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.45, execute: work)
    }

    private func applyLaunchAtLogin(_ enabled: Bool) {
        guard loaded, !suppressLaunchAtLoginChange else { return }
        do {
            try LoginItem.setEnabled(enabled)
            launchAtLoginNeedsApproval = LoginItem.requiresApproval
            // Reflect OS truth (register may leave requiresApproval without .enabled).
            let osOn = LoginItem.isEnabled
            if launchAtLogin != osOn {
                suppressLaunchAtLoginChange = true
                launchAtLogin = osOn
                suppressLaunchAtLoginChange = false
            }
            if LoginItem.requiresApproval {
                status = String(localized: "Klient needs approval in System Settings → General → Login Items.")
                LoginItem.openSystemSettingsLoginItems()
            } else if enabled && osOn {
                status = String(localized: "Klient will open at login.")
            } else if !enabled {
                status = String(localized: "Klient will not open at login.")
            }
        } catch {
            suppressLaunchAtLoginChange = true
            launchAtLogin = LoginItem.isEnabled
            suppressLaunchAtLoginChange = false
            launchAtLoginNeedsApproval = LoginItem.requiresApproval
            status = error.localizedDescription
        }
    }

    private func save(immediate: Bool) {
        saveWork?.cancel()
        guard var cfg = try? Bridge.call("config.get") as? [String: Any] else { return }
        if localOnly {
            if assistantProvider != "ollama" { assistantProvider = "ollama" }
            if spamProvider != "ollama" { spamProvider = "ollama" }
        }
        cfg["language"] = language
        cfg["hold_until_checked"] = holdUntilChecked
        cfg["spam_filter_enabled"] = spamEnabled
        cfg["spam_threshold"] = spamThreshold
        cfg["threads"] = threads
        cfg["send_delay"] = sendDelay
        state.sendDelaySeconds = sendDelay
        cfg["auto_update"] = autoUpdate
        cfg["assistant_provider"] = assistantProvider
        cfg["assistant_model"] = assistantModel
        cfg["spam_provider"] = spamProvider
        cfg["spam_model"] = spamModel
        roleModels["assistant:\(assistantProvider)"] = assistantModel
        roleModels["spam:\(spamProvider)"] = spamModel
        cfg["role_models"] = roleModels
        cfg["local_only"] = localOnly
        cfg["signature"] = signature
        cfg["signature_html"] = signatureHTML
        cfg["signature_use_html"] = signatureUseHTML
        cfg["key_server_lookup"] = keyServerLookup
        cfg["attach_public_key"] = attachPublicKey
        cfg["require_encryption"] = requireEncryption
        cfg["offline_messages"] = offlineMessages
        cfg["auto_empty_days"] = autoEmptyDays
        cfg["protect_subject"] = protectSubject
        cfg["auto_label"] = autoLabel
        cfg["digest"] = digestEnabled
        cfg["digest_hour"] = digestHour
        cfg["spell_check"] = spellCheck
        cfg["notify_buttons"] = notifyButtons
        cfg["feeds"] = spamFeeds.map { [
            "name": $0.name, "url": $0.url, "kind": $0.kind, "enabled": $0.enabled,
        ] as [String: Any] }
        cfg["rules"] = rules.map { $0.asDict() }
        do {
            _ = try Bridge.call("config.save", params: cfg)
            state.signatureText = signature.isEmpty ? nil : signature
            state.signatureHTML = signatureHTML.isEmpty ? nil : signatureHTML
            state.signatureUseHTML = signatureUseHTML
            state.spellCheckEnabled = spellCheck
            state.digestEnabled = digestEnabled
            state.digestHour = digestHour
            state.notifyActionIDs = notifyButtons
            state.requireEncryptionDefault = requireEncryption
            state.attachPublicKeyDefault = attachPublicKey
            if state.threadsEnabled != threads {
                state.threadsEnabled = threads
                state.reloadMessages()
            } else {
                state.threadsEnabled = threads
            }
            state.refreshFilterStatus()
            // Only remount Settings/ContentView when language actually changes.
            // AI Test calls save() first; bumping localeEpoch was resetting selectedTab → General.
            if language != KlientLocale.configCode {
                KlientLocale.apply(language)
                state.refreshLocale()
            }
            status = String(localized: "Saved")
            refreshAIStatus()
        } catch {
            status = error.localizedDescription
        }
    }

    private func saveAPIKey() {
        let key = apiKey.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            _ = try Bridge.call("secrets.saveAPIKey", params: [
                "provider": apiKeyProvider, "key": key,
            ])
            apiKey = ""
            status = key.isEmpty
                ? String(localized: "API key removed")
                : String(localized: "API key saved")
            refreshAIStatus()
        } catch {
            status = error.localizedDescription
        }
    }

    private func refreshOllama() {
        if let s = try? Bridge.call("ollama.status") as? [String: Any] {
            let version = s["installed"] as? String ?? ""
            let installed = !version.isEmpty
            ollamaInstalled = installed
            let addr = s["addr"] as? String ?? ""
            if installed {
                var line = String(format: String(localized: "Version %@"), version)
                if !addr.isEmpty {
                    line += " · " + addr
                }
                let prog = int64Val(s["program_bytes"])
                let mods = int64Val(s["models_bytes"])
                if prog > 0 || mods > 0 {
                    line += " · " + String(format: String(localized: "program %@, models %@"),
                                           byteSizeLabel(prog), byteSizeLabel(mods))
                }
                if let pending = s["pending"] as? String, !pending.isEmpty {
                    line += " · " + String(format: String(localized: "new version %@ pending"), pending)
                }
                ollamaStatus = line
            } else {
                ollamaStatus = String(localized: "Ollama not installed")
            }
        } else {
            ollamaInstalled = false
            ollamaStatus = String(localized: "Ollama not installed")
        }
    }

    private func refreshOllamaModels() {
        guard ollamaInstalled else {
            ollamaModels = []
            return
        }
        DispatchQueue.global().async {
            let list: [OllamaInstalledModel]
            if let r = try? Bridge.call("ollama.listModels") as? [String: Any],
               let raw = r["models"] as? [[String: Any]] {
                list = raw.compactMap { row in
                    guard let name = row["name"] as? String, !name.isEmpty else { return nil }
                    return OllamaInstalledModel(name: name, size: int64Val(row["size"]))
                }
            } else {
                list = []
            }
            DispatchQueue.main.async {
                ollamaModels = list
            }
        }
    }

    @ViewBuilder
    private func modelField(provider: String, selection: Binding<String>, role: ModelPickRole) -> some View {
        if provider == "ollama" {
            if ollamaModels.isEmpty {
                TextField(String(localized: "Model"), text: selection)
            } else {
                Picker(String(localized: "Model"), selection: selection) {
                    ForEach(ollamaModels) { m in
                        Text(m.name).tag(m.name)
                    }
                    if !selection.wrappedValue.isEmpty,
                       !ollamaModels.contains(where: { $0.name == selection.wrappedValue }) {
                        Text(selection.wrappedValue).tag(selection.wrappedValue)
                    }
                }
            }
        } else {
            // Cloud: editable name + Choose… that loads ai.listModels for this provider
            // (API key from Keychain). Previously only a TextField existed here, so users
            // with a saved key could not pick OpenAI/Gemini spam models from a list.
            HStack(alignment: .center, spacing: 8) {
                TextField(String(localized: "Model"), text: selection)
                Button(String(localized: "Choose…")) {
                    openCloudModelPicker(provider: provider, role: role)
                }
                .disabled(modelPickerBusy || localOnly)
                if modelPickerBusy {
                    ProgressView()
                        .controlSize(.small)
                }
            }
        }
    }

    private func openCloudModelPicker(provider: String, role: ModelPickRole) {
        modelPickerBusy = true
        status = String(localized: "Loading models…")
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("ai.listModels", params: ["provider": provider]) as? [String: Any]
                let models = (r?["models"] as? [String]) ?? []
                DispatchQueue.main.async {
                    modelPickerBusy = false
                    cloudModelsProvider = provider
                    cloudModels = models
                    if models.isEmpty {
                        status = String(localized: "No models returned")
                    } else {
                        status = String(format: String(localized: "%d models loaded"), models.count)
                        modelPickerRole = role
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    modelPickerBusy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func switchProviderRole(role: String, from old: String, to new: String, model: Binding<String>) {
        guard old != new, !old.isEmpty else { return }
        roleModels["\(role):\(old)"] = model.wrappedValue
        if let remembered = roleModels["\(role):\(new)"], !remembered.isEmpty {
            model.wrappedValue = remembered
        } else if let defs = providerDefaults[new] {
            model.wrappedValue = role == "spam" ? defs.spam : defs.assistant
        }
    }

    private func isModelInstalled(_ name: String) -> Bool {
        ollamaModels.contains { $0.name == name || $0.name.hasPrefix(name + ":") || name.hasPrefix($0.name) }
    }

    private func inUseSuffix(_ name: String) -> String {
        let used = (assistantProvider == "ollama" && assistantModel == name)
            || (spamProvider == "ollama" && spamModel == name)
        return used ? String(localized: " · in use") : ""
    }

    private func useLocalModel(_ name: String) {
        assistantProvider = "ollama"
        spamProvider = "ollama"
        assistantModel = name
        spamModel = name
        scheduleSave()
        status = String(format: String(localized: "The assistant and the spam filter now use %@"), name)
        refreshAIStatus()
    }

    private func pullOllamaModel(_ name: String) {
        ollamaBusy = true
        state.ollamaPullModel = name
        state.ollamaPullStatus = String(localized: "Starting…")
        state.ollamaPullFrac = -1
        state.ollamaPullDone = 0
        state.ollamaPullTotal = 0
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("ollama.start")
                _ = try Bridge.call("ollama.pull", params: ["model": name])
                DispatchQueue.main.async {
                    ollamaBusy = false
                    state.clearOllamaPull()
                    customPullName = ""
                    refreshOllama()
                    refreshOllamaModels()
                    refreshAIStatus()
                    status = String(format: String(localized: "Model %@ is ready"), name)
                }
            } catch {
                DispatchQueue.main.async {
                    ollamaBusy = false
                    state.clearOllamaPull()
                    status = error.localizedDescription
                }
            }
        }
    }

    private func deleteOllamaModel(_ name: String) {
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("ollama.delete", params: ["model": name])
                DispatchQueue.main.async {
                    refreshOllamaModels()
                    refreshOllama()
                    status = String(format: String(localized: "Deleted %@"), name)
                }
            } catch {
                DispatchQueue.main.async { status = error.localizedDescription }
            }
        }
    }

    private func loadCloudModels(_ provider: String) {
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("ai.listModels", params: ["provider": provider]) as? [String: Any]
                let models = (r?["models"] as? [String]) ?? []
                DispatchQueue.main.async {
                    cloudModelsProvider = provider
                    cloudModels = models
                    if models.isEmpty {
                        status = String(localized: "No models returned")
                    } else {
                        status = String(format: String(localized: "%d models loaded"), models.count)
                    }
                }
            } catch {
                DispatchQueue.main.async { status = error.localizedDescription }
            }
        }
    }

    private func catalogCategoryTitle(_ category: String) -> String {
        switch category {
        case "Suggested": return String(localized: "Suggested models")
        case "Gemma": return "Gemma"
        case "Llama": return "Llama"
        case "Mistral": return "Mistral"
        case "Phi": return "Phi"
        case "Hugging Face": return "Hugging Face"
        default: return category
        }
    }

    private func byteSizeLabel(_ n: Int64) -> String {
        if n >= 1 << 30 { return String(format: "%.1f GB", Double(n) / Double(1 << 30)) }
        if n >= 1 << 20 { return String(format: "%.1f MB", Double(n) / Double(1 << 20)) }
        if n >= 1 << 10 { return String(format: "%.0f kB", Double(n) / Double(1 << 10)) }
        return "\(n) B"
    }

    private func int64Val(_ v: Any?) -> Int64 {
        if let i = v as? Int64 { return i }
        if let i = v as? Int { return Int64(i) }
        if let d = v as? Double { return Int64(d) }
        if let n = v as? NSNumber { return n.int64Value }
        return 0
    }

    private func refreshAIStatus() {
        if let s = try? Bridge.call("ai.status") as? [String: Any] {
            hasAssistant = cfgBool(s["hasAssistant"], false)
            hasSpamAI = cfgBool(s["hasSpam"], false)
            var parts: [String] = []
            if hasAssistant { parts.append(String(localized: "Assistant")) }
            if hasSpamAI { parts.append(String(localized: "Spam Filter")) }
            aiReady = parts.isEmpty ? String(localized: "Not configured") : parts.joined(separator: " · ")
        } else {
            hasAssistant = false
            hasSpamAI = false
            aiReady = String(localized: "Not configured")
        }
        if let u = try? Bridge.call("ai.usage") as? [String: Any] {
            let cost = cfgDouble(u["cost"], 0)
            let unpriced = cfgBool(u["unpriced"], false)
            if let lines = u["lines"] as? [[String: Any]] {
                usageDetailLines = lines
            } else {
                usageDetailLines = []
            }
            if cost > 0 {
                usageLine = String(format: "$%.2f", cost) + (unpriced ? "+" : "")
            } else if unpriced || !usageDetailLines.isEmpty {
                usageLine = String(localized: "Unpriced usage")
            } else {
                usageLine = String(localized: "None yet")
            }
        }
    }

    private func testAI() {
        aiTesting = true
        aiTestOK = nil
        aiTestDetail = ""
        selectedTab = .ai
        KlientSettings.lastTab = .ai
        // Persist pickers so the engine config matches what we are about to test.
        // save() must not remount Settings (locale unchanged) or we lose this tab.
        save(immediate: true)
        selectedTab = .ai
        // Test the configured assistant (and spam when it differs) — never the
        // API-keys picker alone, which used to make Grok/Gemini look "broken"
        // while Claude was still selected there.
        let aProv = localOnly ? "ollama" : assistantProvider
        let sProv = localOnly ? "ollama" : spamProvider
        var targets: [(provider: String, model: String)] = [
            (aProv, assistantModel),
        ]
        if sProv != aProv || spamModel != assistantModel {
            targets.append((sProv, spamModel))
        }
        DispatchQueue.global().async {
            var lastErr: String?
            for t in targets {
                do {
                    _ = try Bridge.call("ai.test", params: [
                        "provider": t.provider,
                        "model": t.model,
                    ])
                } catch {
                    lastErr = error.localizedDescription
                    break
                }
            }
            DispatchQueue.main.async {
                // Keep the user on AI even if something remounted PreferencesView.
                selectedTab = .ai
                KlientSettings.lastTab = .ai
                aiTesting = false
                if let lastErr {
                    aiTestOK = false
                    aiTestDetail = lastErr
                    status = lastErr
                } else {
                    aiTestOK = true
                    aiTestDetail = ""
                    status = String(localized: "Connection test succeeded")
                }
                refreshAIStatus()
            }
        }
    }
}

// MARK: - Rule model

struct PrefRule: Identifiable, Hashable, Equatable {
    var id: UUID
    var name: String
    var field: String
    var contains: String
    var action: String
    var target: String
    var enabled: Bool

    static func new() -> PrefRule {
        PrefRule(id: UUID(), name: "", field: "from", contains: "", action: "move", target: "", enabled: true)
    }

    init(id: UUID = UUID(), name: String, field: String, contains: String, action: String, target: String, enabled: Bool) {
        self.id = id
        self.name = name
        self.field = field
        self.contains = contains
        self.action = action
        self.target = target
        self.enabled = enabled
    }

    init(dict: [String: Any]) {
        id = UUID()
        name = dict["name"] as? String ?? ""
        field = dict["field"] as? String ?? "from"
        contains = dict["contains"] as? String ?? ""
        action = dict["action"] as? String ?? "move"
        target = dict["target"] as? String ?? ""
        enabled = cfgBool(dict["enabled"], true)
    }

    func asDict() -> [String: Any] {
        var d: [String: Any] = [
            "name": name,
            "field": field,
            "contains": contains,
            "action": action,
            "enabled": enabled,
        ]
        if action == "move" || action == "label" {
            d["target"] = target
        }
        return d
    }

    var summary: String {
        let f = PrefRule.fieldLabel(field)
        let a = PrefRule.actionLabel(action)
        let needle = contains.isEmpty ? "…" : "“\(contains)”"
        if action == "move" || action == "label" {
            let t = target.isEmpty ? "…" : target
            return "\(f) \(String(localized: "contains")) \(needle) → \(a) \(t)"
        }
        return "\(f) \(String(localized: "contains")) \(needle) → \(a)"
    }

    static func fieldLabel(_ f: String) -> String {
        switch f {
        case "from": return String(localized: "Sender")
        case "to": return String(localized: "Recipient")
        case "subject": return String(localized: "Subject")
        default: return f
        }
    }

    static func actionLabel(_ a: String) -> String {
        switch a {
        case "move": return String(localized: "Move to")
        case "label": return String(localized: "Add label")
        case "read": return String(localized: "Mark as read")
        case "star": return String(localized: "Add star")
        default: return a
        }
    }
}

private struct RuleEditorSheet: View {
    @State var rule: PrefRule
    var folders: [AppState.FolderRow]
    var onSave: (PrefRule) -> Void
    var onCancel: () -> Void

    private var needsTarget: Bool {
        rule.action == "move" || rule.action == "label"
    }

    private var saveDisabled: Bool {
        let emptyContains = rule.contains.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let emptyTarget = rule.target.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return emptyContains || (needsTarget && emptyTarget)
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(String(localized: "Edit Rule"))
                    .font(.headline)
                Spacer()
            }
            .padding()

            Form {
                TextField(String(localized: "Name"), text: $rule.name)
                Picker(String(localized: "Field"), selection: $rule.field) {
                    Text(PrefRule.fieldLabel("from")).tag("from")
                    Text(PrefRule.fieldLabel("to")).tag("to")
                    Text(PrefRule.fieldLabel("subject")).tag("subject")
                }
                TextField(String(localized: "Contains"), text: $rule.contains)
                Picker(String(localized: "Action"), selection: $rule.action) {
                    Text(PrefRule.actionLabel("move")).tag("move")
                    Text(PrefRule.actionLabel("label")).tag("label")
                    Text(PrefRule.actionLabel("read")).tag("read")
                    Text(PrefRule.actionLabel("star")).tag("star")
                }
                if needsTarget {
                    RuleTargetPicker(rule: $rule, folders: folders)
                }
                Toggle(String(localized: "Enabled"), isOn: $rule.enabled)
            }
            .formStyle(.grouped)

            HStack {
                Button(String(localized: "Cancel"), action: onCancel)
                    .keyboardShortcut(.cancelAction)
                Spacer()
                Button(String(localized: "Save")) { onSave(rule) }
                    .keyboardShortcut(.defaultAction)
                    .disabled(saveDisabled)
            }
            .padding()
        }
        .frame(width: 420, height: 460)
    }
}

private struct RuleTargetPicker: View {
    @Binding var rule: PrefRule
    var folders: [AppState.FolderRow]

    private var isLabelAction: Bool { rule.action == "label" }

    private var targetFolders: [AppState.FolderRow] {
        folders.filter { isLabelAction ? $0.isLabel : !$0.isLabel }
    }

    var body: some View {
        if folders.isEmpty {
            TextField(
                isLabelAction ? String(localized: "Label ID") : String(localized: "Folder ID"),
                text: $rule.target
            )
        } else {
            Picker(
                isLabelAction ? String(localized: "Label") : String(localized: "Folder"),
                selection: $rule.target
            ) {
                Text(String(localized: "Choose…")).tag("")
                ForEach(targetFolders) { f in
                    Text(KlientLocale.folderDisplayName(id: f.id, name: f.name)).tag(f.id)
                }
            }
        }
    }
}

// Tiny helpers so JSON numbers from the Go bridge (often Double) bind cleanly.
private func cfgBool(_ v: Any?, _ d: Bool) -> Bool {
    if let b = v as? Bool { return b }
    if let n = v as? NSNumber { return n.boolValue }
    return d
}

private func cfgInt(_ v: Any?, _ d: Int) -> Int {
    if let i = v as? Int { return i }
    if let n = v as? NSNumber { return n.intValue }
    return d
}

private func cfgDouble(_ v: Any?, _ d: Double) -> Double {
    if let x = v as? Double { return x }
    if let n = v as? NSNumber { return n.doubleValue }
    return d
}

private struct OllamaInstalledModel: Identifiable, Equatable {
    var id: String { name }
    let name: String
    let size: Int64
}

private struct OllamaCatalogEntry: Identifiable, Equatable {
    var id: String { category + "|" + name }
    let name: String
    let label: String
    let size: String
    let category: String
}

extension Notification.Name {
    static let klientOpenSettings = Notification.Name("KlientOpenSettings")
}

enum KlientSettings {
    /// Optional tab to select once Settings appears (`PreferencesView.Tab.rawValue`).
    @MainActor static var pendingTab: PreferencesView.Tab?
    /// Last user-selected tab — survives `localeEpoch` remounts of PreferencesView.
    @MainActor static var lastTab: PreferencesView.Tab?
    /// Prefill Rules editor (e.g. “Create a Rule for the Sender…”).
    @MainActor static var pendingRule: PrefRule?

    /// Opens the SwiftUI `Settings` scene via `OpenSettingsAction` (see `SettingsOpener`).
    @MainActor
    static func open(tab: PreferencesView.Tab? = nil) {
        pendingTab = tab
        if let tab { lastTab = tab }
        NotificationCenter.default.post(
            name: .klientOpenSettings,
            object: nil,
            userInfo: tab.map { ["tab": $0.rawValue] }
        )
        NSApp.activate(ignoringOtherApps: true)
    }
}

/// Lives in the main window so `@Environment(\.openSettings)` is available.
struct SettingsOpener: View {
    @Environment(\.openSettings) private var openSettings

    var body: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
            .onReceive(NotificationCenter.default.publisher(for: .klientOpenSettings)) { _ in
                openSettings()
            }
    }
}
