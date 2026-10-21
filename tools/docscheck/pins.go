package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// modulePin matches a module path written with a version, the shape a
// document uses when it names the exact revision it wants.
var modulePin = regexp.MustCompile(`([a-zA-Z0-9._~-]+\.[a-zA-Z]{2,}/[a-zA-Z0-9._~/+-]+)@(v[0-9][a-zA-Z0-9.+-]*)`)

// repoRoot returns the repository root, derived from this file's own location
// so the gate finds go.mod and the document set the same way from any working
// directory.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// goModPins reads the versions this repository requires. go.mod is the only
// source of truth for them: a document that names one reads as a deliberate
// constraint and drifts the moment the pin moves.
func goModPins() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "go.mod"))
	if err != nil {
		return nil, err
	}
	pins := map[string]string{}
	inRequire := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "require ("):
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire:
			addPin(pins, line)
		case strings.HasPrefix(line, "require "):
			addPin(pins, strings.TrimPrefix(line, "require "))
		}
	}
	return pins, nil
}

// addPin records one require line, which is a module path and a version and
// may end with a comment.
func addPin(pins map[string]string, line string) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
		return
	}
	pins[fields[0]] = fields[1]
}

// checkPins reports a document that names a version of a module this
// repository requires. The pin belongs to go.mod, and a document that repeats
// it goes stale as soon as the pin moves.
func checkPins(path string, pins map[string]string) []string {
	if len(pins) == 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var findings []string
	for i, line := range strings.Split(string(data), "\n") {
		for _, m := range modulePin.FindAllStringSubmatch(line, -1) {
			module, version := m[1], m[2]
			want, ok := pins[module]
			if !ok {
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"%s:%d: names %s at %s, but go.mod is the only source of truth for a module version (it requires %s)",
				path, i+1, module, version, want))
		}
	}
	return findings
}
