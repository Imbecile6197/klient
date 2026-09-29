package mailparse

import (
	"strings"
	"testing"
)

func TestHasRemoteContent(t *testing.T) {
	remote := []string{
		`<img src="https://track.example/p.gif">`,
		`<img SRC='http://x/y.png'>`,
		`<td background="//cdn.example/bg.png">`,
		`<div style="background:url('https://x/y')">`,
		`<style>@import "https://x/a.css";</style>`,
	}
	for _, h := range remote {
		if !HasRemoteContent(h) {
			t.Errorf("not detected: %s", h)
		}
	}
	for _, h := range []string{`<a href="https://example.com">odkaz</a>`, `<img src="cid:abc">`, `<img src="data:image/png;base64,AA">`} {
		if HasRemoteContent(h) {
			t.Errorf("false positive: %s", h)
		}
	}
}

func TestPrepareHTML(t *testing.T) {
	out := PrepareHTML(`<p>Ahoj<img src="cid:logo@x"><img src="cid:missing"></p>`, map[string]string{"logo@x": "data:image/png;base64,AA"}, false)
	if !strings.HasPrefix(out, `<meta http-equiv="Content-Security-Policy"`) {
		t.Error("CSP must come first")
	}
	if !strings.Contains(out, `img-src data:;`) || strings.Contains(out, "https:") {
		t.Error("remote content must be blocked by default")
	}
	if !strings.Contains(out, `src="data:image/png;base64,AA"`) || !strings.Contains(out, `cid:missing`) {
		t.Errorf("cid replacement wrong: %s", out)
	}
	if !strings.Contains(PrepareHTML("x", nil, true), "img-src data: https:") {
		t.Error("remote allowed CSP missing")
	}
}

func TestParseHeaderBlock(t *testing.T) {
	raw := "Received: from a\r\nReceived: from b\r\nthis line is broken\r\nSubject: =?utf-8?q?P=C5=99ehled?=\r\nList-Unsubscribe: <mailto:u@list.example>,\r\n <https://list.example/u>\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n"
	h := ParseHeaderBlock(raw, map[string][]string{"X-Pm-Origin": {"external"}})
	if len(h["Received"]) != 2 || h["Subject"][0] != "Přehled" || h["X-Pm-Origin"][0] != "external" {
		t.Errorf("headers = %v", h)
	}
	u := ParseUnsubscribe(h)
	if u.HTTP != "https://list.example/u" || u.Mailto != "mailto:u@list.example" || !u.OneClick {
		t.Errorf("folded List-Unsubscribe not parsed: %+v", u)
	}
}

func TestParseUnsubscribe(t *testing.T) {
	h := map[string][]string{
		"List-Unsubscribe":      {"<mailto:leave@list.example?subject=unsub>, <http://insecure.example/u>, <https://list.example/u?t=1>"},
		"List-Unsubscribe-Post": {"List-Unsubscribe=One-Click"},
	}
	u := ParseUnsubscribe(h)
	if u.HTTP != "https://list.example/u?t=1" || u.Mailto != "mailto:leave@list.example?subject=unsub" || !u.OneClick {
		t.Errorf("got %+v", u)
	}
	if ParseUnsubscribe(map[string][]string{}).Any() {
		t.Error("empty headers should give nothing")
	}
}
