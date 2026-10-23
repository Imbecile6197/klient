//go:build darwin

package power

import (
	"os/exec"
	"strings"
)

// OnAC reports whether a mains adapter is connected. Machines without a
// battery (desktops) count as on AC.
func OnAC() bool {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return true
	}
	s := string(out)
	if strings.Contains(s, "AC Power") {
		return true
	}
	if strings.Contains(s, "Battery Power") {
		return false
	}
	return true
}
