import SwiftUI
import ObjectiveC

/// Visual language inspired by Apple Mail + Airmail (SF Symbols, materials, accent tint).
enum KlientTheme {
    static let brand = Color.accentColor
    static let unreadDot = Color(nsColor: .systemBlue)
    static let sidebarMin: CGFloat = 196
    static let sidebarIdeal: CGFloat = 220
    static let listMin: CGFloat = 300
    static let listIdeal: CGFloat = 340

    struct FolderStyle {
        let symbol: String
        let tint: Color
    }

    /// Mail.app / Airmail–style mailbox glyphs with soft tints.
    static func folderStyle(_ name: String, isLabel: Bool = false, colorHex: String = "") -> FolderStyle {
        if isLabel {
            return FolderStyle(symbol: "tag.fill", tint: colorFromHex(colorHex) ?? .secondary)
        }
        let n = name.lowercased()
            .folding(options: .diacriticInsensitive, locale: .current)
        if n.contains("inbox") || n.contains("dorucen") {
            return FolderStyle(symbol: "tray.fill", tint: .blue)
        }
        if n.contains("draft") || n.contains("koncept") {
            return FolderStyle(symbol: "doc.fill", tint: .orange)
        }
        if n.contains("sent") || n.contains("odeslan") {
            return FolderStyle(symbol: "paperplane.fill", tint: .blue)
        }
        if n.contains("star") || n.contains("hvezd") || n.contains("flag") {
            return FolderStyle(symbol: "star.fill", tint: .yellow)
        }
        if n.contains("archive") || n.contains("archiv") {
            return FolderStyle(symbol: "archivebox.fill", tint: .brown)
        }
        if n.contains("spam") || n.contains("junk") {
            return FolderStyle(symbol: "xmark.bin.fill", tint: .red)
        }
        if n.contains("trash") || n.contains("kos") || n.contains("deleted") {
            return FolderStyle(symbol: "trash.fill", tint: .red)
        }
        if n.contains("snooze") || n.contains("odloz") {
            return FolderStyle(symbol: "moon.zzz.fill", tint: .indigo)
        }
        if n.contains("schedul") || n.contains("naplan") {
            return FolderStyle(symbol: "clock.fill", tint: .purple)
        }
        if n.contains("all") || n.contains("vesker") || n.contains("vse") {
            return FolderStyle(symbol: "envelope.fill", tint: .gray)
        }
        return FolderStyle(symbol: "folder.fill", tint: .secondary)
    }

    /// Parse Proton label colours like "#8080FF".
    static func colorFromHex(_ hex: String) -> Color? {
        var s = hex.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return nil }
        return Color(
            red: Double((v >> 16) & 0xFF) / 255,
            green: Double((v >> 8) & 0xFF) / 255,
            blue: Double(v & 0xFF) / 255
        )
    }

    static func folderIcon(_ name: String) -> String {
        folderStyle(name).symbol
    }

    static func accountIcon(_ kind: String) -> String {
        switch kind {
        case "proton": return "lock.shield.fill"
        case "gmail": return "envelope.badge.shield.half.filled"
        case "unified": return "tray.2.fill"
        case "seznam": return "envelope.badge.fill"
        default: return "envelope.fill"
        }
    }

    static func accountTint(_ kind: String) -> Color {
        switch kind {
        case "proton": return .purple
        case "gmail": return .red
        case "unified": return .blue
        case "seznam": return .orange
        default: return brand
        }
    }

    static func relativeDate(_ unix: Int64) -> String {
        let date = Date(timeIntervalSince1970: TimeInterval(unix))
        let cal = Calendar.current
        if cal.isDateInToday(date) {
            return date.formatted(date: .omitted, time: .shortened)
        }
        if cal.isDateInYesterday(date) {
            return String(localized: "Yesterday")
        }
        if cal.isDate(date, equalTo: Date(), toGranularity: .weekOfYear) {
            return date.formatted(.dateTime.weekday(.abbreviated))
        }
        if cal.isDate(date, equalTo: Date(), toGranularity: .year) {
            return date.formatted(.dateTime.month(.abbreviated).day())
        }
        return date.formatted(date: .numeric, time: .omitted)
    }

    static func absoluteDate(_ unix: Int64) -> String {
        guard unix > 0 else { return "—" }
        let date = Date(timeIntervalSince1970: TimeInterval(unix))
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    /// Countdown like Linux “za 2 h” / “za 5 min”.
    static func countdown(until unix: Int64) -> String {
        guard unix > 0 else { return "—" }
        let date = Date(timeIntervalSince1970: TimeInterval(unix))
        let sec = Int(date.timeIntervalSinceNow)
        if sec <= 0 { return String(localized: "now") }
        if sec < 60 { return String(format: String(localized: "in %lld s"), sec) }
        if sec < 3600 {
            return String(format: String(localized: "in %lld min"), sec / 60)
        }
        if sec < 86400 {
            let h = sec / 3600
            let m = (sec % 3600) / 60
            if m == 0 {
                return String(format: String(localized: "in %lld h"), h)
            }
            return String(format: String(localized: "in %lld h %lld min"), h, m)
        }
        let d = sec / 86400
        return String(format: String(localized: "in %lld d"), d)
    }
}

struct StatusBanner: View {
    let text: String
    var actionTitle: String? = nil
    var action: (() -> Void)? = nil

    var body: some View {
        if !text.isEmpty {
            HStack(spacing: 10) {
                Text(text)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                if let actionTitle, let action {
                    Button(actionTitle, action: action)
                        .buttonStyle(.borderedProminent)
                        .controlSize(.mini)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            .shadow(color: .black.opacity(0.06), radius: 8, y: 2)
            .transition(.move(edge: .top).combined(with: .opacity))
        }
    }
}

/// Linux-style bottom-left strip: storage, AI / spam / feeds, AI spend (mainview bottom bars).
/// Text wraps within the narrow sidebar column; `.help` shows the full string on hover.
/// Height is capped so `safeAreaInset` cannot expand forever and crush NavigationSplitView.
struct FilterStatusStrip: View {
    let status: AppState.FilterStatus

    /// Soft cap — enough for storage + 3 status lines + usage; overflow scrolls.
    private static let maxHeight: CGFloat = 160

    var body: some View {
        ScrollView(.vertical, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 6) {
                if status.showsStorage {
                    storageBlock
                }
                VStack(alignment: .leading, spacing: 4) {
                    statusLine(
                        symbol: "sparkles",
                        tint: .purple,
                        text: assistantText,
                        help: assistantText
                    )
                    statusLine(
                        symbol: status.spamFilterEnabled ? "flame.fill" : "flame",
                        tint: .orange,
                        text: spamText,
                        help: spamText
                    )
                    statusLine(
                        symbol: "antenna.radiowaves.left.and.right",
                        tint: .secondary,
                        text: feedsText,
                        help: feedsText + "\n" + feedsHelp
                    )
                }
                if status.usageReady {
                    usageLine
                }
            }
            .font(.caption2)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
        }
        .frame(maxWidth: .infinity, maxHeight: Self.maxHeight, alignment: .top)
        .background(.bar)
    }

    private var storageBlock: some View {
        VStack(alignment: .leading, spacing: 3) {
            if status.storageMax > 0 {
                ProgressView(value: min(storageFraction, 1))
                    .progressViewStyle(.linear)
                    .tint(storageTint)
                    .frame(height: 4)
                    .controlSize(.mini)
            }
            Text(storageLabel)
                .multilineTextAlignment(.leading)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .help(storageHelp)
    }

    private var usageLine: some View {
        HStack(alignment: .firstTextBaseline, spacing: 4) {
            Text(usageText)
                .multilineTextAlignment(.leading)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(String(localized: "Details")) {
                KlientSettings.open(tab: .ai)
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color.accentColor)
            .help(String(localized: "Open AI settings"))
        }
        .help(usageText)
    }

    private func statusLine(symbol: String, tint: Color, text: String, help: String) -> some View {
        // HStack (not Label) so wrapped lines stay left-aligned under the icon column.
        HStack(alignment: .top, spacing: 6) {
            Image(systemName: symbol)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(tint)
                .frame(width: 12, alignment: .center)
                .padding(.top, 1)
            Text(text)
                .multilineTextAlignment(.leading)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .help(help)
    }

    private var assistantText: String {
        if !status.hasAssistant {
            return String(localized: "Assistant: not set up")
        }
        if status.assistantLocal {
            return String(format: String(localized: "Assistant: local (%@)"), status.assistantModel)
        }
        let name = status.assistantName.isEmpty ? String(localized: "AI") : status.assistantName
        if status.assistantModel.isEmpty {
            return String(format: String(localized: "Assistant: %@"), name)
        }
        return String(format: String(localized: "Assistant: %@ · %@"), name, status.assistantModel)
    }

    private var spamText: String {
        if !status.spamFilterEnabled {
            return String(localized: "Spam: AI turned off")
        }
        if status.hasSpamAI {
            if status.spamLocal {
                return String(format: String(localized: "Spam: local (%@)"), status.spamModel)
            }
            let name = status.spamName.isEmpty ? String(localized: "AI") : status.spamName
            if status.spamModel.isEmpty {
                return String(format: String(localized: "Spam: %@"), name)
            }
            return String(format: String(localized: "Spam: %@ · %@"), name, status.spamModel)
        }
        return String(localized: "Spam: blocklists only")
    }

    private var feedsText: String {
        if status.blocklistLastUpdate <= 0 {
            return String(localized: "Feeds: never updated")
        }
        let date = Date(timeIntervalSince1970: status.blocklistLastUpdate)
        let relative = date.formatted(.relative(presentation: .named))
        return String(format: String(localized: "Feeds updated %@"), relative)
    }

    private var feedsHelp: String {
        let nets = status.blocklistNetworks
        let domains = status.blocklistDomains
        let urls = status.blocklistURLs
        return String(
            format: String(localized: "%lld networks · %lld domains · %lld links"),
            nets, domains, urls
        )
    }

    private var storageFraction: Double {
        guard status.storageMax > 0 else { return 0 }
        return Double(status.storageUsed) / Double(status.storageMax)
    }

    private var storageTint: Color {
        let f = storageFraction
        if f >= 0.9 { return .red }
        if f >= 0.75 { return .orange }
        return Color.accentColor
    }

    private var storageLabel: String {
        let used = Self.humanSize(status.storageUsed)
        if status.storageMax == 0 {
            return String(format: String(localized: "%@ used"), used)
        }
        let max = Self.humanSize(status.storageMax)
        let pct = storageFraction * 100
        return String(format: String(localized: "%@ of %@ (%.0f %%)"), used, max, pct)
    }

    private var storageHelp: String {
        var tip: String
        if status.storageMax == 0 {
            tip = String(localized: "The total size of the messages in the folders; the server does not report a mailbox limit")
        } else if status.storageSplit {
            tip = String(
                format: String(localized: "Storage for mail, calendar and contacts (without Drive).\nMail: %@\nDrive has its own storage: %@ of %@"),
                Self.humanSize(status.storageMail),
                Self.humanSize(status.storageDrive),
                Self.humanSize(status.storageDriveMax)
            )
        } else if status.storageDrive > 0 {
            tip = String(
                format: String(localized: "Storage shared by all Proton services.\nMail: %@\nDrive: %@"),
                Self.humanSize(status.storageMail),
                Self.humanSize(status.storageDrive)
            )
        } else {
            tip = String(format: String(localized: "Mail takes up %@"), Self.humanSize(status.storageMail))
        }
        if status.storageUpdated > 0 {
            let when = Date(timeIntervalSince1970: status.storageUpdated)
                .formatted(date: .abbreviated, time: .shortened)
            tip += "\n" + String(format: String(localized: "Updated %@"), when)
        }
        return tip
    }

    private var usageText: String {
        let month = Self.money(status.usageMonthCost, unpriced: status.usageMonthUnpriced)
        let today = Self.money(status.usageTodayCost, unpriced: status.usageTodayUnpriced)
        return String(format: String(localized: "AI spending this month: %@ (today %@)"), month, today)
    }

    /// Linux humanSize: GB / MB / kB / B.
    static func humanSize(_ n: UInt64) -> String {
        let v = Double(n)
        if n >= 1 << 30 {
            return String(format: "%.1f GB", v / Double(1 << 30))
        }
        if n >= 1 << 20 {
            return String(format: "%.1f MB", v / Double(1 << 20))
        }
        if n >= 1 << 10 {
            return String(format: "%.0f kB", v / Double(1 << 10))
        }
        return "\(n) B"
    }

    /// Linux money(): `$1.23` / `1,23 $` (cs), with `+ ?` when unpriced.
    static func money(_ v: Double, unpriced: Bool = false) -> String {
        let czech = KlientLocale.resolvedCode() == "cs"
        let base: String
        if v > 0 && v < 0.01 {
            base = String(localized: "< $0.01")
        } else {
            let rounded = (v * 100).rounded() / 100
            let raw = String(format: "%.2f", rounded)
            if czech {
                base = raw.replacingOccurrences(of: ".", with: ",") + " $"
            } else {
                base = "$" + raw
            }
        }
        return unpriced ? base + " + ?" : base
    }
}

extension Notification.Name {
    /// Posted when Preferences → Language is applied (Bundle + AppleLanguages).
    static let klientLanguageDidChange = Notification.Name("KlientLanguageDidChange")
}

/// Applies Preferences → Language so `Bundle` / `String(localized:)` and Go
/// embedded content (What's New, Help) share one effective code: `cs` / `en`.
enum KlientLocale {
    private static let foldersTable = "Folders"
    private static let appleLanguagesKey = "AppleLanguages"

    /// Config value last applied: `""` (system), `"en"`, or `"cs"`.
    private(set) static var configCode: String = ""

    /// Effective UI language after resolving System → macOS preferred list.
    private(set) static var resolvedCodeValue: String = "en"

    /// `cs.lproj` / `en.lproj` used by the Bundle.main localization hook.
    fileprivate static var stringsBundle: Bundle?

    private static var hookInstalled = false

    /// Effective language for bridge calls (`about.*`, `help.prepare`, `update.check`).
    static func resolvedCode() -> String { resolvedCodeValue }

    /// SwiftUI / formatting locale matching Preferences → Language.
    static func locale() -> Locale {
        Locale(identifier: resolvedCodeValue == "cs" ? "cs_CZ" : "en_US")
    }

    static func apply(_ code: String) {
        configCode = code
        if code.isEmpty {
            UserDefaults.standard.removeObject(forKey: appleLanguagesKey)
            resolvedCodeValue = systemLanguageCode()
        } else {
            // Force Bundle / String(localized:) over the system list (also after restart).
            UserDefaults.standard.set([code], forKey: appleLanguagesKey)
            resolvedCodeValue = code.hasPrefix("cs") ? "cs" : "en"
        }
        UserDefaults.standard.synchronize()
        stringsBundle = Bundle.main.path(forResource: resolvedCodeValue, ofType: "lproj")
            .flatMap { Bundle(path: $0) }
        installBundleHook()
        NotificationCenter.default.post(name: .klientLanguageDidChange, object: resolvedCodeValue)
    }

    /// macOS UI language when Preferences → Language is System.
    static func systemLanguageCode() -> String {
        // Prefer Locale.preferredLanguages (reflects AppleLanguages). Avoid
        // UserDefaults(suiteName: ".GlobalPreferences") — unreliable / private.
        if let first = Locale.preferredLanguages.first {
            return first.hasPrefix("cs") ? "cs" : "en"
        }
        return Locale.current.language.languageCode?.identifier == "cs" ? "cs" : "en"
    }

    /// Call from `applicationWillFinishLaunching` before the first `String(localized:)`.
    static func applyFromConfigFile() {
        let url = FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("klient/config.json")
        var lang = ""
        if let data = try? Data(contentsOf: url),
           let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            lang = obj["language"] as? String ?? ""
        }
        apply(lang)
    }

    static func applyFromConfig() {
        guard let cfg = try? Bridge.call("config.get") as? [String: Any] else {
            apply(configCode)
            return
        }
        apply(cfg["language"] as? String ?? "")
    }

    /// Redirect `Bundle.main` lookups to the active `*.lproj` so language can
    /// change in-process (AppleLanguages alone only fully sticks after relaunch).
    private static func installBundleHook() {
        guard !hookInstalled else { return }
        object_setClass(Bundle.main, KlientLanguageBundle.self)
        hookInstalled = true
    }
}

/// Bundle.main stand-in that reads strings from `KlientLocale.stringsBundle`.
private final class KlientLanguageBundle: Bundle, @unchecked Sendable {
    override func localizedString(forKey key: String, value: String?, table tableName: String?) -> String {
        if let b = KlientLocale.stringsBundle {
            let s = b.localizedString(forKey: key, value: nil, table: tableName)
            if s != key {
                return s
            }
        }
        return super.localizedString(forKey: key, value: value, table: tableName)
    }
}

extension KlientLocale {

    /// Unified inbox label shown in the account list.
    static func accountDisplayName(username: String, displayName: String, email: String) -> String {
        let raw = displayName.isEmpty ? email : displayName
        if username == "unified"
            || raw.compare("All Accounts", options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame
            || raw.compare("Unified", options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame {
            return String(localized: "All Accounts")
        }
        return raw
    }

    /// Localize well-known system folder titles for the sidebar / nav title.
    /// Folder `id` values are left unchanged for API calls; only the display string is mapped.
    static func folderDisplayName(id: String, name: String) -> String {
        if let key = systemFolderKey(id: id, name: name) {
            return String(localized: String.LocalizationValue(key), table: foldersTable)
        }
        return name
    }

    private static func systemFolderKey(id: String, name: String) -> String? {
        switch id {
        case "0": return "Inbox"
        case "1", "8": return "Drafts"
        case "2", "7": return "Sent"
        case "3": return "Trash"
        case "4": return "Spam"
        case "5": return "All Mail"
        case "6": return "Archive"
        case "9": return "Outbox"
        case "10": return "Starred"
        case "12": return "Scheduled"
        case "16": return "Snoozed"
        default: break
        }
        return systemFolderKey(fromName: name)
    }

    private static func systemFolderKey(fromName name: String) -> String? {
        var n = name
        if let range = n.range(of: "]") {
            // "[Gmail]/All Mail" → "All Mail"
            n = String(n[n.index(after: range.lowerBound)...])
        }
        n = n.trimmingCharacters(in: CharacterSet(charactersIn: "/").union(.whitespacesAndNewlines))
        if n.uppercased().hasPrefix("INBOX/") { n = String(n.dropFirst(6)) }
        if n.compare("INBOX", options: .caseInsensitive) == .orderedSame { return "Inbox" }
        let key = n.lowercased()
            .folding(options: .diacriticInsensitive, locale: Locale(identifier: "en_US"))
        switch key {
        case "inbox", "dorucene", "dorucena posta":
            return "Inbox"
        case "drafts", "draft", "koncepty", "koncept":
            return "Drafts"
        case "sent", "sent mail", "sent messages", "odeslane", "odeslano":
            return "Sent"
        case "trash", "deleted", "deleted messages", "kos":
            return "Trash"
        case "spam", "junk", "junk e-mail", "junk email":
            return "Spam"
        case "starred", "star", "flagged", "s hvezdickou", "oznacene hvezdickou":
            return "Starred"
        case "archive", "archiv":
            return "Archive"
        case "all mail", "all", "veskera posta", "vsechna posta":
            return "All Mail"
        case "snoozed", "snooze", "odlozene":
            return "Snoozed"
        case "scheduled", "naplanovane":
            return "Scheduled"
        case "outbox":
            return "Outbox"
        default:
            return nil
        }
    }
}
