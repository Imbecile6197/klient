package spam

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/libormacak/klient/internal/config"
)

func TestParseFeeds(t *testing.T) {
	var nets []listedNet
	domains := map[string]string{}

	drop := `{"cidr":"1.10.16.0/20","sblid":"SBL256894","rir":"apnic"}
{"cidr":"2a06:e480::/29","sblid":"SBL1","rir":"ripencc"}
{"type":"metadata","timestamp":1700000000,"size":2,"records":2}`
	parseFeed(config.Feed{Name: "DROP", Kind: "spamhaus-drop-json"}, strings.NewReader(drop), &nets, domains)

	hosts := "# URLhaus\n127.0.0.1\tevil.example.com\n127.0.0.1\t203.0.113.9\n"
	parseFeed(config.Feed{Name: "URLhaus", Kind: "hostfile"}, strings.NewReader(hosts), &nets, domains)

	urls := "https://login.paypa1-secure.net/verify?x=1\nhttp://phish.example.org/a\n"
	parseFeed(config.Feed{Name: "OpenPhish", Kind: "url-list"}, strings.NewReader(urls), &nets, domains)

	b := NewBlocklists()
	b.nets, b.domains = nets, domains

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
	if got := b.CheckDomain("login.paypa1-secure.net"); got != "OpenPhish" {
		t.Errorf("url-list host: got %q", got)
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
