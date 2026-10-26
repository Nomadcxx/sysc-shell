// Package dep reads the pin facts the dependency tooling needs: the module
// requirements a go.mod declares, the external checkouts a workflow pins, the
// published version of a module, and the commit a workflow pin sits behind.
//
// It reads files and asks two services what they know. It writes nothing: the
// reporter is read-only by construction, and the bump tool carries its own
// procedure for the one thing that does write.
package dep

import "strings"

// Pin is one module requirement read from a go.mod.
//
// A pin is either a release version or a pseudo-version. A pseudo-version
// names a commit that has no tag of its own, so there is no release to move
// to; the report says so rather than counting releases behind it.
type Pin struct {
	// Module is the module path, as go.mod writes it.
	Module string
	// Version is the version go.mod requires, as written.
	Version string
	// Indirect is true when go.mod marks the requirement indirect.
	Indirect bool
	// Hold is the reason the pin is deliberately held, read from the
	// comment beside it. It is empty when the pin carries no reason.
	Hold string
	// Line is the line number the requirement is written on.
	Line int
}

// Pseudo reports whether the pin names a pseudo-version: a commit with no
// release tag of its own.
func (p Pin) Pseudo() bool {
	v, err := ParseVersion(p.Version)
	if err != nil {
		return false
	}
	return v.Pseudo
}

// RefPin is one external repository a workflow checks out at a fixed commit.
//
// The pin is a commit, not a release, so the drift it carries is measured in
// commits rather than releases.
type RefPin struct {
	// Repo is the repository that declares the pin.
	Repo string
	// Owner and Name are the repository being checked out.
	Owner string
	Name  string
	// Ref is the commit the workflow pins.
	Ref string
	// Hold is the reason the pin is deliberately held, read from the
	// comment beside it. It is empty when the pin carries no reason.
	Hold string
	// Workflow is the workflow file the pin is written in.
	Workflow string
	// Line is the line number the ref is written on.
	Line int
}

// Full is the owner-qualified name of the pinned repository.
func (r RefPin) Full() string {
	return r.Owner + "/" + r.Name
}

// holdPrefix is the marker a hold reason carries. The house writes it as
// "hold: <reason> (see sysc-NNNN)" above the pin it explains.
const holdPrefix = "hold:"

// HoldReason returns the hold reason a comment carries, or an empty string
// when the comment is not one.
//
// A comment is a hold reason when it carries the "hold:" marker or names the
// issue that explains the pin. The second form is the one the tree already
// uses: the workflow pin that waits for a grouped bump names the issue, and
// the reason is the sentence above it.
func HoldReason(comment string) string {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "//"))
	if text == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(text), holdPrefix) {
		return text
	}
	if strings.Contains(text, "sysc-") {
		return text
	}
	return ""
}
