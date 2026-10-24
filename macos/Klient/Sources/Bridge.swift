import Foundation

enum Bridge {
    private static var eventHandler: ((String) -> Void)?

    static func version() -> String {
        guard let c = KlientVersion() else { return "" }
        defer { KlientFree(c) }
        return String(cString: c)
    }

    static func call(_ method: String, params: Any? = nil, id: Int = 1) throws -> Any? {
        var req: [String: Any] = ["method": method, "id": id]
        if let params {
            req["params"] = params
        }
        // Invalid graphs raise NSException (not Swift Error) — check first.
        guard JSONSerialization.isValidJSONObject(req) else {
            throw BridgeError.encoding
        }
        // Still wrap: rare Foundation paths can raise despite isValidJSONObject.
        let data: Data = try catchNSException {
            try JSONSerialization.data(withJSONObject: req)
        }
        guard let s = String(data: data, encoding: .utf8) else {
            throw BridgeError.encoding
        }
        return try callJSON(s)
    }

    static func callJSON(_ req: String) throws -> Any? {
        // No process-wide lock: Go `macbridge.Call` is concurrent-safe (engine
        // mutex). Serializing every call blocked `proton.loginStart` for the
        // entire `accounts.resumeAll` (Proton unlock can take tens of seconds).
        let str: String = try catchNSException {
            guard let c = KlientCall(req) else { throw BridgeError.empty }
            defer { KlientFree(c) }
            return String(cString: c)
        }
        guard let data = str.data(using: .utf8) else {
            throw BridgeError.decoding
        }
        let obj: [String: Any] = try catchNSException {
            guard let parsed = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                throw BridgeError.decoding
            }
            return parsed
        }
        if let ok = obj["ok"] as? Bool, ok {
            return obj["result"]
        }
        throw BridgeError.remote(
            obj["error"] as? String ?? "unknown error",
            result: obj["result"]
        )
    }

    /// Converts Objective-C `NSException` into `BridgeError` so callers' `do/catch` works.
    private static func catchNSException<T>(_ body: () throws -> T) throws -> T {
        var value: T?
        var thrown: Error?
        let ex = KlientCatchException {
            do {
                value = try body()
            } catch {
                thrown = error
            }
        }
        if let ex {
            let reason = ex.reason ?? ex.name.rawValue
            throw BridgeError.remote(reason)
        }
        if let thrown { throw thrown }
        guard let value else { throw BridgeError.empty }
        return value
    }

    static func initEngine(onEvent: @escaping (String) -> Void) {
        eventHandler = onEvent
        KlientInit(bridgeEventTrampoline)
    }

    fileprivate static func deliverEvent(_ json: String) {
        // Deliver on the Go callback thread; AppState parses off-main and
        // hops to MainActor only for state updates (avoids event-storm jank).
        eventHandler?(json)
    }
}

/// C-compatible trampoline (no Swift captures).
private func bridgeEventTrampoline(_ json: UnsafePointer<CChar>?) {
    guard let json else { return }
    Bridge.deliverEvent(String(cString: json))
}

enum BridgeError: Error, LocalizedError {
    case encoding, decoding, empty
    case remote(String, result: Any? = nil)
    var errorDescription: String? {
        switch self {
        case .encoding: return "request encoding failed"
        case .decoding: return "response decoding failed"
        case .empty: return "empty response"
        case .remote(let s, _): return s
        }
    }
    /// Structured `result` from a failed bridge call (e.g. IMAP cert trust).
    var result: Any? {
        if case .remote(_, let r) = self { return r }
        return nil
    }
}
