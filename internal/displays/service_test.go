package displays

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubUnits stands in for systemd. arm's contract is that the helper is running
// and has said it can reach the compositor before the first output command is
// sent, so the stub does exactly that: it publishes the ready marker the shell
// waits for. Whether the real unit does it is the recovery proof's job.
func stubUnits(t *testing.T) {
	t.Helper()
	dir, err := TicketDir()
	if err != nil {
		t.Fatal(err)
	}
	startRecoveryUnit = func(_ context.Context, _ string, token string) error {
		return os.WriteFile(filepath.Join(dir, token+".ready"), nil, 0o600)
	}
	stopRecoveryUnit = func(context.Context, string) error { return nil }
}

// niriValidator uses the real niri binary, because the whole point of staging
// the candidate through it is that niri agrees with what the shell generated.
func niriValidator(t *testing.T) Validator {
	t.Helper()
	v, err := NiriValidator()
	if err != nil {
		t.Skipf("niri is not installed: %v", err)
	}
	return v
}

// serviceFor is the whole fixture: a set-up root, a live fake niri, and a
// service pointed at both.
func serviceFor(t *testing.T) (*Service, *fakeNiri) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	stubUnits(t)
	root, _ := setupDir(t, userRoot)
	if _, err := Setup(root); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	fake := newFakeNiri(t, filepath.Join(t.TempDir(), "niri.log"))
	svc, err := New(fake.start(t), root, niriValidator(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, fake
}

func TestServiceKeepsAndReverts(t *testing.T) {
	svc, fake := serviceFor(t)
	ctx := context.Background()
	snapshots := svc.Subscribe()

	// Preview: the change is live but nothing is durable yet.
	if err := svc.Preview(ctx, map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if got := fake.scale("DP-1"); got != 1.5 {
		t.Errorf("scale after preview = %v, want 1.5", got)
	}
	if snap := <-snapshots; !snap.Preview || len(snap.Outputs) != 1 || *snap.Outputs[0].Scale != 1.5 {
		t.Errorf("preview snapshot = %+v, want the previewed scale", snap)
	}
	if svc.ticket == nil {
		t.Fatal("Preview left no ticket armed")
	}

	// Keep: the file carries the change and niri was told to reload it.
	if err := svc.Keep(ctx); err != nil {
		t.Fatalf("Keep: %v", err)
	}
	if svc.ticket != nil {
		t.Error("Keep left a ticket armed")
	}
	if n := fake.reloadCount(); n != 1 {
		t.Errorf("reloads = %d, want 1", n)
	}
	managed := readFile(t, filepath.Join(filepath.Dir(svc.Root()), ManagedFileName))
	if !strings.Contains(managed, "scale 1.5") {
		t.Errorf("managed file does not carry the kept scale:\n%s", managed)
	}
	if snap := <-snapshots; snap.Preview || !strings.Contains(snap.Note, "Saved to the niri configuration") {
		t.Errorf("kept snapshot = %+v, want no preview and the confirmation", snap)
	}

	// A second preview followed by a revert gives the outputs back the state
	// the kept file is in, which is the only state worth reverting to.
	if err := svc.Preview(ctx, map[string]Policy{"DP-1": {Scale: ptr(2.0)}}); err != nil {
		t.Fatalf("second Preview: %v", err)
	}
	if got := fake.scale("DP-1"); got != 2 {
		t.Errorf("scale after second preview = %v, want 2", got)
	}
	if err := svc.Revert(ctx); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if got := fake.scale("DP-1"); got != 1.5 {
		t.Errorf("scale after revert = %v, want 1.5", got)
	}
	if svc.ticket != nil {
		t.Error("Revert left a ticket armed")
	}
	// Reverting must not have rewritten the kept file.
	if got := readFile(t, filepath.Join(filepath.Dir(svc.Root()), ManagedFileName)); !strings.Contains(got, "scale 1.5") {
		t.Errorf("revert changed the kept file:\n%s", got)
	}
}

func TestServiceRefusals(t *testing.T) {
	root, _ := setupDir(t, userRoot)
	if _, err := New("socket", root, niriValidator(t)); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("New without ownership = %v, want ErrNotSetUp", err)
	}

	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	stubUnits(t)
	if _, err := Setup(root); err != nil {
		t.Fatal(err)
	}
	fake := newFakeNiri(t, filepath.Join(t.TempDir(), "niri.log"))
	socket := fake.start(t)
	ctx := context.Background()

	// A shell that previews a configuration niri has never read is guessing.
	blind, err := New(socket, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := blind.Preview(ctx, map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); !errors.Is(err, ErrNoValidator) {
		t.Errorf("Preview without a validator = %v, want ErrNoValidator", err)
	}

	svc, err := New(socket, root, niriValidator(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Keep(ctx); err == nil {
		t.Error("Keep without a preview succeeded")
	}
	if err := svc.Preview(ctx, map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if err := svc.Preview(ctx, map[string]Policy{"DP-1": {Scale: ptr(2.0)}}); err == nil {
		t.Error("a second Preview succeeded while one was live")
	}
	// A scale niri refuses must not reach the compositor.
	if err := svc.Preview(ctx, map[string]Policy{"DP-1": {Scale: ptr(99.0)}}); err == nil {
		t.Error("Preview with a scale niri refuses succeeded")
	}
	if got := fake.scale("DP-1"); got != 1.5 {
		t.Errorf("scale = %v, want the live preview left at 1.5", got)
	}
}

func TestServicePreviewFailsOnAMissingOutput(t *testing.T) {
	svc, fake := serviceFor(t)
	fake.mu.Lock()
	delete(fake.outs, "DP-1")
	fake.mu.Unlock()
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err == nil {
		t.Fatal("Preview for an output that is not connected succeeded")
	}
	// Nothing may stay armed after a failed preview.
	if svc.ticket != nil {
		t.Error("a failed preview left a ticket armed")
	}
}

func TestServiceRejectsAnUnownedRoot(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	stubUnits(t)
	root, _ := setupDir(t, userRoot)
	if _, err := Setup(root); err != nil {
		t.Fatal(err)
	}
	fake := newFakeNiri(t, filepath.Join(t.TempDir(), "niri.log"))
	svc, err := New(fake.start(t), root, niriValidator(t))
	if err != nil {
		t.Fatal(err)
	}
	// The user edited the root behind the shell's back: the shell no longer
	// knows what it wrote, so it refuses rather than guessing.
	before := readFile(t, root)
	if err := os.WriteFile(root, []byte(before+"\n// edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); !errors.Is(err, ErrExternalEdit) {
		t.Errorf("Preview after an external edit = %v, want ErrExternalEdit", err)
	}
}

func TestSnapshotHoldsOnlyTheLatest(t *testing.T) {
	svc, _ := serviceFor(t)
	ch := svc.Subscribe()
	// A subscriber that is busy misses the intermediate state, not the latest
	// one, so the channel must never block a command.
	for range 3 {
		svc.Refresh()
	}
	if got := len(ch); got != 1 {
		t.Errorf("channel holds %d snapshots, want 1", got)
	}
	select {
	case snap := <-ch:
		if len(snap.Outputs) != 1 || snap.Outputs[0].Name.Connector != "DP-1" {
			t.Errorf("snapshot = %+v, want the one output", snap)
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot was published")
	}
}
