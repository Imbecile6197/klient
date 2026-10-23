import SwiftUI
import WebKit
import AppKit

/// WKWebView contenteditable body editor with a formatting toolbar
/// (bold / italic / underline / strike / list / link / clear), matching
/// Linux richeditor as far as practical for HTML export.
struct RichTextEditor: NSViewRepresentable {
    @Binding var plainText: String
    @Binding var htmlBody: String
    var spellCheck: Bool = true

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSView {
        let container = NSView(frame: .zero)

        let toolbar = NSStackView()
        toolbar.orientation = .horizontal
        toolbar.spacing = 4
        toolbar.edgeInsets = NSEdgeInsets(top: 4, left: 12, bottom: 4, right: 12)
        toolbar.translatesAutoresizingMaskIntoConstraints = false

        let config = WKWebViewConfiguration()
        config.userContentController.add(context.coordinator, name: "klientEditor")
        let wv = WKWebView(frame: .zero, configuration: config)
        wv.setValue(false, forKey: "drawsBackground")
        wv.navigationDelegate = context.coordinator
        wv.translatesAutoresizingMaskIntoConstraints = false
        context.coordinator.webView = wv

        let buttons: [(String, String, String)] = [
            ("bold", "bold", String(localized: "Bold")),
            ("italic", "italic", String(localized: "Italic")),
            ("underline", "underline", String(localized: "Underline")),
            ("strikeThrough", "strikethrough", String(localized: "Strikethrough")),
            ("insertUnorderedList", "list.bullet", String(localized: "Bulleted List")),
            ("createLink", "link", String(localized: "Link")),
            ("removeFormat", "eraser", String(localized: "Clear Formatting")),
        ]
        for (cmd, symbol, tip) in buttons {
            let btn = NSButton()
            btn.bezelStyle = .inline
            btn.isBordered = false
            btn.image = NSImage(systemSymbolName: symbol, accessibilityDescription: tip)
            btn.toolTip = tip
            btn.target = context.coordinator
            btn.action = #selector(Coordinator.toolbarClick(_:))
            btn.identifier = NSUserInterfaceItemIdentifier(cmd)
            toolbar.addArrangedSubview(btn)
        }

        let sep = NSBox()
        sep.boxType = .separator
        sep.translatesAutoresizingMaskIntoConstraints = false

        container.addSubview(toolbar)
        container.addSubview(sep)
        container.addSubview(wv)
        NSLayoutConstraint.activate([
            toolbar.topAnchor.constraint(equalTo: container.topAnchor),
            toolbar.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            toolbar.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            toolbar.heightAnchor.constraint(equalToConstant: 30),
            sep.topAnchor.constraint(equalTo: toolbar.bottomAnchor),
            sep.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            sep.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            sep.heightAnchor.constraint(equalToConstant: 1),
            wv.topAnchor.constraint(equalTo: sep.bottomAnchor),
            wv.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            wv.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            wv.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])

        context.coordinator.loadInitial()
        return container
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        context.coordinator.parent = self
        context.coordinator.syncFromBindingIfNeeded()
    }

    final class Coordinator: NSObject, WKScriptMessageHandler, WKNavigationDelegate {
        var parent: RichTextEditor
        weak var webView: WKWebView?
        private var loaded = false
        private var lastPushedPlain = ""
        private var suppressPush = false

        init(_ parent: RichTextEditor) {
            self.parent = parent
        }

        func loadInitial() {
            let html = Self.shellHTML(
                body: escapeForContent(parent.plainText),
                seedHTML: parent.htmlBody,
                spellCheck: parent.spellCheck
            )
            webView?.loadHTMLString(html, baseURL: nil)
        }

        func syncFromBindingIfNeeded() {
            guard loaded, !suppressPush else { return }
            let plain = parent.plainText
            guard plain != lastPushedPlain else { return }
            lastPushedPlain = plain
            let js = "window.klientSetText(\(jsonString(plain)));"
            webView?.evaluateJavaScript(js, completionHandler: nil)
        }

        @objc func toolbarClick(_ sender: NSButton) {
            guard let cmd = sender.identifier?.rawValue else { return }
            if cmd == "createLink" {
                promptLink()
                return
            }
            webView?.evaluateJavaScript(
                "document.execCommand('\(cmd)', false, null); window.klientNotify();",
                completionHandler: nil
            )
        }

        private func promptLink() {
            let alert = NSAlert()
            alert.messageText = String(localized: "Link")
            alert.informativeText = String(localized: "Enter a URL (http, https or mailto)")
            alert.addButton(withTitle: String(localized: "OK"))
            alert.addButton(withTitle: String(localized: "Cancel"))
            let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 320, height: 24))
            field.stringValue = "https://"
            alert.accessoryView = field
            guard alert.runModal() == .alertFirstButtonReturn else { return }
            var url = field.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !url.isEmpty else { return }
            if !url.contains("://") && !url.lowercased().hasPrefix("mailto:") {
                url = "https://" + url
            }
            let js = "document.execCommand('createLink', false, \(jsonString(url))); window.klientNotify();"
            webView?.evaluateJavaScript(js, completionHandler: nil)
        }

        func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
            guard let body = message.body as? [String: Any] else { return }
            let plain = body["plain"] as? String ?? ""
            let html = body["html"] as? String ?? ""
            let formatted = body["formatted"] as? Bool ?? false
            DispatchQueue.main.async {
                self.suppressPush = true
                self.lastPushedPlain = plain
                self.parent.plainText = plain
                self.parent.htmlBody = formatted ? html : ""
                self.suppressPush = false
            }
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            loaded = true
            lastPushedPlain = parent.plainText
        }

        private static func shellHTML(body: String, seedHTML: String, spellCheck: Bool = true) -> String {
            let spellcheckAttr = spellCheck ? "true" : "false"
            let content: String
            if !seedHTML.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                content = seedHTML
            } else {
                content = body.replacingOccurrences(of: "\n", with: "<br>")
            }
            return """
            <!DOCTYPE html><html><head><meta charset="utf-8">
            <style>
            html, body { margin: 0; padding: 0; background: transparent; }
            #ed {
              min-height: 100vh; padding: 12px 16px; outline: none;
              font: 14px -apple-system, BlinkMacSystemFont, "Helvetica Neue", sans-serif;
              line-height: 1.45; color: CanvasText;
              word-wrap: break-word;
            }
            a { color: #0a7; }
            </style></head><body>
            <div id="ed" contenteditable="true" spellcheck="\(spellcheckAttr)">\(content)</div>
            <script>
            function klientNotify() {
              const ed = document.getElementById('ed');
              const plain = ed.innerText.replace(/\\u00a0/g, ' ');
              let html = ed.innerHTML;
              html = html.replace(/<div><br><\\/div>/gi, '<br>')
                         .replace(/<div>/gi, '<br>').replace(/<\\/div>/gi, '')
                         .replace(/<strong>/gi, '<b>').replace(/<\\/strong>/gi, '</b>')
                         .replace(/<em>/gi, '<i>').replace(/<\\/em>/gi, '</i>');
              const formatted = /<(b|i|u|s|a|ul|ol|li)\\b/i.test(html);
              window.webkit.messageHandlers.klientEditor.postMessage({
                plain: plain, html: html, formatted: formatted
              });
            }
            window.klientSetText = function(t) {
              const ed = document.getElementById('ed');
              ed.innerHTML = (t || '').replace(/&/g,'&amp;').replace(/</g,'&lt;')
                .replace(/\\n/g, '<br>');
              klientNotify();
            };
            document.getElementById('ed').addEventListener('input', klientNotify);
            document.addEventListener('keydown', function(e) {
              if (!(e.metaKey || e.ctrlKey)) return;
              const k = e.key.toLowerCase();
              if (k === 'b') { e.preventDefault(); document.execCommand('bold'); klientNotify(); }
              if (k === 'i') { e.preventDefault(); document.execCommand('italic'); klientNotify(); }
              if (k === 'u') { e.preventDefault(); document.execCommand('underline'); klientNotify(); }
            });
            </script></body></html>
            """
        }

        private func escapeForContent(_ s: String) -> String {
            s.replacingOccurrences(of: "&", with: "&amp;")
                .replacingOccurrences(of: "<", with: "&lt;")
                .replacingOccurrences(of: ">", with: "&gt;")
        }

        /// JSON-encode a string for JS. Do not pass a bare String to
        /// `JSONSerialization.data(withJSONObject:)` — that raises an
        /// uncaught `NSInvalidArgumentException` ("Invalid top-level type").
        private func jsonString(_ s: String) -> String {
            if let data = try? JSONEncoder().encode(s),
               let out = String(data: data, encoding: .utf8) {
                return out
            }
            return "\"\""
        }
    }
}
