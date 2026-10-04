// Package spam implements the local spam filter: blocklists downloaded every
// UpdateInterval and an AI classifier that makes the final decision.
package spam

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
)

// Blocklists is the in-memory index of all downloaded feeds.
type Blocklists struct {
	mu      sync.RWMutex
	nets    []listedNet
	domains map[string]string // domain -> feed name
	// Pages listed by URL feeds (OpenPhish), as urlKey -> feed name. A whole
	// domain is not blocked for one page: phishing often sits on shared
	// hosting or link shorteners.
	urls map[string]string
	// Feeds whose entries are dangerous pages (phishing, malware): a link
	// to them makes a message spam whatever the AI says.
	linkFeeds map[string]bool

	LastUpdate time.Time
}

type listedNet struct {
	prefix netip.Prefix
	feed   string
}

type meta struct {
	LastUpdate time.Time `json:"last_update"`
}

func NewBlocklists() *Blocklists {
	return &Blocklists{domains: map[string]string{}, urls: map[string]string{}, linkFeeds: map[string]bool{}}
}

func dir() string { return filepath.Join(config.DataDir(), "blocklists") }

// CheckIP returns the name of the feed listing ip, or "".
func (b *Blocklists) CheckIP(ip netip.Addr) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, n := range b.nets {
		if n.prefix.Contains(ip) {
			return n.feed
		}
	}
	return ""
}

// CheckDomain checks host and each of its parent domains.
func (b *Blocklists) CheckDomain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	b.mu.RLock()
	defer b.mu.RUnlock()
	for host != "" {
		if f, ok := b.domains[host]; ok {
			return f
		}
		i := strings.IndexByte(host, '.')
		if i < 0 {
			break
		}
		host = host[i+1:]
		// Never match on a bare TLD.
		if !strings.Contains(host, ".") {
			break
		}
	}
	return ""
}

// CheckLink returns the feed listing the page a link leads to, or "": the
// exact page from a URL feed, or any page on a host of a malware host feed
// (URLhaus).
func (b *Blocklists) CheckLink(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	// Tracking parameters differ per recipient: also the page without them,
	// and the whole host when its front page is listed.
	b.mu.RLock()
	for _, k := range []string{urlKey(u, true), urlKey(u, false), strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") + "/"} {
		if f, ok := b.urls[k]; ok {
			b.mu.RUnlock()
			return f
		}
	}
	b.mu.RUnlock()
	if f := b.CheckDomain(u.Hostname()); f != "" && b.isLinkFeed(f) {
		return f
	}
	return ""
}

func (b *Blocklists) isLinkFeed(feed string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.linkFeeds[feed]
}

// urlKey normalises a URL for comparison: no scheme, user, default port or
// fragment, the host in lower case and no trailing slash.
func urlKey(u *url.URL, query bool) string {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		host += ":" + p
	}
	path := u.EscapedPath()
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	if path == "" {
		path = "/"
	}
	k := host + path
	if query && u.RawQuery != "" {
		k += "?" + u.RawQuery
	}
	return k
}

func (b *Blocklists) Stats() (nets, domains, urls int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.nets), len(b.domains), len(b.urls)
}

// LoadCached parses previously downloaded feeds from disk.
func (b *Blocklists) LoadCached(feeds []config.Feed) error {
	var m meta
	if raw, err := os.ReadFile(filepath.Join(dir(), "meta.json")); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	nets, domains, urls, linkFeeds := []listedNet{}, map[string]string{}, map[string]string{}, map[string]bool{}
	for _, f := range feeds {
		if !f.Enabled {
			continue
		}
		fh, err := os.Open(feedPath(f))
		if err != nil {
			continue
		}
		parseFeed(f, fh, &nets, domains, urls)
		fh.Close()
		if f.Kind == "hostfile" || f.Kind == "url-list" {
			linkFeeds[f.Name] = true
		}
	}
	b.mu.Lock()
	b.nets, b.domains, b.urls, b.linkFeeds, b.LastUpdate = nets, domains, urls, linkFeeds, m.LastUpdate
	b.mu.Unlock()
	return nil
}

func feedPath(f config.Feed) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, f.Name)
	return filepath.Join(dir(), name+".txt")
}

// Update downloads every enabled feed. A feed that fails keeps its previous
// cached copy.
func (b *Blocklists) Update(ctx context.Context, feeds []config.Feed) error {
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	var errs []string
	for _, f := range feeds {
		if !f.Enabled {
			continue
		}
		if err := download(ctx, client, f); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", f.Name, err))
		}
	}
	m, _ := json.Marshal(meta{LastUpdate: time.Now()})
	_ = os.WriteFile(filepath.Join(dir(), "meta.json"), m, 0o600)
	if err := b.LoadCached(feeds); err != nil {
		return err
	}
	if len(errs) > 0 {
		return fmt.Errorf(i18n.T("some feeds could not be downloaded: %s"), strings.Join(errs, "; "))
	}
	return nil
}

func download(ctx context.Context, client *http.Client, f config.Feed) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "klient-mail/0.1 (blocklist updater)")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	tmp := feedPath(f) + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// Feeds are a few MB at most; cap at 64 MB to be safe.
	if _, err := io.Copy(out, io.LimitReader(res.Body, 64<<20)); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, feedPath(f))
}

func parseFeed(f config.Feed, r io.Reader, nets *[]listedNet, domains, urls map[string]string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		switch f.Kind {
		case "spamhaus-drop-json":
			var row struct {
				CIDR string `json:"cidr"`
			}
			if json.Unmarshal([]byte(line), &row) != nil || row.CIDR == "" {
				continue
			}
			if p, err := netip.ParsePrefix(row.CIDR); err == nil {
				*nets = append(*nets, listedNet{p.Masked(), f.Name})
			}
		case "hostfile":
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				addDomain(domains, fields[1], f.Name)
			}
		case "url-list":
			if u, err := url.Parse(line); err == nil && u.Hostname() != "" {
				urls[urlKey(u, true)] = f.Name
			}
		case "domain-list":
			addDomain(domains, strings.Fields(line)[0], f.Name)
		}
	}
}

func addDomain(domains map[string]string, host, feed string) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || host == "localhost" {
		return
	}
	// IP literals in domain feeds are skipped; the domain index is by name.
	if _, err := netip.ParseAddr(host); err == nil {
		return
	}
	domains[host] = feed
}

// RunUpdater loads the cache and refreshes the feeds whenever they are older
// than cfg.UpdateInterval. It checks every 15 minutes so that a laptop waking
// from suspend catches up promptly. onUpdate is called after each attempt.
func (b *Blocklists) RunUpdater(ctx context.Context, cfg config.Config, onUpdate func(error)) {
	_ = b.LoadCached(cfg.Feeds)
	check := func() {
		if time.Since(b.LastUpdate) < cfg.UpdateInterval.Duration {
			return
		}
		err := b.Update(ctx, cfg.Feeds)
		if err != nil {
			log.Printf("blocklist update: %v", err)
		}
		if onUpdate != nil {
			onUpdate(err)
		}
	}
	check()
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}
