//go:build linux

package ollama

import "testing"

func TestKeep(t *testing.T) {
	for name, want := range map[string]bool{
		"bin/ollama":                          true,
		"./bin/ollama":                        true,
		"lib/ollama/libggml-cpu-haswell.so":   true,
		"lib/ollama/vulkan/libggml-vulkan.so": true,
		"lib/ollama/cuda_v12/libggml-cuda.so": false,
		"lib/ollama/rocm/librocblas.so":       false,
		"lib/ollama/mlx_cuda_v13/libmlx.so":   false,
		"share/doc/readme":                    false,
	} {
		if got := keep(name); got != want {
			t.Errorf("keep(%q) = %v", name, got)
		}
	}
}
