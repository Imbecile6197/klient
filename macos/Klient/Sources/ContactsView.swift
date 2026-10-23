import AppKit
import SwiftUI

struct ContactsView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var accountRows: [[String: String]] = []
    @State private var systemPeople: [ContactPerson] = []
    @State private var authStatus: CNContactsAuth = .unknown
    @State private var name = ""
    @State private var email = ""
    @State private var status = ""
    @State private var filter = ""

    private enum CNContactsAuth {
        case unknown, authorized, denied, notDetermined
    }

    var body: some View {
        VStack(spacing: 0) {
            if !accountRows.isEmpty || !systemPeople.isEmpty || authStatus == .denied {
                TextField(String(localized: "Search contacts"), text: $filter)
                    .textFieldStyle(.roundedBorder)
                    .padding(.horizontal, 12)
                    .padding(.top, 10)
                    .padding(.bottom, 6)
            }

            List {
                accountSection
                systemSection
            }
            .listStyle(.inset(alternatesRowBackgrounds: true))

            Divider()
            HStack(spacing: 10) {
                TextField(String(localized: "Name"), text: $name)
                    .textFieldStyle(.roundedBorder)
                TextField(String(localized: "Email"), text: $email)
                    .textFieldStyle(.roundedBorder)
                Button(String(localized: "Add")) { add() }
                    .buttonStyle(.borderedProminent)
                    .disabled(email.isEmpty || state.selectedAccount == nil)
            }
            .padding(12)
            if !status.isEmpty {
                Text(status)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 8)
            }
        }
        .frame(minWidth: 520, minHeight: 420)
        .navigationTitle(String(localized: "Contacts"))
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button(String(localized: "Done")) { dismiss() }
            }
        }
        .task { await reloadAll() }
    }

    @ViewBuilder
    private var accountSection: some View {
        let rows = filteredAccountRows
        Section {
            if rows.isEmpty {
                Text(String(localized: "No account contacts yet."))
                    .foregroundStyle(.secondary)
            } else {
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    contactRow(
                        name: row["name"] ?? "",
                        email: row["email"] ?? "",
                        trailing: {
                            Button(String(localized: "Write")) {
                                write(to: row["email"])
                            }
                            .controlSize(.small)
                            Button(String(localized: "Delete"), role: .destructive) {
                                delete(row["id"] ?? "")
                            }
                            .controlSize(.small)
                        }
                    )
                }
            }
        } header: {
            Text(String(localized: "Klient"))
        }
    }

    @ViewBuilder
    private var systemSection: some View {
        Section {
            switch authStatus {
            case .denied:
                VStack(alignment: .leading, spacing: 8) {
                    Text(String(localized: "Contacts access is turned off. Enable it in System Settings to see people from your Mac."))
                        .foregroundStyle(.secondary)
                    Button(String(localized: "Open System Settings")) {
                        openContactsPrivacySettings()
                    }
                }
                .padding(.vertical, 4)
            case .notDetermined, .unknown:
                VStack(alignment: .leading, spacing: 8) {
                    Text(String(localized: "Allow access to suggest recipients from macOS Contacts."))
                        .foregroundStyle(.secondary)
                    Button(String(localized: "Allow Contacts Access")) {
                        Task { await requestAndReload() }
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                }
                .padding(.vertical, 4)
            case .authorized:
                let people = filteredSystemPeople
                if people.isEmpty {
                    Text(String(localized: "No contacts with email addresses."))
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(people) { person in
                        contactRow(
                            name: person.name,
                            email: person.email,
                            trailing: {
                                Button(String(localized: "Write")) {
                                    write(to: person.email)
                                }
                                .controlSize(.small)
                                if state.selectedAccount != nil {
                                    Button(String(localized: "Import")) {
                                        importSystem(person)
                                    }
                                    .controlSize(.small)
                                }
                            }
                        )
                    }
                }
            }
        } header: {
            Text(String(localized: "Mac Contacts"))
        }
    }

    private func contactRow<Trailing: View>(
        name: String,
        email: String,
        @ViewBuilder trailing: () -> Trailing
    ) -> some View {
        HStack(spacing: 12) {
            ZStack {
                Circle()
                    .fill(KlientTheme.brand.opacity(0.2))
                    .frame(width: 32, height: 32)
                Text(String((name.isEmpty ? email : name).prefix(1)).uppercased())
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(KlientTheme.brand)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(name.isEmpty ? email : name)
                    .font(.body.weight(.medium))
                if !name.isEmpty {
                    Text(email)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            trailing()
        }
        .padding(.vertical, 2)
    }

    private var filteredAccountRows: [[String: String]] {
        let q = filter.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !q.isEmpty else { return accountRows }
        return accountRows.filter {
            ($0["name"] ?? "").lowercased().contains(q)
                || ($0["email"] ?? "").lowercased().contains(q)
        }
    }

    private var filteredSystemPeople: [ContactPerson] {
        SystemContacts.matching(systemPeople, query: filter, limit: 500)
    }

    private func write(to email: String?) {
        guard let email, !email.isEmpty else { return }
        state.composePrefillTo = email
        state.composeReply = nil
        state.composeAction = 0
        state.showCompose = true
        dismiss()
    }

    private func reloadAccount() {
        guard let acc = state.selectedAccount, acc != "unified" else {
            accountRows = []
            return
        }
        accountRows = (try? Bridge.call("contacts.list", params: ["account": acc]) as? [[String: String]]) ?? []
    }

    private func syncAuthStatus() {
        if SystemContacts.isAuthorized {
            authStatus = .authorized
            return
        }
        switch SystemContacts.authorizationStatus {
        case .denied, .restricted:
            authStatus = .denied
        case .notDetermined:
            authStatus = .notDetermined
        default:
            authStatus = .unknown
        }
    }

    private func reloadSystem() {
        syncAuthStatus()
        systemPeople = SystemContacts.isAuthorized ? SystemContacts.fetchWithEmails() : []
    }

    private func reloadAll() async {
        reloadAccount()
        syncAuthStatus()
        if authStatus == .notDetermined {
            _ = await SystemContacts.requestAccess()
        }
        reloadSystem()
    }

    private func requestAndReload() async {
        _ = await SystemContacts.requestAccess()
        reloadSystem()
        if authStatus == .denied {
            status = String(localized: "Contacts access was denied.")
        }
    }

    private func add() {
        guard let acc = state.selectedAccount, acc != "unified" else {
            status = String(localized: "Select a single account")
            return
        }
        do {
            _ = try Bridge.call("contacts.add", params: [
                "account": acc, "name": name, "email": email,
            ])
            name = ""; email = ""
            reloadAccount()
        } catch {
            status = error.localizedDescription
        }
    }

    private func importSystem(_ person: ContactPerson) {
        guard let acc = state.selectedAccount, acc != "unified" else {
            status = String(localized: "Select a single account")
            return
        }
        do {
            _ = try Bridge.call("contacts.add", params: [
                "account": acc, "name": person.name, "email": person.email,
            ])
            status = String(localized: "Imported into Klient contacts")
            reloadAccount()
        } catch {
            status = error.localizedDescription
        }
    }

    private func delete(_ id: String) {
        guard let acc = state.selectedAccount, !id.isEmpty else { return }
        _ = try? Bridge.call("contacts.delete", params: ["account": acc, "id": id])
        reloadAccount()
    }

    private func openContactsPrivacySettings() {
        if let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Contacts") {
            NSWorkspace.shared.open(url)
        }
    }
}
