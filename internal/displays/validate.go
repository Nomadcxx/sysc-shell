package displays

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// validateTimeout is niri's own bound on how long a preview's readback may
// take. Validating a candidate is a parse of a handful of kilobytes, so
// anything slower is a hung process rather than a slow one.
const validateTimeout = 5 * time.Second

// maxValidateOutput bounds what a validator can make this process print. The
// output is user-facing explanation, not a log to keep.
const maxValidateOutput = 64 << 10

// Validator runs niri's own parser over a staged graph. It is a function
// rather than an interface so tests can substitute a script without the
// package growing a provider framework.
type Validator func(ctx context.Context, wrapper string) error

// NiriValidator returns a Validator that shells out to the installed niri.
//
// The wrapper is passed as separate argv elements. There is no shell, no
// concatenation, and no string that a path could escape from: a connector name
// or a path with a space in it reaches niri as one argument.
func NiriValidator() (Validator, error) {
	path, err := exec.LookPath("niri")
	if err != nil {
		return nil, fmt.Errorf("displays: niri is not on PATH, so candidate display settings cannot be validated: %w", err)
	}
	return func(ctx context.Context, wrapper string) error {
		ctx, cancel := context.WithTimeout(ctx, validateTimeout)
		defer cancel()
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, path, "validate", "-c", wrapper)
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("displays: niri validate did not finish within %v: %w", validateTimeout, ctx.Err())
			}
			return fmt.Errorf("displays: niri rejected the staged configuration: %s", excerpt(buf.String()))
		}
		return nil
	}, nil
}

// excerpt trims validator output to something a person can act on, dropping
// the ANSI escapes miette emits and the blank lines between diagnostics.
func excerpt(out string) string {
	out = strings.ReplaceAll(out, "\x1b[0m", "")
	lines := strings.Split(out, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			kept = append(kept, t)
		}
	}
	s := strings.Join(kept, "; ")
	if len(s) > maxValidateOutput {
		s = s[:maxValidateOutput] + "..."
	}
	if s == "" {
		return "no explanation was given"
	}
	return s
}

// StageWrapper writes the two-file candidate graph into dir and returns its
// path plus a cleanup.
//
// The wrapper includes the candidate owned file first and the original root
// second, with absolute paths. Absolute paths matter: a relative include in
// the wrapper would resolve against the temp directory instead of the
// directory the user's includes were written for, and the graph being
// validated would not be the graph niri would load. Ordering matters too —
// the owned file has to win, which is the whole point of the bootstrap.
//
// Nothing is published here. The point is to answer "would niri accept this?"
// against the complete graph before any byte in the configuration directory
// changes.
func StageWrapper(dir string, candidate []byte, root string) (string, func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("displays: create %s: %w", dir, err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	staged := filepath.Join(dir, ManagedFileName)
	if err := writeFileAtomic(staged, candidate, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	doc, err := wrapperDocument(staged, root)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	var buf bytes.Buffer
	if err := kdl.Generate(doc, &buf); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("displays: render wrapper: %w", err)
	}
	wrapper := filepath.Join(dir, "wrapper.kdl")
	if err := writeFileAtomic(wrapper, buf.Bytes(), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return wrapper, cleanup, nil
}

// wrapperDocument builds include(candidate) then include(root) using the KDL
// encoder, so a path needing quotes gets them and no string is assembled by
// hand.
func wrapperDocument(staged, root string) (*document.Document, error) {
	doc := document.New()
	for _, path := range []string{staged, root} {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("displays: %s: %w", path, err)
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("displays: %s is not absolute; a relative include would resolve against the wrong directory", path)
		}
		node := document.NewNode()
		node.SetName("include")
		node.AddArgument(path, "")
		doc.AddNode(node)
	}
	return doc, nil
}

// StageAndValidate stages the candidate and runs the validator over it. The
// cleanup runs before the result is returned, so a caller cannot leak a
// directory by forgetting it.
func StageAndValidate(ctx context.Context, validate Validator, dir string, candidate []byte, root string) error {
	wrapper, cleanup, err := StageWrapper(dir, candidate, root)
	if err != nil {
		return err
	}
	defer cleanup()
	return validate(ctx, wrapper)
}

// ErrNoValidator reports that candidate settings cannot be checked, which
// means they cannot be published either. A shell that previews a config niri
// has never parsed is guessing.
var ErrNoValidator = errors.New("displays: no native validator is available")
