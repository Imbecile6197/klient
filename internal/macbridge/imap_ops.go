package macbridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/imapmail"
	"github.com/Imbecile6197/klient/internal/secrets"
)

func (e *Engine) imapDiscover(params json.RawMessage) Response {
	var p struct {
		Email string `json:"email"`
	}
	if err := parseParams(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	set, err := imapmail.Discover(ctx, p.Email)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: set}
}

func certErrorResult(err error) (map[string]any, bool) {
	var ce *imapmail.CertError
	if !errors.As(err, &ce) || ce == nil {
		return nil, false
	}
	names := ce.Cert.DNSNames
	if len(names) == 0 {
		names = []string{ce.Cert.Subject.CommonName}
	}
	return map[string]any{
		"certError":        true,
		"host":             ce.Host,
		"port":             ce.Port,
		"fingerprint":      ce.Fingerprint,
		"fingerprintText":  ce.FingerprintText(),
		"reason":           ce.Reason(),
		"issuedTo":         strings.Join(names, ", "),
		"issuedBy":         ce.Cert.Issuer.CommonName,
		"notBefore":        ce.Cert.NotBefore.Unix(),
		"notAfter":         ce.Cert.NotAfter.Unix(),
	}, true
}

// imapTrustCert stores a certificate fingerprint for an IMAP account and retries open.
func (e *Engine) imapTrustCert(params json.RawMessage) Response {
	var p struct {
		Server      config.MailServer `json:"server"`
		Password    string            `json:"password"`
		Host        string            `json:"host"`
		Port        int               `json:"port"`
		Fingerprint string            `json:"fingerprint"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if p.Host == "" || p.Fingerprint == "" {
		return Response{OK: false, Error: "host and fingerprint required"}
	}
	key := imapmail.CertKey(p.Host, p.Port)
	if p.Server.TrustedCerts == nil {
		p.Server.TrustedCerts = map[string]string{}
	}
	p.Server.TrustedCerts[key] = p.Fingerprint

	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()
	acc, err := imapmail.Open(ctx, p.Server, p.Password)
	if err != nil {
		if res, ok := certErrorResult(err); ok {
			return Response{OK: false, Error: err.Error(), Result: res}
		}
		return Response{OK: false, Error: err.Error()}
	}
	if err := secrets.SaveMailPassword(p.Server.ID(), p.Password); err != nil {
		acc.Close()
		return Response{OK: false, Error: err.Error()}
	}
	e.mu.Lock()
	id := p.Server.ID()
	found := false
	for i := range e.cfg.MailServers {
		if e.cfg.MailServers[i].ID() == id {
			e.cfg.MailServers[i] = p.Server
			found = true
			break
		}
	}
	if !found {
		e.cfg.MailServers = append(e.cfg.MailServers, p.Server)
	}
	af := false
	for _, a := range e.cfg.Accounts {
		if a == id {
			af = true
			break
		}
	}
	if !af {
		e.cfg.Accounts = append(e.cfg.Accounts, id)
	}
	e.cfg.ActiveAccount = id
	_ = config.Save(e.cfg)
	s := e.replaceSessionLocked(acc)
	res := e.listAccountsLocked()
	e.mu.Unlock()
	e.startSessionLoops(s)
	return res
}
