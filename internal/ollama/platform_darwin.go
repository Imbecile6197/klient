//go:build darwin

package ollama

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const assetName = "ollama-darwin.tgz"

const binaryRel = "ollama"

func serveSysProcAttr() *syscall.SysProcAttr {
	// Pdeathsig is Linux-only. Setpgid still lets us signal the process group.
	return &syscall.SysProcAttr{Setpgid: true}
}

func openArchive(f *os.File) (io.ReadCloser, error) {
	return gzip.NewReader(f)
}

// keep reports whether a file of the darwin tarball is needed. Ollama 0.40+
// ships a separate llama-server next to ollama, plus Metal .metallib shaders
// and CPU ggml backends (.so even on Darwin). Skip license text only.
func keep(name string) bool {
	name = strings.TrimPrefix(name, "./")
	switch name {
	case "ollama", "llama-server", "llama-quantize":
		return true
	}
	base := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		base = name[i+1:]
	}
	switch {
	case strings.HasSuffix(base, ".dylib"),
		strings.HasSuffix(base, ".metallib"),
		strings.HasSuffix(base, ".so"):
		return true
	}
	return false
}

// runtimeComplete reports whether the unpacked Darwin tree can actually run
// models. Older installs kept only ollama + dylibs and missed llama-server.
func runtimeComplete(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "llama-server"))
	return err == nil
}
