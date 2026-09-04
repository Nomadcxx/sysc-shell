package theming

import (
	"strings"
	"testing"
)

// niri's KDL parser rejects a one-line child node that is not terminated:
// `focus-ring { active-color "x" }` is a syntax error where
// `focus-ring { active-color "x"; }` is not. The generated file is included
// from the user's own config.kdl, so an unterminated line here does not break
// the theme -- it breaks the compositor's whole configuration, and the next
// niri restart comes up on defaults.
func TestNiriTemplateOneLinersTerminate(t *testing.T) {
	t.Parallel()
	b, err := tplFS.ReadFile("templates/niri.tpl")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		trim := strings.TrimSpace(line)
		if !strings.Contains(trim, "{") || !strings.Contains(trim, "}") {
			continue
		}
		if !strings.Contains(trim, ";") {
			t.Fatalf("line %d: niri knuffel rejects `{ child \"x\" }` without a semicolon: %s", i+1, trim)
		}
	}
}
