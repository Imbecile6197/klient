package macbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/ollama"
	"github.com/Imbecile6197/klient/internal/relnotes"
	"github.com/Imbecile6197/klient/internal/update"
)

func (e *Engine) ensureOllama() *ollama.Runtime {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ollama != nil {
		return e.ollama
	}
	e.ollama = ollama.New(config.DataDir())
	return e.ollama
}

func (e *Engine) ollamaStatus(_ json.RawMessage) Response {
	r := e.ensureOllama()
	prog, mods := r.DiskUsage()
	return Response{OK: true, Result: map[string]any{
		"installed":     r.Installed(),
		"addr":          r.Addr(),
		"pending":       r.PendingRestart(),
		"program_bytes": prog,
		"models_bytes":  mods,
	}}
}

func (e *Engine) ollamaInstall(_ json.RawMessage) Response {
	r := e.ensureOllama()
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Minute)
	defer cancel()
	rel, err := ollama.Latest(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := r.Install(ctx, rel, nil); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"version": rel.Version}}
}

func (e *Engine) ollamaStart(_ json.RawMessage) Response {
	r := e.ensureOllama()
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Minute)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	// Rebuild AI client with local backend.
	e.mu.Lock()
	cfg := e.cfg
	e.ai = newAIClientWithLocal(e.ctx, cfg, r)
	e.filter.SetAI(e.ai)
	e.mu.Unlock()
	return Response{OK: true, Result: map[string]string{"addr": r.Addr()}}
}

func (e *Engine) ollamaListModels(_ json.RawMessage) Response {
	r := e.ensureOllama()
	if r.Installed() == "" {
		return Response{OK: true, Result: map[string]any{"models": []any{}}}
	}
	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	release, err := r.Acquire(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	defer release()
	list, err := ollama.NewClient(r.Addr()).List(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	models := make([]map[string]any, 0, len(list))
	for _, m := range list {
		models = append(models, map[string]any{
			"name":   m.Name,
			"size":   m.Size,
			"digest": m.Digest,
		})
	}
	return Response{OK: true, Result: map[string]any{"models": models}}
}

func (e *Engine) ollamaPull(params json.RawMessage) Response {
	var p struct {
		Model string `json:"model"`
	}
	if err := parseParams(params, &p); err != nil || strings.TrimSpace(p.Model) == "" {
		return Response{OK: false, Error: "model required"}
	}
	model := strings.TrimSpace(p.Model)
	r := e.ensureOllama()
	if r.Installed() == "" {
		return Response{OK: false, Error: i18n.T("Ollama is not installed")}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Hour)
	defer cancel()
	release, err := r.Acquire(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	defer release()
	err = ollama.NewClient(r.Addr()).Pull(ctx, model, func(status string, done, total int64) {
		frac := 0.0
		if total > 0 {
			frac = float64(done) / float64(total)
		}
		e.emit(Event{Type: "ollamaPullProgress", Payload: map[string]any{
			"model":  model,
			"status": status,
			"done":   done,
			"total":  total,
			"frac":   frac,
		}})
	})
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	e.emit(Event{Type: "ollamaPullProgress", Payload: map[string]any{
		"model":  model,
		"status": "success",
		"done":   int64(1),
		"total":  int64(1),
		"frac":   1.0,
	}})
	return Response{OK: true, Result: map[string]string{"model": model}}
}

func (e *Engine) ollamaDelete(params json.RawMessage) Response {
	var p struct {
		Model string `json:"model"`
	}
	if err := parseParams(params, &p); err != nil || strings.TrimSpace(p.Model) == "" {
		return Response{OK: false, Error: "model required"}
	}
	model := strings.TrimSpace(p.Model)
	r := e.ensureOllama()
	if r.Installed() == "" {
		return Response{OK: false, Error: i18n.T("Ollama is not installed")}
	}
	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	release, err := r.Acquire(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	defer release()
	if err := ollama.NewClient(r.Addr()).Delete(ctx, model); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]string{"model": model}}
}

func (e *Engine) updateCheck(params json.RawMessage) Response {
	var p struct {
		Lang string `json:"lang"`
	}
	_ = parseParams(params, &p)
	lang := p.Lang
	if lang == "" {
		lang = i18n.Lang()
	}

	ctx, cancel := context.WithTimeout(e.ctx, 45*time.Second)
	defer cancel()

	rel, err := update.Latest(ctx)
	e.mu.Lock()
	e.cfg.UpdateLastCheck = time.Now().Format(time.RFC3339)
	cfg := e.cfg
	_ = config.Save(cfg)
	e.mu.Unlock()
	if err != nil {
		// Still return tag/notes/page when the release has no macOS DMG yet.
		if meta, merr := latestReleaseMeta(ctx); merr == nil {
			cur := update.StripChannel(Version)
			latest := update.StripChannel(meta.Version)
			return Response{OK: true, Result: map[string]any{
				"version":     latest,
				"current":     Version,
				"newer":       update.Newer(latest, cur),
				"page":        meta.Page,
				"dmg":         "",
				"notes":       notesForLang(meta.Notes, lang),
				"installable": update.Installable(),
				"auto_update": cfg.AutoUpdate,
				"last_check":  cfg.UpdateLastCheck,
				"goos":        runtime.GOOS,
				"goarch":      runtime.GOARCH,
				"hint":        fmt.Sprintf("No macOS DMG in this release yet — see %s", meta.Page),
				"warning":     err.Error(),
			}}
		}
		return Response{OK: false, Error: err.Error()}
	}

	cur := update.StripChannel(Version)
	latest := update.StripChannel(rel.Version)
	newer := update.Newer(latest, cur)
	hint := fmt.Sprintf("Download the macOS build from %s and replace Klient.app", rel.Page)
	if rel.URL != "" {
		hint = "Download the DMG, open it, and drag Klient to Applications."
	}

	return Response{OK: true, Result: map[string]any{
		"version":     latest,
		"current":     Version,
		"newer":       newer,
		"page":        rel.Page,
		"dmg":         rel.URL,
		"dmg_name":    rel.Name,
		"dmg_size":    rel.Size,
		"has_sha256":  len(rel.SHA256) == 64,
		"notes":       notesForLang(rel.Notes, lang),
		"installable": update.Installable(),
		"auto_update": cfg.AutoUpdate,
		"last_check":  cfg.UpdateLastCheck,
		"goos":        runtime.GOOS,
		"goarch":      runtime.GOARCH,
		"hint":        hint,
	}}
}

// notesForLang picks the English or Czech section from a bilingual release body.
func notesForLang(md, lang string) string {
	blocks := relnotes.Parse(md, lang)
	if len(blocks) == 0 {
		return strings.TrimSpace(md)
	}
	var sb strings.Builder
	for i, b := range blocks {
		if i > 0 {
			sb.WriteByte('\n')
		}
		t := strings.ReplaceAll(b.Text, "**", "")
		t = strings.ReplaceAll(t, "`", "")
		if b.Bullet {
			sb.WriteString("• ")
		}
		sb.WriteString(t)
	}
	return sb.String()
}

type releaseMeta struct {
	Version, Notes, Page string
}

func latestReleaseMeta(ctx context.Context) (releaseMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+update.Repo+"/releases/latest", nil)
	if err != nil {
		return releaseMeta{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseMeta{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return releaseMeta{}, fmt.Errorf("GitHub: %s", res.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&rel); err != nil {
		return releaseMeta{}, err
	}
	return releaseMeta{
		Version: strings.TrimPrefix(rel.TagName, "v"),
		Notes:   rel.Body,
		Page:    rel.HTMLURL,
	}, nil
}

// updateDownload fetches the latest macOS DMG into ~/Downloads (verified when
// a checksum is published) and returns the local path for the UI to open.
func (e *Engine) updateDownload(_ json.RawMessage) Response {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Minute)
	defer cancel()

	rel, err := update.Latest(ctx)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if rel.URL == "" {
		return Response{OK: false, Error: "no macOS DMG in the latest release"}
	}
	if len(rel.SHA256) != 64 {
		// Without a checksum, open the browser URL instead of writing a file.
		return Response{OK: true, Result: map[string]any{
			"path": "",
			"url":  rel.URL,
			"name": rel.Name,
		}}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	path, err := update.Download(ctx, rel, dir, nil)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Result: map[string]any{
		"path":    path,
		"url":     rel.URL,
		"name":    rel.Name,
		"version": update.StripChannel(rel.Version),
	}}
}

func (e *Engine) about(_ json.RawMessage) Response {
	return Response{OK: true, Result: map[string]any{
		"name":    "Klient",
		"version": Version,
		"id":      "eu.libormacak.Klient",
		"go":      runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
		"author":  "Libor Macák",
		"email":   "jsem@libormacak.eu",
	}}
}

func releaseNotesDTO(rels []relnotes.Release) []map[string]any {
	out := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		blocks := make([]map[string]any, 0, len(r.Blocks))
		for _, b := range r.Blocks {
			blocks = append(blocks, map[string]any{"bullet": b.Bullet, "text": b.Text})
		}
		out = append(out, map[string]any{"version": r.Version, "blocks": blocks})
	}
	return out
}

// aboutReleaseNotes returns embedded notes (newest first). Optional "after"
// limits to versions newer than that; empty = current version only when
// "currentOnly" is true, else all up to Version.
func (e *Engine) aboutReleaseNotes(params json.RawMessage) Response {
	var p struct {
		After        string `json:"after"`
		CurrentOnly  bool   `json:"currentOnly"`
		Lang         string `json:"lang"`
	}
	_ = parseParams(params, &p)
	lang := p.Lang
	if lang == "" {
		lang = i18n.Lang()
	}
	after := p.After
	if p.CurrentOnly {
		rels := relnotes.Embedded(lang, "", Version, update.Newer)
		if len(rels) > 1 {
			rels = rels[:1]
		}
		return Response{OK: true, Result: map[string]any{"releases": releaseNotesDTO(rels)}}
	}
	rels := relnotes.Embedded(lang, after, Version, update.Newer)
	return Response{OK: true, Result: map[string]any{"releases": releaseNotesDTO(rels)}}
}

// aboutWhatsNew mirrors Linux checkWhatsNew: after an update returns notes
// since last_run_version and advances that field. Empty result = nothing to show.
func (e *Engine) aboutWhatsNew(params json.RawMessage) Response {
	var p struct {
		Lang string `json:"lang"`
	}
	_ = parseParams(params, &p)
	lang := p.Lang
	if lang == "" {
		lang = i18n.Lang()
	}
	if strings.Contains(Version, "dev") {
		return Response{OK: true, Result: map[string]any{"releases": []any{}, "show": false}}
	}
	e.mu.Lock()
	last := e.cfg.LastRunVersion
	accounts := len(e.cfg.Accounts)
	e.mu.Unlock()
	if last == Version {
		return Response{OK: true, Result: map[string]any{"releases": []any{}, "show": false}}
	}
	var rels []relnotes.Release
	switch {
	case last == "" && accounts == 0:
		// First start ever — remember version, show nothing.
	case last == "":
		rels = relnotes.Embedded(lang, "", Version, update.Newer)
		if len(rels) > 1 {
			rels = rels[:1]
		}
	case update.Newer(Version, last):
		rels = relnotes.Embedded(lang, last, Version, update.Newer)
	}
	e.mu.Lock()
	e.cfg.LastRunVersion = Version
	cfg := e.cfg
	e.mu.Unlock()
	_ = config.Save(cfg)
	return Response{OK: true, Result: map[string]any{
		"releases": releaseNotesDTO(rels),
		"show":     len(rels) > 0,
		"version":  Version,
		"lang":     lang,
	}}
}
