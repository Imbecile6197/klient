// Package update finds new Klient releases on GitHub, downloads the RPM or
// DEB package (the kind Klient was installed from) and verifies its checksum.
// Installing it needs root and is done by the UI through pkexec.
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
	"sync"

	"github.com/Imbecile6197/klient/internal/i18n"
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
	Name    string // package file name
	URL     string // package download URL
	Size    int64
	SHA256  string // hex
}

// Package formats of the release assets.
const (
	RPM = "rpm" // Fedora: klient-VERSION-1.fcNN.x86_64.rpm
	DEB = "deb" // Debian, Ubuntu: klient_VERSION_amd64.deb
)

// Format is the kind of package this Klient was installed from, or "" when it
// was not installed from a package. Replaced in tests.
var Format = sync.OnceValue(func() string {
	if exe, err := os.Executable(); err != nil || exe != "/usr/bin/klient" {
		return ""
	}
	if exec.Command("rpm", "-q", "klient").Run() == nil {
		return RPM
	}
	out, err := exec.Command("dpkg-query", "-W", "-f=${db:Status-Status}", "klient").Output()
	if err == nil && strings.TrimSpace(string(out)) == "installed" {
		return DEB
	}
	return ""
})

// asset reports whether a release file is the package of the given format.
// The copies with a fixed name (klient.x86_64.rpm, klient_amd64.deb) serve
// only as stable links for the first installation.
func asset(name, format string) bool {
	if format == DEB {
		return strings.HasPrefix(name, "klient_") && strings.HasSuffix(name, "_amd64.deb") && name != "klient_amd64.deb"
	}
	return strings.HasPrefix(name, "klient-") && strings.HasSuffix(name, ".x86_64.rpm")
}

// Latest asks GitHub for the newest release and its package in the format
// Klient was installed from (RPM when it was not installed from a package).
func Latest(ctx context.Context) (Release, error) {
	format := Format()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return Release{}, errors.New(i18n.T("there is no release on GitHub yet"))
	}
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf(i18n.T("GitHub responded with %s"), res.Status)
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
		case asset(a.Name, format):
			out.Name, out.URL, out.Size = a.Name, a.URL, a.Size
			out.SHA256 = strings.TrimPrefix(a.Digest, "sha256:")
		case a.Name == "SHA256SUMS":
			sums = a.URL
		}
	}
	if out.URL == "" {
		return Release{}, fmt.Errorf(i18n.T("release %s does not contain a package for this system"), rel.TagName)
	}
	// The published checksum list must agree with GitHub's own digest.
	if sums != "" {
		sum, err := sumFor(ctx, sums, out.Name)
		if err != nil {
			return Release{}, err
		}
		if out.SHA256 != "" && !strings.EqualFold(out.SHA256, sum) {
			return Release{}, errors.New(i18n.T("the release checksums do not match"))
		}
		out.SHA256 = sum
	}
	if len(out.SHA256) != 64 {
		return Release{}, errors.New(i18n.T("the release has no checksum – for security reasons it will not be downloaded"))
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
	return "", fmt.Errorf(i18n.T("SHA256SUMS does not contain %s"), name)
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
		return "", fmt.Errorf(i18n.T("download: %s"), res.Status)
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
		return "", errors.New(i18n.T("the downloaded package has a wrong checksum – it will not be installed"))
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

// Installable reports whether this Klient came from a package, so a new
// package can replace it. Builds run from the source tree are not.
func Installable() bool {
	return Format() != ""
}

// Install installs the downloaded package. pkexec shows the system password
// dialog; dnf or apt checks the package's dependencies.
func Install(ctx context.Context, path string) error {
	cmd := []string{"pkexec", "dnf", "install", "-y", path}
	if Format() == DEB {
		cmd = []string{"pkexec", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", path}
	}
	out, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return errors.New(i18n.T("the installation was cancelled"))
		}
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf(i18n.T("installation failed: %s"), msg)
	}
	return nil
}
