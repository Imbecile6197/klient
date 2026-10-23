import SwiftUI
import WebKit

/// Safe HTML mail view. Prefer `bodyHTML` from Go (`mailparse.PrepareHTML` + CSP).
struct HTMLView: NSViewRepresentable {
    /// Pre-sanitized fragment/document from the bridge (includes CSP meta).
    var bodyHTML: String = ""
    let html: String
    let textFallback: String
    var printToken: Int = 0

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        let wv = WKWebView(frame: .zero, configuration: config)
        wv.navigationDelegate = context.coordinator
        wv.setValue(false, forKey: "drawsBackground")
        return wv
    }

    func updateNSView(_ webView: WKWebView, context: Context) {
        let body = buildDocument()
        if context.coordinator.lastHTML != body {
            context.coordinator.lastHTML = body
            webView.loadHTMLString(body, baseURL: nil)
        }
        if printToken != context.coordinator.lastPrintToken, printToken > 0 {
            context.coordinator.lastPrintToken = printToken
            DispatchQueue.main.async {
                let op = webView.printOperation(with: NSPrintInfo.shared)
                op.showsPrintPanel = true
                op.showsProgressPanel = true
                op.runModal(for: webView.window ?? NSApp.keyWindow ?? NSWindow(),
                            delegate: nil, didRun: nil, contextInfo: nil)
            }
        }
    }

    private func buildDocument() -> String {
        if !bodyHTML.isEmpty {
            return """
            <!DOCTYPE html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <meta name="color-scheme" content="light dark">
            <base target="_blank">
            <style>
            html, body { margin: 0; padding: 0; background: transparent; color: CanvasText;
              font: 15px/1.55 -apple-system, BlinkMacSystemFont, "SF Pro Text", "Helvetica Neue", sans-serif; }
            .wrap { padding: 20px 24px 32px; max-width: 72ch; }
            img { max-width: 100%; height: auto; }
            a { color: LinkText; }
            </style></head><body><div class="wrap">\(bodyHTML)</div></body></html>
            """
        }
        if !html.isEmpty {
            return """
            <!DOCTYPE html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <meta name="color-scheme" content="light dark">
            <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:; form-action 'none'; frame-src 'none'; base-uri 'none'">
            <base target="_blank">
            <style>
            html, body { margin: 0; background: transparent; color: CanvasText;
              font: 15px/1.55 -apple-system, BlinkMacSystemFont, "SF Pro Text", sans-serif;
              word-wrap: break-word; overflow-wrap: anywhere; }
            .wrap { padding: 20px 24px 32px; max-width: 72ch; }
            img { max-width: 100%; height: auto; }
            a { color: LinkText; }
            blockquote { margin: 12px 0; padding-left: 12px; border-left: 3px solid color-mix(in srgb, CanvasText 18%, transparent); color: GrayText; }
            </style></head><body><div class="wrap">\(html)</div></body></html>
            """
        }
        let escaped = textFallback
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
        return """
        <!DOCTYPE html><html><head><meta charset="utf-8">
        <meta name="color-scheme" content="light dark">
        <style>
        html, body { margin: 0; background: transparent; color: CanvasText;
          font: 14px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
        .wrap { padding: 20px 24px; white-space: pre-wrap; word-wrap: break-word; }
        </style></head><body><div class="wrap">\(escaped)</div></body></html>
        """
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var lastHTML = ""
        var lastPrintToken = 0

        func webView(_ webView: WKWebView,
                     decidePolicyFor navigationAction: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            if navigationAction.navigationType == .linkActivated,
               let url = navigationAction.request.url {
                if url.scheme?.lowercased() == "mailto" {
                    NSWorkspace.shared.open(url)
                } else {
                    NSWorkspace.shared.open(url)
                }
                decisionHandler(.cancel)
                return
            }
            decisionHandler(.allow)
        }
    }
}
