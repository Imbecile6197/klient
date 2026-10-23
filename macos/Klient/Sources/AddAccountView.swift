import SwiftUI

struct AddAccountView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var mode = 0 // 0 proton, 1 imap
    @State private var email = ""
    @State private var password = ""
    @State private var name = ""
    @State private var imapHost = ""
    @State private var imapPort = 993
    @State private var smtpHost = ""
    @State private var smtpPort = 465
    @State private var kind = "imap"
    @State private var status = ""
    @State private var busy = false
    @State private var certInfo: [String: Any]?
    @State private var showCertTrust = false

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 6) {
                Text(String(localized: "Add Account"))
                    .font(.title2.weight(.semibold))
                Text(String(localized: "Connect Proton Mail or another IMAP provider."))
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(20)
            .background(.bar)

            Form {
                Section {
                    Picker(String(localized: "Service"), selection: $mode) {
                        Text("Proton Mail").tag(0)
                        Text("IMAP / Gmail / Seznam").tag(1)
                    }
                    .pickerStyle(.segmented)
                }

                if mode == 0 {
                    Section {
                        Text(String(localized: "Proton sign-in supports human verification and two-factor authentication."))
                            .foregroundStyle(.secondary)
                        Button(String(localized: "Open Proton Sign-In")) {
                            dismiss()
                            state.showLogin = true
                        }
                        .buttonStyle(.borderedProminent)
                    }
                } else {
                    Section {
                        TextField(String(localized: "Email"), text: $email)
                            .textContentType(.emailAddress)
                        SecureField(String(localized: "Password / app password"), text: $password)
                        TextField(String(localized: "Display name"), text: $name)
                    }
                    Section {
                        Button(String(localized: "Discover Servers")) { discover() }
                            .disabled(email.isEmpty || busy)
                        if !kind.isEmpty {
                            Text(kind).font(.caption).foregroundStyle(.secondary)
                        }
                        TextField(String(localized: "IMAP host"), text: $imapHost)
                        TextField(String(localized: "SMTP host"), text: $smtpHost)
                    }
                }

                if !status.isEmpty {
                    Section {
                        Text(status).font(.callout).foregroundStyle(.secondary)
                    }
                }
            }
            .formStyle(.grouped)

            HStack {
                Button(String(localized: "Close")) { dismiss() }
                Spacer()
                if mode == 1 {
                    Button(busy ? String(localized: "Connecting…") : String(localized: "Log In")) {
                        openIMAP()
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(busy || email.isEmpty || password.isEmpty || imapHost.isEmpty)
                    .keyboardShortcut(.defaultAction)
                }
            }
            .padding(16)
        }
        .frame(width: 460)
        .sheet(isPresented: $showCertTrust) {
            certTrustSheet
        }
    }

    @ViewBuilder
    private var certTrustSheet: some View {
        let info = certInfo ?? [:]
        NavigationStack {
            Form {
                Section {
                    Text(String(format: String(localized: "The certificate of the server %@ cannot be verified: %@."),
                                  info["host"] as? String ?? "?",
                                  info["reason"] as? String ?? ""))
                        .fixedSize(horizontal: false, vertical: true)
                } footer: {
                    Text(String(localized: "This happens when a certificate has expired or is self-signed, but also when someone intercepts the connection. Trust it only if you know the server and expect this certificate."))
                }
                Section {
                    LabeledContent(String(localized: "Issued to")) {
                        Text(info["issuedTo"] as? String ?? "—")
                            .textSelection(.enabled)
                    }
                    LabeledContent(String(localized: "Issued by")) {
                        Text(info["issuedBy"] as? String ?? "—")
                            .textSelection(.enabled)
                    }
                    LabeledContent("SHA-256") {
                        Text(info["fingerprintText"] as? String ?? info["fingerprint"] as? String ?? "—")
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                    }
                }
            }
            .formStyle(.grouped)
            .navigationTitle(String(localized: "Untrusted Certificate"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "Cancel")) { showCertTrust = false }
                }
                ToolbarItem(placement: .destructiveAction) {
                    Button(String(localized: "Trust This Certificate")) {
                        trustCertAndOpen()
                    }
                }
            }
        }
        .frame(width: 480, height: 420)
    }

    private func discover() {
        busy = true
        status = String(localized: "Looking up servers…")
        DispatchQueue.global().async {
            do {
                let set = try Bridge.call("imap.discover", params: ["email": email]) as? [String: Any]
                DispatchQueue.main.async {
                    busy = false
                    kind = set?["kind"] as? String ?? "imap"
                    imapHost = set?["imap_host"] as? String ?? ""
                    smtpHost = set?["smtp_host"] as? String ?? ""
                    imapPort = set?["imap_port"] as? Int ?? 993
                    smtpPort = set?["smtp_port"] as? Int ?? 465
                    name = set?["name"] as? String ?? name
                    status = String(localized: "Servers found")
                }
            } catch {
                DispatchQueue.main.async {
                    busy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func serverDict() -> [String: Any] {
        [
            "kind": kind, "email": email, "name": name,
            "username": email,
            "imap_host": imapHost, "imap_port": imapPort, "imap_security": "ssl",
            "smtp_host": smtpHost, "smtp_port": smtpPort, "smtp_security": "ssl",
        ]
    }

    private func openIMAP() {
        busy = true
        status = String(localized: "Signing in…")
        let server = serverDict()
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("imap.open", params: [
                    "server": server, "password": password,
                ])
                DispatchQueue.main.async {
                    busy = false
                    state.refreshAccounts()
                    state.selectedAccount = "imap:" + email.lowercased()
                    state.loadFoldersAndMail()
                    dismiss()
                }
            } catch let BridgeError.remote(msg, result) {
                DispatchQueue.main.async {
                    busy = false
                    if let r = result as? [String: Any],
                       (r["certError"] as? Bool) == true {
                        certInfo = r
                        showCertTrust = true
                        status = msg
                    } else {
                        status = msg
                    }
                }
            } catch {
                DispatchQueue.main.async {
                    busy = false
                    status = error.localizedDescription
                }
            }
        }
    }

    private func trustCertAndOpen() {
        guard let info = certInfo else { return }
        busy = true
        showCertTrust = false
        status = String(localized: "Signing in…")
        let server = serverDict()
        let host = info["host"] as? String ?? imapHost
        let port = (info["port"] as? Int) ?? (info["port"] as? NSNumber)?.intValue ?? imapPort
        let fp = info["fingerprint"] as? String ?? ""
        DispatchQueue.global().async {
            do {
                _ = try Bridge.call("imap.trustCert", params: [
                    "server": server,
                    "password": password,
                    "host": host,
                    "port": port,
                    "fingerprint": fp,
                ])
                DispatchQueue.main.async {
                    busy = false
                    state.refreshAccounts()
                    state.selectedAccount = "imap:" + email.lowercased()
                    state.loadFoldersAndMail()
                    dismiss()
                }
            } catch {
                DispatchQueue.main.async {
                    busy = false
                    status = error.localizedDescription
                }
            }
        }
    }
}
