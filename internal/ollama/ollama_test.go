package ollama

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.34.4", "", true},
		{"0.34.4", "0.34.3", true},
		{"0.34.10", "0.34.9", true},
		{"0.34.4", "0.34.4", false},
		{"0.34.3", "0.34.4", false},
		{"1.0.0", "0.99.9", true},
		{"0.35.0-rc1", "0.34.9", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestIsCloud(t *testing.T) {
	if !IsCloud("gemma4:31b-cloud") || IsCloud("qwen3.5:4b") {
		t.Fatal("IsCloud")
	}
}
