import SwiftUI
import AppKit
import UniformTypeIdentifiers

struct ComposeView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var to = ""
    @State private var cc = ""
    @State private var bcc = ""
    @State private var subject = ""
    @State private var bodyText = ""
    @State private var fromAddressId = ""
    @State private var status = ""
    @State private var sending = false
    @State private var scheduleAt = ""
    @State private var security = ""
    @State private var showSchedule = false
    @State private var showBcc = false
    @State private var allSuggestions: [ContactPerson] = []
    @State private var activeField: RecipientField = .to
    @State private var draftID = ""
    @State private var attachments: [ComposeAttachment] = []
    @State private var htmlBody = ""
    @State private var signExternal = true
    @State private var attachPublicKey = false
    @State private var requireEncryption = false
    @State private var showEncryptBlock = false
    @State private var showEncryptWarn = false
    @State private var pendingSendParams: [String: Any]?

    private enum RecipientField {
        case to, cc, bcc
    }

    struct ComposeAttachment: Identifiable {
        let id = UUID()
        let name: String
        let mimeType: String
        let data: Data
    }

    var replyTo: AppState.MessageDetail?
    var action: Int = 0 // 0 new, 1 reply, 2 replyAll, 3 forward

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            fields
            suggestionBar
            if !attachments.isEmpty {
                attachmentBar
                Divider()
            }
            Divider()
            RichTextEditor(plainText: $bodyText, htmlBody: $htmlBody, spellCheck: state.spellCheckEnabled)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .onDrop(of: [.fileURL], isTargeted: nil) { providers in
                    addDroppedFiles(providers)
                }
            Divider()
            footer
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .frame(minWidth: 640, minHeight: 520)
        .task { await preloadContacts() }
        .onAppear {
            preload()
            if state.composeAutoDraftAI, replyTo != nil {
                state.composeAutoDraftAI = false
                draftReplyAI()
            }
        }
        .alert(String(localized: "The Message Cannot Be Sent Encrypted"), isPresented: $showEncryptBlock) {
            Button(String(localized: "OK"), role: .cancel) {}
        } message: {
            Text(String(localized: "Some recipients have no known public key. Remove them, import their keys (Settings → PGP), or turn off “Send only encrypted” for this message.")
                + (security.isEmpty ? "" : "\n\n" + security))
        }
        .confirmationDialog(
            String(localized: "Send Without Encryption?"),
            isPresented: $showEncryptWarn,
            titleVisibility: .visible
        ) {
            Button(String(localized: "Send Anyway"), role: .destructive) {
                if let p = pendingSendParams {
                    finishSend(params: p)
                }
            }
            Button(String(localized: "Cancel"), role: .cancel) {
                pendingSendParams = nil
                sending = false
                status = ""
            }
        } message: {
            Text(String(localized: "Some recipients have no known public key, so the message will reach them unencrypted.")
                + (security.isEmpty ? "" : "\n\n" + security))
        }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.title2.weight(.semibold))
                if !security.isEmpty {
                    Label(security, systemImage: "lock.shield")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer()
            Button(String(localized: "Cancel")) { dismiss() }
                .keyboardShortcut(.cancelAction)
            Button(String(localized: "Save Draft")) { save(send: false) }
                .disabled(sending)
            Button {
                save(send: true)
            } label: {
                Label(sending ? String(localized: "Sending…") : String(localized: "Send"),
                      systemImage: "paperplane.fill")
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
            .disabled(sending || to.trimmingCharacters(in: .whitespaces).isEmpty)
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 14)
        .background(.bar)
    }

    private var fields: some View {
        VStack(spacing: 0) {
            if !state.sendAddresses.isEmpty {
                fieldRow(String(localized: "From")) {
                    Picker("", selection: $fromAddressId) {
                        ForEach(state.sendAddresses, id: \.id) { a in
                            Text(a.email).tag(a.id)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                }
            }
            fieldRow(String(localized: "To")) {
                TextField("", text: $to, prompt: Text(String(localized: "recipients")))
                    .textFieldStyle(.plain)
                    .onChange(of: to) { _, _ in
                        activeField = .to
                        refreshEncryption()
                    }
                    .onSubmit { activeField = .to }
            }
            fieldRow(String(localized: "Cc")) {
                HStack {
                    TextField("", text: $cc, prompt: Text(String(localized: "optional")))
                        .textFieldStyle(.plain)
                        .onChange(of: cc) { _, _ in
                            activeField = .cc
                            refreshEncryption()
                        }
                    if !showBcc {
                        Button(String(localized: "Bcc")) { showBcc = true }
                            .buttonStyle(.borderless)
                            .controlSize(.small)
                    }
                }
            }
            if showBcc || !bcc.isEmpty {
                fieldRow(String(localized: "Bcc")) {
                    TextField("", text: $bcc, prompt: Text(String(localized: "optional")))
                        .textFieldStyle(.plain)
                        .onChange(of: bcc) { _, _ in
                            activeField = .bcc
                            refreshEncryption()
                        }
                }
            }
            fieldRow(String(localized: "Subject")) {
                TextField("", text: $subject)
                    .textFieldStyle(.plain)
            }
            if state.accountE2E {
                VStack(alignment: .leading, spacing: 6) {
                    Toggle(String(localized: "Sign unencrypted messages with PGP too"), isOn: $signExternal)
                    Toggle(String(localized: "Attach my public key"), isOn: $attachPublicKey)
                    Toggle(String(localized: "Send only encrypted"), isOn: $requireEncryption)
                }
                .font(.caption)
                .padding(.horizontal, 20)
                .padding(.vertical, 6)
            }
            if showSchedule {
                fieldRow(String(localized: "Send later")) {
                    DatePicker(
                        "",
                        selection: Binding(
                            get: {
                                ISO8601DateFormatter().date(from: scheduleAt) ?? Date().addingTimeInterval(3600)
                            },
                            set: { scheduleAt = ISO8601DateFormatter().string(from: $0) }
                        ),
                        displayedComponents: [.date, .hourAndMinute]
                    )
                    .labelsHidden()
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func fieldRow<Content: View>(_ label: String, @ViewBuilder content: () -> Content) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label)
                .font(.callout.weight(.medium))
                .foregroundStyle(.secondary)
                .frame(width: 72, alignment: .trailing)
            content()
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private var suggestionBar: some View {
        let queryText: String = {
            switch activeField {
            case .to: return to
            case .cc: return cc
            case .bcc: return bcc
            }
        }()
        let query = SystemContacts.lastToken(in: queryText)
        let matches = SystemContacts.matching(allSuggestions, query: query, limit: 8)
        if !query.isEmpty && !matches.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    ForEach(matches) { person in
                        Button {
                            applySuggestion(person)
                        } label: {
                            VStack(alignment: .leading, spacing: 1) {
                                Text(person.name.isEmpty ? person.email : person.name)
                                    .font(.caption.weight(.medium))
                                    .lineLimit(1)
                                if !person.name.isEmpty {
                                    Text(person.email)
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                        .lineLimit(1)
                                }
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(KlientTheme.brand.opacity(0.12), in: RoundedRectangle(cornerRadius: 6))
                        }
                        .buttonStyle(.plain)
                        .help(person.source == .system
                              ? String(localized: "Mac Contacts")
                              : String(localized: "Klient"))
                    }
                }
                .padding(.horizontal, 20)
                .padding(.vertical, 6)
            }
            .background(Color(nsColor: .controlBackgroundColor).opacity(0.6))
        }
    }

    private var attachmentBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(attachments) { att in
                    HStack(spacing: 6) {
                        Image(systemName: "paperclip")
                        Text(att.name)
                            .lineLimit(1)
                        Text(ByteCountFormatter.string(fromByteCount: Int64(att.data.count), countStyle: .file))
                            .foregroundStyle(.secondary)
                        Button {
                            attachments.removeAll { $0.id == att.id }
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                    .font(.caption)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6))
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 8)
        }
    }

    private var footer: some View {
        HStack {
            Button {
                pickAttachments()
            } label: {
                Label(String(localized: "Attach"), systemImage: "paperclip")
            }
            .disabled(sending)

            if action == 1 || action == 2, replyTo != nil {
                Button {
                    draftReplyAI()
                } label: {
                    Label(String(localized: "Draft Reply with AI"), systemImage: "sparkles")
                }
                .disabled(sending)
            }

            Button {
                improveDraft()
            } label: {
                Label(String(localized: "Improve with AI"), systemImage: "wand.and.stars")
            }
            .disabled(sending || bodyText.isEmpty)

            Toggle(String(localized: "Schedule"), isOn: $showSchedule)
                .toggleStyle(.button)
                .onChange(of: showSchedule) { _, on in
                    if on && scheduleAt.isEmpty {
                        scheduleAt = ISO8601DateFormatter().string(from: Date().addingTimeInterval(3600))
                    }
                    if !on { scheduleAt = "" }
                }

            if !status.isEmpty {
                Text(status)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.bar)
        .controlSize(.small)
    }

    private func applySuggestion(_ person: ContactPerson) {
        switch activeField {
        case .to:
            to = SystemContacts.replaceLastToken(in: to, with: person.formatted)
            refreshEncryption()
        case .cc:
            cc = SystemContacts.replaceLastToken(in: cc, with: person.formatted)
            refreshEncryption()
        case .bcc:
            bcc = SystemContacts.replaceLastToken(in: bcc, with: person.formatted)
            refreshEncryption()
        }
    }

    private func preloadContacts() async {
        _ = await SystemContacts.requestAccess()
        var people = SystemContacts.fetchWithEmails()
        if let acc = state.selectedAccount, acc != "unified" {
            let account = (try? Bridge.call("contacts.list", params: ["account": acc]) as? [[String: String]]) ?? []
            let recent = (try? Bridge.call("contacts.recent", params: ["account": acc, "max": 30]) as? [[String: String]]) ?? []
            var seen = Set(people.map { $0.email.lowercased() })
            for row in account + recent {
                let email = (row["email"] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                guard !email.isEmpty else { continue }
                let key = email.lowercased()
                guard !seen.contains(key) else { continue }
                seen.insert(key)
                people.append(ContactPerson(
                    name: row["name"] ?? "",
                    email: email,
                    source: .account
                ))
            }
        }
        allSuggestions = people.sorted {
            $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
        }
    }

    private var title: String {
        if !draftID.isEmpty { return String(localized: "Edit Draft") }
        switch action {
        case 1: return String(localized: "Reply")
        case 2: return String(localized: "Reply All")
        case 3: return String(localized: "Forward")
        default: return String(localized: "New Message")
        }
    }

    private func preload() {
        signExternal = true
        attachPublicKey = state.attachPublicKeyDefault
        requireEncryption = state.requireEncryptionDefault
        if let draft = DraftOpenStore.shared.take() {
            draftID = draft["id"] as? String ?? ""
            to = draft["to"] as? String ?? ""
            cc = draft["cc"] as? String ?? ""
            bcc = draft["bcc"] as? String ?? ""
            showBcc = !bcc.isEmpty
            subject = draft["subject"] as? String ?? ""
            bodyText = draft["body"] as? String ?? ""
            if let html = draft["html"] as? String, !html.isEmpty {
                htmlBody = html
                if bodyText.isEmpty { bodyText = html }
            }
            fromAddressId = draft["fromAddressId"] as? String ?? state.sendAddresses.first?.id ?? ""
            if let rawAtts = draft["attachments"] as? [[String: Any]] {
                attachments = rawAtts.compactMap { row in
                    guard let name = row["name"] as? String,
                          let b64 = row["data"] as? String,
                          let data = Data(base64Encoded: b64), !data.isEmpty else { return nil }
                    return ComposeAttachment(
                        name: name,
                        mimeType: row["mimeType"] as? String ?? "application/octet-stream",
                        data: data
                    )
                }
            }
            refreshEncryption()
            return
        }
        if fromAddressId.isEmpty {
            fromAddressId = replyTo?.addressId.isEmpty == false
                ? (replyTo?.addressId ?? "")
                : (state.sendAddresses.first?.id ?? "")
        }
        if replyTo == nil, let prefill = state.composePrefillTo {
            to = prefill
            state.composePrefillTo = nil
            refreshEncryption()
            return
        }
        guard let msg = replyTo else { return }
        // Opening a draft sets composeReply with drafts label — already handled via DraftOpenStore.
        if msg.labels.contains(state.labels.drafts), DraftOpenStore.shared.pending == nil {
            // Fields may already be on msg from openDraft path without store (fallback).
            to = msg.to
            subject = msg.subject
            bodyText = msg.text
            if !msg.addressId.isEmpty { fromAddressId = msg.addressId }
            return
        }
        subject = msg.subject.hasPrefix("Re:") || msg.subject.hasPrefix("Fwd:")
            ? msg.subject
            : (action == 3 ? "Fwd: \(msg.subject)" : "Re: \(msg.subject)")
        to = action == 3 ? "" : msg.sender
        if action != 3 {
            bodyText = "\n\nOn \(msg.sender) wrote:\n> " + msg.text.replacingOccurrences(of: "\n", with: "\n> ")
        } else {
            bodyText = "\n\n---------- Forwarded message ----------\n" + msg.text
        }
        if let sig = state.signatureText, !sig.isEmpty, action == 0 {
            bodyText = "\n\n" + sig + bodyText
        }
    }

    private func refreshEncryption() {
        guard state.accountE2E, let acc = state.selectedAccount else { return }
        let all = [to, cc, bcc].joined(separator: ", ")
        guard !all.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            security = ""
            return
        }
        DispatchQueue.global().async {
            if let result = try? Bridge.call("mail.planEncryption", params: [
                "account": acc, "to": all,
            ]) as? [[String: Any]] {
                let parts = result.map { row -> String in
                    let addr = row["address"] as? String ?? ""
                    let scheme = row["scheme"] as? String ?? "clear"
                    return "\(addr): \(scheme)"
                }
                DispatchQueue.main.async { security = parts.joined(separator: " · ") }
            }
        }
    }

    private func encryptionSecure(_ modes: [[String: Any]]) -> Bool {
        modes.allSatisfy { ($0["scheme"] as? String ?? "clear") != "clear" }
    }

    private func pickAttachments() {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = true
        panel.canChooseDirectories = false
        panel.allowedContentTypes = [.data, .item, .content]
        guard panel.runModal() == .OK else { return }
        for url in panel.urls {
            addFile(url)
        }
    }

    private func addDroppedFiles(_ providers: [NSItemProvider]) -> Bool {
        var any = false
        for p in providers {
            _ = p.loadObject(ofClass: URL.self) { url, _ in
                guard let url else { return }
                DispatchQueue.main.async { addFile(url) }
            }
            any = true
        }
        return any
    }

    private func addFile(_ url: URL) {
        guard let data = try? Data(contentsOf: url) else { return }
        let total = attachments.reduce(0) { $0 + $1.data.count } + data.count
        if total > 25 * 1024 * 1024 {
            status = String(localized: "The attachments exceed the 25 MB limit")
            return
        }
        let mime = UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
        attachments.append(ComposeAttachment(name: url.lastPathComponent, mimeType: mime, data: data))
    }

    private func improveDraft() {
        sending = true
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("ai.improve", params: [
                    "draft": bodyText, "instruction": "Improve clarity and tone",
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    sending = false
                    if let t = result?["text"] as? String { bodyText = t }
                }
            } catch {
                DispatchQueue.main.async {
                    sending = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func draftReplyAI() {
        guard let msg = replyTo else { return }
        sending = true
        status = String(localized: "Drafting…")
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call("ai.draftReply", params: [
                    "from": msg.sender,
                    "subject": msg.subject,
                    "body": msg.text,
                    "instruction": "",
                ]) as? [String: Any]
                DispatchQueue.main.async {
                    sending = false
                    if let t = (result?["reply"] as? String) ?? (result?["text"] as? String) {
                        bodyText = t + (bodyText.isEmpty ? "" : "\n\n" + bodyText)
                        status = ""
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    sending = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func save(send: Bool) {
        guard let acc = state.selectedAccount else { return }
        sending = true
        status = send ? String(localized: "Sending…") : String(localized: "Saving…")
        let outHTML = htmlBody.trimmingCharacters(in: .whitespacesAndNewlines)
        var params: [String: Any] = [
            "account": acc,
            "fromAddressId": fromAddressId,
            "to": to, "cc": cc, "bcc": bcc, "subject": subject,
            "body": bodyText, "html": outHTML,
            "action": action,
            "parentId": replyTo?.id ?? "",
            "deliveryTime": showSchedule ? scheduleAt : "",
            "signExternal": state.accountE2E && signExternal,
            "attachPublicKey": state.accountE2E && attachPublicKey,
        ]
        if !draftID.isEmpty {
            params["id"] = draftID
        }
        if !attachments.isEmpty {
            params["attachments"] = attachments.map { att -> [String: String] in
                [
                    "name": att.name,
                    "mimeType": att.mimeType,
                    "data": att.data.base64EncodedString(),
                ]
            }
        }
        if send && state.accountE2E {
            prepareSendWithEncryptionCheck(params: params)
            return
        }
        if send {
            finishSend(params: params)
            return
        }
        performBridge(method: "mail.saveDraft", params: params, isSend: false)
    }

    private func prepareSendWithEncryptionCheck(params: [String: Any]) {
        guard let acc = state.selectedAccount else { return }
        let all = [to, cc, bcc].joined(separator: ", ")
        DispatchQueue.global().async {
            let modes = (try? Bridge.call("mail.planEncryption", params: [
                "account": acc, "to": all,
            ]) as? [[String: Any]]) ?? []
            let secure = encryptionSecure(modes)
            let parts = modes.map { row -> String in
                let addr = row["address"] as? String ?? ""
                let scheme = row["scheme"] as? String ?? "clear"
                return "\(addr): \(scheme)"
            }.joined(separator: " · ")
            DispatchQueue.main.async {
                security = parts
                if secure {
                    finishSend(params: params)
                    return
                }
                if requireEncryption {
                    sending = false
                    status = ""
                    showEncryptBlock = true
                    return
                }
                pendingSendParams = params
                showEncryptWarn = true
            }
        }
    }

    private func finishSend(params: [String: Any]) {
        var params = params
        pendingSendParams = nil
        if showSchedule {
            performBridge(method: "mail.send", params: params, isSend: true)
            return
        }
        let delay = state.sendDelaySeconds
        if delay > 0 {
            DispatchQueue.global().async {
                do {
                    let result = try Bridge.call("mail.saveDraft", params: params) as? [String: Any]
                    DispatchQueue.main.async {
                        sending = false
                        if let id = result?["id"] as? String, !id.isEmpty {
                            draftID = id
                            params["id"] = id
                        }
                        dismiss()
                        state.beginUndoSend(params: params, delay: delay, restore: [
                            "to": to, "cc": cc, "bcc": bcc, "subject": subject,
                            "body": bodyText, "html": htmlBody,
                            "fromAddressId": fromAddressId,
                            "id": draftID,
                            "action": action,
                            "signExternal": signExternal,
                        ])
                    }
                } catch {
                    DispatchQueue.main.async {
                        sending = false
                        status = error.localizedDescription
                    }
                }
            }
            return
        }
        performBridge(method: "mail.send", params: params, isSend: true)
    }

    private func performBridge(method: String, params: [String: Any], isSend: Bool) {
        DispatchQueue.global().async {
            do {
                let result = try Bridge.call(method, params: params) as? [String: Any]
                DispatchQueue.main.async {
                    sending = false
                    if let id = result?["id"] as? String, !id.isEmpty {
                        draftID = id
                    }
                    if isSend {
                        if state.handleSendResult(result) {
                            dismiss()
                            return
                        }
                        state.status = String(localized: "Sent")
                        dismiss()
                        state.reloadMessages()
                    } else {
                        status = String(localized: "Draft saved")
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    sending = false
                    status = error.localizedDescription
                }
            }
        }
    }
}
