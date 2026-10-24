import SwiftUI
import WebKit

/// Safe HTML mail view. Prefer `bodyHTML` from Go (`mailparse.PrepareHTML` + CSP).
struct HTMLView: NSViewRepresentable, Equatable {
    /// Pre-sanitized fragment/document from the bridge (includes CSP meta).
    var bodyHTML: String = ""
    let html: String
    let textFallback: String
    var printToken: Int = 0

    static func == (lhs: HTMLView, rhs: HTMLView) -> Bool {
        lhs.bodyHTML == rhs.bodyHTML
            && lhs.html == rhs.html
            && lhs.textFallback == rhs.textFallback
            && lhs.printToken == rhs.printToken
    }

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
        // Compare inputs first — avoid rebuilding a multi-100KB document string
        // on every SwiftUI body pass (spam banner, summary, status, etc.).
        let sameContent =
            context.coordinator.lastBodyHTML == bodyHTML
            && context.coordinator.lastHTMLSrc == html
            && context.coordinator.lastText == textFallback
        if !sameContent {
            context.coordinator.lastBodyHTML = bodyHTML
            context.coordinator.lastHTMLSrc = html
            context.coordinator.lastText = textFallback
            let body = buildDocument()
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
            // PrepareHTML = CSP metas + baseStyle + fragment. Keep CSP in <head>
            // (http-equiv is ignored if left inside the body wrap) and avoid a
            // second nested style/CSP shell around the same payload.
            let (headExtra, fragment) = Self.splitPrepared(bodyHTML)
            return """
            <!DOCTYPE html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <meta name="color-scheme" content="light dark">
            <base target="_blank">
            \(headExtra)
            <style>
            html, body { margin: 0; padding: 0; background: transparent; color: CanvasText;
              font: 15px/1.55 -apple-system, BlinkMacSystemFont, "SF Pro Text", "Helvetica Neue", sans-serif; }
            .wrap { padding: 20px 24px 32px; max-width: 72ch; }
            img { max-width: 100%; height: auto; }
            a { color: LinkText; }
            </style></head><body><div class="wrap">\(fragment)</div></body></html>
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

    /// Split Go PrepareHTML output into head extras (CSP + style) and body fragment.
    private static func splitPrepared(_ prepared: String) -> (String, String) {
        if let range = prepared.range(of: "</style>") {
            return (String(prepared[..<range.upperBound]), String(prepared[range.upperBound...]))
        }
        return ("", prepared)
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var lastHTML = ""
        var lastBodyHTML = ""
        var lastHTMLSrc = ""
        var lastText = ""
        var lastPrintToken = 0

        func webView(_ webView: WKWebView,
                     decidePolicyFor navigationAction: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            if navigationAction.navigationType == .linkActivated,
               let url = navigationAction.request.url {
                NSWorkspace.shared.open(url)
                decisionHandler(.cancel)
                return
            }
            decisionHandler(.allow)
        }
    }
}
