// Command klient is a GNOME mail client for Proton Mail with native PGP, an
// AI assistant and an AI-driven local spam filter.
package main

import (
	"log"
	"os"

	"github.com/Imbecile6197/klient/internal/config"
	"github.com/Imbecile6197/klient/internal/ui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v (using the default settings)", err)
	}
	os.Exit(ui.Run(cfg))
}
