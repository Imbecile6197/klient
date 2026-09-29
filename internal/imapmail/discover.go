package imapmail

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/libormacak/klient/internal/config"
)

// Known services (checked first, no network needed).
var presets = []struct {
	kind    string
	domains []string
	imap    string
	smtp    string
}{
	{"seznam", []string{"seznam.cz", "email.cz", "post.cz", "spoluzaci.cz"}, "imap.seznam.cz", "smtp.seznam.cz"},
	{"gmail", []string{"gmail.com", "googlemail.com"}, "imap.gmail.com", "smtp.gmail.com"},
}

// KindForEmail guesses the service of an address ("seznam", "gmail", "imap").
func KindForEmail(email string) string {
	d := domainOf(email)
	for _, p := range presets {
		for _, pd := range p.domains {
			if d == pd {
				return p.kind
			}
		}
	}
	return "imap"
}

func domainOf(email string) string {
	_, d, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	return d
}

// Discover finds the servers of an address: known services, then Mozilla's
// ISP database (the same one Thunderbird uses; only the domain is sent), the
// domain's own autoconfig, the domain of its MX record, and finally the
// usual imap./smtp. host names.
func Discover(ctx context.Context, email string) (config.MailServer, error) {
	email = strings.TrimSpace(email)
	domain := domainOf(email)
	if domain == "" {
		return config.MailServer{}, errors.New("neplatná e-mailová adresa")
	}
	base := config.MailServer{Email: email, Username: email, Kind: KindForEmail(email)}
	for _, p := range presets {
		if p.kind == base.Kind {
			s := base
			s.IMAPHost, s.IMAPPort, s.IMAPSecurity = p.imap, 993, "ssl"
			s.SMTPHost, s.SMTPPort, s.SMTPSecurity = p.smtp, 465, "ssl"
			return s, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	urls := []string{
		"https://autoconfig.thunderbird.net/v1.1/" + domain,
		"https://autoconfig." + domain + "/mail/config-v1.1.xml?emailaddress=" + email,
		"https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml",
	}
	for _, u := range urls {
		if s, ok := fetchAutoconfig(ctx, u, base); ok {
			return s, nil
		}
	}
	// Hosted domains: look up the mail provider by MX.
	if mxs, err := net.DefaultResolver.LookupMX(ctx, domain); err == nil && len(mxs) > 0 {
		mx := strings.TrimSuffix(strings.ToLower(mxs[0].Host), ".")
		parts := strings.Split(mx, ".")
		if len(parts) >= 2 {
			mxDomain := strings.Join(parts[len(parts)-2:], ".")
			if mxDomain != domain {
				if s, ok := fetchAutoconfig(ctx, "https://autoconfig.thunderbird.net/v1.1/"+mxDomain, base); ok {
					return s, nil
				}
			}
		}
	}
	s := base
	s.IMAPHost, s.IMAPPort, s.IMAPSecurity = "imap."+domain, 993, "ssl"
	s.SMTPHost, s.SMTPPort, s.SMTPSecurity = "smtp."+domain, 465, "ssl"
	return s, fmt.Errorf("nastavení serverů pro %s se nepodařilo zjistit – zkontrolujte odhad", domain)
}

type clientConfig struct {
	Provider struct {
		Incoming []server `xml:"incomingServer"`
		Outgoing []server `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

type server struct {
	Type       string `xml:"type,attr"`
	Hostname   string `xml:"hostname"`
	Port       int    `xml:"port"`
	SocketType string `xml:"socketType"`
	Username   string `xml:"username"`
}

func fetchAutoconfig(ctx context.Context, url string, base config.MailServer) (config.MailServer, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return base, false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return base, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return base, false
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var cc clientConfig
	if xml.Unmarshal(body, &cc) != nil {
		return base, false
	}
	return parseAutoconfig(cc, base)
}

func parseAutoconfig(cc clientConfig, base config.MailServer) (config.MailServer, bool) {
	s := base
	sec := func(t string) string {
		if strings.EqualFold(t, "STARTTLS") {
			return "starttls"
		}
		if strings.EqualFold(t, "SSL") {
			return "ssl"
		}
		return "" // plain text: not supported
	}
	user := func(pattern string) string {
		local, _, _ := strings.Cut(base.Email, "@")
		switch pattern {
		case "%EMAILLOCALPART%":
			return local
		case "", "%EMAILADDRESS%":
			return base.Email
		}
		return strings.NewReplacer("%EMAILADDRESS%", base.Email, "%EMAILLOCALPART%", local, "%EMAILDOMAIN%", domainOf(base.Email)).Replace(pattern)
	}
	for _, in := range cc.Provider.Incoming {
		if in.Type == "imap" && sec(in.SocketType) != "" {
			s.IMAPHost, s.IMAPPort, s.IMAPSecurity, s.Username = in.Hostname, in.Port, sec(in.SocketType), user(in.Username)
			break
		}
	}
	for _, out := range cc.Provider.Outgoing {
		if out.Type == "smtp" && sec(out.SocketType) != "" {
			s.SMTPHost, s.SMTPPort, s.SMTPSecurity = out.Hostname, out.Port, sec(out.SocketType)
			break
		}
	}
	return s, s.IMAPHost != "" && s.SMTPHost != ""
}
