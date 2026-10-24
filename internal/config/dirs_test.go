package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDirHonoursXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg-config"))
	got := ConfigDir()
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), appName)
	if got != want {
		t.Fatalf("ConfigDir() = %q, want %q (demo isolation on macOS)", got, want)
	}
}

func TestDataDirHonoursXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))
	got := DataDir()
	want := filepath.Join(os.Getenv("XDG_DATA_HOME"), appName)
	if got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}
