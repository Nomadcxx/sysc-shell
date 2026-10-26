package dep

import "strings"

// ParseWorkflow reads the external repository pins a workflow file declares.
//
// A pin is a step that checks out another repository at a fixed commit: the
// `repository` and `ref` keys of one step. The action a step runs with is
// not a pin of this kind; Dependabot watches those.
func ParseWorkflow(repo, workflow, content string) []RefPin {
	var out []RefPin
	lines := strings.Split(content, "\n")

	var repository, ref string
	var refLine int
	var withIndent int
	var scriptIndent int
	var comments []string
	var stepComments []string

	flush := func() {
		if repository != "" && ref != "" {
			pin := RefPin{
				Repo:     repo,
				Owner:    ownerOf(repository),
				Name:     nameOf(repository),
				Ref:      ref,
				Workflow: workflow,
				Line:     refLine,
			}
			pin.Hold = holdIn(comments, stepComments)
			out = append(out, pin)
		}
		repository, ref, refLine = "", "", 0
		withIndent = 0
	}

	for i, raw := range lines {
		indent := indentOf(raw)
		line := strings.TrimSpace(raw)

		// A line inside a run block is script, not YAML: its comments are
		// not comments and its keys are not keys.
		if scriptIndent > 0 {
			if strings.TrimSpace(line) == "" || indent > scriptIndent {
				continue
			}
			scriptIndent = 0
		}

		if line == "" {
			comments = nil
			continue
		}
		if strings.HasPrefix(line, "#") {
			comments = append(comments, line)
			continue
		}
		if strings.HasPrefix(line, "- ") || line == "-" {
			flush()
			stepComments = comments
			comments = nil
			continue
		}
		key, value, ok := keyValue(line)
		if !ok {
			continue
		}
		if key == "run" && (value == "|" || value == ">" || strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">")) {
			scriptIndent = indent
			continue
		}
		if withIndent > 0 && indent <= withIndent {
			withIndent = 0
		}
		switch {
		case key == "with":
			withIndent = indent
		case withIndent > 0 && key == "repository":
			repository = value
		case withIndent > 0 && key == "ref":
			ref = value
			refLine = i + 1
		}
	}
	flush()
	return out
}

// indentOf counts the leading spaces of a line.
func indentOf(line string) int {
	n := 0
	for _, r := range line {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

// keyValue reads a mapping key and its value from one line of a workflow.
func keyValue(line string) (string, string, bool) {
	key, value, found := strings.Cut(line, ":")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}

// ownerOf names the owner of an owner-qualified repository path.
func ownerOf(path string) string {
	owner, _, found := strings.Cut(path, "/")
	if !found {
		return ""
	}
	return owner
}

// nameOf names the repository of an owner-qualified repository path.
func nameOf(path string) string {
	_, name, found := strings.Cut(path, "/")
	if !found {
		return path
	}
	return name
}

// holdIn returns the hold reason a pair of comment blocks carries: the
// comment above the pin first, then the comment above the step it sits in.
//
// The whole block is the reason. A block explains itself across its lines,
// and quoting one line of it drops the explanation.
func holdIn(above, step []string) string {
	for _, block := range [][]string{above, step} {
		if reason := holdBlock(block); reason != "" {
			return reason
		}
	}
	return ""
}

// holdBlock returns the comment block as a reason when one line of it
// qualifies as one.
func holdBlock(block []string) string {
	qualifies := false
	var cleaned []string
	for _, line := range block {
		text := strings.TrimSpace(line)
		text = strings.TrimPrefix(text, "//")
		text = strings.TrimPrefix(text, "#")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		cleaned = append(cleaned, text)
		if HoldReason(text) != "" {
			qualifies = true
		}
	}
	if !qualifies {
		return ""
	}
	return strings.Join(cleaned, " ")
}
