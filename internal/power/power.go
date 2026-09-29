// Package power tells whether the laptop runs on mains power, so heavy
// background work (local AI) can wait for the charger.
package power

import (
	"os"
	"path/filepath"
	"strings"
)

// OnAC reports whether a mains adapter is connected. Machines without a
// battery (desktops) count as on AC.
func OnAC() bool {
	supplies, _ := filepath.Glob("/sys/class/power_supply/*")
	hasBattery := false
	for _, s := range supplies {
		typ := read(filepath.Join(s, "type"))
		switch typ {
		case "Mains", "USB":
			if read(filepath.Join(s, "online")) == "1" {
				return true
			}
		case "Battery":
			if read(filepath.Join(s, "scope")) != "Device" { // not a mouse battery
				hasBattery = true
			}
		}
	}
	return !hasBattery
}

func read(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
