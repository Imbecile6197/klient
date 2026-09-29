package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLatestAndDownload(t *testing.T) {
	pkg := []byte("fake rpm payload")
	sum := sha256.Sum256(pkg)
	hexSum := hex.EncodeToString(sum[:])
	digest := hexSum
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.7.0","body":"notes","html_url":"%[1]s/rel","assets":[
				{"name":"klient-0.7.0-1.fc44.x86_64.rpm","browser_download_url":"%[1]s/pkg","size":%[2]d,"digest":"sha256:%[3]s"},
				{"name":"SHA256SUMS","browser_download_url":"%[1]s/sums","size":10}]}`, srv.URL, len(pkg), digest)
		case "/sums":
			fmt.Fprintf(w, "%s  klient-0.7.0-1.fc44.x86_64.rpm\n", hexSum)
		case "/pkg":
			w.Write(pkg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	ctx := context.Background()
	rel, err := Latest(ctx)
	if err != nil || rel.Version != "0.7.0" || rel.SHA256 != hexSum {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	dir := t.TempDir()
	var last int64
	path, err := Download(ctx, rel, dir, func(d, _ int64) { last = d })
	if err != nil || filepath.Base(path) != rel.Name || last != int64(len(pkg)) {
		t.Fatalf("Download = %q, %v (progress %d)", path, err, last)
	}
	// Tampered package is refused.
	os.Remove(path)
	rel.SHA256 = hex.EncodeToString(make([]byte, 32))
	if _, err := Download(ctx, rel, dir, nil); err == nil {
		t.Fatal("bad checksum accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("bad download left on disk")
	}
	// Mismatch between GitHub digest and SHA256SUMS.
	digest = hex.EncodeToString(make([]byte, 32))
	if _, err := Latest(ctx); err == nil {
		t.Fatal("checksum mismatch not detected")
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.6.10", "0.6.9", true}, {"0.6.7", "0.6.7", false}, {"0.6.7", "0.7.0", false}, {"1.0.0", "0.9.9", true}, {"0.6.8", "0.1.0-dev", true}} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%s,%s) != %v", c.a, c.b, c.want)
		}
	}
}
