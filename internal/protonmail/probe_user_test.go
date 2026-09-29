//go:build probe

package protonmail

import "github.com/libormacak/klient/internal/config"

// probeUser is the first account logged in to Klient.
func probeUser() string {
	cfg, _ := config.Load()
	if len(cfg.Accounts) > 0 {
		return cfg.Accounts[0]
	}
	return ""
}
