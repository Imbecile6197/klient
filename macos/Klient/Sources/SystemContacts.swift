import Contacts
import Foundation

/// A person with an email address, from macOS Contacts or the Klient account book.
struct ContactPerson: Identifiable, Hashable {
    enum Source: String {
        case system
        case account
    }

    var id: String { "\(source.rawValue):\(email.lowercased())" }
    let name: String
    let email: String
    let source: Source

    /// Display used in compose chips / suggestions (Mail-style).
    var formatted: String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return email }
        return "\(trimmed) <\(email)>"
    }
}

/// Native macOS Address Book access via Contacts.framework.
enum SystemContacts {
    private static let store = CNContactStore()

    static var authorizationStatus: CNAuthorizationStatus {
        CNContactStore.authorizationStatus(for: .contacts)
    }

    static var isAuthorized: Bool {
        switch authorizationStatus {
        case .authorized:
            return true
        case .denied, .restricted, .notDetermined:
            return false
        @unknown default:
            // macOS 15+ limited access still allows reading contacts the user shared
            return authorizationStatus.rawValue == 4 // .limited
        }
    }

    /// Request Contacts permission. Must be called from a context that can show the system sheet.
    static func requestAccess() async -> Bool {
        switch authorizationStatus {
        case .authorized:
            return true
        case .denied, .restricted:
            return false
        case .notDetermined:
            break
        @unknown default:
            if authorizationStatus.rawValue == 4 { return true } // .limited
        }
        do {
            return try await store.requestAccess(for: .contacts)
        } catch {
            return false
        }
    }

    /// Fetch unified contacts that have at least one email address.
    static func fetchWithEmails() -> [ContactPerson] {
        guard isAuthorized else { return [] }

        let keys: [CNKeyDescriptor] = [
            CNContactGivenNameKey as CNKeyDescriptor,
            CNContactFamilyNameKey as CNKeyDescriptor,
            CNContactOrganizationNameKey as CNKeyDescriptor,
            CNContactEmailAddressesKey as CNKeyDescriptor,
            CNContactIdentifierKey as CNKeyDescriptor,
        ]
        let request = CNContactFetchRequest(keysToFetch: keys)
        request.unifyResults = true
        request.sortOrder = .givenName

        var out: [ContactPerson] = []
        var seen = Set<String>()
        do {
            try store.enumerateContacts(with: request) { contact, _ in
                let display = displayName(for: contact)
                for labeled in contact.emailAddresses {
                    let email = (labeled.value as String)
                        .trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !email.isEmpty else { continue }
                    let key = email.lowercased()
                    guard !seen.contains(key) else { continue }
                    seen.insert(key)
                    out.append(ContactPerson(name: display, email: email, source: .system))
                }
            }
        } catch {
            return []
        }
        return out
    }

    /// Filter suggestions by the fragment currently being typed (last comma-separated token).
    static func matching(_ people: [ContactPerson], query: String, limit: Int = 12) -> [ContactPerson] {
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !q.isEmpty else { return Array(people.prefix(limit)) }
        return people
            .filter {
                $0.email.lowercased().contains(q)
                    || $0.name.lowercased().contains(q)
            }
            .prefix(limit)
            .map { $0 }
    }

    /// Replace the last comma-separated recipient token with `replacement`.
    static func replaceLastToken(in field: String, with replacement: String) -> String {
        let parts = field.split(separator: ",", omittingEmptySubsequences: false).map(String.init)
        guard !parts.isEmpty else { return replacement }
        var mutable = parts
        mutable[mutable.count - 1] = " " + replacement
        return mutable
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
            .joined(separator: ", ")
    }

    static func lastToken(in field: String) -> String {
        field.split(separator: ",", omittingEmptySubsequences: false)
            .last
            .map { String($0).trimmingCharacters(in: .whitespacesAndNewlines) }
            ?? field.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func displayName(for contact: CNContact) -> String {
        let given = contact.givenName.trimmingCharacters(in: .whitespacesAndNewlines)
        let family = contact.familyName.trimmingCharacters(in: .whitespacesAndNewlines)
        let person = [given, family].filter { !$0.isEmpty }.joined(separator: " ")
        if !person.isEmpty { return person }
        let org = contact.organizationName.trimmingCharacters(in: .whitespacesAndNewlines)
        return org
    }
}
