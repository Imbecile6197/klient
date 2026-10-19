package imapmail

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/zalando/go-keyring"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

// fakeServer is a scripted IMAP server that can stop answering one command,
// like a connection that died behind a VPN or after a suspend.
type fakeServer struct {
	silent atomic.Value // command name the server does not answer
	slow   atomic.Bool  // NOOP answered slowly, with data coming meanwhile
	counts sync32
}

type sync32 struct{ conns, idles, logins atomic.Int32 }

func startFake(t *testing.T) (config.MailServer, *fakeServer) {
	tc, pool := selfSigned(t)
	testRootCAs, noCache = pool, true
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	keyring.MockInit()
	f := &fakeServer{}
	f.silent.Store("")
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
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
			f.counts.conns.Add(1)
			go f.serve(c)
		}
	}()
	return config.MailServer{
		Kind: "imap", Email: "jan@example.cz", Username: "jan@example.cz",
		IMAPHost: "127.0.0.1", IMAPPort: ln.Addr().(*net.TCPAddr).Port, IMAPSecurity: "ssl",
	}, f
}

func (f *fakeServer) serve(c net.Conn) {
	defer c.Close()
	w := func(s string) { c.Write([]byte(s + "\r\n")) }
	w("* OK [CAPABILITY IMAP4rev1 IDLE AUTH=PLAIN] ready")
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag, cmd := fields[0], strings.ToUpper(fields[1])
		if cmd == "UID" && len(fields) > 2 {
			cmd = "UID " + strings.ToUpper(fields[2])
		}
		switch cmd {
		case "LOGIN":
			f.counts.logins.Add(1)
		case "IDLE":
			f.counts.idles.Add(1)
		}
		if cmd == f.silent.Load().(string) {
			continue // read on, never answer
		}
		switch cmd {
		case "LOGIN":
			w(tag + " OK [CAPABILITY IMAP4rev1 IDLE] logged in")
		case "CAPABILITY":
			w("* CAPABILITY IMAP4rev1 IDLE")
			w(tag + " OK done")
		case "LIST":
			w(`* LIST () "/" INBOX`)
			w(tag + " OK done")
		case "STATUS":
			w("* STATUS INBOX (UIDNEXT 5)")
			w(tag + " OK done")
		case "SELECT", "EXAMINE":
			w("* 4 EXISTS")
			w("* OK [UIDVALIDITY 1] valid")
			w("* OK [UIDNEXT 5] next")
			w(tag + " OK [READ-WRITE] selected")
		case "NOOP":
			if f.slow.Load() {
				for range 12 {
					w("* OK still working")
					time.Sleep(100 * time.Millisecond)
				}
			}
			w(tag + " OK done")
		case "IDLE":
			w("+ idling")
			if _, err := r.ReadString('\n'); err != nil { // DONE
				return
			}
			w(tag + " OK idle done")
		case "LOGOUT":
			w("* BYE")
			w(tag + " OK bye")
			return
		default:
			w(tag + " OK done")
		}
	}
}

func shortLimits(t *testing.T) {
	lim, renew, answer := defaultLimits, idleRenew, idleAnswer
	defaultLimits = limits{dial: 500 * time.Millisecond, quiet: 300 * time.Millisecond}
	idleRenew, idleAnswer = time.Minute, 300*time.Millisecond
	t.Cleanup(func() { defaultLimits, idleRenew, idleAnswer = lim, renew, answer })
}

// A server that does not answer the start of IDLE: the watching gives the
// connection up and connects again instead of waiting for good.
func TestWatchIdleStartSilent(t *testing.T) {
	shortLimits(t)
	set, f := startFake(t)
	f.silent.Store("IDLE")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	if err := acc.Events(ctx, func(protonmail.Summary) {}, func() {}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for f.counts.idles.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := f.counts.idles.Load(); n < 2 {
		t.Fatalf("IDLE tried %d times: the watching waits for good", n)
	}
}

// A server that does not answer the login: connecting fails in time and
// counts as offline.
func TestLoginSilent(t *testing.T) {
	shortLimits(t)
	set, f := startFake(t)
	f.silent.Store("LOGIN")
	start := time.Now()
	err := Test(context.Background(), set, "tajne")
	if err == nil || !IsOffline(err) {
		t.Fatalf("err = %v, want an offline error", err)
	}
	if d := time.Since(start); d > 8*time.Second {
		t.Fatalf("took %s", d)
	}
}

// A command on the command connection that gets no answer: the connection
// is given up (and made again once), the caller gets an error in time.
func TestCommandSilent(t *testing.T) {
	shortLimits(t)
	set, f := startFake(t)
	acc, err := Open(context.Background(), set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	f.silent.Store("NOOP")
	start := time.Now()
	err = acc.with(func(c *imapclient.Client) error { return c.Noop().Wait() })
	if err == nil || !IsOffline(err) {
		t.Fatalf("err = %v, want an offline error", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("took %s", d)
	}
	if n := f.counts.logins.Load(); n < 2 {
		t.Fatalf("%d logins: the dead connection was not made again", n)
	}
	// The server answers again: the next command works.
	f.silent.Store("")
	if err := acc.with(func(c *imapclient.Client) error { return c.Noop().Wait() }); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

// A slow command that keeps receiving data is not cut off.
func TestCommandSlowButAlive(t *testing.T) {
	shortLimits(t)
	set, f := startFake(t)
	acc, err := Open(context.Background(), set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	f.slow.Store(true)
	if err := acc.with(func(c *imapclient.Client) error { return c.Noop().Wait() }); err != nil {
		t.Fatalf("slow but answering server cut off: %v", err)
	}
	if n := f.counts.logins.Load(); n != 1 {
		t.Fatalf("%d logins, want 1", n)
	}
}
