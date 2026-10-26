package dep

import (
	"fmt"
	"strings"
)

// Requirement is one require line of a go.mod, with the comment it carries.
type Requirement struct {
	// Module is the module path the line requires.
	Module string
	// Version is the version the line requires.
	Version string
	// Comment is the trailing comment of the line, without its "//" marker.
	Comment string
	// Line is the line number the requirement is written on.
	Line int
}

// Indirect reports whether the requirement is marked indirect.
func (r Requirement) Indirect() bool {
	return strings.Contains(r.Comment, "indirect")
}

// ParseGoMod reads the requirements a go.mod declares.
//
// A requirement is either a line inside a require block or a single-line
// require statement. The comment beside a requirement is read for a hold
// reason: the trailing comment first, then the comment block directly above
// the line.
func ParseGoMod(content string) []Pin {
	var out []Pin
	for _, req := range Requirements(content) {
		pin := Pin{
			Module:   req.Module,
			Version:  req.Version,
			Indirect: req.Indirect(),
			Hold:     HoldReason(req.Comment),
			Line:     req.Line,
		}
		if pin.Hold == "" {
			pin.Hold = holdAbove(content, req.Line)
		}
		out = append(out, pin)
	}
	return out
}

// Requirements reads the require lines of a go.mod in the order they are
// written.
func Requirements(content string) []Requirement {
	var out []Requirement
	lines := strings.Split(content, "\n")
	inBlock := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require ("):
			inBlock = true
			continue
		case strings.HasPrefix(line, "require "):
			if req, ok := requirement(strings.TrimPrefix(line, "require "), i+1); ok {
				out = append(out, req)
			}
			continue
		case inBlock:
			if req, ok := requirement(line, i+1); ok {
				out = append(out, req)
			}
		}
	}
	return out
}

// requirement reads one require statement: a module path, a version and an
// optional trailing comment.
func requirement(text string, line int) (Requirement, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "//") {
		return Requirement{}, false
	}
	var comment string
	if i := strings.Index(text, "//"); i >= 0 {
		comment = strings.TrimSpace(text[i+2:])
		text = strings.TrimSpace(text[:i])
	}
	fields := strings.Fields(text)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
		return Requirement{}, false
	}
	return Requirement{Module: fields[0], Version: fields[1], Comment: comment, Line: line}, true
}

// holdAbove returns the hold reason the comment block directly above a line
// carries.
func holdAbove(content string, line int) string {
	lines := strings.Split(content, "\n")
	for i := line - 2; i >= 0; i-- {
		text := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(text, "//") {
			return ""
		}
		if reason := HoldReason(text); reason != "" {
			return reason
		}
	}
	return ""
}

// GoDirectiveLine returns the go directive of a go.mod: the line that names
// the toolchain floor.
func GoDirectiveLine(content string) (string, bool) {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "go ") {
			return line, true
		}
	}
	return "", false
}

// GoDirectiveComment returns the comment block directly above the go
// directive. The comment names why the floor sits where it does, so it is
// part of what a bump has to leave alone.
func GoDirectiveComment(content string) []string {
	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		if !strings.HasPrefix(strings.TrimSpace(raw), "go ") {
			continue
		}
		var block []string
		for j := i - 1; j >= 0; j-- {
			text := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(text, "//") {
				break
			}
			block = append([]string{text}, block...)
		}
		return block
	}
	return nil
}

// IgnoreLines returns the ignore directives of a go.mod. The one that matters
// keeps the docs directory out of the module's package list.
func IgnoreLines(content string) []string {
	var out []string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "ignore ") {
			out = append(out, line)
		}
	}
	return out
}

// Skeleton returns the lines of a go.mod with the require lines removed.
//
// The skeleton is what a bump must not change: everything that is not a
// require line. Comparing the skeleton before and after is how the tool
// proves that a bump touched the require block and nothing else.
func Skeleton(content string) []string {
	var out []string
	lines := strings.Split(content, "\n")
	inBlock := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case inBlock && line == ")":
			inBlock = false
			out = append(out, raw)
			continue
		case strings.HasPrefix(line, "require ("):
			inBlock = true
			out = append(out, raw)
			continue
		case strings.HasPrefix(line, "require "):
			out = append(out, raw)
			continue
		case inBlock:
			if _, ok := requirement(line, 0); ok {
				continue
			}
		}
		out = append(out, raw)
	}
	return out
}

// SetRequire returns the go.mod text with the requirement for one module
// written at a new version.
//
// Only the version changes. The comment the line carries stays, so a
// requirement that is marked indirect stays marked indirect.
func SetRequire(content, module, version string) (string, error) {
	lines := strings.Split(content, "\n")
	inBlock := false
	found := false
	for i, raw := range lines {
		text := strings.TrimSpace(raw)
		var body string
		switch {
		case inBlock && text == ")":
			inBlock = false
			continue
		case strings.HasPrefix(text, "require ("):
			inBlock = true
			continue
		case strings.HasPrefix(text, "require "):
			body = strings.TrimPrefix(text, "require ")
		case inBlock:
			body = text
		default:
			continue
		}
		req, ok := requirement(body, i+1)
		if !ok || req.Module != module {
			continue
		}
		lines[i] = replaceVersion(raw, module, version)
		found = true
	}
	if !found {
		return "", fmt.Errorf("%s is not required by this go.mod", module)
	}
	return strings.Join(lines, "\n"), nil
}

// replaceVersion rewrites the version of one require line.
//
// Everything else the line carries stays: the indentation, the `require`
// keyword of a single-line requirement, and the comment. Only the version
// token is written again.
func replaceVersion(raw, module, version string) string {
	start := strings.Index(raw, module)
	if start < 0 {
		return raw
	}
	rest := raw[start+len(module):]
	offset := len(raw) - len(rest)
	for offset < len(raw) && (raw[offset] == ' ' || raw[offset] == '\t') {
		offset++
	}
	end := offset
	for end < len(raw) && raw[end] != ' ' && raw[end] != '\t' {
		end++
	}
	return raw[:offset] + version + raw[end:]
}
