package ui

import (
	"context"
	"net/url"
	"strings"

	"github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// heightJS runs in an isolated script world (not the page's), so it works
// even though the e-mail's own scripts are disabled. It reports the document
// height so the view can be as tall as the message instead of scrolling
// inside the conversation.
const heightJS = `(function () {
  var last = 0;
  function send() {
    var b = document.body;
    if (!b) return;
    // Measure the content itself (all nodes of the body), never the
    // viewport: measuring the viewport made the view grow on every report.
    var range = document.createRange();
    range.selectNodeContents(b);
    var h = Math.ceil(range.getBoundingClientRect().bottom + window.scrollY);
    h += parseInt(getComputedStyle(b).marginBottom || 0);
    if (Math.abs(h - last) < 3) return;
    last = h;
    window.webkit.messageHandlers.height.postMessage(h);
  }
  send();
  var ro = new ResizeObserver(send);
  ro.observe(document.documentElement);
  if (document.body) ro.observe(document.body);
  window.addEventListener('load', send);
  document.addEventListener('load', send, true); // images loading later
})();`

const scriptWorld = "klient"

// newHTMLView renders a prepared (CSP-protected) HTML document. Page scripts,
// plugins and navigation are disabled; clicked links go to onLink.
func newHTMLView(doc string, onLink func(uri string)) *webkit.WebView {
	view := webkit.NewWebView()
	s := view.Settings()
	s.SetEnableJavascript(true)        // needed for our isolated height script only
	s.SetEnableJavascriptMarkup(false) // <script>, on*="" and javascript: in the mail are ignored
	s.SetEnablePageCache(false)
	s.SetEnableDeveloperExtras(false)
	s.SetAutoLoadImages(true) // what may load is decided by the CSP

	ucm := view.UserContentManager()
	ucm.RegisterScriptMessageHandler("height", scriptWorld)
	ucm.AddScript(webkit.NewUserScriptForWorld(heightJS, webkit.UserContentInjectTopFrame,
		webkit.UserScriptInjectAtDocumentEnd, scriptWorld, nil, nil))
	ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		h := int(v.ToInt32())
		if h < 40 {
			h = 40
		}
		if h > 40000 {
			h = 40000
		}
		// Ignore reports equal to the current size: they come from the
		// view resizing itself and would only make it grow step by step.
		if _, cur := view.SizeRequest(); cur == h+8 {
			return
		}
		view.SetSizeRequest(-1, h+8)
	})

	view.ConnectDecidePolicy(func(d webkit.PolicyDecisioner, t webkit.PolicyDecisionType) bool {
		if t != webkit.PolicyDecisionTypeNavigationAction && t != webkit.PolicyDecisionTypeNewWindowAction {
			return false
		}
		nd, ok := d.(*webkit.NavigationPolicyDecision)
		if !ok {
			webkit.BasePolicyDecision(d).Ignore()
			return true
		}
		action := nd.NavigationAction()
		uri := action.Request().URI()
		if t == webkit.PolicyDecisionTypeNavigationAction && uri == "about:blank" &&
			action.NavigationType() != webkit.NavigationTypeLinkClicked {
			return false // the initial load of our own document
		}
		webkit.BasePolicyDecision(d).Ignore()
		if action.NavigationType() == webkit.NavigationTypeLinkClicked || t == webkit.PolicyDecisionTypeNewWindowAction {
			onLink(uri)
		}
		return true
	})

	view.SetHExpand(true)
	view.SetSizeRequest(-1, 80)
	view.LoadHtml(doc, "about:blank")
	return view
}

// openLink handles a link clicked in a message: mailto: opens the composer,
// web links open in the default browser.
func (a *App) openLink(uri string) {
	u, err := url.Parse(uri)
	if err != nil {
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "mailto":
		a.openMailto(uri)
	case "http", "https":
		gtk.NewURILauncher(uri).Launch(context.Background(), a.gtkWindow(), nil)
	}
}

// remoteAllowed reports whether remote images are allowed for a sender.
func (a *App) remoteAllowed(sender string) bool {
	sender = strings.ToLower(sender)
	domain := ""
	if i := strings.LastIndexByte(sender, '@'); i >= 0 {
		domain = sender[i:]
	}
	for _, s := range a.cfg.RemoteContentSenders {
		s = strings.ToLower(s)
		if s == sender || (domain != "" && s == domain) {
			return true
		}
	}
	return false
}

func (a *App) allowRemoteFor(sender string) {
	if sender == "" || a.remoteAllowed(sender) {
		return
	}
	a.cfg.RemoteContentSenders = append(a.cfg.RemoteContentSenders, strings.ToLower(sender))
	a.saveConfig()
}
