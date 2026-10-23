package macbridge

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Imbecile6197/klient/docs"
	"github.com/Imbecile6197/klient/internal/i18n"
)

// helpPrepare writes embedded docs into the cache directory (parity with
// Linux help.go) and returns the file URL of the page for the UI language.
func (e *Engine) helpPrepare(params json.RawMessage) Response {
	var p struct {
		Lang string `json:"lang"`
	}
	_ = parseParams(params, &p)
	lang := p.Lang
	if lang == "" {
		lang = i18n.Lang()
	}

	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), "Library", "Caches")
	}
	dir = filepath.Join(dir, "eu.libormacak.Klient", "help")
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
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	})
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	page := docs.Page(lang)
	pagePath := filepath.Join(dir, page)
	return Response{OK: true, Result: map[string]any{
		"dir":      dir,
		"page":     page,
		"pagePath": pagePath,
		"lang":     lang,
	}}
}
