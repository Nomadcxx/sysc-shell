package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// checkFile returns one finding per problem in a document. Every finding names
// the file and the line of the fence that produced it, so a reader lands on the
// example rather than on the checker.
func checkFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot read: %v", path, err)}
	}
	var findings []string
	for _, b := range extract(string(data)) {
		if b.skipped != "" {
			continue
		}
		findings = append(findings, checkBlock(path, b)...)
	}
	return findings
}

func checkBlock(path string, b block) []string {
	at := fmt.Sprintf("%s:%d: ", path, b.line)
	switch b.info {
	case "json":
		return checkJSON(at, strings.Join(b.lines, "\n"))
	case "kdl":
		var findings []string
		for _, line := range b.lines {
			findings = append(findings, checkKDL(at, line)...)
		}
		return findings
	case "sh", "bash":
		var findings []string
		for _, line := range b.lines {
			if isShellComment(line) {
				continue
			}
			findings = append(findings, checkShellCommand(at, line)...)
		}
		return findings
	default:
		return nil
	}
}

// checkJSON judges a JSON example: an object is a configuration candidate and
// goes through the loader that runs at startup, an array is a bar item list and
// every entry's id must be in the widget vocabulary.
func checkJSON(at, body string) []string {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	if trimmed == "" {
		return []string{at + "empty json block"}
	}
	switch trimmed[0] {
	case '{':
		if _, err := config.Parse([]byte(body)); err != nil {
			return []string{at + err.Error()}
		}
		var decoded any
		if err := json.Unmarshal([]byte(body), &decoded); err == nil {
			return checkPanelValue(at, decoded)
		}
		return nil
	case '[':
		return checkItemArray(at, body)
	default:
		return []string{at + "json block is neither a config object nor an item array"}
	}
}

func checkItemArray(at, body string) []string {
	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		return []string{at + err.Error()}
	}
	known := map[string]bool{}
	for _, id := range config.KnownItemIDs() {
		known[id] = true
	}
	var findings []string
	for _, item := range items {
		if item.ID != "" && !known[item.ID] {
			findings = append(findings, fmt.Sprintf("%sunknown bar item id %q", at, item.ID))
		}
	}
	return findings
}
