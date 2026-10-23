import SwiftUI
import AppKit

struct AboutView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            VStack(spacing: 12) {
                Image(systemName: "envelope.badge.shield.half.filled")
                    .font(.system(size: 48, weight: .light))
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(KlientTheme.brand)
                Text("Klient")
                    .font(.largeTitle.weight(.bold))
                Text(String(format: String(localized: "Version %@"), state.aboutInfo["version"] as? String ?? state.version))
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 28)
            .background(
                LinearGradient(
                    colors: [KlientTheme.brand.opacity(0.16), Color(nsColor: .windowBackgroundColor)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )

            Form {
                Section {
                    LabeledContent(String(localized: "Author")) {
                        let author = state.aboutInfo["author"] as? String ?? "Libor Macák"
                        let email = state.aboutInfo["email"] as? String ?? "jsem@libormacak.eu"
                        VStack(alignment: .trailing, spacing: 2) {
                            Text(author)
                                .foregroundStyle(.secondary)
                            if let mailURL = URL(string: "mailto:\(email)") {
                                Link(email, destination: mailURL)
                            }
                        }
                    }
                    LabeledContent(String(localized: "Runtime")) {
                        Text(state.aboutInfo["go"] as? String ?? "—")
                            .foregroundStyle(.secondary)
                    }
                    LabeledContent("App ID") {
                        Text(state.aboutInfo["id"] as? String ?? "eu.libormacak.Klient")
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                } footer: {
                    Text(String(localized: "A mail client with Proton Mail, PGP and an AI spam filter. Mail stays on your machine."))
                }
            }
            .formStyle(.grouped)

            HStack {
                Button(String(localized: "What's New")) {
                    dismiss()
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
                        state.openWhatsNew()
                    }
                }
                Spacer()
                Button(String(localized: "OK")) { dismiss() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
            }
            .padding(20)
        }
        .frame(width: 420)
    }
}

struct WhatsNewView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "sparkles.rectangle.stack")
                    .font(.system(size: 32, weight: .light))
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(KlientTheme.brand)
                VStack(alignment: .leading, spacing: 2) {
                    Text(String(format: String(localized: "What's New in Klient %@"),
                                 state.aboutInfo["version"] as? String ?? state.version))
                        .font(.title2.weight(.semibold))
                    Text(String(localized: "Release notes for this version."))
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(20)
            .background(
                LinearGradient(
                    colors: [KlientTheme.brand.opacity(0.14), Color(nsColor: .windowBackgroundColor)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )

            ScrollView {
                LazyVStack(alignment: .leading, spacing: 16) {
                    if state.whatsNewReleases.isEmpty {
                        Text(String(localized: "No release notes for this build."))
                            .foregroundStyle(.secondary)
                            .padding()
                    } else {
                        ForEach(Array(state.whatsNewReleases.enumerated()), id: \.offset) { _, rel in
                            let ver = rel["version"] as? String ?? ""
                            let blocks = rel["blocks"] as? [[String: Any]] ?? []
                            VStack(alignment: .leading, spacing: 8) {
                                if state.whatsNewReleases.count > 1 {
                                    Text(String(format: String(localized: "Version %@"), ver))
                                        .font(.headline)
                                }
                                ForEach(Array(blocks.enumerated()), id: \.offset) { _, b in
                                    let text = b["text"] as? String ?? ""
                                    let bullet = b["bullet"] as? Bool ?? false
                                    HStack(alignment: .top, spacing: 8) {
                                        if bullet {
                                            Text("•")
                                                .foregroundStyle(KlientTheme.brand)
                                        }
                                        Text(Self.stripMarkdown(text))
                                            .font(.callout)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                            .textSelection(.enabled)
                                    }
                                }
                            }
                        }
                    }
                }
                .padding(20)
            }

            HStack {
                Spacer()
                Button(String(localized: "Close")) { dismiss() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
            }
            .padding(16)
        }
        .frame(width: 520, height: 460)
    }

    private static func stripMarkdown(_ s: String) -> String {
        var t = s
        t = t.replacingOccurrences(of: "**", with: "")
        t = t.replacingOccurrences(of: "`", with: "")
        return t
    }
}

struct SnoozePickerSheet: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(String(localized: "Snooze Until"))
                    .font(.headline)
                Spacer()
            }
            .padding()

            DatePicker(
                "",
                selection: $state.snoozePickerDate,
                in: Date().addingTimeInterval(60)...,
                displayedComponents: [.date, .hourAndMinute]
            )
            .datePickerStyle(.graphical)
            .labelsHidden()
            .padding(.horizontal)

            HStack {
                Button(String(localized: "Cancel")) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Spacer()
                Button(String(localized: "Confirm")) {
                    state.snoozeUntil(state.snoozePickerDate)
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
            }
            .padding()
        }
        .frame(width: 360)
    }
}

struct AutoReplyView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var enabled = false
    @State private var subject = ""
    @State private var message = ""
    @State private var status = ""

    var body: some View {
        VStack(spacing: 0) {
            Form {
                Section {
                    Toggle(String(localized: "Reply automatically"), isOn: $enabled)
                    TextField(String(localized: "Subject"), text: $subject)
                }
                Section(String(localized: "Message")) {
                    TextEditor(text: $message)
                        .font(.body)
                        .frame(minHeight: 140)
                }
                if !status.isEmpty {
                    Section {
                        Text(status).foregroundStyle(.secondary)
                    }
                }
            }
            .formStyle(.grouped)

            HStack {
                Button(String(localized: "Cancel")) { dismiss() }
                Spacer()
                Button(String(localized: "Save")) { save() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
            }
            .padding(16)
        }
        .frame(width: 480, height: 400)
        .onAppear(perform: load)
    }

    private func load() {
        guard let acc = state.selectedAccount, acc != "unified" else {
            status = String(localized: "Select a single account")
            return
        }
        if let r = try? Bridge.call("autoReply.get", params: ["account": acc]) as? [String: Any] {
            enabled = r["Enabled"] as? Bool ?? r["enabled"] as? Bool ?? false
            subject = r["Subject"] as? String ?? r["subject"] as? String ?? ""
            message = r["Message"] as? String ?? r["message"] as? String ?? ""
        }
    }

    private func save() {
        guard let acc = state.selectedAccount, acc != "unified" else { return }
        do {
            _ = try Bridge.call("autoReply.set", params: [
                "account": acc,
                "reply": [
                    "Enabled": enabled,
                    "Subject": subject,
                    "Message": message,
                ],
            ])
            dismiss()
        } catch {
            status = error.localizedDescription
        }
    }
}

struct UpdateView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var busy = false
    @State private var actionStatus = ""

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "arrow.triangle.2.circlepath.circle.fill")
                    .font(.system(size: 36, weight: .light))
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(KlientTheme.brand)
                VStack(alignment: .leading, spacing: 2) {
                    Text(String(localized: "Updates"))
                        .font(.title2.weight(.semibold))
                    Text(String(localized: "New versions come from GitHub."))
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(20)
            .background(
                LinearGradient(
                    colors: [KlientTheme.brand.opacity(0.14), Color(nsColor: .windowBackgroundColor)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )

            if let info = state.updateInfo {
                Form {
                    Section {
                        LabeledContent(String(localized: "Current")) {
                            Text(info["current"] as? String ?? state.version)
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }
                        LabeledContent(String(localized: "Latest")) {
                            Text(info["version"] as? String ?? "—")
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }
                    } footer: {
                        if state.jsonBool(info["newer"]) {
                            Text(String(localized: "A newer version is available."))
                                .foregroundStyle(.orange)
                        } else {
                            Text(String(localized: "You are on the latest release."))
                        }
                    }

                    if let notes = info["notes"] as? String, !notes.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        Section(String(localized: "What's New")) {
                            ScrollView {
                                Text(notes)
                                    .font(.callout)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .textSelection(.enabled)
                            }
                            .frame(minHeight: 100, maxHeight: 160)
                        }
                    }

                    if state.jsonBool(info["newer"]) {
                        Section {
                            Text(String(localized: "Download the DMG, open it, and drag Klient to Applications."))
                                .font(.callout)
                                .foregroundStyle(.secondary)
                        }
                    }

                    if !actionStatus.isEmpty {
                        Section {
                            Text(actionStatus).foregroundStyle(.secondary)
                        }
                    }
                }
                .formStyle(.grouped)

                HStack {
                    Button(String(localized: "Check Again")) {
                        actionStatus = ""
                        state.checkForUpdates()
                    }
                    .disabled(busy)

                    if let page = info["page"] as? String, let url = URL(string: page) {
                        Button(String(localized: "Release Page")) { NSWorkspace.shared.open(url) }
                    }

                    Spacer()

                    if state.jsonBool(info["newer"]),
                       let dmg = info["dmg"] as? String, !dmg.isEmpty {
                        Button(String(localized: "Download DMG")) { downloadDMG(info: info) }
                            .buttonStyle(.borderedProminent)
                            .disabled(busy)
                    }

                    Button(String(localized: "Close")) { dismiss() }
                        .keyboardShortcut(.cancelAction)
                }
                .padding(16)
            } else {
                ProgressView(String(localized: "Checking…"))
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .padding(40)
            }
        }
        .frame(width: 520, height: 480)
    }

    private func downloadDMG(info: [String: Any]) {
        busy = true
        actionStatus = String(localized: "Downloading…")
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let result = try Bridge.call("update.download") as? [String: Any]
                DispatchQueue.main.async {
                    busy = false
                    if let path = result?["path"] as? String, !path.isEmpty {
                        let url = URL(fileURLWithPath: path)
                        NSWorkspace.shared.open(url)
                        actionStatus = String(format: String(localized: "Saved to %@ — open the DMG and drag Klient to Applications."), url.lastPathComponent)
                        state.status = actionStatus
                    } else if let remote = result?["url"] as? String, let url = URL(string: remote) {
                        NSWorkspace.shared.open(url)
                        actionStatus = String(localized: "Opened the download in your browser.")
                    } else if let dmg = info["dmg"] as? String, let url = URL(string: dmg) {
                        NSWorkspace.shared.open(url)
                        actionStatus = String(localized: "Opened the download in your browser.")
                    } else {
                        actionStatus = String(localized: "No DMG is available for this release.")
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    busy = false
                    // Fall back to opening the release asset URL directly.
                    if let dmg = info["dmg"] as? String, let url = URL(string: dmg) {
                        NSWorkspace.shared.open(url)
                        actionStatus = String(localized: "Opened the download in your browser.")
                    } else {
                        actionStatus = error.localizedDescription
                    }
                }
            }
        }
    }
}

/// Simplified local-model comparison: run ai.test on each installed Ollama model.
struct CompareModelsSheet: View {
    let ollamaModels: [String]
    var onChoose: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var results: [(model: String, ok: Bool, detail: String)] = []
    @State private var busy = false
    @State private var status = ""

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 12) {
                Text(String(localized: "Each installed local model is probed with a short test request. Pick the one that works best for you."))
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal)
                if busy {
                    ProgressView(status.isEmpty ? String(localized: "Comparing…") : status)
                        .padding()
                }
                List {
                    ForEach(results, id: \.model) { row in
                        HStack {
                            Image(systemName: row.ok ? "checkmark.circle.fill" : "xmark.circle.fill")
                                .foregroundStyle(row.ok ? Color.green : Color.red)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(row.model).font(.body.weight(.medium))
                                if !row.detail.isEmpty {
                                    Text(row.detail).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            Spacer()
                            if row.ok {
                                Button(String(localized: "Use")) {
                                    onChoose(row.model)
                                    dismiss()
                                }
                                .buttonStyle(.borderedProminent)
                                .controlSize(.small)
                            }
                        }
                    }
                }
            }
            .navigationTitle(String(localized: "Compare Models"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "Close")) { dismiss() }
                }
                ToolbarItem(placement: .primaryAction) {
                    Button(String(localized: "Run")) { runCompare() }
                        .disabled(busy || ollamaModels.isEmpty)
                }
            }
            .onAppear {
                if results.isEmpty { runCompare() }
            }
        }
        .frame(width: 480, height: 420)
    }

    private func runCompare() {
        busy = true
        results = []
        status = String(localized: "Comparing…")
        let models = ollamaModels
        DispatchQueue.global().async {
            var out: [(String, Bool, String)] = []
            for m in models {
                DispatchQueue.main.async {
                    status = String(format: String(localized: "Testing %@…"), m)
                }
                do {
                    _ = try Bridge.call("ai.test", params: [
                        "provider": "ollama", "model": m,
                    ])
                    out.append((m, true, String(localized: "OK")))
                } catch {
                    out.append((m, false, error.localizedDescription))
                }
            }
            DispatchQueue.main.async {
                results = out.map { (model: $0.0, ok: $0.1, detail: $0.2) }
                busy = false
                status = ""
            }
        }
    }
}

struct ShortcutsHelpView: View {
    @Environment(\.dismiss) private var dismiss

    private let rows: [(String, String)] = [
        ("⌘N", String(localized: "New Message")),
        ("⌘R", String(localized: "Reply")),
        ("⇧⌘R", String(localized: "Reply All")),
        ("⇧⌘F", String(localized: "Forward")),
        ("⌘P", String(localized: "Print…")),
        ("⌘,", String(localized: "Settings…")),
        ("⌘?", String(localized: "Klient Help")),
    ]

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(String(localized: "Keyboard Shortcuts"))
                    .font(.title2.weight(.semibold))
                Spacer()
                Button(String(localized: "Close")) { dismiss() }
            }
            .padding(20)
            .background(.bar)
            Form {
                Section {
                    ForEach(rows, id: \.0) { key, title in
                        LabeledContent(title) {
                            Text(key).foregroundStyle(.secondary).monospaced()
                        }
                    }
                }
            }
            .formStyle(.grouped)
        }
        .frame(width: 420, height: 360)
    }
}
