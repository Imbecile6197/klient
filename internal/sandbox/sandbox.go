// Package sandbox handles what differs when Klient runs as a Flatpak: the
// application cannot write the host's autostart entries or default
// applications, and updates come from the Flatpak repository instead of the
// built-in updater.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Flatpak reports whether Klient runs inside a Flatpak sandbox.
var Flatpak = sync.OnceValue(func() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
})

// ErrDenied means the user or the desktop refused the request.
var ErrDenied = errors.New("the request was refused")

// RequestAutostart asks the Background portal to start Klient hidden after
// login (on=true) or to stop doing so. It returns whether autostart is now
// on. GNOME may ask the user the first time.
func RequestAutostart(ctx context.Context, on bool, reason string) (bool, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	// The portal answers with a Response signal on a request object whose
	// path is derived from our bus name and a token; subscribe before the
	// call so the answer cannot be missed.
	token := fmt.Sprintf("klient%d", time.Now().UnixNano())
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	path := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	); err != nil {
		return false, err
	}
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)

	opts := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"reason":       dbus.MakeVariant(reason),
		"autostart":    dbus.MakeVariant(on),
		"commandline":  dbus.MakeVariant([]string{"klient", "--background"}),
	}
	portal := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var handle dbus.ObjectPath
	if err := portal.CallWithContext(ctx, "org.freedesktop.portal.Background.RequestBackground", 0, "", opts).Store(&handle); err != nil {
		return false, err
	}
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case s := <-signals:
			if s.Name != "org.freedesktop.portal.Request.Response" || (s.Path != path && s.Path != handle) || len(s.Body) < 2 {
				continue
			}
			if code, _ := s.Body[0].(uint32); code != 0 {
				return false, ErrDenied
			}
			results, _ := s.Body[1].(map[string]dbus.Variant)
			auto, _ := results["autostart"].Value().(bool)
			return auto, nil
		}
	}
}
