package wallpaper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/fakebin"
)

// withToolDirs points the fallback search at dirs for one test, so the check
// does not depend on what this machine happens to have installed.
func withToolDirs(t *testing.T, dirs ...string) {
	t.Helper()
	restore := toolDirs.Swap(dirs)
	t.Cleanup(restore)
}

// TestDefaultToolDirsCoverThePerUserInstall pins the fallback list the shell
// probes once PATH misses.
//
// The list is what makes a per-user install visible to a unit whose PATH is
// the manager default, so a change to it is a change to what the shell can
// run (sysc-925, sysc-1955). Every other test replaced the seam, so nothing
// covered the list itself.
func TestDefaultToolDirsCoverThePerUserInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := []string{filepath.Join(home, ".local", "bin"), "/usr/local/bin", "/usr/bin"}
	if got := defaultToolDirs(); !slices.Equal(got, want) {
		t.Fatalf("defaultToolDirs() = %v, want %v", got, want)
	}
}

// TestResolveToolFallsBackToInstallDirs pins the install locations the shell
// probes once PATH misses.
//
// The shell runs from a systemd unit whose PATH is the manager default,
// /usr/local/bin:/usr/bin. A per-user install under ~/.local/bin is then on
// disk and invisible, and the Terminal Art section blamed the user for a
// binary that was there (sysc-925).
func TestResolveToolFallsBackToInstallDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty"))

	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(localBin, "sysc-terminal")
	if err := os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	withToolDirs(t, localBin)

	got, err := resolveTool("sysc-terminal")
	if err != nil {
		t.Fatalf("resolveTool(sysc-terminal) = %v, want the per-user install", err)
	}
	if got != installed {
		t.Fatalf("resolveTool(sysc-terminal) = %q, want %q", got, installed)
	}

	// A name that is not installed anywhere still reports absence, so the
	// caller can keep distinguishing missing from broken.
	if _, err := resolveTool("sysc-terminal-absent"); err == nil {
		t.Fatal("resolveTool must report a name that is nowhere on disk")
	}
}

// TestExtractVideoStillFindsAnInstallOutsidePath pins that the frame extractor
// goes through the same resolver as the engine's own tools.
//
// exec.LookPath alone left an ffmpeg under ~/.local/bin invisible to the
// shell's unit, and a video tile then reported no extractor for a binary that
// was on disk (sysc-1955). PATH is empty here, so the only way the call can
// succeed is the install-directory fallback.
func TestExtractVideoStillFindsAnInstallOutsidePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty"))

	install := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	// The extractor writes whatever its last argument names, which is the
	// destination the thumbnailer asked for.
	installed := filepath.Join(install, "ffmpeg")
	if err := fakebin.Write(installed, "#!/bin/sh\nfor last do :; done\nprintf x > \"$last\"\n", 0o755); err != nil {
		t.Fatal(err)
	}
	withToolDirs(t, install)

	src := filepath.Join(home, "clip.mp4")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(home, "still.png")
	if err := extractVideoStill(context.Background(), src, dst); err != nil {
		t.Fatalf("extractVideoStill with an install outside PATH: %v", err)
	}
	if info, err := os.Stat(dst); err != nil || info.Size() == 0 {
		t.Fatalf("the extractor did not write %s: %v", dst, err)
	}

	// An ffmpeg that is nowhere is still reported as absent, so a caller can
	// tell a missing extractor from a broken one.
	if err := os.Remove(installed); err != nil {
		t.Fatal(err)
	}
	if err := extractVideoStill(context.Background(), src, dst); !errors.Is(err, errNoExtractor) {
		t.Fatalf("extractVideoStill without an ffmpeg = %v, want errNoExtractor", err)
	}
}

// TestResolveToolPrefersPath pins the order: a build the user put on PATH
// wins over the well-known directory, because that is the build they chose.
func TestResolveToolPrefersPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	onPath := t.TempDir()
	chosen := filepath.Join(onPath, "sysc-terminal")
	if err := os.WriteFile(chosen, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", onPath)

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "sysc-terminal"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	withToolDirs(t, other)

	got, err := resolveTool("sysc-terminal")
	if err != nil {
		t.Fatal(err)
	}
	if got != chosen {
		t.Fatalf("resolveTool(sysc-terminal) = %q, want the PATH build %q", got, chosen)
	}
}

// TestResolveToolLeavesAPathAlone pins that an argv that already names a
// program is not searched for, so a caller that pinned a location keeps it.
func TestResolveToolLeavesAPathAlone(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	withToolDirs(t, t.TempDir())
	for _, name := range []string{"/opt/bin/gslapper", "./gslapper", "../gslapper"} {
		got, err := resolveTool(name)
		if err != nil || got != name {
			t.Fatalf("resolveTool(%q) = %q, %v; want the name unchanged", name, got, err)
		}
	}
}

// TestSpawnNameResolvesTheProgram pins that a launch runs the same binary the
// probe found. A shell that reports sysc-terminal as installed and then fails
// to start it is the failure this pairs with sysc-925.
func TestSpawnNameResolvesTheProgram(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty"))

	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(localBin, "sysc-terminal")
	if err := os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	withToolDirs(t, localBin)

	got := spawnName([]string{"sysc-terminal", "-I", "/run/s.sock", "--list"})
	want := []string{installed, "-I", "/run/s.sock", "--list"}
	if !slices.Equal(got, want) {
		t.Fatalf("spawnName = %v, want %v", got, want)
	}

	// The argv handed in is not modified: a caller that keeps its own copy
	// must still see its own program name.
	source := []string{"sysc-terminal", "--list"}
	_ = spawnName(source)
	if !slices.Equal(source, []string{"sysc-terminal", "--list"}) {
		t.Fatalf("spawnName rewrote the caller's argv: %v", source)
	}

	// A name that is nowhere on disk reaches exec unchanged, so the error
	// names what the caller asked for.
	if got := spawnName([]string{"sysc-terminal-absent"}); !slices.Equal(got, []string{"sysc-terminal-absent"}) {
		t.Fatalf("spawnName(absent) = %v, want the name unchanged", got)
	}
}

// TestProbeFindsAPerUserTerminalInstall pins the symptom the ticket reports: a
// sysc-terminal in ~/.local/bin, a service PATH of /usr/local/bin:/usr/bin, and
// a Terminal Art section that says the engine is not installed.
func TestProbeFindsAPerUserTerminalInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty"))

	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	terminal := filepath.Join(localBin, "sysc-terminal")
	if err := fakebin.Write(terminal, "#!/bin/sh\nif [ \"$1\" != \"--list\" ]; then exit 2; fi\necho 'effect fire 0'\n", 0o755); err != nil {
		t.Fatal(err)
	}
	withToolDirs(t, localBin)

	caps := probeCapabilities(nil)
	if !caps.Terminal {
		t.Fatal("a sysc-terminal in ~/.local/bin was reported as not installed")
	}
	if !slices.Contains(caps.Catalog.Effects, "fire") {
		t.Fatalf("catalog = %+v, want the effect the per-user build lists", caps.Catalog)
	}
	if caps.EngineFor(KindEffect) != EngineTerminal {
		t.Fatalf("kind effect engine = %q, want %q", caps.EngineFor(KindEffect), EngineTerminal)
	}
}

// TestTheGSlapperProbeIsBounded pins the bound on the gslapper capability
// probe.
//
// The probe runs while the shell holds the registry lock, so a build that
// does not answer --help would hold that lock for the life of the process
// (sysc-2007). The sibling catalog probe is bounded; this one is now too.
func TestTheGSlapperProbeIsBounded(t *testing.T) {
	dir := t.TempDir()
	hanging := filepath.Join(dir, "gslapper")
	if err := fakebin.Write(hanging, "#!/bin/sh\nexec /bin/sleep 30\n", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	withToolDirs(t, dir)

	start := time.Now()
	caps := probeCapabilities(nil)
	held := time.Since(start)

	if held > 10*time.Second {
		t.Fatalf("the capability probe took %s, want it bounded by the probe timeout", held)
	}
	if caps.GSlapper {
		t.Fatal("a probe that never answered reported the capability")
	}
}
