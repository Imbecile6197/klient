import SwiftUI
import WebKit
import AppKit

/// Full help from embedded docs (Linux help.go): browse + search, Czech when available.
struct HelpView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var pageURL: URL?
    @State private var rootDir: URL?
    @State private var errorText = ""
    @State private var searchText = ""
    @State private var canGoBack = false
    @State private var findToken = 0

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Button {
                    NotificationCenter.default.post(name: .klientHelpGoBack, object: nil)
                } label: {
                    Image(systemName: "chevron.backward")
                }
                .disabled(!canGoBack)
                .help(String(localized: "Back"))

                Text(String(localized: "Help"))
                    .font(.headline)

                Spacer()

                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(.secondary)
                    TextField(String(localized: "Search"), text: $searchText)
                        .textFieldStyle(.plain)
                        .frame(width: 180)
                        .onSubmit { findToken += 1 }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6, style: .continuous))

                Button(String(localized: "Close")) { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)

            Divider()

            if let pageURL, let rootDir {
                HelpWebView(
                    pageURL: pageURL,
                    rootDir: rootDir,
                    searchText: searchText,
                    findToken: findToken,
                    canGoBack: $canGoBack
                )
            } else if !errorText.isEmpty {
                ContentUnavailableView {
                    Label(String(localized: "Help"), systemImage: "questionmark.circle")
                } description: {
                    Text(errorText)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(minWidth: 900, minHeight: 640)
        .task { await prepare() }
    }

    private func prepare() async {
        do {
            let result = try Bridge.call("help.prepare", params: state.langParams()) as? [String: Any]
            guard let dir = result?["dir"] as? String,
                  let pagePath = result?["pagePath"] as? String else {
                errorText = String(localized: "The help could not be prepared.")
                return
            }
            rootDir = URL(fileURLWithPath: dir, isDirectory: true)
            pageURL = URL(fileURLWithPath: pagePath)
        } catch {
            errorText = String(localized: "The help could not be prepared: ") + error.localizedDescription
        }
    }
}

extension Notification.Name {
    static let klientHelpGoBack = Notification.Name("KlientHelpGoBack")
}

private struct HelpWebView: NSViewRepresentable {
    let pageURL: URL
    let rootDir: URL
    let searchText: String
    let findToken: Int
    @Binding var canGoBack: Bool

    func makeCoordinator() -> Coordinator { Coordinator(canGoBack: $canGoBack) }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = true
        let wv = WKWebView(frame: .zero, configuration: config)
        wv.navigationDelegate = context.coordinator
        wv.allowsBackForwardNavigationGestures = true
        context.coordinator.webView = wv
        wv.loadFileURL(pageURL, allowingReadAccessTo: rootDir)
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.goBack),
            name: .klientHelpGoBack,
            object: nil
        )
        return wv
    }

    func updateNSView(_ webView: WKWebView, context: Context) {
        if findToken != context.coordinator.lastFindToken {
            context.coordinator.lastFindToken = findToken
            context.coordinator.find(searchText)
        }
    }

    static func dismantleNSView(_ nsView: WKWebView, coordinator: Coordinator) {
        NotificationCenter.default.removeObserver(coordinator, name: .klientHelpGoBack, object: nil)
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var webView: WKWebView?
        var lastFindToken = 0
        private var canGoBack: Binding<Bool>

        init(canGoBack: Binding<Bool>) {
            self.canGoBack = canGoBack
        }

        @objc func goBack() {
            webView?.goBack()
        }

        func find(_ text: String) {
            guard let webView else { return }
            let q = text.trimmingCharacters(in: .whitespacesAndNewlines)
            if q.isEmpty {
                let config = WKFindConfiguration()
                webView.find("", configuration: config) { _ in }
                return
            }
            let config = WKFindConfiguration()
            config.caseSensitive = false
            config.wraps = true
            webView.find(q, configuration: config) { _ in }
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            canGoBack.wrappedValue = webView.canGoBack
        }

        func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
            canGoBack.wrappedValue = webView.canGoBack
        }

        func webView(
            _ webView: WKWebView,
            decidePolicyFor navigationAction: WKNavigationAction,
            decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
        ) {
            guard let url = navigationAction.request.url else {
                decisionHandler(.allow)
                return
            }
            let scheme = url.scheme?.lowercased() ?? ""
            if scheme == "file" {
                decisionHandler(.allow)
                return
            }
            if scheme == "http" || scheme == "https" || scheme == "mailto" {
                NSWorkspace.shared.open(url)
                decisionHandler(.cancel)
                return
            }
            decisionHandler(.cancel)
        }
    }
}
