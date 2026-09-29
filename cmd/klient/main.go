// Command klient is a GNOME mail client for Proton Mail with native PGP, an
// AI assistant and an AI-driven local spam filter.
package main

import (
	"log"
	"os"

	"github.com/libormacak/klient/internal/config"
	"github.com/libormacak/klient/internal/ui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v (používám výchozí nastavení)", err)
	}
	os.Exit(ui.Run(cfg))
}
