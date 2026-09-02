package shell

import (
	"regexp"
	"strings"
)

// parsePowerProfiles ports Nomadcxx/noctalia-gamermode's powerprofilesctl list parser.
var profileLine = regexp.MustCompile(`^\s*(\*?)\s*([A-Za-z0-9][A-Za-z0-9_-]*):\s*$`)

func parsePowerProfiles(text string) (names []string, active string) {
	for _, line := range strings.Split(text, "\n") {
		m := profileLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		names = append(names, m[2])
		if m[1] == "*" {
			active = m[2]
		}
	}
	return names, active
}

func powerProfileLabel(name string) string {
	switch name {
	case "power-saver":
		return "Power saver"
	case "balanced":
		return "Balanced"
	case "performance":
		return "Performance"
	}
	return name
}

func profileSupports(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func powerProfileSetArgv(name string) []string {
	return []string{"powerprofilesctl", "set", name}
}
