package imapmail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/Imbecile6197/klient/internal/protonmail"
)

// A server that stops answering must not stop the watching for good.
func TestAnsweredSilentServer(t *testing.T) {
	cli, srv := net.Pipe()
	go func() {
		srv.Write([]byte("* OK ready\r\n"))
		r := bufio.NewReader(srv)
		for { // read commands, never answer
			if _, err := r.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	c := imapclient.New(cli, nil)
	defer c.Close()

	start := time.Now()
	err := answered(c, 100*time.Millisecond, func() error { return c.Noop().Wait() })
	if !errors.Is(err, errNoAnswer) {
		t.Fatalf("err = %v, want errNoAnswer", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
	select {
	case <-c.Closed():
	case <-time.After(time.Second):
		t.Fatal("connection not closed")
	}
}

// freezer forwards connections to the server until freeze: from then on the
// connections open so far stay open but carry nothing, like a connection that
// died silently behind a VPN or after a suspend. New connections work.
type freezer struct {
	mu    sync.Mutex
	conns []*atomic.Bool
}

func (f *freezer) freeze() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.Store(true)
	}
}

func (f *freezer) start(t *testing.T, target string) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	pipe := func(dst, src net.Conn, frozen *atomic.Bool) {
		buf := make([]byte, 4096)
		for {
			n, err := src.Read(buf)
			if err != nil {
				return
			}
			if !frozen.Load() {
				dst.Write(buf[:n])
			}
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s, err := net.Dial("tcp", target)
			if err != nil {
				c.Close()
				continue
			}
			frozen := new(atomic.Bool)
			f.mu.Lock()
			f.conns = append(f.conns, frozen)
			f.mu.Unlock()
			t.Cleanup(func() { c.Close(); s.Close() })
			go pipe(s, c, frozen)
			go pipe(c, s, frozen)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// New mail must still reach onNew (the spam check) after the watching
// connection silently stopped answering.
func TestEventsSilentConnection(t *testing.T) {
	renew, answer := idleRenew, idleAnswer
	idleRenew, idleAnswer = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { idleRenew, idleAnswer = renew, answer })

	set, u, _ := startServers(t)
	var f freezer
	set.IMAPPort = f.start(t, net.JoinHostPort(set.IMAPHost, strconv.Itoa(set.IMAPPort)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	got := make(chan string, 4)
	if err := acc.Events(ctx, func(s protonmail.Summary) { got <- s.Subject }, func() {}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // watcher connected and idling
	f.freeze()
	appendMsg(t, u, "INBOX", hello)
	select {
	case s := <-got:
		if s != "Ahoj češtino" {
			t.Fatalf("subject %q", s)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("new mail never reached the spam check")
	}
}
