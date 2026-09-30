package imapmail

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// newCert makes a certificate for localhost; with ca == nil it is
// self-signed.
func newCert(t *testing.T, notBefore, notAfter time.Time, ca *tls.Certificate) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  ca == nil,
		BasicConstraintsValid: true,
	}
	parent, signer := tmpl, any(key)
	if ca != nil {
		parent, signer = ca.Leaf, ca.PrivateKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// serve answers TLS handshakes with cert and returns the port.
func serve(t *testing.T, cert tls.Certificate) int {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func handshake(port int, trusted map[string]string) error {
	d := &net.Dialer{Timeout: 5 * time.Second}
	c, err := tls.DialWithDialer(d, "tcp", "127.0.0.1:"+strconv.Itoa(port), tlsConfig("localhost", port, trusted))
	if err == nil {
		c.Close()
	}
	return err
}

func TestTrustedCertificates(t *testing.T) {
	i18n.Set("en")
	now := time.Now()
	ca := newCert(t, now.Add(-time.Hour), now.Add(time.Hour), nil)
	old := testRootCAs
	testRootCAs = x509.NewCertPool()
	testRootCAs.AddCert(ca.Leaf)
	defer func() { testRootCAs = old }()

	// A valid certificate needs nothing.
	if err := handshake(serve(t, newCert(t, now.Add(-time.Hour), now.Add(time.Hour), &ca)), nil); err != nil {
		t.Fatalf("valid certificate refused: %v", err)
	}

	// An expired one is refused with a CertError…
	expired := newCert(t, now.Add(-48*time.Hour), now.Add(-24*time.Hour), &ca)
	port := serve(t, expired)
	err := handshake(port, nil)
	var ce *CertError
	if !errors.As(err, &ce) {
		t.Fatalf("expired: want CertError, got %v", err)
	}
	if !strings.Contains(ce.Reason(), "expired on") || ce.Key() != CertKey("localhost", port) {
		t.Errorf("reason %q, key %q", ce.Reason(), ce.Key())
	}
	if IsOffline(connErr(err)) {
		t.Error("a certificate problem must not count as offline")
	}
	// …and accepted once trusted.
	trusted := map[string]string{ce.Key(): ce.Fingerprint}
	if err := handshake(port, trusted); err != nil {
		t.Fatalf("trusted certificate refused: %v", err)
	}

	// A different invalid certificate on the same server is refused again.
	self := newCert(t, now.Add(-time.Hour), now.Add(time.Hour), nil)
	port2 := serve(t, self)
	err = handshake(port2, map[string]string{CertKey("localhost", port2): ce.Fingerprint})
	if !errors.As(err, &ce) || ce.Reason() != "it is self-signed" {
		t.Fatalf("changed certificate: want self-signed CertError, got %v", err)
	}
	if n := len(strings.Split(ce.FingerprintText(), ":")); n != 32 {
		t.Errorf("fingerprint text has %d groups", n)
	}
}

// The whole login: an untrusted server certificate stops Open and TestSMTP
// with a CertError (not "offline"); after trusting both, the account works.
func TestOpenWithUntrustedCertificate(t *testing.T) {
	i18n.Set("en")
	set, _, _ := startServers(t)
	trustedCAs := testRootCAs
	testRootCAs = x509.NewCertPool() // the test servers' certificate is now unknown
	defer func() { testRootCAs = trustedCAs }()
	ctx := context.Background()

	_, err := Open(ctx, set, "tajne")
	var ce *CertError
	if !errors.As(err, &ce) || IsOffline(err) {
		t.Fatalf("Open: want CertError, got %v", err)
	}
	set.TrustedCerts = map[string]string{ce.Key(): ce.Fingerprint}
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatalf("Open with the trusted certificate: %v", err)
	}
	defer acc.Close()

	err = TestSMTP(ctx, acc)
	if !errors.As(err, &ce) || ce.Port != set.SMTPPort {
		t.Fatalf("TestSMTP: want CertError for the SMTP port, got %v", err)
	}
	acc.TrustCert(ce)
	if err := TestSMTP(ctx, acc); err != nil {
		t.Fatalf("TestSMTP with the trusted certificate: %v", err)
	}
	if n := len(acc.Settings().TrustedCerts); n != 2 {
		t.Errorf("%d trusted certificates, want 2", n)
	}
}
