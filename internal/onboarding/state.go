// Package onboarding records whether the first-start wizard has been seen.
// It is a lifecycle marker only: the wizard's answers live in configuration,
// written through the normal validated settings path.
package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Outcome is the recorded result, or "" for unseen.
type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomeDismissed Outcome = "dismissed"
)

// StateVersion is format provenance, not a reason to reopen the wizard.
const StateVersion = 1

// State is the marker document.
type State struct {
	Version int
	Outcome Outcome
}

// Unseen reports whether the wizard has never finished or been dismissed.
// A malformed record reads unseen so one damaged file cannot suppress setup.
func (s State) Unseen() bool {
	return s.Outcome != OutcomeCompleted && s.Outcome != OutcomeDismissed
}

// StateRoot is $XDG_STATE_HOME/sysc-shell/onboarding, falling back to
// $HOME/.local/state as the specification requires; a relative environment
// value is ignored rather than resolved against the current directory.
func StateRoot() string {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "sysc-shell", "onboarding")
}

// StatePath is the file the marker commits to.
func StatePath() string { return filepath.Join(StateRoot(), "state.json") }

// Load reads the marker. A missing file is unseen without an error; a file
// that exists but cannot be understood is returned as unseen with the error,
// and is left on disk untouched for recovery.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("onboarding: read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("onboarding: %s is malformed: %w", path, err)
	}
	if s.Outcome != "" && s.Outcome != OutcomeCompleted && s.Outcome != OutcomeDismissed {
		return State{}, fmt.Errorf("onboarding: %s holds unknown outcome %q", path, s.Outcome)
	}
	return s, nil
}

// Mark atomically records an outcome: unique temp in the destination
// directory, mode 0600, sync, rename. A crash before rename leaves the old
// state, never a half-written one.
func Mark(path string, outcome Outcome) error {
	if outcome != OutcomeCompleted && outcome != OutcomeDismissed {
		return fmt.Errorf("onboarding: %q is not a recorded outcome", outcome)
	}
	data, err := json.Marshal(State{Version: StateVersion, Outcome: outcome})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("onboarding: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("onboarding: create temp: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("onboarding: replace %s: %w", path, err)
	}
	ok = true
	return nil
}
