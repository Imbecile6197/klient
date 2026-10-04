package spam

import (
	"context"
	"net/mail"
	"net/netip"
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

func TestParseFeeds(t *testing.T) {
	var nets []listedNet
	domains := map[string]string{}

	drop := `{"cidr":"1.10.16.0/20","sblid":"SBL256894","rir":"apnic"}
{"cidr":"2a06:e480::/29","sblid":"SBL1","rir":"ripencc"}
{"type":"metadata","timestamp":1700000000,"size":2,"records":2}`
	urlIdx := map[string]string{}
	parseFeed(config.Feed{Name: "DROP", Kind: "spamhaus-drop-json"}, strings.NewReader(drop), &nets, domains, urlIdx)

	hosts := "# URLhaus\n127.0.0.1\tevil.example.com\n127.0.0.1\t203.0.113.9\n"
	parseFeed(config.Feed{Name: "URLhaus", Kind: "hostfile"}, strings.NewReader(hosts), &nets, domains, urlIdx)

	urls := "https://login.paypa1-secure.net/verify?x=1\nhttp://phish.example.org/a\n"
	parseFeed(config.Feed{Name: "OpenPhish", Kind: "url-list"}, strings.NewReader(urls), &nets, domains, urlIdx)

	b := NewBlocklists()
	b.nets, b.domains, b.urls = nets, domains, urlIdx

	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
	if got := b.CheckIP(netip.MustParseAddr("1.10.20.5")); got != "DROP" {
		t.Errorf("IPv4 in DROP range: got %q", got)
	}
	if got := b.CheckIP(netip.MustParseAddr("2a06:e481::1")); got != "DROP" {
		t.Errorf("IPv6 in DROP range: got %q", got)
	}
	if got := b.CheckIP(netip.MustParseAddr("8.8.8.8")); got != "" {
		t.Errorf("clean IP listed by %q", got)
	}
	if got := b.CheckDomain("EVIL.example.com."); got != "URLhaus" {
		t.Errorf("exact domain: got %q", got)
	}
	if got := b.CheckDomain("cdn.evil.example.com"); got != "URLhaus" {
		t.Errorf("subdomain of listed domain: got %q", got)
	}
	if got := b.CheckDomain("example.com"); got != "" {
		t.Errorf("parent of listed domain must not match, got %q", got)
	}
	// A phishing page does not block its whole domain.
	if got := b.CheckDomain("login.paypa1-secure.net"); got != "" {
		t.Errorf("url-list host must not be a domain entry, got %q", got)
	}
	if got := b.CheckLink("https://login.paypa1-secure.net/verify?x=1"); got != "OpenPhish" {
		t.Errorf("url-list page: got %q", got)
	}
	if _, ok := domains["203.0.113.9"]; ok {
		t.Error("IP literal must not be indexed as a domain")
	}
}

func TestReceivedIPs(t *testing.T) {
	h := map[string][]string{
		"Received": {
			"from mail.example.net (mail.example.net [198.51.100.7]) by mx.proton.me",
			"from internal (unknown [10.0.0.5]) by relay",
			"from v6host ([2001:db8::1]) by mx",
		},
		"Subject": {"[1.2.3.4] not a header we read"},
	}
	ips := receivedIPs(h)
	var got []string
	for _, ip := range ips {
		got = append(got, ip.String())
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "198.51.100.7") {
		t.Errorf("public IPv4 missing: %v", got)
	}
	if strings.Contains(joined, "10.0.0.5") {
		t.Errorf("private IP must be skipped: %v", got)
	}
	if strings.Contains(joined, "1.2.3.4") {
		t.Errorf("only Received headers are read: %v", got)
	}
}

func TestListed(t *testing.T) {
	m := map[string]bool{"@spam.example": true, "boss@work.example": true}
	if !listed(m, "anyone@SPAM.example") {
		t.Error("domain entry should match")
	}
	if !listed(m, "Boss@work.example") {
		t.Error("address entry should match case-insensitively")
	}
	if listed(m, "other@work.example") {
		t.Error("other address on same domain must not match")
	}
}

func TestCheckLink(t *testing.T) {
	var nets []listedNet
	domains, urls := map[string]string{}, map[string]string{}
	parseFeed(config.Feed{Name: "OpenPhish", Kind: "url-list"}, strings.NewReader(
		"https://u.to/AbCd\nhttp://Shared.Host.example/~bad/login.php\nhttps://phish.example/\nhttps://q.example/p?id=7\n"), &nets, domains, urls)
	parseFeed(config.Feed{Name: "URLhaus", Kind: "hostfile"}, strings.NewReader("127.0.0.1\tmalware.example\n"), &nets, domains, urls)
	parseFeed(config.Feed{Name: "Mine", Kind: "domain-list"}, strings.NewReader("meh.example\n"), &nets, domains, urls)
	b := NewBlocklists()
	b.nets, b.domains, b.urls = nets, domains, urls
	b.linkFeeds = map[string]bool{"OpenPhish": true, "URLhaus": true}

	for link, want := range map[string]string{
		"https://u.to/AbCd":                          "OpenPhish",
		"http://u.to/AbCd/":                          "OpenPhish", // scheme and trailing slash do not matter
		"https://u.to/AbCd?utm_source=mail#top":      "OpenPhish", // nor tracking parameters
		"https://u.to/other":                         "",          // the shortener itself is fine
		"https://shared.host.example/~bad/login.php": "OpenPhish",
		"https://shared.host.example/~good/":         "",
		"https://phish.example/any/page":             "OpenPhish", // the front page is listed: the whole host
		"https://sub.phish.example/":                 "",
		"https://q.example/p?id=7":                   "OpenPhish",
		"https://q.example/p?id=8":                   "",
		"http://cdn.malware.example/payload.exe":     "URLhaus",
		"https://meh.example/":                       "", // a domain list is evidence, not a dangerous link
		"mailto:someone@u.to":                        "",
	} {
		if got := b.CheckLink(link); got != want {
			t.Errorf("CheckLink(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestDangerousLinkIsSpam(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	b := NewBlocklists()
	b.urls = map[string]string{"evil.example/login": "OpenPhish"}
	b.domains = map[string]string{"meh.example": "Mine"}
	b.linkFeeds = map[string]bool{"OpenPhish": true}
	f := NewFilter(config.Default(), b)
	f.st.Allow["friend@example.org"] = true // even a trusted sender's account can be stolen

	msg := &protonmail.Message{Links: []string{"https://evil.example/login?u=1", "https://meh.example/"}}
	msg.Meta.ID = "m1"
	msg.Meta.Sender = &mail.Address{Address: "friend@example.org"}
	d := f.Evaluate(context.Background(), msg)
	if !d.Spam || d.Source != "dangerous-link" {
		t.Errorf("dangerous link: spam=%v source=%q", d.Spam, d.Source)
	}

	msg.Links = []string{"https://meh.example/"}
	msg.Meta.ID = "m2"
	if d := f.Evaluate(context.Background(), msg); d.Spam || d.Source != "allowlist" {
		t.Errorf("only evidence: spam=%v source=%q", d.Spam, d.Source)
	}
}
