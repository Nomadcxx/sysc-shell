package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/ipc"
)

// checkIPCArgs judges a `sysc-shell ipc <method> [params]` example. An unknown
// method, params that are not JSON, or a panel the shell does not have all fail
// here rather than when a reader copies the line into a terminal.
func checkIPCArgs(at, method, params string) []string {
	var findings []string
	if !knownMethod(method) {
		findings = append(findings, fmt.Sprintf("%sunknown ipc method %q", at, method))
	}
	if params == "" {
		return findings
	}
	var decoded any
	if err := json.Unmarshal([]byte(params), &decoded); err != nil {
		return append(findings, fmt.Sprintf("%sipc params are not json: %v", at, err))
	}
	return append(findings, checkPanelValue(at, decoded)...)
}

// knownMethod reports whether the socket dispatches the method exactly or
// through one of its delegated families.
func knownMethod(method string) bool {
	for _, name := range ipc.Methods() {
		if name == method {
			return true
		}
	}
	for _, prefix := range ipc.MethodPrefixes() {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
}

// checkPanelValue finds every "panel" key anywhere in a decoded params object
// and asks the panel dispatcher whether it knows the value.
func checkPanelValue(at string, value any) []string {
	switch v := value.(type) {
	case map[string]any:
		var findings []string
		for key, item := range v {
			if key == "panel" {
				if name, ok := item.(string); ok && !knownPanel(name) {
					findings = append(findings, fmt.Sprintf("%sunknown panel %q", at, name))
				}
				continue
			}
			findings = append(findings, checkPanelValue(at, item)...)
		}
		return findings
	case []any:
		var findings []string
		for _, item := range v {
			findings = append(findings, checkPanelValue(at, item)...)
		}
		return findings
	default:
		return nil
	}
}

func knownPanel(name string) bool {
	for _, id := range ipc.PanelIDs() {
		if id == name {
			return true
		}
	}
	return false
}

// checkKDL walks the quoted tokens of a kdl line and checks the argv runs that
// name the socket.
func checkKDL(at, line string) []string {
	var findings []string
	for _, argv := range kdlArgv(line) {
		if len(argv) < 3 || argv[0] != "sysc-shell" || argv[1] != "ipc" {
			continue
		}
		params := ""
		if len(argv) > 3 {
			// An escape this gate does not decode is not a failure: the method
			// still gets checked, the params are left alone.
			params = argv[3]
		}
		findings = append(findings, checkIPCArgs(at, argv[2], params)...)
	}
	return findings
}

// kdlArgv returns the decoded argv of every `spawn`-like call in a line.
func kdlArgv(line string) [][]string {
	var out [][]string
	var current []string
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '"':
			value, next, ok := readQuoted(runes, i)
			if !ok {
				return out
			}
			current = append(current, value)
			i = next - 1
		case '{', '}':
			if len(current) > 0 {
				out = append(out, current)
			}
			current = nil
		}
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

// readQuoted decodes one KDL string starting at the opening quote. It reports
// false for an escape it does not know, so the caller can skip rather than
// guess.
func readQuoted(runes []rune, start int) (value string, next int, ok bool) {
	var b strings.Builder
	for i := start + 1; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			if i+1 >= len(runes) {
				return "", 0, false
			}
			switch runes[i+1] {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 's':
				b.WriteByte(' ')
			default:
				return "", 0, false
			}
			i++
		case '"':
			return b.String(), i + 1, true
		default:
			b.WriteRune(runes[i])
		}
	}
	return "", 0, false
}

// checkShellCommand judges one line of a shell fence. Only a line that names
// the socket is read; every other command in the block is prose.
func checkShellCommand(at, line string) []string {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "sysc-shell" || fields[1] != "ipc" {
		return nil
	}
	params := ""
	if len(fields) > 3 {
		params = unquote(fields[3])
	}
	return checkIPCArgs(at, fields[2], params)
}

// unquote strips one matching pair of single or double quotes, which is what a
// shell block needs for `{"panel":"control-center"}`.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

// isShellComment keeps prose that begins with # out of the command scan. A
// comment cannot name the socket anyway, so this only saves scanning it.
func isShellComment(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}
