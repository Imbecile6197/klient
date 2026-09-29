// Package update finds new Klient releases on GitHub, downloads the RPM
// package and verifies its checksum. Installing it needs root and is done by
// the UI through pkexec.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is the GitHub repository the releases come from.
const Repo = "Imbecile6197/klient"

// apiBase is replaced in tests.
var apiBase = "https://api.github.com"

// Release is the newest published version.
type Release struct {
	Version string // without the "v"
	Notes   string // release description (Markdown)
	Page    string // release web page
	Name    string // RPM file name
	URL     string // RPM download URL
	Size    int64
	SHA256  string // hex
}

// Latest asks GitHub for the newest release and its RPM package.
func Latest(ctx context.Context) (Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return Release{}, errors.New("na GitHubu zatím není žádné vydání")
	}
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub odpověděl %s", res.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&rel); err != nil {
		return Release{}, err
	}
	out := Release{Version: strings.TrimPrefix(rel.TagName, "v"), Notes: rel.Body, Page: rel.HTMLURL}
	sums := ""
	for _, a := range rel.Assets {
		switch {
		case strings.HasPrefix(a.Name, "klient-") && strings.HasSuffix(a.Name, ".x86_64.rpm"):
			out.Name, out.URL, out.Size = a.Name, a.URL, a.Size
			out.SHA256 = strings.TrimPrefix(a.Digest, "sha256:")
		case a.Name == "SHA256SUMS":
			sums = a.URL
		}
	}
	if out.URL == "" {
		return Release{}, fmt.Errorf("vydání %s neobsahuje balíček RPM", rel.TagName)
	}
	// The published checksum list must agree with GitHub's own digest.
	if sums != "" {
		sum, err := sumFor(ctx, sums, out.Name)
		if err != nil {
			return Release{}, err
		}
		if out.SHA256 != "" && !strings.EqualFold(out.SHA256, sum) {
			return Release{}, errors.New("kontrolní součty vydání nesouhlasí")
		}
		out.SHA256 = sum
	}
	if len(out.SHA256) != 64 {
		return Release{}, errors.New("vydání nemá kontrolní součet – z bezpečnostních důvodů se nestáhne")
	}
	return out, nil
}

func sumFor(ctx context.Context, url, name string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SHA256SUMS: %s", res.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS neobsahuje %s", name)
}

// Newer reports whether version a is newer than b ("0.6.10" > "0.6.9").
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

// Download fetches the package into dir and checks its SHA-256. An already
// downloaded, valid file is reused. It returns the file path.
func Download(ctx context.Context, rel Release, dir string, progress Progress) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.Base(rel.Name))
	if ok, _ := verify(path, rel.SHA256); ok {
		return path, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stahování: %s", res.Status)
	}
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				tmp.Close()
				return "", err
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, rel.Size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return "", rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, rel.SHA256) {
		return "", errors.New("stažený balíček má špatný kontrolní součet – nebude nainstalován")
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func verify(path, sum string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum), nil
}

// Installable reports whether this Klient came from the RPM package, so a
// new package can replace it. Builds run from the source tree are not.
func Installable() bool {
	exe, err := os.Executable()
	if err != nil || exe != "/usr/bin/klient" {
		return false
	}
	return exec.Command("rpm", "-q", "klient").Run() == nil
}

// Install installs the downloaded package. pkexec shows the system password
// dialog; dnf checks the package's dependencies.
func Install(ctx context.Context, path string) error {
	out, err := exec.CommandContext(ctx, "pkexec", "dnf", "install", "-y", path).CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return errors.New("instalace byla zrušena")
		}
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("instalace selhala: %s", msg)
	}
	return nil
}
