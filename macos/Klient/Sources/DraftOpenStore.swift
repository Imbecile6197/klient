import Foundation

/// Hands an opened draft from AppState to ComposeView (one-shot).
@MainActor
final class DraftOpenStore {
    static let shared = DraftOpenStore()

    private(set) var pending: [String: Any]?

    func apply(_ result: [String: Any]) {
        pending = result
    }

    func take() -> [String: Any]? {
        defer { pending = nil }
        return pending
    }
}
