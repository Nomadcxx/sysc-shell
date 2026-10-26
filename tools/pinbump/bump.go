package main

import (
	"fmt"
	"strings"

	"github.com/Nomadcxx/sysc-shell/tools/internal/dep"
)

// Bump is one module version change.
type Bump struct {
	// Module is the module path.
	Module string
	// Version is the version the requirement moves to.
	Version string
}

// String writes the bump the way the command line names it.
func (b Bump) String() string {
	return b.Module + "=" + b.Version
}

// ParseBump reads a module=version pair.
func ParseBump(text string) (Bump, error) {
	module, version, found := strings.Cut(strings.TrimSpace(text), "=")
	if !found || module == "" || version == "" {
		return Bump{}, fmt.Errorf("%q is not a module=version pair", text)
	}
	return Bump{Module: module, Version: version}, nil
}

// Verify checks that a bump changed the require block and nothing else.
//
// This is the whole point of the tool. The recorded procedure says to
// hand-edit one require line and nothing else, because the go command
// rewrites the metadata around it: the go directive and its comment, and
// the ignore directive that keeps the docs directory out of the package
// list. The go command reads the directive for the toolchain it installs, so
// a rewrite there makes CI install a toolchain with a known flaw in it.
//
// The checks are the skeleton, which is everything that is not a require
// line; the protected lines by name; and the requirement set, which may
// differ only in the modules the bump names.
func Verify(original, updated string, bumps []Bump) error {
	// The protected lines are checked first, so the failure names the line
	// that was damaged rather than the difference it made.
	if err := verifyProtected(original, updated); err != nil {
		return err
	}
	if err := verifyRequirements(original, updated, bumps); err != nil {
		return err
	}
	return verifySkeleton(original, updated)
}

// verifySkeleton checks that everything outside the require lines is
// unchanged.
func verifySkeleton(original, updated string) error {
	before := dep.Skeleton(original)
	after := dep.Skeleton(updated)
	if len(before) != len(after) {
		return fmt.Errorf("the bump changed %s outside the require block", linesText(abs(len(after)-len(before))))
	}
	for i := range before {
		if before[i] != after[i] {
			return fmt.Errorf("the bump changed a line outside the require block:\n  - %s\n  + %s",
				strings.TrimSpace(before[i]), strings.TrimSpace(after[i]))
		}
	}
	return nil
}

// verifyProtected checks the two lines the recorded procedure names as
// protected: the go directive with its comment, and the ignore directive.
func verifyProtected(original, updated string) error {
	before, ok := dep.GoDirectiveLine(original)
	if !ok {
		return fmt.Errorf("the go.mod has no go directive")
	}
	after, ok := dep.GoDirectiveLine(updated)
	if !ok {
		return fmt.Errorf("the bump removed the go directive")
	}
	if before != after {
		return fmt.Errorf("the bump changed the go directive: %q became %q", before, after)
	}
	beforeComment := dep.GoDirectiveComment(original)
	afterComment := dep.GoDirectiveComment(updated)
	if strings.Join(beforeComment, "\n") != strings.Join(afterComment, "\n") {
		return fmt.Errorf("the bump changed the comment above the go directive")
	}
	beforeIgnore := dep.IgnoreLines(original)
	afterIgnore := dep.IgnoreLines(updated)
	if strings.Join(beforeIgnore, "\n") != strings.Join(afterIgnore, "\n") {
		return fmt.Errorf("the bump changed the ignore directives: %v became %v", beforeIgnore, afterIgnore)
	}
	return nil
}

// verifyRequirements checks that the requirement set differs only in the
// modules the bump names.
func verifyRequirements(original, updated string, bumps []Bump) error {
	before := map[string]dep.Requirement{}
	for _, req := range dep.Requirements(original) {
		before[req.Module] = req
	}
	after := map[string]dep.Requirement{}
	for _, req := range dep.Requirements(updated) {
		after[req.Module] = req
	}
	wanted := map[string]Bump{}
	for _, b := range bumps {
		wanted[b.Module] = b
	}
	for module, req := range after {
		old, ok := before[module]
		if !ok {
			return fmt.Errorf("the bump added a requirement for %s", module)
		}
		bump, isBumped := wanted[module]
		switch {
		case isBumped && req.Version != bump.Version:
			return fmt.Errorf("the bump did not move %s to %s: it requires %s", module, bump.Version, req.Version)
		case !isBumped && req.Version != old.Version:
			return fmt.Errorf("the bump moved %s from %s to %s without being asked to", module, old.Version, req.Version)
		case req.Comment != old.Comment:
			return fmt.Errorf("the bump changed the comment on the requirement for %s", module)
		}
	}
	for module := range before {
		if _, ok := after[module]; !ok {
			return fmt.Errorf("the bump removed the requirement for %s", module)
		}
	}
	for module := range wanted {
		if _, ok := after[module]; !ok {
			return fmt.Errorf("the bump names %s, which the go.mod does not require", module)
		}
	}
	return nil
}

// linesText says how many lines changed.
func linesText(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

// abs is the absolute value of an int.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
