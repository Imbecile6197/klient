//go:build linux

package ollama

import (
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/klauspost/compress/zstd"
)

const assetName = "ollama-linux-amd64.tar.zst"

const binaryRel = "bin/ollama"

func serveSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

func openArchive(f *os.File) (io.ReadCloser, error) {
	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	return zstdReadCloser{zr}, nil
}

type zstdReadCloser struct{ *zstd.Decoder }

func (z zstdReadCloser) Close() error {
	z.Decoder.Close()
	return nil
}

// keep reports whether a file of the archive is needed. The CPU build needs
// the binary and the CPU/Vulkan backends; the NVIDIA and AMD runtimes are
// most of the archive and are skipped.
func keep(name string) bool {
	name = strings.TrimPrefix(name, "./")
	if name == binaryRel {
		return true
	}
	if !hasPathPrefix(name, "lib/ollama/") {
		return false
	}
	for _, skip := range []string{"cuda", "rocm", "mlx", "jetpack"} {
		if containsFold(name, skip) {
			return false
		}
	}
	return true
}

// runtimeComplete is always true on Linux once the main binary exists;
// keep() already retains lib/ollama (including llama-server).
func runtimeComplete(dir string) bool {
	return true
}
