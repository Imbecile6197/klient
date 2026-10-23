//go:build linux

package config

import (
	"os"
	"path/filepath"
)

func DataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share", appName)
}
