//go:build darwin

package ollama

import "testing"

func TestKeep(t *testing.T) {
	for name, want := range map[string]bool{
		"ollama":                     true,
		"llama-server":               true,
		"llama-quantize":             true,
		"libggml.0.dylib":            true,
		"libllama.dylib":             true,
		"libggml-cpu-x64.so":         true,
		"mlx_metal_v4/mlx.metallib":  true,
		"mlx_metal_v3/libmlx.dylib":  true,
		"share/doc/readme":           false,
		"CPP_HTTPLIB_LICENSE":        false,
		"bin/ollama":                 false,
	} {
		if got := keep(name); got != want {
			t.Errorf("keep(%q) = %v", name, got)
		}
	}
}
