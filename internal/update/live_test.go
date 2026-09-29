//go:build live

package update

import (
	"context"
	"testing"
)

// go test -tags live ./internal/update/ checks the real GitHub release.
func TestLiveRelease(t *testing.T) {
	ctx := context.Background()
	rel, err := Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest %s %s (%d B) sha256 %s", rel.Version, rel.Name, rel.Size, rel.SHA256)
	path, err := Download(ctx, rel, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("verified", path)
}
