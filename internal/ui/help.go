package ui

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/Imbecile6197/klient/docs"
	"github.com/Imbecile6197/klient/internal/i18n"
)

// showHelp opens the user documentation in the default web browser, in the
// interface language. The pages are embedded in the binary and written to
// the cache directory, so they always match the running version.
func (a *App) showHelp() {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	dir = filepath.Join(dir, "klient", "help")
	err = fs.WalkDir(docs.Files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := docs.Files.ReadFile(name)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
			return nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	})
	if err != nil {
		a.toast(i18n.T("The help could not be prepared: ") + err.Error())
		return
	}
	path := filepath.Join(dir, docs.Page(i18n.Lang()))
	gtk.NewFileLauncher(gio.NewFileForPath(path)).Launch(context.Background(), a.gtkWindow(), nil)
}
