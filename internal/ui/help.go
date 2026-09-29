package ui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/docs"
)

// showHelp opens the user documentation in the default web browser. The
// page is embedded in the binary and written to the cache directory, so it
// always matches the running version.
func (a *App) showHelp() {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	path := filepath.Join(dir, "klient", "napoveda.html")
	if old, err := os.ReadFile(path); err != nil || !bytes.Equal(old, docs.Index) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(path, docs.Index, 0o644)
		}
		if err != nil {
			a.toast("Nápovědu se nepodařilo připravit: " + err.Error())
			return
		}
	}
	gtk.NewFileLauncher(gio.NewFileForPath(path)).Launch(context.Background(), a.gtkWindow(), nil)
}
