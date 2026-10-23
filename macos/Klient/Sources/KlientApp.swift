import SwiftUI
import AppKit

@main
struct KlientApp: App {
    @StateObject private var state = AppState()
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
                .id(state.localeEpoch)
                .frame(minWidth: 1080, minHeight: 640)
                .onAppear {
                    appDelegate.state = state
                    // Defer so the first SwiftUI frame paints before Go/Proton work.
                    DispatchQueue.main.async { state.start() }
                }
        }
        .defaultSize(width: 1280, height: 800)
        .windowToolbarStyle(.unified)
        .commands {
            CommandGroup(replacing: .newItem) {
                Button(String(localized: "New Message")) { state.newMessage() }
                    .keyboardShortcut("n", modifiers: .command)
            }
            CommandGroup(replacing: .appSettings) {
                Button(String(localized: "Settings…")) { KlientSettings.open() }
                    .keyboardShortcut(",", modifiers: .command)
            }
            CommandGroup(after: .appInfo) {
                Button(String(localized: "Check for Updates…")) { state.checkForUpdates() }
            }
            CommandGroup(replacing: .help) {
                Button(String(localized: "Klient Help")) { state.openHelp() }
                    .keyboardShortcut("?", modifiers: .command)
                Button(String(localized: "Outbox")) { state.showOutbox = true }
            }
            CommandMenu(String(localized: "Mailbox")) {
                Button(String(localized: "Proton Sign In…")) { state.showLogin = true }
                Button(String(localized: "Add IMAP Account…")) { state.showAddAccount = true }
                Button(String(localized: "Log Out")) { state.logout() }
                Divider()
                Button(String(localized: "Contacts…")) { state.showContacts = true }
                Button(String(localized: "Automatic Reply…")) { state.showAutoReply = true }
                Button(String(localized: "Settings…")) { KlientSettings.open() }
                    .keyboardShortcut(",", modifiers: .command)
            }
            CommandMenu(String(localized: "Message")) {
                Button(String(localized: "Reply")) { state.reply(all: false) }
                    .keyboardShortcut("r", modifiers: .command)
                Button(String(localized: "Reply All")) { state.reply(all: true) }
                    .keyboardShortcut("r", modifiers: [.command, .shift])
                Button(String(localized: "Forward")) { state.forward() }
                    .keyboardShortcut("f", modifiers: [.command, .shift])
                Divider()
                Button(String(localized: "Archive")) { state.moveSelected(to: state.labels.archive) }
                Button(String(localized: "Trash")) { state.moveSelected(to: state.labels.trash) }
                Button(String(localized: "Mark as read")) { state.markRead() }
                Button(String(localized: "Mark Unread")) { state.markUnread() }
                Divider()
                Button(String(localized: "Print…")) { state.requestPrint() }
                    .keyboardShortcut("p", modifiers: .command)
                Button(String(localized: "Unsubscribe")) { state.unsubscribeOpen() }
                Divider()
                Button(String(localized: "Apply Rules to Inbox")) { state.applyRulesToInbox() }
                if let email = state.openMessage?.senderEmail, !email.isEmpty {
                    Button(String(localized: "Create a Rule for the Sender…")) {
                        state.createRuleFromSender(email)
                    }
                }
            }
            CommandMenu("AI") {
                Button(String(localized: "Ask Your Mail…")) { state.showAskAI = true }
                Button(String(localized: "Ask about this mail")) { state.askAboutOpenMail() }
                Button(String(localized: "Morning Overview")) { state.runDigest() }
                Button(String(localized: "Summarize Message")) { state.summarizeOpen() }
                Button(String(localized: "Draft Reply with AI")) { state.draftReplyWithAI() }
                Button(String(localized: "Suggest Label (AI)")) { state.suggestLabelForOpen() }
                Button(String(localized: "Scan Inbox for Spam")) { state.scanSpam() }
                Divider()
                Button(String(localized: "Install / Update Ollama")) { state.installOllama() }
            }
            CommandMenu("Klient") {
                Button(String(localized: "About Klient")) { state.showAbout = true }
                Button(String(localized: "What's New")) { state.openWhatsNew() }
                Button(String(localized: "Keyboard Shortcuts")) { state.showShortcuts = true }
                Divider()
                Button(String(localized: "Test Keyring")) { state.keyringSmoke() }
                Button(String(localized: "Set as Default Mail App")) { state.registerMailtoDefault() }
            }
        }

        Settings {
            PreferencesView()
                .environmentObject(state)
                .environment(\.locale, state.uiLocale)
                .id(state.localeEpoch)
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    weak var state: AppState?

    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls where url.scheme?.lowercased() == "mailto" {
            Task { @MainActor in
                state?.pendingMailto = url.absoluteString
                let path = String(url.absoluteString.dropFirst("mailto:".count))
                let email = path.split(separator: "?").first.map(String.init) ?? path
                state?.composePrefillTo = email.removingPercentEncoding
                state?.composeReply = nil
                state?.composeAction = 0
                state?.showCompose = true
            }
        }
    }

    func applicationWillFinishLaunching(_ notification: Notification) {
        // Apply Preferences → Language before Bundle resolves String(localized:) / cs.lproj.
        KlientLocale.applyFromConfigFile()
        NSApp.setActivationPolicy(.regular)
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        true
    }
}
