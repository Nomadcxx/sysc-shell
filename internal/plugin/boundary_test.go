package plugin

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// launcherOptions run the helper through this test binary as the launcher,
// which TestMain turns into LaunchMain.
func launcherOptions(t *testing.T) RuntimeOptions {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := helperOptions()
	opts.Launcher = self
	opts.StateDir = t.TempDir()
	return opts
}

// probeOutput starts the probe helper and returns what it reported.
func probeOutput(t *testing.T, c Candidate, opts RuntimeOptions) (string, Status) {
	t.Helper()
	r := NewRuntime(c, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (stderr %q)", err, r.Status().Stderr)
	}
	defer r.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := r.Status()
		if strings.Count(string(st.Stderr), "probe ") >= 3 {
			return string(st.Stderr), st
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe reported %q", st.Stderr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheLauncherAppliesTheMeasuredCeilings(t *testing.T) {
	t.Parallel()
	m := installHelper(t, "probe")
	out, st := probeOutput(t, Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, launcherOptions(t))
	for _, want := range []string{
		"probe rlimit as 8589934592",
		"probe rlimit nofile 1024",
		"probe rlimit fsize 1073741824",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("probe output %q lacks %q", out, want)
		}
	}
	if st.Boundary != BoundaryLimited {
		t.Errorf("boundary = %q, want %q for a plugin that declares no paths", st.Boundary, BoundaryLimited)
	}
	if st.State != StateRunning {
		t.Errorf("state = %q; the launcher must hand over to a working plugin", st.State)
	}
}

func TestWithoutALauncherNothingIsBounded(t *testing.T) {
	t.Parallel()
	m := installHelper(t, "probe")
	opts := helperOptions()
	_, st := probeOutput(t, Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, opts)
	if st.Boundary != BoundaryNone {
		t.Errorf("boundary = %q, want %q", st.Boundary, BoundaryNone)
	}
}

func TestAPluginPastTheAddressCeilingFailsThroughTheUsualPath(t *testing.T) {
	t.Parallel()
	m := installHelper(t, "alloc-after-hello")

	// Unbounded, the same allocation succeeds: the ceiling is what stops it.
	free := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, helperOptions())
	if err := free.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStderr(t, free, "helper: allocated")
	free.Stop()

	r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, launcherOptions(t))
	_ = r.Start(context.Background())
	defer r.Stop()
	deadline := time.Now().Add(20 * time.Second)
	for r.Status().State != StateFailed {
		if time.Now().After(deadline) {
			t.Fatalf("status = %+v, want failed", r.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := r.Status()
	if st.Starts != MaxAutoStarts {
		t.Errorf("starts = %d; the restart budget must stay %d", st.Starts, MaxAutoStarts)
	}
	if strings.Contains(string(st.Stderr), "helper: allocated") {
		t.Error("the allocation succeeded under the ceiling")
	}
}

func waitStderr(t *testing.T, r *Runtime, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(string(r.Status().Stderr), want) {
		if time.Now().After(deadline) {
			t.Fatalf("stderr %q never contained %q", r.Status().Stderr, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
