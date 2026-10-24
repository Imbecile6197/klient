import AppKit
import Foundation
import UserNotifications
import CoreServices

@MainActor
final class AppState: ObservableObject {
    @Published var version: String = ""
    @Published var accounts: [AccountRow] = []
    @Published var selectedAccount: String?
    @Published var folders: [FolderRow] = []
    @Published var selectedFolder: String = ""
    @Published var messages: [MessageRow] = []
    @Published var selectedMessageID: String?
    /// Multi-select (Cmd/Shift-click in the list).
    @Published var selectedMessageIDs: Set<String> = []
    @Published var openMessage: MessageDetail?
    @Published var threadMessages: [MessageRow] = []
    @Published var status: String = ""
    /// Proton API down or IMAP started without a connection (Linux offline banner).
    @Published var offlineBanner = false
    @Published var accountStartedOffline = false
    @Published var protonAPIOffline = false
    @Published var searchQuery = ""
    @Published var threadsEnabled = true
    /// Expanded thread card IDs (stacked conversation UI).
    @Published var expandedThreadIDs: Set<String> = []
    /// Sender PGP key-change / pending notice for the open message.
    @Published var senderKeyNotice: SenderKeyNotice?
    @Published var showFolderNamePrompt = false
    @Published var folderPromptMode: FolderPromptMode = .create
    @Published var folderPromptName = ""
    @Published var folderPromptTargetID = ""
    @Published var pendingPrint = false
    @Published var showUnsubscribeConfirm = false

    enum FolderPromptMode {
        case create, rename
    }

    @Published var loginUsername = ""
    @Published var loginPassword = ""
    @Published var loginTOTP = ""
    @Published var needsTOTP = false
    @Published var needsMailboxPassword = false
    @Published var needsHV = false
    @Published var hvURL = ""
    @Published var mailboxPassword = ""
    @Published var showLogin = false
    @Published var loggingIn = false

    @Published var showCompose = false
    @Published var composeReply: MessageDetail?
    @Published var composeAction = 0
    @Published var composePrefillTo: String?
    @Published var showContacts = false
    @Published var showAddAccount = false
    @Published var showAskAI = false
    @Published var askQuestion = ""
    @Published var askAnswer = ""
    /// Visible AI summary panel in the message reader (not only the status strip).
    @Published var messageSummary = ""
    @Published var messageSummaryBusy = false
    /// Spam-filter verdict for the open message (from `spam.decision`).
    @Published var spamDecision: SpamDecision?
    /// True while an on-open evaluate is running (no stored decision yet).
    @Published var spamDecisionBusy = false
    /// When opening compose as reply, run Draft Reply with AI once.
    @Published var composeAutoDraftAI = false
    @Published var showAbout = false
    @Published var showAutoReply = false
    @Published var showUpdate = false
    @Published var showWhatsNew = false
    @Published var showHelp = false
    @Published var showOutbox = false
    @Published var whatsNewReleases: [[String: Any]] = []
    @Published var updateInfo: [String: Any]?
    @Published var invites: [[String: Any]] = []
    @Published var aboutInfo: [String: Any] = [:]
    @Published var outboxItems: [OutboxItem] = []
    /// Bumps when Preferences → Language changes so views re-resolve `String(localized:)`.
    @Published var localeEpoch: Int = 0
    @Published var uiLocale: Locale = KlientLocale.locale()
    @Published var ollamaPullModel = ""
    @Published var ollamaPullStatus = ""
    @Published var ollamaPullFrac: Double = -1
    @Published var ollamaPullDone: Int64 = 0
    @Published var ollamaPullTotal: Int64 = 0

    func clearOllamaPull() {
        ollamaPullModel = ""
        ollamaPullStatus = ""
        ollamaPullFrac = -1
        ollamaPullDone = 0
        ollamaPullTotal = 0
    }

    var outboxCount: Int { outboxItems.count }

    /// Params that pin Go-side content (release notes, help) to Preferences → Language.
    func langParams(_ extra: [String: Any] = [:]) -> [String: Any] {
        var p = extra
        p["lang"] = KlientLocale.resolvedCode()
        return p
    }

    /// Remounts views that key off `localeEpoch` so `String(localized:)` re-resolves.
    /// No-ops when the effective locale is unchanged — avoids wiping Settings tab state
    /// (e.g. AI Test → save → jump back to General).
    func refreshLocale() {
        let next = KlientLocale.locale()
        guard next.identifier != uiLocale.identifier else { return }
        uiLocale = next
        localeEpoch &+= 1
    }

    struct OutboxItem: Identifiable, Equatable {
        let id: String
        let account: String
        let email: String
        let subject: String
        let to: String
        let error: String
        let waiting: Bool
        let sending: Bool
    }

    @Published var sendAddresses: [SendAddress] = []
    @Published var signatureText: String?
    @Published var signatureHTML: String?
    @Published var signatureUseHTML = false
    @Published var unreadTotal = 0
    @Published var pendingMailto: String?
    @Published var unifiedEnabled = false

    /// Preferences → send_delay (seconds); 0 = send immediately.
    @Published var sendDelaySeconds = 5
    @Published var spellCheckEnabled = true
    @Published var digestEnabled = false
    @Published var digestHour = 7
    @Published var notifyActionIDs: [String] = ["read", "archive", "trash", "spam"]
    @Published var requireEncryptionDefault = false
    @Published var attachPublicKeyDefault = false
    @Published var accountE2E = false
    @Published var readerPlainText = false
    @Published var showShortcuts = false
    @Published var hiddenFolderIDs: Set<String> = []
    @Published var folderPromptAsLabel = false

    /// Undo-send banner (Linux undoToast).
    @Published var undoSendSecondsLeft = 0
    @Published var undoSendActive = false
    private var undoSendParams: [String: Any]?
    private var undoSendRestore: [String: Any]?
    private var undoSendTimer: Timer?
    private var digestTimer: Timer?

    /// Custom snooze date picker sheet.
    @Published var showSnoozePicker = false
    @Published var snoozePickerDate = Date().addingTimeInterval(3600)

    /// Bottom-left status strip: AI providers + feeds + storage + AI spend (Linux bottom bars).
    @Published var filterStatus = FilterStatus()

    /// Proton/IMAP system folder IDs from the Go core.
    var labels = SystemLabels()

    struct FilterStatus: Equatable {
        var hasAssistant = false
        var hasSpamAI = false
        var spamFilterEnabled = true
        var assistantName = ""
        var assistantModel = ""
        var assistantLocal = false
        var spamName = ""
        var spamModel = ""
        var spamLocal = false
        var blocklistNetworks = 0
        var blocklistDomains = 0
        var blocklistURLs = 0
        /// Unix seconds; 0 means never updated.
        var blocklistLastUpdate: TimeInterval = 0

        /// Mailbox quota from mail.caps (Linux updateStorage).
        var storageUsed: UInt64 = 0
        var storageMax: UInt64 = 0
        var storageMail: UInt64 = 0
        var storageDrive: UInt64 = 0
        var storageDriveMax: UInt64 = 0
        var storageSplit = false
        /// Unix seconds; 0 means unknown.
        var storageUpdated: TimeInterval = 0

        /// AI spend this month / today from ai.usage (Linux usageLabel).
        var usageReady = false
        var usageMonthCost: Double = 0
        var usageTodayCost: Double = 0
        var usageMonthUnpriced = false
        var usageTodayUnpriced = false

        var showsStorage: Bool { storageMax > 0 || storageUsed > 0 }
    }

    struct SystemLabels {
        var inbox = "0"
        var drafts = "1"
        var sent = "2"
        var trash = "3"
        var spam = "4"
        var allMail = "5"
        var archive = "6"
        var starred = "10"
        var scheduled = "12"
        var snoozed = "16"
    }

    struct AccountRow: Identifiable, Hashable {
        var id: String { username }
        let username: String
        let email: String
        let displayName: String
        let kind: String
    }

    struct FolderRow: Identifiable, Hashable {
        let id: String
        let name: String
        let custom: Bool
        /// Proton custom label (tag), not an exclusive folder.
        let isLabel: Bool
        let color: String

        init(id: String, name: String, custom: Bool, isLabel: Bool = false, color: String = "") {
            self.id = id
            self.name = name
            self.custom = custom
            self.isLabel = isLabel
            self.color = color
        }
    }

    struct MessageRow: Identifiable, Hashable {
        let id: String
        let subject: String
        let sender: String
        let senderEmail: String
        let time: Int64
        let unread: Bool
        let conversationId: String
        var threadCount: Int = 1
        var hasAttachments: Bool = false
        var labels: [String] = []
        /// Local spam-filter marked this as spam (list junk badge).
        var isSpam: Bool = false
        var spamCategory: String = ""
    }

    /// Stored outcome from the local spam filter (`spam.decision`).
    struct SpamDecision: Equatable {
        var spam: Bool
        var source: String
        var category: String
        var spamProbability: Double
        var reason: String
        var hits: [String]

        var bannerText: String {
            switch source {
            case "ai":
                let why = reason.isEmpty ? "" : " – \(reason)"
                return String(
                    format: String(localized: "AI spam filter: %@ (%.0f %%)%@"),
                    Self.categoryName(category.isEmpty ? (spam ? "spam" : "ham") : category),
                    spamProbability * 100,
                    why
                )
            case "dangerous-link":
                return String(
                    format: String(localized: "Spam filter: a link leads to a known phishing or malware page – %@"),
                    hits.joined(separator: "; ")
                )
            case "user":
                return spam
                    ? String(localized: "You marked this as spam")
                    : String(localized: "You marked this as legitimate")
            case "none", "":
                if !category.isEmpty || spamProbability > 0 {
                    return String(
                        format: String(localized: "Spam filter: %@ (%.0f %%)"),
                        Self.categoryName(category.isEmpty ? "unknown" : category),
                        spamProbability * 100
                    )
                }
                return String(localized: "Spam filter: not yet classified")
            default:
                let detail = hits.joined(separator: "; ")
                if detail.isEmpty {
                    return String(format: String(localized: "Spam filter (%@)"), source)
                }
                return String(
                    format: String(localized: "Spam filter (%@): %@"),
                    source,
                    detail
                )
            }
        }

        /// Show "Not Spam" when auto-classified as spam (Linux banner parity).
        var showsNotSpamAction: Bool {
            spam && source != "user"
        }

        static func categoryName(_ raw: String) -> String {
            switch raw {
            case "ham": return String(localized: "legitimate")
            case "scam": return String(localized: "scam")
            case "unknown": return String(localized: "unknown")
            case "newsletter", "spam", "phishing", "malware":
                return raw
            default:
                return raw.isEmpty ? String(localized: "unknown") : raw
            }
        }
    }

    struct MessageDetail {
        let id: String
        let subject: String
        let sender: String
        let senderEmail: String
        let to: String
        let html: String
        let text: String
        /// CSP-safe document fragment from Go (mailparse.PrepareHTML).
        let bodyHTML: String
        let hasRemoteContent: Bool
        let remoteAllowed: Bool
        let unsubscribe: [String: Any]?
        let conversationId: String
        let attachments: [[String: Any]]
        let labels: [String]
        let time: Int64
        var addressId: String = ""
    }

    struct UnsubscribeInfo {
        let http: String
        let mailto: String
        let oneClick: Bool
        var any: Bool { !http.isEmpty || !mailto.isEmpty }
    }

    struct SenderKeyNotice: Equatable {
        let email: String
        let text: String
        let warn: Bool
        let pending: Bool
    }

    struct SendAddress: Hashable {
        let id: String
        let email: String
        let name: String
    }

    func start() {
        Bridge.initEngine { [weak self] json in
            Task { @MainActor in self?.handleEvent(json) }
        }
        version = Bridge.version()
        KlientLocale.applyFromConfig()
        refreshLocale()
        requestNotifications()
        loadSystemLabels()
        if let cfg = try? Bridge.call("config.get") as? [String: Any] {
            signatureText = cfg["signature"] as? String
            signatureHTML = cfg["signature_html"] as? String
            threadsEnabled = boolVal(cfg["threads"], default: true)
            if let n = cfg["send_delay"] as? Int {
                sendDelaySeconds = n
            } else if let n = cfg["send_delay"] as? NSNumber {
                sendDelaySeconds = n.intValue
            }
            if let b = cfg["signature_use_html"] as? Bool {
                signatureUseHTML = b
            } else if let n = cfg["signature_use_html"] as? NSNumber {
                signatureUseHTML = n.boolValue
            } else {
                signatureUseHTML = false
            }
            spellCheckEnabled = boolVal(cfg["spell_check"], default: true)
            digestEnabled = boolVal(cfg["digest"], default: false)
            if let n = cfg["digest_hour"] as? Int {
                digestHour = n
            } else if let n = cfg["digest_hour"] as? NSNumber {
                digestHour = n.intValue
            }
            requireEncryptionDefault = boolVal(cfg["require_encryption"], default: false)
            attachPublicKeyDefault = boolVal(cfg["attach_public_key"], default: false)
            if let nb = cfg["notify_buttons"] as? [String] {
                notifyActionIDs = nb
            }
        }
        if let about = try? Bridge.call("about") as? [String: Any] {
            aboutInfo = about
        }
        status = String(localized: "Connecting…")
        MenuBarController.shared.attach(state: self)
        scheduleAutoUpdateCheck()
        startDigestTimer()
        registerNotificationCategories()
        if ProcessInfo.processInfo.environment["KLIENT_OPEN_SETTINGS"] != nil {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) {
                KlientSettings.open()
            }
        }
        // Never block the AppKit main thread on Proton Resume / IMAP open —
        // that freezes the window before first paint (looks like "won't start").
        let demo = ProcessInfo.processInfo.environment["KLIENT_DEMO"] != nil
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            if demo {
                _ = try? Bridge.call("demo.start")
            } else {
                _ = try? Bridge.call("accounts.resumeAll")
            }
            _ = try? Bridge.call("accounts.unified", params: ["enable": true])
            DispatchQueue.main.async {
                guard let self else { return }
                self.unifiedEnabled = true
                self.refreshAccounts()
                if self.accounts.isEmpty {
                    // Saved accounts that failed to resume (network blip) must not
                    // flash the Proton login sheet — only a truly empty install.
                    let saved = (try? Bridge.call("config.get") as? [String: Any])?["accounts"] as? [String] ?? []
                    if saved.isEmpty {
                        self.showLogin = true
                    } else {
                        self.status = String(localized: "Could not restore accounts – check the connection")
                    }
                } else if self.selectedAccount == nil {
                    self.selectedAccount = self.accounts.first(where: { $0.username == "unified" })?.username
                        ?? self.accounts.first?.username
                    self.loadFoldersAndMail()
                }
                self.refreshFilterStatus()
                self.refreshOutbox()
                self.status = "Klient \(self.version)"
                DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { [weak self] in
                    self?.checkWhatsNew()
                }
            }
        }
    }

    private func startDigestTimer() {
        digestTimer?.invalidate()
        digestTimer = Timer.scheduledTimer(withTimeInterval: 60, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.tickDigest() }
        }
    }

    private func tickDigest() {
        guard digestEnabled else { return }
        let hour = Calendar.current.component(.hour, from: Date())
        let minute = Calendar.current.component(.minute, from: Date())
        guard hour == digestHour, minute == 0 else { return }
        let today = ISO8601DateFormatter().string(from: Date()).prefix(10)
        guard var cfg = try? Bridge.call("config.get") as? [String: Any] else { return }
        let last = cfg["digest_last"] as? String ?? ""
        guard last != String(today) else { return }
        cfg["digest_last"] = String(today)
        _ = try? Bridge.call("config.save", params: cfg)
        runDigest()
    }

    private func registerNotificationCategories() {
        let actions: [(String, String)] = [
            ("read", String(localized: "Mark as read")),
            ("archive", String(localized: "Archive")),
            ("trash", String(localized: "Trash")),
            ("spam", String(localized: "Spam")),
        ]
        let unActions = actions
            .filter { notifyActionIDs.contains($0.0) }
            .map { UNNotificationAction(identifier: $0.0, title: $0.1, options: []) }
        let cat = UNNotificationCategory(
            identifier: "KLIENT_MAIL",
            actions: unActions,
            intentIdentifiers: [],
            options: []
        )
        UNUserNotificationCenter.current().setNotificationCategories([cat])
        UNUserNotificationCenter.current().delegate = NotificationActionDelegate.shared
        NotificationActionDelegate.shared.state = self
    }

    func openHelp() {
        showHelp = true
    }

    func refreshOutbox() {
        DispatchQueue.global().async {
            let result = (try? Bridge.call("outbox.list")) as? [String: Any]
            let raw = result?["items"] as? [[String: Any]] ?? []
            DispatchQueue.main.async {
                self.outboxItems = raw.map { self.parseOutboxItem($0) }
            }
        }
    }

    private func parseOutboxItem(_ row: [String: Any]) -> OutboxItem {
        OutboxItem(
            id: row["id"] as? String ?? "",
            account: row["account"] as? String ?? "",
            email: row["email"] as? String ?? "",
            subject: row["subject"] as? String ?? "",
            to: row["to"] as? String ?? "",
            error: row["error"] as? String ?? "",
            waiting: boolVal(row["waiting"]),
            sending: boolVal(row["sending"])
        )
    }

    func retryOutboxItem(_ id: String) {
        DispatchQueue.global().async {
            _ = try? Bridge.call("outbox.send", params: ["id": id])
            DispatchQueue.main.async { self.refreshOutbox() }
        }
    }

    func deleteOutboxItem(_ id: String) {
        DispatchQueue.global().async {
            _ = try? Bridge.call("outbox.delete", params: ["id": id])
            DispatchQueue.main.async { self.refreshOutbox() }
        }
    }

    func editOutboxItem(_ id: String) {
        DispatchQueue.global().async {
            do {
                guard let result = try Bridge.call("outbox.edit", params: ["id": id]) as? [String: Any] else {
                    return
                }
                DispatchQueue.main.async {
                    if let acc = result["account"] as? String, !acc.isEmpty {
                        self.selectedAccount = acc
                    }
                    DraftOpenStore.shared.apply(result)
                    self.composeReply = nil
                    self.composeAction = result["action"] as? Int ?? 0
                    self.composePrefillTo = nil
                    self.showCompose = true
                    self.refreshOutbox()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    /// Opens Settings → Rules with a rule prefilled for this sender (Linux message menu).
    func createRuleFromSender(_ email: String) {
        let addr = email.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !addr.isEmpty else { return }
        let name = String(format: String(localized: "From %@"), addr)
        KlientSettings.pendingRule = PrefRule(
            name: name, field: "from", contains: addr,
            action: "move", target: "", enabled: true
        )
        KlientSettings.open(tab: .rules)
    }

    func handleSendResult(_ result: [String: Any]?) -> Bool {
        if boolVal(result?["queued"]) {
            let waiting = boolVal(result?["waiting"])
            status = waiting
                ? String(localized: "No connection – the message waits in the Outbox and will be sent automatically")
                : String(localized: "Sending failed – the message waits in the Outbox")
            refreshOutbox()
            showOutbox = true
            return true
        }
        return false
    }

    func checkWhatsNew() {
        guard let r = try? Bridge.call("about.whatsNew", params: langParams()) as? [String: Any] else { return }
        let show = boolVal(r["show"])
        let releases = r["releases"] as? [[String: Any]] ?? []
        guard show, !releases.isEmpty else { return }
        whatsNewReleases = releases
        showWhatsNew = true
    }

    func openWhatsNew() {
        if let r = try? Bridge.call("about.releaseNotes", params: langParams(["currentOnly": true])) as? [String: Any],
           let releases = r["releases"] as? [[String: Any]], !releases.isEmpty {
            whatsNewReleases = releases
        } else {
            whatsNewReleases = []
        }
        showWhatsNew = true
    }

    /// Like Linux selfUpdateLoop: once a day when AutoUpdate is on, after mail has a chance to load.
    private func scheduleAutoUpdateCheck() {
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 180) { [weak self] in
            guard let self else { return }
            guard let cfg = try? Bridge.call("config.get") as? [String: Any] else { return }
            let auto: Bool = {
                if let b = cfg["auto_update"] as? Bool { return b }
                if let n = cfg["auto_update"] as? NSNumber { return n.boolValue }
                return true
            }()
            guard auto else { return }
            if let last = cfg["update_last_check"] as? String, !last.isEmpty {
                let fre = ISO8601DateFormatter()
                fre.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
                let plain = ISO8601DateFormatter()
                if let t = fre.date(from: last) ?? plain.date(from: last),
                   Date().timeIntervalSince(t) < 24 * 3600 {
                    return
                }
            }
            Task { @MainActor in
                self.checkForUpdates(silent: true)
            }
        }
    }

    /// Refresh footer status off the main thread. Never call Bridge from a
    /// SwiftUI body/task on the main actor while `accounts.resumeAll` may hold
    /// the bridge lock — that freezes the first window paint.
    func refreshFilterStatus() {
        let account = selectedAccount
        DispatchQueue.global(qos: .utility).async { [weak self] in
            var next = FilterStatus()
            if let s = try? Bridge.call("ai.status") as? [String: Any] {
                next.hasAssistant = JSONVal.bool(s["hasAssistant"])
                next.hasSpamAI = JSONVal.bool(s["hasSpam"])
                next.spamFilterEnabled = JSONVal.bool(s["spamFilterEnabled"], default: true)
                next.assistantName = s["assistantName"] as? String ?? ""
                next.assistantModel = s["assistantModel"] as? String ?? ""
                next.assistantLocal = JSONVal.bool(s["assistantLocal"])
                next.spamName = s["spamName"] as? String ?? ""
                next.spamModel = s["spamModel"] as? String ?? ""
                next.spamLocal = JSONVal.bool(s["spamLocal"])
                next.blocklistNetworks = JSONVal.int(s["blocklistNetworks"])
                next.blocklistDomains = JSONVal.int(s["blocklistDomains"])
                next.blocklistURLs = JSONVal.int(s["blocklistURLs"])
                next.blocklistLastUpdate = JSONVal.time(s["blocklistLastUpdate"])
            }
            JSONVal.fillStorage(account: account, into: &next)
            JSONVal.fillUsage(into: &next)
            DispatchQueue.main.async {
                self?.filterStatus = next
            }
        }
    }

    private func boolVal(_ v: Any?, default def: Bool = false) -> Bool { JSONVal.bool(v, default: def) }
    private func intVal(_ v: Any?) -> Int { JSONVal.int(v) }
    private func timeVal(_ v: Any?) -> TimeInterval { JSONVal.time(v) }
    private func uint64Val(_ v: Any?) -> UInt64 { JSONVal.uint64(v) }
    private func doubleVal(_ v: Any?) -> Double { JSONVal.double(v) }

    func loadSystemLabels() {
        guard let m = try? Bridge.call("mail.systemLabels") as? [String: String] else { return }
        labels.inbox = m["inbox"] ?? labels.inbox
        labels.spam = m["spam"] ?? labels.spam
        labels.trash = m["trash"] ?? labels.trash
        labels.archive = m["archive"] ?? labels.archive
        labels.starred = m["starred"] ?? labels.starred
        labels.drafts = m["drafts"] ?? labels.drafts
        labels.sent = m["sent"] ?? labels.sent
        labels.allMail = m["allMail"] ?? labels.allMail
        labels.scheduled = m["scheduled"] ?? labels.scheduled
        labels.snoozed = m["snoozed"] ?? labels.snoozed
    }

    func refreshAccounts() {
        guard let result = try? Bridge.call("accounts.list") as? [[String: Any]] else { return }
        accounts = result.compactMap { row in
            guard let username = row["username"] as? String else { return nil }
            return AccountRow(
                username: username,
                email: row["email"] as? String ?? "",
                displayName: row["displayName"] as? String ?? "",
                kind: row["kind"] as? String ?? ""
            )
        }
        if selectedAccount == nil {
            selectedAccount = accounts.first?.username
        }
    }

    func loadFoldersAndMail() {
        guard let acc = selectedAccount else { return }
        if let caps = try? Bridge.call("mail.caps", params: ["account": acc]) as? [String: Any] {
            accountE2E = boolVal(caps["e2e"])
            accountStartedOffline = boolVal(caps["startedOffline"])
            refreshOfflineBanner()
            if let addrs = caps["sendAddresses"] as? [[String: Any]] {
                sendAddresses = addrs.compactMap { a in
                    guard let id = a["id"] as? String, let email = a["email"] as? String else { return nil }
                    return SendAddress(id: id, email: email, name: a["name"] as? String ?? "")
                }
            }
            if let st = caps["storage"] as? [String: Any] {
                var next = filterStatus
                next.storageUsed = uint64Val(st["used"])
                next.storageMax = uint64Val(st["max"])
                next.storageMail = uint64Val(st["mail"])
                next.storageDrive = uint64Val(st["drive"])
                next.storageDriveMax = uint64Val(st["driveMax"])
                next.storageSplit = boolVal(st["split"])
                next.storageUpdated = timeVal(st["updated"])
                filterStatus = next
            }
        }
        if let result = try? Bridge.call("folders.list", params: ["account": acc]) as? [[String: Any]] {
            folders = result.compactMap { row in
                guard let id = row["id"] as? String, let name = row["name"] as? String else { return nil }
                let custom = boolVal(row["custom"])
                // folder==false means Proton label (tag); missing/true = exclusive folder.
                let isFolder = boolVal(row["folder"], default: true)
                return FolderRow(
                    id: id, name: name, custom: custom,
                    isLabel: custom && !isFolder,
                    color: row["color"] as? String ?? ""
                )
            }
            if selectedFolder.isEmpty || folders.first(where: { $0.id == selectedFolder }) == nil {
                selectedFolder = folders.first?.id ?? ""
            }
        }
        if let counts = try? Bridge.call("mail.unreadCounts", params: ["account": acc]) as? [String: Int] {
            unreadTotal = counts.values.reduce(0, +)
            NSApp.dockTile.badgeLabel = unreadTotal > 0 ? "\(unreadTotal)" : nil
            MenuBarController.shared.rebuildMenu()
        }
        loadHiddenFolders(for: acc)
        reloadMessages()
    }

    private func loadHiddenFolders(for acc: String) {
        guard let cfg = try? Bridge.call("config.get") as? [String: Any],
              let hidden = cfg["hidden_folders"] as? [String: [String]] else {
            hiddenFolderIDs = []
            return
        }
        hiddenFolderIDs = Set(hidden[acc] ?? [])
    }

    func toggleFolderHidden(_ folderID: String) {
        guard let acc = selectedAccount, acc != "unified" else { return }
        if hiddenFolderIDs.contains(folderID) {
            hiddenFolderIDs.remove(folderID)
        } else {
            hiddenFolderIDs.insert(folderID)
        }
        guard var cfg = try? Bridge.call("config.get") as? [String: Any] else { return }
        var map = cfg["hidden_folders"] as? [String: [String]] ?? [:]
        map[acc] = Array(hiddenFolderIDs)
        cfg["hidden_folders"] = map
        _ = try? Bridge.call("config.save", params: cfg)
    }

    func reloadMessages() {
        guard let acc = selectedAccount else { return }
        if !searchQuery.trimmingCharacters(in: .whitespaces).isEmpty {
            runSearch()
            return
        }
        let params: [String: Any] = [
            "account": acc, "folder": selectedFolder, "page": 0, "pageSize": 50,
        ]
        guard let result = try? Bridge.call("mail.list", params: params) as? [[String: Any]] else {
            messages = []
            return
        }
        applyMessageList(result.map(parseMessageRow))
        syncOnlineIfNeeded()
    }

    func runSearch() {
        guard let acc = selectedAccount else { return }
        let q = searchQuery.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else {
            reloadMessages()
            return
        }
        guard let result = try? Bridge.call("mail.search", params: [
            "account": acc, "folder": selectedFolder, "query": q, "max": 50,
        ]) as? [[String: Any]] else {
            messages = []
            return
        }
        applyMessageList(result.map(parseMessageRow))
    }

    private func applyMessageList(_ rows: [MessageRow]) {
        if threadsEnabled {
            messages = groupThreads(rows)
        } else {
            messages = rows
        }
    }

    private func groupThreads(_ rows: [MessageRow]) -> [MessageRow] {
        var seen = Set<String>()
        var counts: [String: Int] = [:]
        for m in rows {
            let key = m.conversationId.isEmpty ? m.id : m.conversationId
            counts[key, default: 0] += 1
        }
        var out: [MessageRow] = []
        out.reserveCapacity(rows.count)
        for m in rows {
            let key = m.conversationId.isEmpty ? m.id : m.conversationId
            if seen.contains(key) { continue }
            seen.insert(key)
            var row = m
            row.threadCount = counts[key] ?? 1
            out.append(row)
        }
        return out
    }

    private func parseMessageRow(_ row: [String: Any]) -> MessageRow {
        MessageRow(
            id: row["id"] as? String ?? "",
            subject: row["subject"] as? String ?? "(no subject)",
            sender: row["sender"] as? String ?? "",
            senderEmail: row["senderEmail"] as? String ?? "",
            time: (row["time"] as? Int64) ?? Int64(row["time"] as? Int ?? 0),
            unread: boolVal(row["unread"]),
            conversationId: row["conversationId"] as? String ?? (row["id"] as? String ?? ""),
            hasAttachments: boolVal(row["hasAttachments"]),
            labels: row["labels"] as? [String] ?? [],
            isSpam: boolVal(row["isSpam"]),
            spamCategory: row["spamCategory"] as? String ?? ""
        )
    }

    /// Custom Proton folders (exclusive), excluding labels.
    var mailboxFolders: [FolderRow] {
        folders.filter { !$0.isLabel }
    }

    /// Custom Proton labels (tags) synced from the account.
    var tagLabels: [FolderRow] {
        folders.filter { $0.isLabel }
    }

    func open(_ id: String) {
        guard let acc = selectedAccount else { return }
        selectedMessageID = id
        if !selectedMessageIDs.contains(id) {
            selectedMessageIDs = [id]
        }
        guard let result = try? Bridge.call("mail.getFull", params: ["account": acc, "id": id]) as? [String: Any] else {
            status = String(localized: "Failed to open message")
            return
        }
        openMessage = parseDetail(result, fallbackID: id)
        messageSummary = ""
        messageSummaryBusy = false
        applySummaryLayoutFixtureIfNeeded()
        loadSpamDecision(for: id, account: acc, evaluateIfNeeded: true)
        loadInvites(for: id)
        loadThread(for: openMessage?.conversationId ?? id)
        refreshSenderKeyNotice()
        if isDraftsFolder {
            // Drafts open in the composer (Linux openDraft).
            openDraft(id)
            return
        }
        reloadMessages()
    }

    private func parseDetail(_ result: [String: Any], fallbackID: String) -> MessageDetail {
        MessageDetail(
            id: result["id"] as? String ?? fallbackID,
            subject: result["subject"] as? String ?? "",
            sender: result["sender"] as? String ?? "",
            senderEmail: result["senderEmail"] as? String ?? "",
            to: result["to"] as? String ?? "",
            html: result["html"] as? String ?? "",
            text: result["text"] as? String ?? "",
            bodyHTML: result["bodyHTML"] as? String ?? "",
            hasRemoteContent: boolVal(result["hasRemoteContent"]),
            remoteAllowed: boolVal(result["remoteAllowed"]),
            unsubscribe: result["unsubscribe"] as? [String: Any],
            conversationId: result["conversationId"] as? String ?? fallbackID,
            attachments: result["attachments"] as? [[String: Any]] ?? [],
            labels: result["labels"] as? [String] ?? [],
            time: (result["time"] as? Int64) ?? Int64(result["time"] as? Int ?? 0)
        )
    }

    var isDraftsFolder: Bool {
        selectedFolder == labels.drafts
    }

    func loadThread(for conversationId: String) {
        guard threadsEnabled, let acc = selectedAccount, !conversationId.isEmpty else {
            threadMessages = []
            expandedThreadIDs = []
            return
        }
        guard let result = try? Bridge.call("mail.thread", params: [
            "account": acc, "conversationId": conversationId,
        ]) as? [[String: Any]] else {
            threadMessages = []
            expandedThreadIDs = []
            return
        }
        threadMessages = result.map(parseMessageRow)
        // Expand the open message and any unread ones (Linux openThread).
        var expand = Set<String>()
        if let openID = openMessage?.id {
            expand.insert(openID)
        }
        for tm in threadMessages where tm.unread {
            expand.insert(tm.id)
        }
        if expand.isEmpty, let last = threadMessages.last {
            expand.insert(last.id)
        }
        expandedThreadIDs = expand
    }

    func toggleThreadCard(_ id: String) {
        if expandedThreadIDs.contains(id) {
            expandedThreadIDs.remove(id)
        } else {
            expandedThreadIDs.insert(id)
            if openMessage?.id != id {
                open(id)
            }
        }
    }

    func refreshOfflineBanner() {
        offlineBanner = protonAPIOffline || accountStartedOffline
    }

    func syncOnlineIfNeeded() {
        guard accountStartedOffline, let acc = selectedAccount else { return }
        guard let caps = try? Bridge.call("mail.caps", params: ["account": acc]) as? [String: Any] else { return }
        if !boolVal(caps["startedOffline"]) {
            accountStartedOffline = false
            refreshOfflineBanner()
            loadFoldersAndMail()
        }
    }

    func offlineCacheStats() -> (available: Bool, messages: Int, bodies: Int) {
        let acc = selectedAccount ?? ""
        guard let r = try? Bridge.call("cache.stats", params: ["account": acc]) as? [String: Any] else {
            return (false, 0, 0)
        }
        return (boolVal(r["available"]), intVal(r["messages"]), intVal(r["bodies"]))
    }

    func clearOfflineCache() -> Bool {
        let acc = selectedAccount ?? ""
        do {
            _ = try Bridge.call("cache.clear", params: ["account": acc])
            status = String(localized: "Offline cache cleared")
            return true
        } catch {
            status = error.localizedDescription
            return false
        }
    }

    func refreshSenderKeyNotice() {
        guard let msg = openMessage else {
            senderKeyNotice = nil
            return
        }
        let sender = msg.senderEmail.isEmpty ? msg.sender : msg.senderEmail
        guard !sender.isEmpty,
              let r = try? Bridge.call("pgp.senderKeyNotice", params: ["sender": sender]) as? [String: Any],
              boolVal(r["show"]) else {
            senderKeyNotice = nil
            return
        }
        senderKeyNotice = SenderKeyNotice(
            email: r["email"] as? String ?? sender,
            text: r["text"] as? String ?? "",
            warn: boolVal(r["warn"]),
            pending: boolVal(r["pending"])
        )
    }

    func acceptSenderPendingKey() {
        guard let n = senderKeyNotice else { return }
        _ = try? Bridge.call("pgp.acceptPending", params: ["email": n.email])
        refreshSenderKeyNotice()
        status = String(localized: "Key accepted")
    }

    func rejectSenderPendingKey() {
        guard let n = senderKeyNotice else { return }
        _ = try? Bridge.call("pgp.rejectPending", params: ["email": n.email])
        refreshSenderKeyNotice()
        status = String(localized: "Key rejected")
    }

    func isPGPKeyAttachment(_ att: [String: Any]) -> Bool {
        let name = (att["name"] as? String ?? "").lowercased()
        let mime = (att["mimeType"] as? String ?? "").lowercased()
        if mime == "application/pgp-keys" { return true }
        if name.hasSuffix(".asc") || name.hasSuffix(".pub") { return true }
        if name.hasSuffix(".key") && name.contains("pgp") { return true }
        if name.hasPrefix("publickey") { return true }
        return false
    }

    func importPGPAttachment(_ att: [String: Any]) {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        let attID = att["id"] as? String ?? att["name"] as? String ?? ""
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let r = try Bridge.call("pgp.importAttachment", params: [
                    "account": acc, "messageId": msg.id, "attachmentId": attID,
                ]) as? [String: Any]
                let emails = (r?["emails"] as? [String]) ?? []
                DispatchQueue.main.async {
                    self.status = emails.isEmpty
                        ? String(localized: "Key imported")
                        : String(format: String(localized: "Key imported for %@"), emails.joined(separator: ", "))
                    self.refreshSenderKeyNotice()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func bulkToggleStar() {
        let ids = Array(selectedMessageIDs)
        guard !ids.isEmpty, let acc = selectedAccount, acc != "unified" else {
            toggleStar()
            return
        }
        let anyUnstarred = ids.contains { id in
            let row = messages.first(where: { $0.id == id })
            return !(row?.labels.contains(labels.starred) ?? false)
        }
        _ = try? Bridge.call("mail.setLabel", params: [
            "account": acc, "label": labels.starred, "on": anyUnstarred, "ids": ids,
        ])
        status = anyUnstarred
            ? String(localized: "Starred")
            : String(localized: "Star removed")
        reloadMessages()
    }

    func allowRemoteContent() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        let sender = msg.senderEmail.isEmpty ? msg.sender : msg.senderEmail
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("mail.allowRemote", params: [
                    "account": acc, "id": msg.id, "sender": sender,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    if let result {
                        self.openMessage = self.parseDetail(result, fallbackID: msg.id)
                    }
                    self.status = String(localized: "Remote content allowed for this sender")
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func unsubscribeOpen() {
        guard openMessage?.unsubscribe != nil else { return }
        showUnsubscribeConfirm = true
    }

    func confirmUnsubscribe() {
        showUnsubscribeConfirm = false
        guard let msg = openMessage, let u = msg.unsubscribe else { return }
        let oneClick = boolVal(u["oneClick"])
        let http = u["http"] as? String ?? ""
        let mailto = u["mailto"] as? String ?? ""
        if oneClick, !http.isEmpty {
            DispatchQueue.global().async {
                do {
                    _ = try Bridge.call("mail.unsubscribe", params: [
                        "http": http, "oneClick": true,
                    ])
                    DispatchQueue.main.async {
                        self.status = String(localized: "Unsubscribe request sent")
                    }
                } catch {
                    DispatchQueue.main.async { self.status = error.localizedDescription }
                }
            }
            return
        }
        if !mailto.isEmpty, let url = URL(string: mailto) {
            NSWorkspace.shared.open(url)
            return
        }
        if !http.isEmpty, let url = URL(string: http) {
            NSWorkspace.shared.open(url)
        }
    }

    var unsubscribeConfirmBody: String {
        guard let u = openMessage?.unsubscribe else { return "" }
        if boolVal(u["oneClick"]) {
            return String(localized: "Klient sends the sender an unsubscribe request (one click, as defined by RFC 8058). The sender will see your IP address, just as when you click a link.")
        }
        if let m = u["mailto"] as? String, !m.isEmpty {
            return String(localized: "A prefilled unsubscribe request opens for you to send.")
        }
        return String(localized: "The sender's unsubscribe page opens in the browser.")
    }

    func targetMessageIDs() -> [String] {
        if !selectedMessageIDs.isEmpty {
            return Array(selectedMessageIDs)
        }
        if let id = selectedMessageID ?? openMessage?.id {
            return [id]
        }
        return []
    }

    func bulkMove(to folder: String) {
        guard let acc = selectedAccount else { return }
        let ids = targetMessageIDs()
        guard !ids.isEmpty else { return }
        // Proton labels are tags: add via setLabel, do not Move (Linux parity).
        if folder == labels.starred || folders.first(where: { $0.id == folder })?.isLabel == true {
            _ = try? Bridge.call("mail.setLabel", params: [
                "account": acc, "label": folder, "on": true, "ids": ids,
            ])
            let name = folders.first(where: { $0.id == folder })?.name ?? folder
            status = String(format: String(localized: "Added: %@"), name)
            reloadMessages()
            if let openID = openMessage?.id, ids.contains(openID) {
                open(openID)
            }
            return
        }
        _ = try? Bridge.call("mail.move", params: ["account": acc, "folder": folder, "ids": ids])
        openMessage = nil
        messageSummary = ""
        messageSummaryBusy = false
        spamDecision = nil
        spamDecisionBusy = false
        selectedMessageIDs = []
        selectedMessageID = nil
        reloadMessages()
    }

    /// Adds or removes a Proton label (tag) on the current selection / open message.
    /// Adds unless every target already has the label (Linux parity).
    func toggleLabel(_ labelID: String) {
        guard selectedAccount != nil, selectedAccount != "unified" else { return }
        let ids = targetMessageIDs()
        guard !ids.isEmpty else { return }
        let missing = ids.contains { id in
            if let om = openMessage, om.id == id {
                return !om.labels.contains(labelID)
            }
            if let m = messages.first(where: { $0.id == id }) {
                return !m.labels.contains(labelID)
            }
            return true
        }
        setLabel(labelID, on: missing, ids: ids)
    }

    func setLabel(_ labelID: String, on: Bool, ids: [String]? = nil) {
        guard let acc = selectedAccount, acc != "unified" else { return }
        let targetIDs = ids ?? targetMessageIDs()
        guard !targetIDs.isEmpty else { return }
        _ = try? Bridge.call("mail.setLabel", params: [
            "account": acc, "label": labelID, "on": on, "ids": targetIDs,
        ])
        let name = folders.first(where: { $0.id == labelID })?.name ?? labelID
        status = on
            ? String(format: String(localized: "Label %@ added"), name)
            : String(format: String(localized: "Label %@ removed"), name)
        if let openID = openMessage?.id, targetIDs.contains(openID) {
            open(openID)
        }
        reloadMessages()
    }

    func messageHasLabel(_ labelID: String) -> Bool {
        if let msg = openMessage {
            return msg.labels.contains(labelID)
        }
        if selectedMessageIDs.count == 1, let id = selectedMessageIDs.first,
           let row = messages.first(where: { $0.id == id }) {
            return row.labels.contains(labelID)
        }
        return false
    }

    func bulkMarkRead(_ read: Bool) {
        guard let acc = selectedAccount else { return }
        let ids = targetMessageIDs()
        guard !ids.isEmpty else { return }
        let method = read ? "mail.markRead" : "mail.markUnread"
        _ = try? Bridge.call(method, params: ["account": acc, "ids": ids])
        reloadMessages()
    }

    func createFolder(name: String, asLabel: Bool = false) {
        guard let acc = selectedAccount, acc != "unified" else {
            status = String(localized: "Select a single account")
            return
        }
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty else { return }
        let label = asLabel || folderPromptAsLabel
        folderPromptAsLabel = false
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("folders.create", params: [
                    "account": acc, "name": n, "label": label,
                ])
                DispatchQueue.main.async {
                    self.status = label
                        ? String(format: String(localized: "Label %@ created"), n)
                        : String(format: String(localized: "Folder %@ created"), n)
                    self.loadFoldersAndMail()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func renameFolder(id: String, name: String) {
        guard let acc = selectedAccount, acc != "unified" else { return }
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty else { return }
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("folders.rename", params: [
                    "account": acc, "id": id, "name": n,
                ])
                DispatchQueue.main.async {
                    self.status = String(format: String(localized: "Renamed to %@"), n)
                    self.loadFoldersAndMail()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func deleteFolder(id: String, name: String) {
        guard let acc = selectedAccount, acc != "unified" else { return }
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("folders.delete", params: ["account": acc, "id": id])
                DispatchQueue.main.async {
                    self.status = String(format: String(localized: "%@ deleted"), name)
                    if self.selectedFolder == id {
                        self.selectedFolder = self.labels.inbox
                    }
                    self.loadFoldersAndMail()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func openDraft(_ id: String) {
        guard let acc = selectedAccount else { return }
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("mail.openDraft", params: [
                    "account": acc, "id": id,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    guard let result else { return }
                    let detail = MessageDetail(
                        id: result["id"] as? String ?? id,
                        subject: result["subject"] as? String ?? "",
                        sender: "",
                        senderEmail: "",
                        to: result["to"] as? String ?? "",
                        html: result["html"] as? String ?? "",
                        text: result["body"] as? String ?? "",
                        bodyHTML: "",
                        hasRemoteContent: false,
                        remoteAllowed: false,
                        unsubscribe: nil,
                        conversationId: id,
                        attachments: [],
                        labels: [self.labels.drafts],
                        time: 0,
                        addressId: result["fromAddressId"] as? String ?? ""
                    )
                    self.composeReply = detail
                    self.composeAction = result["action"] as? Int ?? 0
                    self.composePrefillTo = nil
                    DraftOpenStore.shared.apply(result)
                    self.showCompose = true
                    self.status = String(localized: "Draft opened")
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func deleteDraft(_ id: String? = nil) {
        guard let acc = selectedAccount else { return }
        let draftID = id ?? openMessage?.id ?? selectedMessageID
        guard let draftID else { return }
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("mail.deleteDraft", params: ["account": acc, "id": draftID])
                DispatchQueue.main.async {
                    self.openMessage = nil
                    self.selectedMessageID = nil
                    self.selectedMessageIDs.remove(draftID)
                    self.status = String(localized: "Draft deleted")
                    self.reloadMessages()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func requestPrint() {
        pendingPrint = true
    }

    func loadInvites(for msgID: String) {
        guard let acc = selectedAccount else { invites = []; return }
        invites = (try? Bridge.call("invite.parse", params: [
            "account": acc, "messageId": msgID,
        ]) as? [[String: Any]]) ?? []
    }

    func replyInvite(status: String, invite: [String: Any]) {
        guard let acc = selectedAccount, let msg = openMessage,
              let ics = invite["ics"] as? String else { return }
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("invite.reply", params: [
                    "account": acc, "messageId": msg.id, "ics": ics, "status": status,
                ])
                DispatchQueue.main.async { self.status = "Invitation reply sent" }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func openInviteInCalendar(_ invite: [String: Any]) {
        guard let ics = invite["ics"] as? String else { return }
        if let result = try? Bridge.call("invite.saveICS", params: [
            "ics": ics, "summary": invite["summary"] as? String ?? "event",
        ]) as? [String: Any], let path = result["path"] as? String {
            NSWorkspace.shared.open(URL(fileURLWithPath: path))
        }
    }

    func moveSelected(to folder: String) {
        bulkMove(to: folder)
    }

    func markUnread() {
        bulkMarkRead(false)
    }

    func markRead() {
        bulkMarkRead(true)
    }

    func toggleStar() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        let starred = msg.labels.contains(labels.starred)
        _ = try? Bridge.call("mail.setLabel", params: [
            "account": acc, "label": labels.starred, "on": !starred, "ids": [msg.id],
        ])
        open(msg.id)
    }

    func markSpam(_ isSpam: Bool) {
        guard let msg = openMessage else { return }
        _ = try? Bridge.call("spam.feedback", params: [
            "messageId": msg.id, "from": msg.sender, "isSpam": isSpam,
        ])
        if !isSpam {
            spamDecision = nil
        }
        moveSelected(to: isSpam ? labels.spam : labels.inbox)
        status = isSpam
            ? String(localized: "Marked as spam")
            : String(localized: "Marked as not spam")
    }

    /// Fetch spam-filter verdict for the reader banner (Linux `updateSpamUI`).
    /// When no stored decision exists and `evaluateIfNeeded`, classifies on open
    /// (ham / spam / unknown with probability) like a manual inbox scan.
    func loadSpamDecision(for id: String, account: String? = nil, evaluateIfNeeded: Bool = false) {
        guard !id.isEmpty else {
            spamDecision = nil
            spamDecisionBusy = false
            return
        }
        let acc = account ?? selectedAccount ?? ""
        if applySpamDecisionResult(
            try? Bridge.call("spam.decision", params: ["messageId": id, "account": acc]) as? [String: Any]
        ) {
            spamDecisionBusy = false
            return
        }
        guard evaluateIfNeeded, !acc.isEmpty else {
            spamDecision = nil
            spamDecisionBusy = false
            return
        }
        spamDecisionBusy = true
        DispatchQueue.global(qos: .userInitiated).async {
            let result = try? Bridge.call("spam.decision", params: [
                "messageId": id,
                "account": acc,
                "evaluate": true,
            ]) as? [String: Any]
            DispatchQueue.main.async {
                guard self.selectedMessageID == id else { return }
                _ = self.applySpamDecisionResult(result)
                self.spamDecisionBusy = false
                // Ham/spam badge may have appeared for the list row.
                if self.boolVal(result?["evaluated"]) {
                    self.reloadMessages()
                    self.refreshFilterStatus()
                }
            }
        }
    }

    /// Returns true when a decision was applied (including source `none` / unknown).
    @discardableResult
    private func applySpamDecisionResult(_ result: [String: Any]?) -> Bool {
        guard let result, boolVal(result["known"]),
              let decision = result["decision"] as? [String: Any]
        else {
            spamDecision = nil
            return false
        }
        let source = decision["source"] as? String ?? ""
        let verdict = decision["verdict"] as? [String: Any] ?? [:]
        let prob: Double
        if let d = verdict["spam_probability"] as? Double {
            prob = d
        } else if let n = verdict["spam_probability"] as? NSNumber {
            prob = n.doubleValue
        } else {
            prob = 0
        }
        spamDecision = SpamDecision(
            spam: boolVal(decision["spam"]),
            source: source.isEmpty ? "none" : source,
            category: verdict["category"] as? String ?? "",
            spamProbability: prob,
            reason: verdict["reason"] as? String ?? "",
            hits: decision["hits"] as? [String] ?? []
        )
        return true
    }

    func applyRulesToInbox() {
        status = "Applying rules…"
        let acc = selectedAccount == "unified" ? "" : (selectedAccount ?? "")
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("rules.applyInbox", params: [
                    "account": acc,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    let n = result?["applied"] as? Int ?? 0
                    self.status = "Rules applied to \(n) messages"
                    self.reloadMessages()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    /// Queries GitHub for the latest release. When `silent`, only opens the sheet if a newer version exists.
    func checkForUpdates(silent: Bool = false) {
        if !silent {
            status = String(localized: "Checking for updates…")
            updateInfo = nil
            showUpdate = true
        }
        let params = langParams()
        DispatchQueue.global().async {
            do {
                let info = try Bridge.call("update.check", params: params) as? [String: Any]
                DispatchQueue.main.async {
                    self.updateInfo = info
                    let newer = self.jsonBool(info?["newer"])
                    if newer {
                        self.showUpdate = true
                        let ver = info?["version"] as? String ?? ""
                        self.status = String(format: String(localized: "Update available: %@"), ver)
                        if silent {
                            self.notifyUpdateAvailable(version: ver)
                        }
                    } else if !silent {
                        self.showUpdate = true
                        self.status = String(localized: "Klient is up to date")
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    if !silent {
                        self.showUpdate = true
                        self.status = error.localizedDescription
                    }
                }
            }
        }
    }

    private func notifyUpdateAvailable(version: String) {
        let content = UNMutableNotificationContent()
        content.title = String(localized: "A New Version of Klient Is Ready")
        content.body = String(format: String(localized: "Version %@ is available on GitHub."), version)
        content.sound = .default
        let req = UNNotificationRequest(identifier: "klient-update", content: content, trigger: nil)
        UNUserNotificationCenter.current().add(req)
    }

    func installOllama() {
        status = "Installing Ollama (this may take a while)…"
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("ollama.install") as? [String: Any]
                _ = try Bridge.call("ollama.start")
                DispatchQueue.main.async {
                    self.status = "Ollama \(r?["version"] as? String ?? "") ready"
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func emptyFolder(_ folderID: String) {
        guard let acc = selectedAccount, acc != "unified" else { return }
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("folders.empty", params: [
                    "account": acc, "folder": folderID,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    self.status = "Deleted \(r?["deleted"] as? Int ?? 0) messages"
                    self.reloadMessages()
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func snoozeHours(_ hours: Int) {
        snoozeUntil(Date().addingTimeInterval(Double(hours) * 3600))
    }

    func snoozeUntil(_ date: Date) {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        if date.timeIntervalSinceNow < 60 {
            status = String(localized: "The chosen time has already passed")
            return
        }
        let until = ISO8601DateFormatter().string(from: date)
        let ids = [msg.conversationId.isEmpty ? msg.id : msg.conversationId]
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("mail.snooze", params: [
                    "account": acc, "ids": ids, "until": until,
                ])
                DispatchQueue.main.async {
                    self.status = String(format: String(localized: "Snoozed – returns %@"),
                                         KlientTheme.relativeDate(Int64(date.timeIntervalSince1970)))
                    self.reloadMessages()
                    self.openMessage = nil
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func unsnooze() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        let ids = [msg.conversationId.isEmpty ? msg.id : msg.conversationId]
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("mail.unsnooze", params: ["account": acc, "ids": ids])
                DispatchQueue.main.async {
                    self.status = String(localized: "Returned to the inbox")
                    self.reloadMessages()
                    self.openMessage = nil
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func cancelScheduled() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("mail.cancelScheduled", params: ["account": acc, "id": msg.id])
                DispatchQueue.main.async {
                    self.status = String(localized: "Sending cancelled; the message is in Drafts")
                    self.reloadMessages()
                    self.openDraft(msg.id)
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    var isScheduledFolder: Bool { selectedFolder == labels.scheduled }
    var isSnoozedFolder: Bool { selectedFolder == labels.snoozed }

    var openMessageIsScheduled: Bool {
        guard let msg = openMessage else { return false }
        return msg.labels.contains(labels.scheduled) || isScheduledFolder
    }

    var openMessageIsSnoozed: Bool {
        guard let msg = openMessage else { return false }
        return msg.labels.contains(labels.snoozed) || isSnoozedFolder
    }

    // MARK: - Undo send

    func beginUndoSend(params: [String: Any], delay: Int, restore: [String: Any]) {
        cancelUndoSend(fire: false)
        undoSendParams = params
        undoSendRestore = restore
        undoSendSecondsLeft = delay
        undoSendActive = true
        status = String(format: String(localized: "The message will be sent in %lld s"), delay)
        undoSendTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in
                guard let self else { return }
                self.undoSendSecondsLeft -= 1
                if self.undoSendSecondsLeft > 0 {
                    self.status = String(format: String(localized: "The message will be sent in %lld s"),
                                         self.undoSendSecondsLeft)
                    return
                }
                self.finishUndoSend(send: true)
            }
        }
    }

    func undoPendingSend() {
        finishUndoSend(send: false)
    }

    private func finishUndoSend(send: Bool) {
        undoSendTimer?.invalidate()
        undoSendTimer = nil
        undoSendActive = false
        undoSendSecondsLeft = 0
        let params = undoSendParams
        let restore = undoSendRestore
        undoSendParams = nil
        undoSendRestore = nil
        if send, let params {
            DispatchQueue.global().async {
                do {
                    let result = try Bridge.call("mail.send", params: params) as? [String: Any]
                    DispatchQueue.main.async {
                        if self.handleSendResult(result) { return }
                        self.status = String(localized: "Sent")
                        self.reloadMessages()
                    }
                } catch {
                    DispatchQueue.main.async { self.status = error.localizedDescription }
                }
            }
            return
        }
        // Restore composer from the safety draft / in-memory fields.
        if let restore {
            DraftOpenStore.shared.apply(restore)
            composeReply = nil
            composeAction = restore["action"] as? Int ?? 0
            showCompose = true
            status = String(localized: "Sending cancelled")
        } else {
            status = String(localized: "Sending cancelled")
        }
    }

    private func cancelUndoSend(fire: Bool) {
        undoSendTimer?.invalidate()
        undoSendTimer = nil
        if fire { finishUndoSend(send: true) }
        else {
            undoSendActive = false
            undoSendParams = nil
            undoSendRestore = nil
            undoSendSecondsLeft = 0
        }
    }

    func summarizeOpen() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        let msgID = msg.id
        messageSummaryBusy = true
        messageSummary = ""
        status = String(localized: "Summarizing…")
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("ai.summarize", params: [
                    "account": acc,
                    "messageId": msg.id,
                    "conversationId": msg.conversationId,
                    "from": msg.sender,
                    "subject": msg.subject,
                    "body": msg.text,
                    "html": msg.html,
                ]) as? [String: Any]
                let summary = result?["summary"] as? String ?? String(localized: "No summary")
                DispatchQueue.main.async {
                    guard self.openMessage?.id == msgID else { return }
                    self.messageSummaryBusy = false
                    self.messageSummary = summary
                    self.status = String(localized: "Summary ready")
                    self.refreshFilterStatus()
                }
            } catch {
                DispatchQueue.main.async {
                    guard self.openMessage?.id == msgID else { return }
                    self.messageSummaryBusy = false
                    self.messageSummary = ""
                    self.status = error.localizedDescription
                }
            }
        }
    }

    func dismissMessageSummary() {
        messageSummary = ""
        messageSummaryBusy = false
    }

    /// Layout-debug only (`KLIENT_SUMMARY_FIXTURE=short|long|busy`). Injects a
    /// synthetic summary when opening mail so NavigationSplitView height can be
    /// verified without a live AI provider.
    private func applySummaryLayoutFixtureIfNeeded() {
        switch ProcessInfo.processInfo.environment["KLIENT_SUMMARY_FIXTURE"] {
        case "short":
            messageSummary = "Krátké shrnutí: schůzka zítra v 10:00."
        case "long":
            messageSummary = String(
                repeating: "Dlouhé AI shrnutí e-mailu s více větami a kontextem. ",
                count: 60
            )
        case "busy":
            messageSummaryBusy = true
        default:
            break
        }
    }

    func askAboutOpenMail() {
        if let msg = openMessage {
            let subj = msg.subject.isEmpty ? String(localized: "(no subject)") : msg.subject
            askQuestion = String(format: String(localized: "What is this message about: %@?"), subj)
        }
        askAnswer = ""
        showAskAI = true
    }

    func draftReplyWithAI() {
        guard openMessage != nil else { return }
        composeAutoDraftAI = true
        reply(all: false)
    }

    func scanSpam() {
        guard let acc = selectedAccount else { return }
        status = String(localized: "Checking the inbox…")
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("spam.scanInbox", params: ["account": acc, "max": 40]) as? [String: Any]
                DispatchQueue.main.async {
                    let moved = JSONVal.int(result?["moved"])
                    self.status = String(
                        format: String(localized: "Done, moved to spam: %d"),
                        moved
                    )
                    self.reloadMessages()
                }
            } catch {
                DispatchQueue.main.async {
                    self.status = String(localized: "The check failed: ") + error.localizedDescription
                }
            }
        }
    }

    func runAskAI() {
        guard let acc = selectedAccount else { return }
        askAnswer = "Thinking…"
        let q = askQuestion
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("ai.askMail", params: [
                    "account": acc, "question": q,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    self.askAnswer = result?["answer"] as? String ?? ""
                    self.refreshFilterStatus()
                }
            } catch {
                DispatchQueue.main.async { self.askAnswer = error.localizedDescription }
            }
        }
    }

    func runDigest() {
        guard let acc = selectedAccount else { return }
        status = String(localized: "Building morning overview…")
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("ai.digest", params: ["account": acc]) as? [String: Any]
                let text = result?["digest"] as? String ?? ""
                DispatchQueue.main.async {
                    self.status = text.isEmpty ? String(localized: "Done") : text
                    self.refreshFilterStatus()
                    if !text.isEmpty {
                        let content = UNMutableNotificationContent()
                        content.title = String(localized: "Morning mail overview")
                        content.body = text
                        content.sound = .default
                        UNUserNotificationCenter.current().add(
                            UNNotificationRequest(identifier: "klient-digest", content: content, trigger: nil)
                        )
                    }
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func exportEML() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        guard let result = try? Bridge.call("mail.exportEML", params: ["account": acc, "id": msg.id]) as? [String: Any],
              let b64 = result["data"] as? String,
              let data = Data(base64Encoded: b64) else { return }
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "message.eml"
        if panel.runModal() == .OK, let url = panel.url {
            try? data.write(to: url)
            status = "Saved \(url.lastPathComponent)"
        }
    }

    func saveAttachment(_ att: [String: Any]) {
        guard let data = fetchAttachmentData(att) else { return }
        let panel = NSSavePanel()
        panel.nameFieldStringValue = (att["name"] as? String) ?? "attachment"
        if panel.runModal() == .OK, let url = panel.url {
            try? data.write(to: url)
            status = String(localized: "Saved attachment")
        }
    }

    func openAttachment(_ att: [String: Any]) {
        guard let data = fetchAttachmentData(att) else { return }
        let name = (att["name"] as? String) ?? "attachment"
        let tmp = FileManager.default.temporaryDirectory.appendingPathComponent(name)
        do {
            try data.write(to: tmp)
            NSWorkspace.shared.open(tmp)
            status = String(format: String(localized: "Opened %@"), name)
        } catch {
            status = error.localizedDescription
        }
    }

    private func fetchAttachmentData(_ att: [String: Any]) -> Data? {
        guard let acc = selectedAccount, let msg = openMessage,
              let attID = att["id"] as? String else { return nil }
        guard let result = try? Bridge.call("mail.attachment", params: [
            "account": acc, "messageId": msg.id, "attachmentId": attID,
        ]) as? [String: Any],
              let b64 = result["data"] as? String,
              let data = Data(base64Encoded: b64) else { return nil }
        return data
    }

    func suggestLabelForOpen() {
        guard let acc = selectedAccount, let msg = openMessage else { return }
        status = String(localized: "The AI is choosing a label…")
        DispatchQueue.global().async {
            do {
                let r = try Bridge.call("ai.suggestLabel", params: [
                    "account": acc, "messageId": msg.id,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    if let err = r?["error"] as? String, !err.isEmpty {
                        self.status = err
                        return
                    }
                    let label = r?["label"] as? String ?? ""
                    let reason = r?["reason"] as? String ?? ""
                    if label.isEmpty {
                        self.status = String(localized: "The AI found no suitable label")
                            + (reason.isEmpty ? "" : ": " + reason)
                    } else {
                        self.status = String(format: String(localized: "Suggested label: %@"), label)
                            + (reason.isEmpty ? "" : " — " + reason)
                        if let id = r?["labelId"] as? String, !id.isEmpty {
                            _ = try? Bridge.call("mail.setLabel", params: [
                                "account": acc, "label": id, "on": true, "ids": [msg.id],
                            ])
                            self.open(msg.id)
                        }
                    }
                }
            } catch {
                DispatchQueue.main.async { self.status = error.localizedDescription }
            }
        }
    }

    func bulkMarkSpam(_ isSpam: Bool) {
        let ids = Array(selectedMessageIDs)
        guard !ids.isEmpty else {
            markSpam(isSpam)
            return
        }
        for id in ids {
            if let row = messages.first(where: { $0.id == id }) {
                _ = try? Bridge.call("spam.feedback", params: [
                    "messageId": id, "from": row.sender, "isSpam": isSpam,
                ])
            }
        }
        moveSelected(to: isSpam ? labels.spam : labels.inbox)
        status = isSpam
            ? String(localized: "Marked as spam")
            : String(localized: "Marked as not spam")
    }

    func newMessage() {
        composeReply = nil
        composeAction = 0
        if let to = composePrefillTo {
            // Prefill handled in compose via pendingMailto-style; open blank and set via status
            _ = to
        }
        showCompose = true
    }

    func reply(all: Bool) {
        composeReply = openMessage
        composeAction = all ? 2 : 1
        showCompose = true
    }

    func forward() {
        composeReply = openMessage
        composeAction = 3
        showCompose = true
    }

    func logout() {
        guard let acc = selectedAccount else { return }
        _ = try? Bridge.call("account.logout", params: ["account": acc])
        openMessage = nil
        refreshAccounts()
        if accounts.isEmpty {
            showLogin = true
        } else {
            selectedAccount = accounts.first?.username
            loadFoldersAndMail()
        }
    }

    // MARK: - Login (unchanged HV flow)

    func loginStart() {
        loggingIn = true
        status = "Signing in…"
        let user = loginUsername
        let pass = loginPassword
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let result = try Bridge.call("proton.loginStart", params: [
                    "username": user, "password": pass,
                ]) as? [String: Any]
                DispatchQueue.main.async { self.handleLoginResult(result) }
            } catch {
                DispatchQueue.main.async {
                    self.loggingIn = false
                    self.status = error.localizedDescription
                }
            }
        }
    }

    func openHumanVerification() {
        guard let url = URL(string: hvURL), !hvURL.isEmpty else { return }
        NSWorkspace.shared.open(url)
        status = "Complete verification in the browser, then click Continue"
    }

    func continueAfterHV() {
        loggingIn = true
        status = "Signing in after verification…"
        let user = loginUsername
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let result = try Bridge.call("proton.loginContinueHV", params: [
                    "username": user,
                ]) as? [String: Any]
                DispatchQueue.main.async { self.handleLoginResult(result) }
            } catch {
                DispatchQueue.main.async {
                    self.loggingIn = false
                    self.status = error.localizedDescription
                }
            }
        }
    }

    func jsonBool(_ value: Any?) -> Bool {
        if let b = value as? Bool { return b }
        if let n = value as? NSNumber { return n.boolValue }
        return false
    }

    private func handleLoginResult(_ result: [String: Any]?) {
        loggingIn = false
        if jsonBool(result?["needsHV"]) {
            needsHV = true
            hvURL = result?["hvURL"] as? String ?? ""
            status = "Proton requires human verification"
            openHumanVerification()
            return
        }
        needsHV = false
        hvURL = ""
        needsTOTP = jsonBool(result?["needsTOTP"])
        needsMailboxPassword = jsonBool(result?["needsMailboxPassword"])
        if needsTOTP {
            status = "Enter TOTP code"
        } else if needsMailboxPassword {
            status = "Enter mailbox password"
        } else {
            loginFinish()
        }
    }

    func submitTOTP() {
        loggingIn = true
        status = "Checking code…"
        let user = loginUsername.trimmingCharacters(in: .whitespacesAndNewlines)
        let code = loginTOTP
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let result = try Bridge.call("proton.loginTOTP", params: [
                    "username": user, "code": code,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    self.loggingIn = false
                    let needBox = (result?["needsMailboxPassword"] as? Bool)
                        ?? (result?["needsMailboxPassword"] as? NSNumber)?.boolValue
                        ?? false
                    self.needsMailboxPassword = needBox
                    self.needsTOTP = false
                    if needBox {
                        self.status = "Enter mailbox password"
                    } else {
                        self.loginFinish()
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    self.loggingIn = false
                    self.status = error.localizedDescription
                }
            }
        }
    }

    func loginFinish() {
        loggingIn = true
        status = "Unlocking mailbox…"
        let user = loginUsername.trimmingCharacters(in: .whitespacesAndNewlines)
        let box = mailboxPassword
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let result = try Bridge.call("proton.loginFinish", params: [
                    "username": user, "mailboxPassword": box,
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    self.showLogin = false
                    self.loginPassword = ""
                    self.loginTOTP = ""
                    self.mailboxPassword = ""
                    self.needsHV = false
                    self.hvURL = ""
                    self.needsTOTP = false
                    self.needsMailboxPassword = false
                    self.loggingIn = false
                    self.refreshAccounts()
                    if let uname = result?["username"] as? String, !uname.isEmpty {
                        self.selectedAccount = uname
                    } else {
                        self.selectedAccount = self.accounts.first?.username
                    }
                    self.loadFoldersAndMail()
                    self.status = "Signed in"
                }
            } catch {
                DispatchQueue.main.async {
                    self.loggingIn = false
                    self.status = error.localizedDescription
                }
            }
        }
    }

    func keyringSmoke() {
        do {
            _ = try Bridge.call("keyring.smoke")
            status = "Keyring OK"
        } catch {
            status = "Keyring: \(error.localizedDescription)"
        }
    }

    private func handleEvent(_ json: String) {
        guard let data = json.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = obj["type"] as? String else { return }
        switch type {
        case "mailChanged", "sessionRecovered":
            if type == "sessionRecovered" {
                // Silent keyring re-auth succeeded — never leave LoginSheet up.
                showLogin = false
            }
            if let acc = obj["account"] as? String, acc == selectedAccount || selectedAccount == "unified" {
                reloadMessages()
            }
        case "sessionRecoverFailed":
            if let payload = obj["payload"] as? [String: Any],
               let msg = payload["message"] as? String, !msg.isEmpty {
                status = String(localized: "Connection problem: ") + msg
            }
        case "offlineStatus":
            if let payload = obj["payload"] as? [String: Any] {
                let offline = boolVal(payload["offline"])
                let source = payload["source"] as? String ?? ""
                if source == "proton" {
                    protonAPIOffline = offline
                } else if source == "started" {
                    if let acc = obj["account"] as? String, acc == selectedAccount {
                        accountStartedOffline = offline
                    }
                }
                refreshOfflineBanner()
            }
        case "storageChanged":
            if let acc = obj["account"] as? String, acc == selectedAccount,
               let payload = obj["payload"] as? [String: Any] {
                var next = filterStatus
                next.storageUsed = uint64Val(payload["used"])
                next.storageMax = uint64Val(payload["max"])
                next.storageMail = uint64Val(payload["mail"])
                next.storageDrive = uint64Val(payload["drive"])
                next.storageDriveMax = uint64Val(payload["driveMax"])
                next.storageSplit = boolVal(payload["split"])
                next.storageUpdated = timeVal(payload["updated"])
                filterStatus = next
            }
        case "eventsError":
            if let err = obj["payload"] as? String {
                status = String(localized: "Watching for new mail could not be started: ") + err
            }
        case "newMail":
            if var payload = obj["payload"] as? [String: Any] {
                if let acc = obj["account"] as? String {
                    payload["account"] = acc
                }
                notifyNewMail(payload)
                if let acc = obj["account"] as? String, acc == selectedAccount {
                    reloadMessages()
                }
            }
        case "spamMoved":
            status = String(localized: "Spam filter moved a message")
            reloadMessages()
        case "rulesApplied":
            if let payload = obj["payload"] as? [String: Any],
               let rules = payload["rules"] as? [String] {
                status = String(format: String(localized: "Rules: %@"), rules.joined(separator: ", "))
            }
            reloadMessages()
        case "usageChanged":
            refreshFilterStatus()
        case "blocklistsUpdated", "configChanged":
            refreshFilterStatus()
            if let cfg = try? Bridge.call("config.get") as? [String: Any] {
                let on = boolVal(cfg["threads"], default: true)
                if on != threadsEnabled {
                    threadsEnabled = on
                    reloadMessages()
                } else {
                    threadsEnabled = on
                }
                let lang = cfg["language"] as? String ?? ""
                if lang != KlientLocale.configCode {
                    KlientLocale.apply(lang)
                    refreshLocale()
                }
            }
        case "deauth":
            let payload = obj["payload"] as? [String: Any]
            if let msg = payload?["message"] as? String, !msg.isEmpty {
                status = msg
            } else {
                status = String(localized: "Session expired – please sign in again")
            }
            let dead = obj["account"] as? String
            if let dead, dead == selectedAccount {
                openMessage = nil
                messages = []
            }
            refreshAccounts()
            // Only open Proton LoginSheet on genuine auth loss (needsLogin),
            // never for IMAP password rejects or when the session is still live.
            let kind = payload?["kind"] as? String ?? ""
            let needsLogin: Bool = {
                if let b = payload?["needsLogin"] as? Bool { return b }
                if let n = payload?["needsLogin"] as? NSNumber { return n.boolValue }
                return kind != "imap"
            }()
            guard needsLogin else { return }
            if let dead, accounts.contains(where: { $0.username == dead }) {
                // Session was not removed — spurious / non-fatal; keep UI.
                return
            }
            let real = accounts.filter { $0.username != "unified" && $0.kind != "unified" }
            if real.isEmpty {
                if let dead, !dead.isEmpty { loginUsername = dead }
                showLogin = true
            } else if let dead, selectedAccount == dead || selectedAccount == "unified" {
                // Linux parity: login only when the active view lost its account.
                loginUsername = dead
                showLogin = true
                selectedAccount = real.first?.username
                loadFoldersAndMail()
            }
            // Secondary account died while another remains: status only, no sheet.
        case "outboxChanged":
            if let payload = obj["payload"] as? [String: Any],
               let raw = payload["items"] as? [[String: Any]] {
                outboxItems = raw.map { parseOutboxItem($0) }
            } else {
                refreshOutbox()
            }
        case "outboxQueued":
            if let payload = obj["payload"] as? [String: Any] {
                let waiting = boolVal(payload["waiting"])
                status = waiting
                    ? String(localized: "No connection – the message waits in the Outbox and will be sent automatically")
                    : String(localized: "Sending failed – the message waits in the Outbox")
            }
            refreshOutbox()
        case "outboxSent":
            if let payload = obj["payload"] as? [String: Any],
               let subj = payload["subject"] as? String {
                status = String(format: String(localized: "Sent from the Outbox: %@"), subj)
            }
            refreshOutbox()
            reloadMessages()
        case "outboxSendFailed":
            if let payload = obj["payload"] as? [String: Any],
               let err = payload["error"] as? String {
                status = String(localized: "Sending failed: ") + err
            }
            refreshOutbox()
        case "ollamaPullProgress":
            if let payload = obj["payload"] as? [String: Any] {
                ollamaPullModel = payload["model"] as? String ?? ollamaPullModel
                ollamaPullStatus = payload["status"] as? String ?? ""
                if let f = payload["frac"] as? Double {
                    ollamaPullFrac = f
                } else if let n = payload["frac"] as? NSNumber {
                    ollamaPullFrac = n.doubleValue
                }
                if let d = payload["done"] as? Int64 {
                    ollamaPullDone = d
                } else if let n = payload["done"] as? NSNumber {
                    ollamaPullDone = n.int64Value
                }
                if let t = payload["total"] as? Int64 {
                    ollamaPullTotal = t
                } else if let n = payload["total"] as? NSNumber {
                    ollamaPullTotal = n.int64Value
                }
            }
        default:
            break
        }
    }

    private func requestNotifications() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    private func notifyNewMail(_ payload: [String: Any]) {
        let content = UNMutableNotificationContent()
        content.title = payload["sender"] as? String ?? "Klient"
        content.body = payload["subject"] as? String ?? "New message"
        content.sound = .default
        content.categoryIdentifier = "KLIENT_MAIL"
        let id = payload["id"] as? String ?? UUID().uuidString
        content.userInfo = [
            "messageId": id,
            "account": payload["account"] as? String ?? selectedAccount ?? "",
        ]
        UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: "mail-\(id)", content: content, trigger: nil)
        )
        MenuBarController.shared.rebuildMenu()
    }

    func handleNotificationAction(_ action: String, messageId: String, account: String) {
        let acc = account.isEmpty ? (selectedAccount ?? "") : account
        guard !acc.isEmpty, !messageId.isEmpty else { return }
        switch action {
        case "read":
            _ = try? Bridge.call("mail.markRead", params: ["account": acc, "ids": [messageId]])
        case "archive":
            _ = try? Bridge.call("mail.move", params: [
                "account": acc, "ids": [messageId], "folder": labels.archive,
            ])
        case "trash":
            _ = try? Bridge.call("mail.move", params: [
                "account": acc, "ids": [messageId], "folder": labels.trash,
            ])
        case "spam":
            _ = try? Bridge.call("spam.feedback", params: [
                "messageId": messageId, "from": "", "isSpam": true,
            ])
            _ = try? Bridge.call("mail.move", params: [
                "account": acc, "ids": [messageId], "folder": labels.spam,
            ])
        default:
            break
        }
        reloadMessages()
        MenuBarController.shared.rebuildMenu()
    }

    func registerMailtoDefault() {
        let bundle = Bundle.main.bundleIdentifier ?? "eu.libormacak.Klient"
        LSSetDefaultHandlerForURLScheme("mailto" as CFString, bundle as CFString)
        status = "Registered as mailto handler"
    }
}

/// Handles notification action buttons (read / archive / trash / spam).
final class NotificationActionDelegate: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationActionDelegate()
    weak var state: AppState?

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let info = response.notification.request.content.userInfo
        let msgId = info["messageId"] as? String ?? ""
        let account = info["account"] as? String ?? ""
        let action = response.actionIdentifier
        if action != UNNotificationDefaultActionIdentifier,
           action != UNNotificationDismissActionIdentifier {
            Task { @MainActor in
                state?.handleNotificationAction(action, messageId: msgId, account: account)
            }
        } else {
            Task { @MainActor in
                NSApp.activate(ignoringOtherApps: true)
            }
        }
        completionHandler()
    }
}

/// Nonisolated JSON helpers for background Bridge decoding (footer status, etc.).
private enum JSONVal {
    static func bool(_ v: Any?, default def: Bool = false) -> Bool {
        if let b = v as? Bool { return b }
        if let n = v as? NSNumber { return n.boolValue }
        return def
    }

    static func int(_ v: Any?) -> Int {
        if let i = v as? Int { return i }
        if let n = v as? NSNumber { return n.intValue }
        return 0
    }

    static func time(_ v: Any?) -> TimeInterval {
        if let d = v as? Double { return d }
        if let i = v as? Int { return TimeInterval(i) }
        if let n = v as? NSNumber { return n.doubleValue }
        return 0
    }

    static func uint64(_ v: Any?) -> UInt64 {
        if let u = v as? UInt64 { return u }
        if let i = v as? Int, i >= 0 { return UInt64(i) }
        if let n = v as? NSNumber { return n.uint64Value }
        return 0
    }

    static func double(_ v: Any?) -> Double {
        if let d = v as? Double { return d }
        if let i = v as? Int { return Double(i) }
        if let n = v as? NSNumber { return n.doubleValue }
        return 0
    }

    static func fillStorage(account: String?, into status: inout AppState.FilterStatus) {
        guard let acc = account,
              let caps = try? Bridge.call("mail.caps", params: ["account": acc]) as? [String: Any],
              let st = caps["storage"] as? [String: Any] else {
            status.storageUsed = 0
            status.storageMax = 0
            status.storageMail = 0
            status.storageDrive = 0
            status.storageDriveMax = 0
            status.storageSplit = false
            status.storageUpdated = 0
            return
        }
        status.storageUsed = uint64(st["used"])
        status.storageMax = uint64(st["max"])
        status.storageMail = uint64(st["mail"])
        status.storageDrive = uint64(st["drive"])
        status.storageDriveMax = uint64(st["driveMax"])
        status.storageSplit = bool(st["split"])
        status.storageUpdated = time(st["updated"])
    }

    static func fillUsage(into status: inout AppState.FilterStatus) {
        guard let u = try? Bridge.call("ai.usage") as? [String: Any] else {
            status.usageReady = false
            return
        }
        status.usageReady = true
        status.usageMonthCost = double(u["cost"])
        status.usageTodayCost = double(u["todayCost"])
        status.usageMonthUnpriced = bool(u["unpriced"])
        status.usageTodayUnpriced = bool(u["todayUnpriced"])
    }
}
