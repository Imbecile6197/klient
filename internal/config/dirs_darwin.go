//go:build darwin

package config

import (
	"os"
	"path/filepath"
)

func DataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}
	// Same folder as ConfigDir on macOS (Application Support).
	return ConfigDir()
}
