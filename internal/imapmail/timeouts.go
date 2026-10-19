package imapmail

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/Imbecile6197/klient/internal/i18n"
)

// The IMAP library waits for the server's answers without a time limit: a
// connection that silently stopped answering (after a network change, a VPN
// reconnect or a suspend) would keep a command waiting for good. The helpers
// here put limits on that and close such a connection, so that it is made
// again.

// limits are read from defaultLimits when an account is made (tests set
// shorter ones). dial covers connecting, TLS and the login; quiet is how
// long a command may go without any data over the connection.
type limits struct {
	dial, quiet time.Duration
}

var defaultLimits = limits{dial: 30 * time.Second, quiet: 60 * time.Second}

func (a *Account) limits() limits {
	l := a.lim
	if l.dial == 0 {
		l.dial = defaultLimits.dial
	}
	if l.quiet == 0 {
		l.quiet = defaultLimits.quiet
	}
	return l
}

type noAnswerErr struct{}

func (noAnswerErr) Error() string { return i18n.T("the server stopped answering") }

var errNoAnswer error = noAnswerErr{}

// within runs f; without a result after limit it calls stop (which closes
// the connection f waits on) and reports errNoAnswer.
func within(limit time.Duration, stop func(), f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		stop()
		// The library does not always end a pending command when the
		// connection closes; do not wait for it for long.
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return errNoAnswer
	}
}

// answered runs a command on c with a fixed time limit (the watching
// connection, whose commands are short).
func answered(c *imapclient.Client, limit time.Duration, f func() error) error {
	return within(limit, func() { c.Close() }, f)
}

// activeConn remembers when data last went over the connection, in either
// direction.
type activeConn struct {
	net.Conn
	last atomic.Int64 // unix nanoseconds
}

func newActiveConn(c net.Conn) *activeConn {
	a := &activeConn{Conn: c}
	a.touch()
	return a
}

func (a *activeConn) touch() { a.last.Store(time.Now().UnixNano()) }

func (a *activeConn) Read(b []byte) (int, error) {
	n, err := a.Conn.Read(b)
	if n > 0 {
		a.touch()
	}
	return n, err
}

func (a *activeConn) Write(b []byte) (int, error) {
	n, err := a.Conn.Write(b)
	if n > 0 {
		a.touch()
	}
	return n, err
}

func (a *activeConn) quietFor() time.Duration {
	return time.Since(time.Unix(0, a.last.Load()))
}

// activity maps a client to its connection, for progressing.
var activity sync.Map // *imapclient.Client -> *activeConn

func track(c *imapclient.Client, conn *activeConn) {
	activity.Store(c, conn)
	go func() {
		<-c.Closed()
		activity.Delete(c)
	}()
}

// progressing runs f on c and closes c only when nothing has gone over the
// connection for quiet while f runs: a long download that keeps coming in,
// or a large upload, is not cut off – a connection that went silent is.
func progressing(c *imapclient.Client, quiet time.Duration, f func() error) error {
	v, ok := activity.Load(c)
	if !ok {
		return f()
	}
	conn := v.(*activeConn)
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- f() }()
	tick := time.NewTicker(max(quiet/4, 10*time.Millisecond))
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			if conn.quietFor() > quiet && time.Since(start) > quiet {
				c.Close()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
				return &netErr{errNoAnswer}
			}
		}
	}
}
