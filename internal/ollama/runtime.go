// Package ollama runs a private copy of Ollama for Klient's local AI: it
// installs the official release into Klient's data directory, keeps it up to
// date and starts `ollama serve` on 127.0.0.1 only while Klient runs.
package ollama

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/Imbecile6197/klient/internal/i18n"
)

const (
	releaseAPI = "https://api.github.com/repos/ollama/ollama/releases/latest"
	assetName  = "ollama-linux-amd64.tar.zst"
)

// Runtime is the managed Ollama installation and its server process.
type Runtime struct {
	dir string // …/klient/ollama

	mu      sync.Mutex
	cmd     *exec.Cmd
	addr    string // host:port of the running server
	busy    int    // requests in flight (an update waits for 0)
	pending string // installed version waiting for a restart
}

func New(dataDir string) *Runtime {
	return &Runtime{dir: filepath.Join(dataDir, "ollama")}
}

func (r *Runtime) ModelsDir() string { return filepath.Join(r.dir, "models") }

func (r *Runtime) current() string { return filepath.Join(r.dir, "current") }

func (r *Runtime) binary() string { return filepath.Join(r.current(), "bin", "ollama") }

// Installed returns the installed version ("" if none).
func (r *Runtime) Installed() string {
	target, err := os.Readlink(r.current())
	if err != nil {
		return ""
	}
	if _, err := os.Stat(r.binary()); err != nil {
		return ""
	}
	return strings.TrimPrefix(filepath.Base(target), "v")
}

// Release is the newest published Ollama version.
type Release struct {
	Version string // without "v"
	URL     string
	Size    int64
	SHA256  string
}

// Latest asks GitHub for the newest release.
func Latest(ctx context.Context) (Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf(i18n.T("GitHub responded with %s"), res.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	out := Release{Version: strings.TrimPrefix(rel.TagName, "v")}
	var sums string
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			out.URL, out.Size = a.URL, a.Size
			out.SHA256 = strings.TrimPrefix(a.Digest, "sha256:")
		case "sha256sum.txt":
			sums = a.URL
		}
	}
	if out.URL == "" {
		return Release{}, fmt.Errorf(i18n.T("release %s does not contain %s"), rel.TagName, assetName)
	}
	// Cross-check with the published checksum list.
	if sums != "" {
		if list, err := fetchSmall(ctx, sums); err == nil {
			for _, line := range strings.Split(list, "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && strings.TrimPrefix(f[1], "./") == assetName {
					if out.SHA256 != "" && !strings.EqualFold(out.SHA256, f[0]) {
						return Release{}, errors.New(i18n.T("the checksums of the Ollama release do not match"))
					}
					out.SHA256 = f[0]
				}
			}
		}
	}
	if out.SHA256 == "" {
		return Release{}, errors.New(i18n.T("the Ollama release has no checksum"))
	}
	return out, nil
}

func fetchSmall(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return string(b), err
}

// Newer reports whether version a is newer than b ("0.34.4" > "0.34.10" is false).
func Newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscan(strings.SplitN(pa[i], "-", 2)[0], &x)
		}
		if i < len(pb) {
			fmt.Sscan(strings.SplitN(pb[i], "-", 2)[0], &y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// Progress reports downloaded bytes of total.
type Progress func(done, total int64)

// keep reports whether a file of the archive is needed. The CPU build needs
// the binary and the CPU/Vulkan backends; the NVIDIA and AMD runtimes are
// most of the archive and are skipped.
func keep(name string) bool {
	name = strings.TrimPrefix(name, "./")
	if name == "bin/ollama" {
		return true
	}
	if !strings.HasPrefix(name, "lib/ollama/") {
		return false
	}
	for _, skip := range []string{"cuda", "rocm", "mlx", "jetpack"} {
		if strings.Contains(strings.ToLower(name), skip) {
			return false
		}
	}
	return true
}

// Install downloads, verifies and unpacks a release next to the current
// one and switches to it. A running server keeps the old files until
// Restart.
func (r *Runtime) Install(ctx context.Context, rel Release, progress Progress) error {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(r.dir, "download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf(i18n.T("downloading Ollama failed: %w"), err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf(i18n.T("downloading Ollama failed: %s"), res.Status)
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 1<<20)
	last := time.Now()
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil && time.Since(last) > 300*time.Millisecond {
				progress(done, rel.Size)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf(i18n.T("downloading Ollama failed: %w"), rerr)
		}
	}
	if progress != nil {
		progress(done, rel.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, rel.SHA256) {
		return errors.New(i18n.T("the checksum of the downloaded Ollama does not match – the file was discarded"))
	}

	dest := filepath.Join(r.dir, "v"+rel.Version)
	_ = os.RemoveAll(dest)
	if err := extract(tmp, dest); err != nil {
		_ = os.RemoveAll(dest)
		return fmt.Errorf(i18n.T("unpacking Ollama failed: %w"), err)
	}
	// Sanity check before switching.
	if out, err := exec.CommandContext(ctx, filepath.Join(dest, "bin", "ollama"), "--version").CombinedOutput(); err != nil {
		_ = os.RemoveAll(dest)
		return fmt.Errorf(i18n.T("the new Ollama cannot be started: %v %s"), err, out)
	}
	link := r.current() + ".new"
	_ = os.Remove(link)
	if err := os.Symlink(filepath.Base(dest), link); err != nil {
		return err
	}
	if err := os.Rename(link, r.current()); err != nil {
		return err
	}
	r.mu.Lock()
	r.pending = rel.Version
	r.mu.Unlock()
	r.cleanup(filepath.Base(dest))
	return nil
}

// cleanup removes old versions except keep (and the one a running server
// still uses, which is removed on the next update).
func (r *Runtime) cleanup(keepDir string) {
	entries, _ := os.ReadDir(r.dir)
	r.mu.Lock()
	running := r.cmd != nil
	r.mu.Unlock()
	var old []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "v") && e.Name() != keepDir {
			old = append(old, e.Name())
		}
	}
	if running && len(old) > 0 {
		old = old[:len(old)-1] // the newest old one may still be in use
	}
	for _, name := range old {
		_ = os.RemoveAll(filepath.Join(r.dir, name))
	}
}

func extract(f *os.File, dest string) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	zr, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) || !keep(name) {
			continue
		}
		target := filepath.Join(dest, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			if strings.Contains(hdr.Linkname, "..") || filepath.IsAbs(hdr.Linkname) {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "ollama")); err != nil {
		return errors.New(i18n.T("the archive does not contain bin/ollama"))
	}
	return nil
}

// ---- Server ------------------------------------------------------------------

// Addr returns the base URL of the running server ("" if stopped).
func (r *Runtime) Addr() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		return ""
	}
	return "http://" + r.addr
}

func freePort() (string, error) {
	// Prefer a fixed port so model downloads survive restarts cleanly.
	for _, addr := range []string{"127.0.0.1:11435", "127.0.0.1:0"} {
		l, err := net.Listen("tcp", addr)
		if err == nil {
			a := l.Addr().String()
			l.Close()
			return a, nil
		}
	}
	return "", errors.New(i18n.T("no free port for Ollama"))
}

// Start runs `ollama serve` (if installed and not running) and waits until
// it answers.
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.cmd != nil {
		r.mu.Unlock()
		return nil
	}
	if r.Installed() == "" {
		r.mu.Unlock()
		return errors.New(i18n.T("Ollama is not installed"))
	}
	addr, err := freePort()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if err := os.MkdirAll(r.ModelsDir(), 0o700); err != nil {
		r.mu.Unlock()
		return err
	}
	// Resolve the symlink now: an update can replace "current" while this
	// server runs from the old directory.
	bin, _ := filepath.EvalSymlinks(r.binary())
	cmd := exec.Command(bin, "serve")
	cmd.Env = append(os.Environ(),
		"OLLAMA_HOST="+addr,
		"OLLAMA_MODELS="+r.ModelsDir(),
		"OLLAMA_KEEP_ALIVE=5m",
		"OLLAMA_NUM_PARALLEL=1",
		"OLLAMA_MAX_LOADED_MODELS=1",
		"OLLAMA_ORIGINS=",
		// Never offload prompts to Ollama's cloud models.
		"OLLAMA_NO_CLOUD=1",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	logf, _ := os.OpenFile(filepath.Join(r.dir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if logf != nil {
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf(i18n.T("Ollama cannot be started: %w"), err)
	}
	r.cmd, r.addr, r.pending = cmd, addr, ""
	r.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		if logf != nil {
			logf.Close()
		}
		r.mu.Lock()
		if r.cmd == cmd {
			r.cmd = nil
		}
		r.mu.Unlock()
	}()

	c := NewClient("http://" + addr)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Version(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	r.Stop()
	return errors.New(i18n.T("Ollama did not start (details in ~/.local/share/klient/ollama/server.log)"))
}

// Stop ends the server.
func (r *Runtime) Stop() {
	r.mu.Lock()
	cmd := r.cmd
	r.cmd = nil
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
}

// Acquire marks a request in flight; the returned func releases it. After
// an update the server restarts on the new version once it is idle.
func (r *Runtime) Acquire(ctx context.Context) (func(), error) {
	r.mu.Lock()
	restart := r.pending != "" && r.cmd != nil && r.busy == 0
	r.mu.Unlock()
	if restart {
		r.Stop()
	}
	if err := r.Start(ctx); err != nil {
		return func() {}, err
	}
	r.mu.Lock()
	r.busy++
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.busy--
		r.mu.Unlock()
	}, nil
}

// Idle reports that no request is in flight.
func (r *Runtime) Idle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy == 0
}

// PendingRestart is the version installed but not yet running.
func (r *Runtime) PendingRestart() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		return ""
	}
	return r.pending
}

// DiskUsage returns the size of the installation and of the models.
func (r *Runtime) DiskUsage() (program, models int64) {
	_ = filepath.Walk(r.dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasPrefix(p, r.ModelsDir()) {
			models += info.Size()
		} else {
			program += info.Size()
		}
		return nil
	})
	return
}
