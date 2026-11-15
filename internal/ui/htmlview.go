package ui

import (
	"context"
	"net/url"
	"strings"

	"github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
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

// darkJS gives a message designed for a light background dark colors, for
// the dark style. Like heightJS it runs in the isolated script world. It
// keeps each colour's hue and saturation and only flips its lightness:
// light backgrounds become dark, dark text becomes light, and colours that
// already fit (white text on a dark banner, dark backgrounds) stay as they
// are. Images are left alone. Transparent backgrounds stay transparent, so
// the window's own background shows through.
const darkJS = `(function () {
  function parse(c) {
    var m = /rgba?\(([^)]+)\)/.exec(c);
    if (!m) return null;
    var p = m[1].split(/[\s,\/]+/).filter(Boolean).map(parseFloat);
    return { r: p[0] / 255, g: p[1] / 255, b: p[2] / 255, a: p.length > 3 ? p[3] : 1 };
  }
  function hsl(c) {
    var max = Math.max(c.r, c.g, c.b), min = Math.min(c.r, c.g, c.b);
    var h = 0, s = 0, l = (max + min) / 2, d = max - min;
    if (d > 0) {
      s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
      if (max === c.r) h = (c.g - c.b) / d + (c.g < c.b ? 6 : 0);
      else if (max === c.g) h = (c.b - c.r) / d + 2;
      else h = (c.r - c.g) / d + 4;
      h *= 60;
    }
    return { h: h, s: s, l: l, a: c.a };
  }
  function css(x, l) {
    return 'hsla(' + x.h.toFixed(1) + ',' + (x.s * 100).toFixed(1) + '%,' +
      (l * 100).toFixed(1) + '%,' + x.a + ')';
  }
  var root = document.documentElement;
  root.style.setProperty('color-scheme', 'dark');
  var els = [root].concat(Array.prototype.slice.call(document.querySelectorAll('*')));
  // Read every computed style first, then write, so the page is laid out
  // only once.
  var todo = [];
  els.forEach(function (el) {
    var cs = getComputedStyle(el);
    var fg = parse(cs.color), bg = parse(cs.backgroundColor);
    todo.push([el, fg && hsl(fg), bg && bg.a > 0 ? hsl(bg) : null]);
  });
  todo.forEach(function (t) {
    var el = t[0], fg = t[1], bg = t[2];
    if (fg && fg.l < 0.6) el.style.setProperty('color', css(fg, 0.9 - fg.l * 0.45), 'important');
    if (bg && bg.l > 0.4) el.style.setProperty('background-color', css(bg, 0.1 + (1 - bg.l) * 0.3), 'important');
  });
})();`

const scriptWorld = "klient"

// newHTMLView renders a prepared (CSP-protected) HTML document. Page scripts,
// plugins and navigation are disabled; clicked links go to onLink. With dark
// set, the message gets dark colours (darkJS) on a transparent background.
func newHTMLView(doc string, onLink func(uri string), dark bool) *webkit.WebView {
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
	if dark {
		ucm.AddScript(webkit.NewUserScriptForWorld(darkJS, webkit.UserContentInjectTopFrame,
			webkit.UserScriptInjectAtDocumentEnd, scriptWorld, nil, nil))
		transparent := gdk.NewRGBA(0, 0, 0, 0)
		view.SetBackgroundColor(&transparent)
	}
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
