import AppKit

/// Menu bar extra mirroring Linux tray: unread badge + quick actions.
@MainActor
final class MenuBarController: NSObject {
    static let shared = MenuBarController()

    private var item: NSStatusItem?
    private weak var state: AppState?

    func attach(state: AppState) {
        self.state = state
        if item == nil {
            let it = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
            it.button?.image = NSImage(systemSymbolName: "envelope.fill", accessibilityDescription: "Klient")
            it.button?.imagePosition = .imageLeading
            item = it
        }
        rebuildMenu()
        NotificationCenter.default.addObserver(
            forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in self?.rebuildMenu() }
        }
    }

    func rebuildMenu() {
        guard let state, let item else { return }
        let menu = NSMenu()
        let unread = state.unreadTotal
        item.button?.title = unread > 0 ? " \(unread)" : ""
        menu.addItem(withTitle: "Klient", action: nil, keyEquivalent: "")
        menu.addItem(.separator())
        menu.addItem(withTitle: String(localized: "New Message"), action: #selector(compose), keyEquivalent: "n")
        menu.addItem(withTitle: String(localized: "Show Window"), action: #selector(showWindow), keyEquivalent: "")
        menu.addItem(.separator())
        menu.addItem(withTitle: String(localized: "Check for Updates…"), action: #selector(updates), keyEquivalent: "")
        menu.addItem(withTitle: String(localized: "Quit"), action: #selector(quit), keyEquivalent: "q")
        for it in menu.items where it.action != nil {
            it.target = self
        }
        item.menu = menu
    }

    @objc private func compose() { state?.newMessage(); NSApp.activate(ignoringOtherApps: true) }
    @objc private func showWindow() {
        NSApp.activate(ignoringOtherApps: true)
        NSApp.windows.first?.makeKeyAndOrderFront(nil)
    }
    @objc private func updates() { state?.checkForUpdates() }
    @objc private func quit() { NSApp.terminate(nil) }
}
