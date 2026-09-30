package imapmail

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// CertError means a server's certificate failed verification. The user may
// trust this one certificate (by its fingerprint) for the server; any other
// invalid certificate is refused again.
type CertError struct {
	Host        string
	Port        int
	Cert        *x509.Certificate
	Fingerprint string // SHA-256 of the certificate, hex
	Err         error  // why the verification failed
}

func (e *CertError) Error() string {
	return fmt.Sprintf(i18n.T("the certificate of %s is not trusted: %s"), e.Host, e.Reason())
}

func (e *CertError) Unwrap() error { return e.Err }

// Key identifies the server in config.MailServer.TrustedCerts.
func (e *CertError) Key() string { return CertKey(e.Host, e.Port) }

// Reason explains the verification failure in words.
func (e *CertError) Reason() string {
	var inv x509.CertificateInvalidError
	var host x509.HostnameError
	var auth x509.UnknownAuthorityError
	switch {
	case errors.As(e.Err, &inv) && inv.Reason == x509.Expired:
		now := time.Now()
		if now.After(e.Cert.NotAfter) {
			return fmt.Sprintf(i18n.T("it expired on %s"), e.Cert.NotAfter.Local().Format(i18n.T("Jan 2, 2006 15:04")))
		}
		return fmt.Sprintf(i18n.T("it is valid only from %s"), e.Cert.NotBefore.Local().Format(i18n.T("Jan 2, 2006 15:04")))
	case errors.As(e.Err, &host):
		return fmt.Sprintf(i18n.T("it was issued for a different server (%s)"), strings.Join(certNames(e.Cert), ", "))
	case errors.As(e.Err, &auth):
		if e.Cert.CheckSignatureFrom(e.Cert) == nil {
			return i18n.T("it is self-signed")
		}
		return i18n.T("it was issued by an unknown certificate authority")
	}
	return e.Err.Error()
}

// FingerprintText groups the fingerprint for reading: "AB:CD:…".
func (e *CertError) FingerprintText() string {
	f := strings.ToUpper(e.Fingerprint)
	var b strings.Builder
	for i := 0; i < len(f); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(f[i:min(i+2, len(f))])
	}
	return b.String()
}

// CertKey is the "host:port" key of a trusted certificate.
func CertKey(host string, port int) string {
	return net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port))
}

func certNames(c *x509.Certificate) []string {
	if len(c.DNSNames) > 0 {
		return c.DNSNames
	}
	return []string{c.Subject.CommonName}
}

func fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// tlsConfig verifies the server certificate as usual; a certificate that
// fails is still accepted when its fingerprint is the one the user trusted
// for this server (trusted: "host:port" -> fingerprint).
func tlsConfig(host string, port int, trusted map[string]string) *tls.Config {
	pin := trusted[CertKey(host, port)]
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		// The standard verification is done below, where a failure can be
		// told apart and matched against the trusted certificate.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New(i18n.T("the server sent no certificate"))
			}
			leaf := cs.PeerCertificates[0]
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			_, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: testRootCAs, Intermediates: inter})
			if err == nil {
				return nil
			}
			fp := fingerprint(leaf)
			if pin != "" && strings.EqualFold(pin, fp) {
				return nil
			}
			return &CertError{Host: host, Port: port, Cert: leaf, Fingerprint: fp, Err: err}
		},
	}
}

// TrustCert accepts the certificate of a CertError for this account's
// connections from now on. The caller saves the settings (Settings).
func (a *Account) TrustCert(e *CertError) {
	a.pins.Lock()
	defer a.pins.Unlock()
	m := make(map[string]string, len(a.set.TrustedCerts)+1)
	for k, v := range a.set.TrustedCerts {
		m[k] = v
	}
	m[e.Key()] = e.Fingerprint
	a.set.TrustedCerts = m
}

// trusted returns the trusted certificates (the map is never modified in
// place, so it can be read without the lock afterwards).
func (a *Account) trusted() map[string]string {
	a.pins.Lock()
	defer a.pins.Unlock()
	return a.set.TrustedCerts
}
