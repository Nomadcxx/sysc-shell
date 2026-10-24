package plugin

import (
	"context"
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// installContainedHelper is installHelperWith for a confined plugin: the
// test binary is linked into the plugin's own directory, because a confined
// plugin may execute only what lies beneath it and the system directories.
func installContainedHelper(t *testing.T, mode, manifest string) Manifest {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(self, filepath.Join(bin, "helper")); err != nil {
		data, rerr := os.ReadFile(self)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if err := os.WriteFile(filepath.Join(bin, "helper"), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %q %s %s\n", filepath.Join(bin, "helper"), HelperFlag, mode)
	if err := os.WriteFile(filepath.Join(bin, "sysc-plugin-timer"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAConfinedPluginReachesOnlyWhatItDeclared(t *testing.T) {
	if LandlockABI() < 1 {
		t.Skip("this kernel provides no landlock; recorded unrun rather than asserted")
	}
	// The secret, the declared directory and the plugin all live under the
	// test's temporary root, so the system rules here leave /tmp out: what
	// the plugin may reach there is exactly what the boundary grants.
	secret := filepath.Join(t.TempDir(), ".ssh")
	allowed := t.TempDir()
	for path, body := range map[string]string{
		filepath.Join(secret, "id_ed25519"): "private",
		filepath.Join(allowed, "auth.json"): "{}",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := string(manifestWith(t, FilesystemMinor, map[string]any{"read": []string{allowed}}))
	m := installContainedHelper(t, "probe", manifest)
	opts := launcherOptions(t)
	opts.SystemPaths = []PathRule{
		{Path: "/usr", Access: AccessReadExec}, {Path: "/bin", Access: AccessReadExec},
		{Path: "/lib", Access: AccessReadExec}, {Path: "/lib64", Access: AccessReadExec},
		{Path: "/etc", Access: AccessRead}, {Path: "/proc", Access: AccessRead},
		{Path: "/sys", Access: AccessRead}, {Path: "/dev", Access: AccessDevice},
	}
	other := filepath.Join(opts.StateDir, "org.example.other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(opts.StateDir, m.ID, "note")
	probes := []string{
		"read:" + filepath.Join(secret, "id_ed25519"),
		"read:" + filepath.Join(allowed, "auth.json"),
		"write:" + filepath.Join(allowed, "planted"),
		"write:" + own,
		"read:" + filepath.Join(other, "state.json"),
		"write:" + filepath.Join(m.Dir, "planted"),
	}
	t.Setenv("SYSC_PROBE", strings.Join(probes, ";"))
	r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (stderr %q)", err, r.Status().Stderr)
	}
	defer r.Stop()
	waitStderr(t, r, "probe write "+filepath.Join(m.Dir, "planted"))
	out := string(r.Status().Stderr)
	for _, want := range []string{
		"probe read " + filepath.Join(secret, "id_ed25519") + " denied",
		"probe read " + filepath.Join(allowed, "auth.json") + " ok",
		"probe write " + filepath.Join(allowed, "planted") + " denied",
		"probe write " + own + " ok",
		"probe read " + filepath.Join(other, "state.json") + " denied",
		"probe write " + filepath.Join(m.Dir, "planted") + " denied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("probe output lacks %q:\n%s", want, out)
		}
	}
	if st := r.Status(); st.Boundary != declaredBoundary() || st.State != StateRunning {
		t.Errorf("status = %q %q, want a running confined plugin", st.State, st.Boundary)
	}
}

func TestAnUndeclaredPluginIsBoundedButNotConfined(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := installContainedHelper(t, "probe", timerManifest)
	t.Setenv("SYSC_PROBE", "read:"+secret)
	r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, launcherOptions(t))
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	waitStderr(t, r, "probe read "+secret)
	if out := string(r.Status().Stderr); !strings.Contains(out, "probe read "+secret+" ok") {
		t.Fatalf("an undeclared plugin was confined: %s", out)
	}
	if st := r.Status(); st.Boundary != BoundaryLimited {
		t.Fatalf("boundary = %q, want %q: the manager must say it is unconfined", st.Boundary, BoundaryLimited)
	}
}

// confinedHelper installs a declared plugin to run under the real boundary.
func confinedHelper(t *testing.T, mode string) (Manifest, RuntimeOptions) {
	t.Helper()
	if LandlockABI() < 1 {
		t.Skip("this kernel provides no landlock; recorded unrun rather than asserted")
	}
	m := installContainedHelper(t, mode, string(manifestWith(t, FilesystemMinor, map[string]any{})))
	return m, launcherOptions(t)
}

func TestConfinementLeavesTheDegradedPathAlone(t *testing.T) {
	t.Parallel()
	m, opts := confinedHelper(t, "flood")
	opts.Limits.UpdatesPerSecond, opts.Limits.UpdateBurst = 10, 20
	r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	waitRuntimeState(t, r, StateDegraded)
	if st := r.Status(); st.Starts != 1 || st.Boundary != declaredBoundary() || !strings.Contains(st.Failure, "budget") {
		t.Fatalf("status = %+v", st)
	}
}

func TestConfinementLeavesTheRestartBudgetAlone(t *testing.T) {
	t.Parallel()
	m, opts := confinedHelper(t, "crash-after-hello")
	r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, opts)
	_ = r.Start(context.Background())
	defer r.Stop()
	waitRuntimeState(t, r, StateFailed)
	if st := r.Status(); st.Starts != MaxAutoStarts || st.Boundary != declaredBoundary() {
		t.Fatalf("status = %+v, want %d starts inside the confined boundary", st, MaxAutoStarts)
	}
}

// listenUnix serves a socket outside any plugin's domain until the test ends.
func listenUnix(t *testing.T, address string) {
	t.Helper()
	l, err := net.Listen("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
}

// probeRun starts the probe under the real boundary with ops in SYSC_PROBE
// and returns its report once the last op has one.
func probeRun(t *testing.T, decl map[string]any, ops []string) string {
	t.Helper()
	m := installContainedHelper(t, "probe", string(manifestWith(t, FilesystemMinor, decl)))
	t.Setenv("SYSC_PROBE", strings.Join(ops, ";"))
	r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, launcherOptions(t))
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (stderr %q)", err, r.Status().Stderr)
	}
	t.Cleanup(r.Stop)
	last := ops[len(ops)-1]
	kind, target, _ := strings.Cut(last, ":")
	waitStderr(t, r, "probe "+kind+" "+target+" ")
	return string(r.Status().Stderr)
}

func TestAConfinedPluginReachesOnlyTheSocketsItDeclared(t *testing.T) {
	if LandlockABI() < resolveUnixABI {
		t.Skipf("this kernel offers Landlock ABI %d; socket confinement needs %d. Recorded unrun.", LandlockABI(), resolveUnixABI)
	}
	dir := sockpath.Dir(t, "sock")
	declared, other := filepath.Join(dir, "declared.sock"), filepath.Join(dir, "other.sock")
	listenUnix(t, declared)
	listenUnix(t, other)
	out := probeRun(t, map[string]any{"sockets": []string{declared}}, []string{"connect:" + declared, "connect:" + other})
	for _, want := range []string{"probe connect " + declared + " ok", "probe connect " + other + " denied"} {
		if !strings.Contains(out, want) {
			t.Errorf("probe output lacks %q:\n%s", want, out)
		}
	}
}

func TestAConfinedPluginIsIsolatedFromAbstractSocketsAndOtherProcesses(t *testing.T) {
	if LandlockABI() < scopeABI {
		t.Skipf("this kernel offers Landlock ABI %d; the scopes need %d. Recorded unrun.", LandlockABI(), scopeABI)
	}
	abstract := fmt.Sprintf("@sysc-probe-%d", os.Getpid())
	listenUnix(t, abstract)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep to start")
	}
	foreign := exec.Command(sleep, "30")
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = foreign.Process.Kill(); _ = foreign.Wait() })
	pid := strconv.Itoa(foreign.Process.Pid)

	out := probeRun(t, map[string]any{}, []string{"connect:" + abstract, "signal:" + pid, "child-signal:" + sleep})
	for _, want := range []string{
		"probe connect " + abstract + " denied",
		"probe signal " + pid + " denied",
		"probe child-signal " + sleep + " ok",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("probe output lacks %q:\n%s", want, out)
		}
	}
	// Declaring "signals": "any" opens signals and nothing else.
	out = probeRun(t, map[string]any{"signals": "any"}, []string{"connect:" + abstract, "signal:" + pid})
	for _, want := range []string{"probe connect " + abstract + " denied", "probe signal " + pid + " ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("with signals any, probe output lacks %q:\n%s", want, out)
		}
	}
}

// declaredBoundary is the level a declared plugin runs at on this kernel.
func declaredBoundary() Boundary {
	b, _ := boundaryOf(Manifest{Filesystem: &Filesystem{}}, "/launcher", LandlockABI())
	return b
}
