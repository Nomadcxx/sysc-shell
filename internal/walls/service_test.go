package walls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	configGateRevision  = "fc7172d31b8fbfc952df817e5b677f84a4f8e3b9"
	previewGateRevision = "e97eeb631d46e3147cbc79acdaf4644522ce414d"
	clientPackage       = "github.com/Nomadcxx/sysc-walls/cmd/client"
	daemonPackage       = "github.com/Nomadcxx/sysc-walls/cmd/daemon"
	displayPackage      = "github.com/Nomadcxx/sysc-walls/cmd/display"
)

func buildInfo(pkg, revision string, modified bool) *debug.BuildInfo {
	return &debug.BuildInfo{
		Path: pkg,
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.modified", Value: fmt.Sprint(modified)},
		},
	}
}

func TestVerifyBuildIdentity(t *testing.T) {
	tests := []struct {
		name      string
		info      *debug.BuildInfo
		pkg       string
		revisions []string
		wantErr   string
	}{
		{"accepted config gate", buildInfo(clientPackage, configGateRevision, false), clientPackage, []string{configGateRevision, previewGateRevision}, ""},
		{"accepted combined gate", buildInfo(clientPackage, previewGateRevision, false), clientPackage, []string{configGateRevision, previewGateRevision}, ""},
		{"dirty", buildInfo(clientPackage, configGateRevision, true), clientPackage, []string{configGateRevision}, "modified"},
		{"wrong package", buildInfo(daemonPackage, configGateRevision, false), clientPackage, []string{configGateRevision}, "package"},
		{"unaccepted revision", buildInfo(clientPackage, "old-revision", false), clientPackage, []string{configGateRevision}, "revision"},
		{"missing metadata", &debug.BuildInfo{Path: clientPackage}, clientPackage, []string{configGateRevision}, "vcs.revision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyBuildIdentity(tt.info, tt.pkg, tt.revisions)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("verifyBuildIdentity() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("verifyBuildIdentity() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestSnapshotSeparatesPersistentEnablementAndRunning(t *testing.T) {
	for _, tt := range []struct {
		name       string
		unitState  string
		active     string
		sub        string
		stale      bool
		wantEnable bool
		wantRun    bool
	}{
		{"enabled and running", "enabled", "active", "running", false, true, true},
		{"runtime enabled", "enabled-runtime", "active", "running", false, false, true},
		{"static and exited", "static", "active", "exited", false, false, false},
		{"masked and failed", "masked", "failed", "failed", false, false, false},
		{"activating", "enabled", "activating", "start", false, true, false},
		{"stale", "enabled", "active", "running", true, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := Snapshot{UnitKnown: true, UnitFileState: tt.unitState, ActiveState: tt.active, SubState: tt.sub, UnitStale: tt.stale}
			if got := s.EnabledAtLogin(); got != tt.wantEnable {
				t.Errorf("EnabledAtLogin() = %v, want %v", got, tt.wantEnable)
			}
			if got := s.Running(); got != tt.wantRun {
				t.Errorf("Running() = %v, want %v", got, tt.wantRun)
			}
		})
	}
}

type fakeCommand struct {
	mu      sync.Mutex
	show    []commandResult
	env     []commandResult
	lookups int
	calls   [][]string
	results map[string]commandResult
}

type commandResult struct {
	out string
	err error
}

func (f *fakeCommand) run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "systemctl" {
		f.mu.Lock()
		defer f.mu.Unlock()
		call := append([]string{name}, args...)
		f.calls = append(f.calls, call)
		result := f.results[strings.Join(call, " ")]
		return []byte(result.out), result.err
	}
	if len(args) < 2 || args[0] != "--user" {
		return nil, fmt.Errorf("unexpected command %q %q", name, args)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var q *[]commandResult
	if args[1] == "show-environment" {
		q = &f.env
	} else if args[1] == "show" {
		q = &f.show
	} else {
		call := append([]string{name}, args...)
		f.calls = append(f.calls, call)
		result := f.results[strings.Join(call, " ")]
		return []byte(result.out), result.err
	}
	if len(*q) == 0 {
		return nil, errors.New("no fake command result")
	}
	r := (*q)[0]
	*q = (*q)[1:]
	return []byte(r.out), r.err
}

func (f *fakeCommand) callsCopy() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := make([][]string, len(f.calls))
	for i, call := range f.calls {
		calls[i] = append([]string(nil), call...)
	}
	return calls
}

func (f *fakeCommand) lookup(name string, paths map[string]string) (string, error) {
	f.mu.Lock()
	f.lookups++
	f.mu.Unlock()
	path := paths[name]
	if path == "" {
		return "", os.ErrNotExist
	}
	return path, nil
}

func makeInstall(t *testing.T, dir string, names ...string) map[string]string {
	t.Helper()
	paths := make(map[string]string, len(names))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("test executable"), 0o700); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	return paths
}

func fakeIdentities(paths map[string]string, omit ...string) map[string]*debug.BuildInfo {
	missing := make(map[string]bool, len(omit))
	for _, p := range omit {
		missing[p] = true
	}
	out := make(map[string]*debug.BuildInfo)
	for name, path := range paths {
		if missing[name] {
			continue
		}
		switch name {
		case "sysc-walls-client":
			out[path] = buildInfo(clientPackage, configGateRevision, false)
		case "sysc-walls-daemon":
			out[path] = buildInfo(daemonPackage, previewGateRevision, false)
		case "sysc-walls-display":
			out[path] = buildInfo(displayPackage, previewGateRevision, false)
		}
	}
	return out
}

func loadedUnitOutput(daemon, unitState, active, sub, env, envFiles string) string {
	return fmt.Sprintf("LoadState=loaded\nFragmentPath=/home/test/.config/systemd/user/sysc-walls.service\n"+
		"ExecStart={ path=%s ; argv[]=%s -start ; ignore_errors=no ; start_time=[Mon 2026-10-04 12:00:00 UTC] }\n"+
		"Environment=%s\nEnvironmentFiles=%s\nUnitFileState=%s\nActiveState=%s\nSubState=%s\n",
		daemon, daemon, env, envFiles, unitState, active, sub)
}

func newTestService(t *testing.T, home string, command *fakeCommand, paths map[string]string, readFile func(string) ([]byte, error), identities map[string]*debug.BuildInfo) *Service {
	t.Helper()
	s := newServiceWithDependencies(testDependencies(home, command, paths, readFile, identities))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testDependencies(home string, command *fakeCommand, paths map[string]string, readFile func(string) ([]byte, error), identities map[string]*debug.BuildInfo) serviceDependencies {
	return serviceDependencies{
		run:      command.run,
		lookPath: func(name string) (string, error) { return command.lookup(name, paths) },
		homeDir:  func() (string, error) { return home, nil },
		readFile: readFile,
		readBuildInfo: func(path string) (*debug.BuildInfo, error) {
			info := identities[path]
			if info == nil {
				return nil, os.ErrNotExist
			}
			return info, nil
		},
	}
}

func waitSnapshot(t *testing.T, service *Service, match func(Snapshot) bool) Snapshot {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		snap := service.Snapshot()
		if match(snap) {
			return snap
		}
		select {
		case <-service.Updates():
		case <-timer.C:
			t.Fatalf("timed out waiting for snapshot; last = %+v", service.Snapshot())
		}
	}
}

func TestLoadedUnitUsesSiblingBinariesAndReadsConfig(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "usr-local-bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	configPath := filepath.Join(home, ".config", "sysc-walls", "daemon.conf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "# keep invalid values visible\n[idle]\ntimeout = 0s\n[animation]\neffect = unknown-effect\ntheme = rama\nfile = ~/art.png\ndatetime = maybe\n[datetime]\nposition = top\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	command := &fakeCommand{
		show: []commandResult{{out: loadedUnitOutput(paths["sysc-walls-daemon"], "enabled-runtime", "active", "running", "", "[]")}},
		env:  []commandResult{{out: "HOME=" + home + "\n"}},
	}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.ConfigKnown })
	if snap.DaemonPath != paths["sysc-walls-daemon"] || snap.ClientPath != paths["sysc-walls-client"] || snap.DisplayPath != paths["sysc-walls-display"] {
		t.Fatalf("selected paths = daemon:%q client:%q display:%q", snap.DaemonPath, snap.ClientPath, snap.DisplayPath)
	}
	if snap.UnitFileState != "enabled-runtime" || snap.ActiveState != "active" || snap.SubState != "running" {
		t.Fatalf("unit state lost raw values: %+v", snap)
	}
	if !snap.ConfigSourceMatches || !snap.ConfigReadable || !snap.CanApply || snap.CanPreview || snap.PreviewAvailabilityError == "" {
		t.Fatalf("capabilities = %+v", snap)
	}
	if snap.Timeout != "0s" || snap.Effect != "unknown-effect" || snap.DateTime != "maybe" || snap.Artwork != "~/art.png" {
		t.Fatalf("raw config values not retained: %+v", snap)
	}
	command.mu.Lock()
	lookups := command.lookups
	command.mu.Unlock()
	if lookups != 0 {
		t.Fatalf("loaded-unit discovery consulted PATH %d times", lookups)
	}
}

func TestMissingUnitUsesOnePATHInstallAndMissingConfigDefaultsReadOnly(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	command := &fakeCommand{show: []commandResult{{out: "LoadState=not-found\nUnitFileState=not-found\nActiveState=inactive\nSubState=dead\n"}}}
	service := newTestService(t, home, command, paths, os.ReadFile, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.ConfigKnown })
	if snap.LoadState != "not-found" || snap.ServiceAvailable {
		t.Fatalf("missing unit was not kept separate from standalone capabilities: %+v", snap)
	}
	if !snap.ConfigSourceMatches || !snap.ConfigReadable || !snap.CanApply || !snap.CanPreview {
		t.Fatalf("complete verified PATH installation not available: %+v", snap)
	}
	if snap.Effect != "matrix-art" || snap.Theme != "rama" || snap.Timeout != "5m" || snap.DateTime != "false" || snap.DateTimePosition != "bottom" || snap.Artwork != "" {
		t.Fatalf("default config values = %+v", snap)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing config read created its parent: stat err = %v", err)
	}
}

func TestUnsupportedConfigSourceDoesNotReadDefaultPath(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	other := filepath.Join(t.TempDir(), "other.conf")
	command := &fakeCommand{
		show: []commandResult{{out: fmt.Sprintf("LoadState=loaded\nFragmentPath=/tmp/unit\n"+
			"ExecStart={ path=%s ; argv[]=%s -start -config %s ; ignore_errors=no }\n"+
			"Environment=\nEnvironmentFiles=[]\nUnitFileState=disabled\nActiveState=inactive\nSubState=dead\n",
			paths["sysc-walls-daemon"], paths["sysc-walls-daemon"], other)}},
	}
	reads := 0
	read := func(string) ([]byte, error) { reads++; return []byte("[animation]\neffect=fire\n"), nil }
	service := newTestService(t, home, command, map[string]string{}, read, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown })
	if snap.ConfigSourceMatches || snap.ConfigKnown || snap.CanApply || snap.CanPreview {
		t.Fatalf("unsupported config source enabled actions: %+v", snap)
	}
	if reads != 0 {
		t.Fatalf("read guessed default config %d times", reads)
	}
}

func TestSavedArtworkMustBeReadableRegularFileInsideAllowedRootsForPreview(t *testing.T) {
	for _, tt := range []struct {
		name        string
		artwork     func(t *testing.T, home, allowed string) string
		wantPreview bool
	}{
		{"empty uses built-in artwork", func(*testing.T, string, string) string { return "" }, true},
		{"tilde path inside home share root", func(t *testing.T, home, allowed string) string {
			path := filepath.Join(allowed, "art.png")
			if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "~/" + strings.TrimPrefix(path, home+string(os.PathSeparator))
		}, true},
		{"relative path", func(*testing.T, string, string) string { return "art.png" }, false},
		{"missing file", func(_ *testing.T, _ string, allowed string) string { return filepath.Join(allowed, "missing.png") }, false},
		{"directory", func(_ *testing.T, _ string, allowed string) string { return allowed }, false},
		{"outside allowed roots", func(t *testing.T, _ string, _ string) string {
			path := filepath.Join(t.TempDir(), "outside.png")
			if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}, false},
		{"symlink escapes allowed root", func(t *testing.T, _ string, allowed string) string {
			outside := filepath.Join(t.TempDir(), "outside.png")
			if err := os.WriteFile(outside, []byte("image"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(allowed, "escape.png")
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			return path
		}, false},
		{"unreadable file", func(t *testing.T, _ string, allowed string) string {
			path := filepath.Join(allowed, "unreadable.png")
			if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			return path
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "unreadable file" && os.Geteuid() == 0 {
				t.Skip("root can read mode-000 files")
			}
			home := t.TempDir()
			allowed := filepath.Join(home, ".local", "share", "sysc-walls")
			if err := os.MkdirAll(allowed, 0o700); err != nil {
				t.Fatal(err)
			}
			artwork := tt.artwork(t, home, allowed)
			configPath := filepath.Join(home, ".config", "sysc-walls", "daemon.conf")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
				t.Fatal(err)
			}
			config := "[idle]\ntimeout=5m\n[animation]\neffect=fire\ntheme=rama\nfile=" + artwork + "\n"
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
			command := &fakeCommand{
				show: []commandResult{{out: loadedUnitOutput(paths["sysc-walls-daemon"], "disabled", "inactive", "dead", "", "[]")}},
				env:  []commandResult{{out: "HOME=" + home + "\n"}},
			}
			service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
			snapshot := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.ConfigKnown })
			if !snapshot.CanApply {
				t.Fatalf("saved artwork unexpectedly disabled unrelated Apply: %+v", snapshot)
			}
			if snapshot.CanPreview != tt.wantPreview {
				t.Fatalf("CanPreview = %v, want %v: %+v", snapshot.CanPreview, tt.wantPreview, snapshot)
			}
			if !tt.wantPreview && (snapshot.PreviewAvailabilityError == "" || !strings.Contains(strings.ToLower(snapshot.PreviewAvailabilityError), "artwork")) {
				t.Fatalf("invalid artwork was not explained: %+v", snapshot)
			}
		})
	}
}

func TestConfigSourceMismatchKeepsUnitManagementAvailable(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	other := filepath.Join(t.TempDir(), "other.conf")
	unit := fmt.Sprintf("LoadState=loaded\nFragmentPath=/tmp/unit\n"+
		"ExecStart={ path=%s ; argv[]=%s -start -config %s ; ignore_errors=no }\n"+
		"Environment=\nEnvironmentFiles=[]\nUnitFileState=disabled\nActiveState=inactive\nSubState=dead\n",
		paths["sysc-walls-daemon"], paths["sysc-walls-daemon"], other)
	command := &fakeCommand{show: []commandResult{{out: unit}}}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
	snapshot := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.ConfigSourceError != "" })
	if snapshot.ConfigSourceMatches || snapshot.CanApply || snapshot.CanPreview {
		t.Fatalf("mismatched config source enabled config actions: %+v", snapshot)
	}
	if !snapshot.ServiceAvailable {
		t.Fatalf("loaded unit management was disabled by the config mismatch: %+v", snapshot)
	}
	if !service.SetEnabled(true) {
		t.Fatal("could not queue enablement for a loaded unit with a config mismatch")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		calls := command.callsCopy()
		if len(calls) > 0 {
			if fmt.Sprint(calls[0]) != fmt.Sprint([]string{"systemctl", "--user", "enable", "--now", unitName}) {
				t.Fatalf("unit action = %q", calls[0])
			}
			return
		}
		select {
		case <-service.Updates():
		case <-deadline.C:
			t.Fatal("loaded unit did not receive enable action")
		}
	}
}

func TestExplicitDefaultConfigPathIsAccepted(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	configPath := filepath.Join(home, ".config", "sysc-walls", "daemon.conf")
	out := loadedUnitOutput(paths["sysc-walls-daemon"], "disabled", "inactive", "dead", "HOME=/different", "[]")
	out = strings.Replace(out, " -start ;", " -start -config "+configPath+" ;", 1)
	command := &fakeCommand{show: []commandResult{{out: out}}}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.ConfigKnown })
	if !snap.ConfigSourceMatches || !snap.CanApply || !snap.CanPreview {
		t.Fatalf("explicit path resolving to shell config was rejected: %+v", snap)
	}
}

func TestHomeAndEnvironmentFileOverridesDoNotReadDefaultConfig(t *testing.T) {
	for _, tt := range []struct {
		name     string
		env      string
		envFiles string
		manager  string
	}{
		{name: "unit HOME differs", env: "HOME=/different", envFiles: "[]"},
		{name: "manager HOME differs", env: "", envFiles: "[]", manager: "HOME=/different\n"},
		{name: "environment file", env: "", envFiles: "{ path=/tmp/walls.env (ignore_errors=no) }", manager: "HOME="},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
			command := &fakeCommand{
				show: []commandResult{{out: loadedUnitOutput(paths["sysc-walls-daemon"], "disabled", "inactive", "dead", tt.env, tt.envFiles)}},
			}
			if tt.env == "" && tt.envFiles == "[]" {
				command.env = []commandResult{{out: tt.manager}}
			}
			reads := 0
			read := func(string) ([]byte, error) { reads++; return nil, nil }
			service := newTestService(t, home, command, map[string]string{}, read, fakeIdentities(paths))
			snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown })
			if snap.ConfigSourceMatches || snap.ConfigKnown || snap.CanApply || snap.CanPreview {
				t.Fatalf("override was treated as shell config: %+v", snap)
			}
			if reads != 0 {
				t.Fatalf("read guessed default config %d times", reads)
			}
		})
	}
}

func TestMissingDisplayDisablesPreviewOnly(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client")
	command := &fakeCommand{
		show: []commandResult{{out: loadedUnitOutput(paths["sysc-walls-daemon"], "disabled", "inactive", "dead", "", "[]")}},
		env:  []commandResult{{out: "HOME=" + home + "\n"}},
	}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown })
	if !snap.CanApply || snap.CanPreview {
		t.Fatalf("missing display disabled unrelated capabilities incorrectly: %+v", snap)
	}
	if !strings.Contains(snap.DisplayIdentityError, "sysc-walls-display") {
		t.Fatalf("missing preview binary path not explained: %q", snap.DisplayIdentityError)
	}
}

func TestDirtyClientBuildDisablesApplyButKeepsPreview(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	identities := fakeIdentities(paths)
	identities[paths["sysc-walls-client"]] = buildInfo(clientPackage, configGateRevision, true)
	command := &fakeCommand{
		show: []commandResult{{out: loadedUnitOutput(paths["sysc-walls-daemon"], "disabled", "inactive", "dead", "", "[]")}},
		env:  []commandResult{{out: "HOME=" + home + "\n"}},
	}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, identities)
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown })
	if snap.CanApply || !snap.CanPreview {
		t.Fatalf("dirty client identity affected unrelated preview capability: %+v", snap)
	}
	if !strings.Contains(snap.ClientIdentityError, "vcs.modified") || !strings.Contains(snap.ClientIdentityError, paths["sysc-walls-client"]) {
		t.Fatalf("dirty client failure lacks path and reason: %q", snap.ClientIdentityError)
	}
}

func TestMissingUnitRejectsBinariesFromDifferentPrefixes(t *testing.T) {
	home := t.TempDir()
	paths := map[string]string{}
	for i, name := range []string{"sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display"} {
		path := makeInstall(t, filepath.Join(t.TempDir(), fmt.Sprintf("prefix-%d", i)), name)
		paths[name] = path[name]
	}
	command := &fakeCommand{show: []commandResult{{out: "LoadState=not-found\nUnitFileState=not-found\n"}}}
	service := newTestService(t, home, command, paths, os.ReadFile, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.ConfigKnown })
	if snap.CanApply || snap.CanPreview {
		t.Fatalf("mixed PATH prefixes were combined into a toolchain: %+v", snap)
	}
}

func TestConfigReadFailureKeepsUnitState(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	command := &fakeCommand{
		show: []commandResult{{out: loadedUnitOutput(paths["sysc-walls-daemon"], "enabled", "active", "running", "", "[]")}},
		env:  []commandResult{{out: "HOME=" + home + "\n"}},
	}
	readErr := func(string) ([]byte, error) { return nil, os.ErrPermission }
	service := newTestService(t, home, command, map[string]string{}, readErr, fakeIdentities(paths))
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && !s.ConfigReadable })
	if snap.ActiveState != "active" || snap.SubState != "running" {
		t.Fatalf("config error hid independent unit state: %+v", snap)
	}
	if snap.CanApply || snap.CanPreview || snap.ConfigReadError == "" {
		t.Fatalf("unreadable config did not disable dependent actions: %+v", snap)
	}
}

func TestRefreshFailureMarksUnitStaleThenRecovers(t *testing.T) {
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	command := &fakeCommand{
		show: []commandResult{
			{out: loadedUnitOutput(paths["sysc-walls-daemon"], "enabled", "active", "running", "", "[]")},
			{err: errors.New("systemctl unavailable")},
			{out: loadedUnitOutput(paths["sysc-walls-daemon"], "disabled", "inactive", "dead", "", "[]")},
		},
		env: []commandResult{{out: "HOME=" + home + "\n"}, {out: "HOME=" + home + "\n"}},
	}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
	first := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && !s.UnitStale })
	if first.ActiveState != "active" || !first.CanPreview {
		t.Fatalf("initial observation = %+v", first)
	}
	service.Refresh()
	stale := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitStale })
	if !stale.UnitKnown || stale.ActiveState != "active" || stale.StateError == "" || stale.CanPreview {
		t.Fatalf("failed refresh should retain stale values and disable stateful action: %+v", stale)
	}
	service.Refresh()
	recovered := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && !s.UnitStale && s.UnitFileState == "disabled" })
	if recovered.ActiveState != "inactive" || recovered.SubState != "dead" || !recovered.CanPreview {
		t.Fatalf("successful refresh did not recover: %+v", recovered)
	}
}

func TestInitialRefreshIsQueuedAtStartup(t *testing.T) {
	command := &fakeCommand{show: []commandResult{{out: "LoadState=not-found\nUnitFileState=not-found\n"}}}
	service := newTestService(t, t.TempDir(), command, nil, os.ReadFile, nil)
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown })
	if snap.LoadState != "not-found" {
		t.Fatalf("initial refresh snapshot = %+v", snap)
	}
}

func newActionService(t *testing.T, unitState, active, sub string, results map[string]commandResult) (*Service, *fakeCommand, map[string]string) {
	t.Helper()
	home := t.TempDir()
	paths := makeInstall(t, filepath.Join(t.TempDir(), "bin"), "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	unitOutput := loadedUnitOutput(paths["sysc-walls-daemon"], unitState, active, sub, "", "[]")
	command := &fakeCommand{
		show:    []commandResult{{out: unitOutput}, {out: unitOutput}},
		env:     []commandResult{{out: "HOME=" + home + "\n"}, {out: "HOME=" + home + "\n"}},
		results: results,
	}
	service := newTestService(t, home, command, map[string]string{}, os.ReadFile, fakeIdentities(paths))
	waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.CanApply })
	return service, command, paths
}

func TestApplyUsesOneBatchAndTryRestartsOnlyRunningUnit(t *testing.T) {
	service, command, paths := newActionService(t, "enabled", "active", "running", nil)
	service.Apply([]Setting{{Key: "effect", Value: "fire"}, {Key: "datetime", Value: "true"}})
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return strings.Contains(s.ActionMessage, "saved") })
	if snap.ActionError != "" || !strings.Contains(snap.ActionMessage, "saved") {
		t.Fatalf("Apply result = %+v", snap)
	}
	calls := command.callsCopy()
	want := [][]string{
		{paths["sysc-walls-client"], "set", "effect", "fire", "datetime", "true"},
		{"systemctl", "--user", "try-restart", unitName},
	}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("commands = %q, want %q", calls, want)
	}
}

func TestApplyDoesNotStartInactiveService(t *testing.T) {
	service, command, paths := newActionService(t, "disabled", "inactive", "dead", nil)
	service.Apply([]Setting{{Key: "theme", Value: "rama"}})
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionMessage != "" })
	if snap.ActionError != "" {
		t.Fatalf("Apply error = %q", snap.ActionError)
	}
	calls := command.callsCopy()
	want := [][]string{{paths["sysc-walls-client"], "set", "theme", "rama"}}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("commands = %q, want %q", calls, want)
	}
}

func TestApplyFailureSkipsRestartAndKeepsError(t *testing.T) {
	service, command, paths := newActionService(t, "enabled", "active", "running", nil)
	client := paths["sysc-walls-client"]
	command.mu.Lock()
	command.results = make(map[string]commandResult)
	command.results[client+" set effect fire"] = commandResult{out: "invalid effect", err: errors.New("exit status 1")}
	command.mu.Unlock()
	service.Apply([]Setting{{Key: "effect", Value: "fire"}})
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionError != "" })
	if !strings.Contains(snap.ActionError, "invalid effect") {
		t.Fatalf("client error did not include output: %q", snap.ActionError)
	}
	if calls := command.callsCopy(); len(calls) != 1 || calls[0][0] != client {
		t.Fatalf("failed Apply should skip restart, commands = %q", calls)
	}
}

func TestApplyRestartFailureKeepsSavedStateMessageAndRefreshes(t *testing.T) {
	service, command, paths := newActionService(t, "enabled", "active", "running", map[string]commandResult{
		"systemctl --user try-restart " + unitName: {out: "restart refused", err: errors.New("exit status 1")},
	})
	service.Apply([]Setting{{Key: "effect", Value: "fire"}})
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionError != "" })
	if !strings.Contains(snap.ActionError, "restart refused") || !strings.Contains(snap.ActionMessage, "saved") {
		t.Fatalf("restart failure result = %+v", snap)
	}
	if snap.UnitFileState != "enabled" || snap.ActiveState != "active" || snap.SubState != "running" {
		t.Fatalf("unit state did not refresh after failed restart: %+v", snap)
	}
	calls := command.callsCopy()
	if len(calls) != 2 || calls[0][0] != paths["sysc-walls-client"] || fmt.Sprint(calls[1]) != fmt.Sprint([]string{"systemctl", "--user", "try-restart", unitName}) {
		t.Fatalf("commands = %q", calls)
	}
}

func TestEnableAndRuntimeCommandsUseDistinctVerbs(t *testing.T) {
	for _, tt := range []struct {
		name      string
		unitState string
		run       func(*Service)
		want      []string
	}{
		{"enable", "disabled", func(s *Service) { s.SetEnabled(true) }, []string{"systemctl", "--user", "enable", "--now", unitName}},
		{"disable", "enabled", func(s *Service) { s.SetEnabled(false) }, []string{"systemctl", "--user", "disable", "--now", unitName}},
		{"runtime start", "disabled", func(s *Service) { s.SetRuntimeRunning(true) }, []string{"systemctl", "--user", "start", unitName}},
		{"runtime stop", "enabled", func(s *Service) { s.SetRuntimeRunning(false) }, []string{"systemctl", "--user", "stop", unitName}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service, command, _ := newActionService(t, tt.unitState, "inactive", "dead", nil)
			tt.run(service)
			snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionMessage != "" })
			if snap.ActionError != "" {
				t.Fatalf("command result = %+v", snap)
			}
			calls := command.callsCopy()
			if len(calls) != 1 || fmt.Sprint(calls[0]) != fmt.Sprint(tt.want) {
				t.Fatalf("commands = %q, want %q", calls, tt.want)
			}
		})
	}
}

func TestUnitActionsRejectUnavailableServiceState(t *testing.T) {
	command := &fakeCommand{show: []commandResult{{err: errors.New("systemctl unavailable")}}}
	service := newTestService(t, t.TempDir(), command, nil, os.ReadFile, nil)
	waitSnapshot(t, service, func(s Snapshot) bool { return s.StateError != "" })
	if service.SetEnabled(true) || service.SetRuntimeRunning(true) {
		t.Fatal("unavailable service action was queued")
	}
	if calls := command.callsCopy(); len(calls) != 0 {
		t.Fatalf("unavailable service received commands: %q", calls)
	}
}

func TestRuntimeStopAllowsMaskedUnit(t *testing.T) {
	service, command, _ := newActionService(t, "masked", "active", "running", nil)
	if service.SetRuntimeRunning(true) {
		t.Fatal("runtime start was queued for a masked unit")
	}
	if !service.SetRuntimeRunning(false) {
		t.Fatal("runtime stop was rejected for a masked but loaded unit")
	}
	snapshot := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionMessage != "" })
	if snapshot.ActionError != "" {
		t.Fatalf("runtime stop result = %+v", snapshot)
	}
	calls := command.callsCopy()
	want := []string{"systemctl", "--user", "stop", unitName}
	if len(calls) != 1 || fmt.Sprint(calls[0]) != fmt.Sprint(want) {
		t.Fatalf("commands = %q, want %q", calls, want)
	}
}

func TestValidateSettingsPatch(t *testing.T) {
	for _, tt := range []struct {
		name    string
		patch   []Setting
		wantErr bool
	}{
		{"valid batch", []Setting{{Key: "effect", Value: "fire"}, {Key: "datetime", Value: "true"}}, false},
		{"empty", nil, true},
		{"unknown key", []Setting{{Key: "shell.config", Value: "x"}}, true},
		{"duplicate", []Setting{{Key: "theme", Value: "rama"}, {Key: "theme", Value: "ocean"}}, true},
		{"nul", []Setting{{Key: "file", Value: "bad\x00path"}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateSettingsPatch(tt.patch); (err != nil) != tt.wantErr {
				t.Fatalf("validateSettingsPatch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseIdleTimeout(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Duration
		bad   bool
	}{
		{"1s", time.Second, false},
		{"60s", time.Minute, false},
		{"5m", 5 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"0s", 0, true},
		{"-1m", 0, true},
		{"1.5m", 0, true},
		{"1m30s", 0, true},
		{"500ms", 0, true},
		{"25h", 0, true},
		{"", 0, true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseIdleTimeout(tt.value)
			if tt.bad {
				if err == nil {
					t.Fatalf("parseIdleTimeout(%q) = %s, want error", tt.value, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseIdleTimeout(%q) = %s, %v, want %s", tt.value, got, err, tt.want)
			}
		})
	}
}

func TestPreviewDurationUsesSavedTimeoutAndCap(t *testing.T) {
	for _, tt := range []struct {
		name    string
		running bool
		timeout string
		stale   bool
		want    time.Duration
		wantErr bool
	}{
		{"active default", true, "5m", false, 10 * time.Second, false},
		{"active short", true, "20s", false, 10 * time.Second, false},
		{"active smaller than cap", true, "10s", false, 5 * time.Second, false},
		{"active minimum duration", true, "4s", false, 2 * time.Second, false},
		{"inactive ignores idle timeout", false, "invalid", false, 10 * time.Second, false},
		{"active malformed timeout", true, "bad", false, 0, true},
		{"active below minimum", true, "3s", false, 0, true},
		{"active one second computed duration", true, "2s", false, 0, true},
		{"active too short", true, "1s", false, 0, true},
		{"unknown state", false, "5m", true, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := Snapshot{UnitKnown: true, UnitStale: tt.stale, Timeout: tt.timeout}
			if tt.running {
				snapshot.ActiveState, snapshot.SubState = "active", "running"
			}
			got, err := previewDuration(snapshot)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("previewDuration() = %s, %v; want %s, error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func testPreviewDaemon(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", path, "./testdata/preview")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build preview child: %v: %s", err, output)
	}
}

func newPreviewTestService(t *testing.T, timeout, daemonScript string, newTimer func(time.Duration) timerHandle) (*Service, map[string]string, string, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "install")
	paths := makeInstall(t, dir, "sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("SYSC_WALLS_STARTED_FILE", ready+".started")
	if daemonScript == "" {
		t.Setenv("SYSC_WALLS_READY_FILE", ready)
		testPreviewDaemon(t, paths["sysc-walls-daemon"])
	} else if err := os.WriteFile(paths["sysc-walls-daemon"], []byte(daemonScript), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".config", "sysc-walls", "daemon.conf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[idle]\ntimeout="+timeout+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unitOutput := loadedUnitOutput(paths["sysc-walls-daemon"], "enabled", "active", "running", "", "[]")
	command := &fakeCommand{
		show:    make([]commandResult, 12),
		env:     make([]commandResult, 12),
		results: make(map[string]commandResult),
	}
	for i := range command.show {
		command.show[i] = commandResult{out: unitOutput}
		command.env[i] = commandResult{out: "HOME=" + home + "\n"}
	}
	deps := testDependencies(home, command, paths, os.ReadFile, fakeIdentities(paths))
	if newTimer != nil {
		deps.newTimer = newTimer
	}
	service := newServiceWithDependencies(deps)
	t.Cleanup(func() { _ = service.Close() })
	waitSnapshot(t, service, func(s Snapshot) bool { return s.UnitKnown && s.CanPreview })
	return service, paths, ready, dir
}

func TestPreviewOwnsAndStopsChild(t *testing.T) {
	service, _, ready, dir := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing })
	readyData := waitFile(t, ready)
	fields := strings.SplitN(string(readyData), "\n", 2)
	if len(fields) != 2 {
		t.Fatalf("preview child readiness = %q", readyData)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		t.Fatalf("preview child pid %q: %v", fields[0], err)
	}
	if got := strings.Split(fields[1], string(os.PathListSeparator))[0]; got != dir {
		t.Fatalf("preview PATH begins with %q, want selected install %q", got, dir)
	}
	if !service.StopPreview() {
		t.Fatal("Stop Preview request was not queued")
	}
	stopped := waitSnapshot(t, service, func(s Snapshot) bool { return !s.Previewing && strings.Contains(s.ActionMessage, "stopped") })
	if stopped.PreviewStopping || stopped.ActionError != "" {
		t.Fatalf("Stop Preview result = %+v", stopped)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("preview child pid %d still exists after Stop: %v", pid, err)
	}
}

func TestRefreshPreservesOwnedPreviewAndBlocksConflictingActions(t *testing.T) {
	service, _, _, _ := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing && s.PreviewReady })
	for {
		select {
		case <-service.Updates():
		default:
			goto drained
		}
	}
drained:
	service.Refresh()
	var refreshed Snapshot
	select {
	case <-service.Updates():
		refreshed = service.Snapshot()
	case <-time.After(time.Second):
		t.Fatal("explicit refresh did not publish a snapshot")
	}
	if !refreshed.Previewing || !refreshed.PreviewReady || refreshed.PreviewStopping {
		t.Fatalf("refresh lost the owned Preview session: %+v", refreshed)
	}
	if service.Apply([]Setting{{Key: "effect", Value: "fire"}}) {
		t.Fatal("Apply was queued while the Preview child remained owned")
	}
	if service.SetEnabled(true) {
		t.Fatal("normal service start was queued while the Preview child remained owned")
	}
}

func TestPreviewStartupDeadlineStopsUnreadyOwnedChild(t *testing.T) {
	t.Setenv("SYSC_WALLS_NO_READY", "1")
	factory, timers, durations := newManualTimerFactory()
	service, _, ready, _ := newPreviewTestService(t, "5m", "", factory)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing && !s.PreviewReady })
	started := waitFile(t, ready+".started")
	pid, err := strconv.Atoi(strings.TrimSpace(string(started)))
	if err != nil {
		t.Fatalf("preview child pid %q: %v", started, err)
	}
	if len(*timers) != 2 || (*durations)[0] != previewReadyTimeout || (*durations)[1] != previewMax {
		t.Fatalf("Preview timers = %v, want startup %s and overall %s", *durations, previewReadyTimeout, previewMax)
	}
	(*timers)[0].C <- time.Now()
	snapshot := waitSnapshot(t, service, func(s Snapshot) bool { return !s.Previewing })
	if !strings.Contains(snapshot.PreviewError, "readiness") {
		t.Fatalf("startup timeout was not reported: %+v", snapshot)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("unready Preview child %d was not stopped and reaped: %v", pid, err)
	}
}

func TestOverallPreviewDeadlineStopsUnreadyOwnedChild(t *testing.T) {
	t.Setenv("SYSC_WALLS_NO_READY", "1")
	factory, timers, _ := newManualTimerFactory()
	service, _, ready, _ := newPreviewTestService(t, "5m", "", factory)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing && !s.PreviewReady })
	started := waitFile(t, ready+".started")
	pid, err := strconv.Atoi(strings.TrimSpace(string(started)))
	if err != nil {
		t.Fatalf("preview child pid %q: %v", started, err)
	}
	if len(*timers) != 2 {
		t.Fatalf("Preview created %d timers, want startup and overall deadlines", len(*timers))
	}
	(*timers)[1].C <- time.Now()
	snapshot := waitSnapshot(t, service, func(s Snapshot) bool { return !s.Previewing })
	if !strings.Contains(snapshot.PreviewError, "safety deadline") {
		t.Fatalf("overall startup bound was not reported: %+v", snapshot)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("unready Preview child %d was not stopped at the overall deadline: %v", pid, err)
	}
}

func TestCloseStopsAndReapsOwnedPreviewBeforeReadiness(t *testing.T) {
	t.Setenv("SYSC_WALLS_NO_READY", "1")
	service, _, ready, _ := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing && !s.PreviewReady })
	started := waitFile(t, ready+".started")
	pid, err := strconv.Atoi(strings.TrimSpace(string(started)))
	if err != nil {
		t.Fatalf("preview child pid %q: %v", started, err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close() failed to use safe cleanup for an unready child: %v", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("unready Preview child %d still exists after Close: %v", pid, err)
	}
}

func TestCloseWaitsForUnresponsivePreviewAfterCleanupTimeout(t *testing.T) {
	t.Setenv("SYSC_WALLS_NO_READY", "1")
	t.Setenv("SYSC_WALLS_IGNORE_TERM", "1")
	service, _, ready, _ := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing && !s.PreviewReady })
	started := waitFile(t, ready+".started")
	pid, err := strconv.Atoi(strings.TrimSpace(string(started)))
	if err != nil {
		t.Fatalf("preview child pid %q: %v", started, err)
	}
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
				select {
				case <-time.After(5 * time.Millisecond):
				case <-deadline.C:
					return
				}
			}
		}
	})
	closeDone := make(chan error, 1)
	go func() { closeDone <- service.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close() returned while the unresponsive Preview child was still alive: %v", err)
	case <-time.After(previewCleanup + 100*time.Millisecond):
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("Preview child %d exited before cleanup ownership was released: %v", pid, err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("end unresponsive Preview child %d for shutdown test: %v", pid, err)
	}
	select {
	case err := <-closeDone:
		if err == nil || !strings.Contains(err.Error(), "did not finish cleanup") {
			t.Fatalf("Close() = %v, want the cleanup timeout after the child exited", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after the owned Preview child exited")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("Preview child %d was not reaped after shutdown: %v", pid, err)
	}
}

type manualTimer struct {
	C       chan time.Time
	stopped bool
	mu      sync.Mutex
}

func (t *manualTimer) isStopped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}

func newManualTimerFactory() (func(time.Duration) timerHandle, *[]*manualTimer, *[]time.Duration) {
	timers := make([]*manualTimer, 0, 2)
	durations := make([]time.Duration, 0, 2)
	factory := func(duration time.Duration) timerHandle {
		timer := &manualTimer{C: make(chan time.Time, 1)}
		timers = append(timers, timer)
		durations = append(durations, duration)
		return timerHandle{C: timer.C, stop: func() bool {
			timer.mu.Lock()
			timer.stopped = true
			timer.mu.Unlock()
			return true
		}}
	}
	return factory, &timers, &durations
}

func TestPreviewDeadlineUsesInjectedTimerAndStopsOwnedChild(t *testing.T) {
	factory, timers, durations := newManualTimerFactory()
	service, _, ready, _ := newPreviewTestService(t, "10s", "", factory)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing })
	readyData := waitFile(t, ready)
	waitSnapshot(t, service, func(s Snapshot) bool { return s.PreviewReady })
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(readyData), "\n", 2)[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(*durations) < 2 || (*durations)[0] != previewReadyTimeout || (*durations)[1] != 5*time.Second {
		t.Fatalf("preview timer durations = %v, want startup 5s then preview 5s", *durations)
	}
	(*timers)[1].C <- time.Now()
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return !s.Previewing && strings.Contains(s.ActionMessage, "time limit") })
	if snap.ActionError != "" || !(*timers)[0].isStopped() || !(*timers)[1].isStopped() {
		t.Fatalf("timed Preview result = %+v, timers stopped=%v/%v", snap, (*timers)[0].isStopped(), (*timers)[1].isStopped())
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("timed Preview child pid %d still exists: %v", pid, err)
	}
}

func TestDuplicatePreviewRequestsOwnAtMostOneChild(t *testing.T) {
	service, _, ready, _ := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() || !service.Preview() {
		t.Fatal("duplicate Preview requests were not queued")
	}
	snap := waitSnapshot(t, service, func(s Snapshot) bool { return !s.Previewing && s.ActionMessage == "Preview stopped." })
	if snap.ActionError != "" {
		t.Fatalf("second Preview request failed to stop first: %+v", snap)
	}
	waitFile(t, ready)
	count, err := os.ReadFile(ready + ".count")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Fields(string(count)); len(lines) != 1 {
		t.Fatalf("Preview spawned %d owned children, want exactly 1 (%q)", len(lines), count)
	}
}

func TestCloseStopsAndReapsOwnedPreview(t *testing.T) {
	service, _, ready, _ := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitSnapshot(t, service, func(s Snapshot) bool { return s.Previewing })
	readyData := waitFile(t, ready)
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(readyData), "\n", 2)[0]))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if service.Snapshot().Previewing {
		t.Fatalf("snapshot still reports Preview after Close: %+v", service.Snapshot())
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("preview child pid %d still exists after Close: %v", pid, err)
	}
}

func TestEnableIsRejectedWhilePreviewOwnsAChild(t *testing.T) {
	service, _, ready, _ := newPreviewTestService(t, "5m", "", nil)
	if !service.Preview() {
		t.Fatal("Preview request was not queued")
	}
	waitFile(t, ready)
	waitSnapshot(t, service, func(s Snapshot) bool { return s.PreviewReady })
	if service.SetEnabled(true) {
		t.Fatal("enablement was queued while Preview owned a child")
	}
	if service.SetRuntimeRunning(true) {
		t.Fatal("runtime start was queued while Preview owned a child")
	}
	if service.Apply([]Setting{{Key: "effect", Value: "fire"}}) {
		t.Fatal("Apply was queued while Preview owned a child")
	}
}

func TestPreviewChildExitAndStartFailureClearSession(t *testing.T) {
	t.Run("early exit stops deadlines", func(t *testing.T) {
		factory, timers, _ := newManualTimerFactory()
		service, _, _, _ := newPreviewTestService(t, "5m", "#!/bin/sh\nexit 0\n", factory)
		if !service.Preview() {
			t.Fatal("Preview request was not queued")
		}
		waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionMessage == "Preview session ended." })
		if len(*timers) != 2 || !(*timers)[0].isStopped() || !(*timers)[1].isStopped() {
			t.Fatalf("early exit left Preview timers live: %+v", *timers)
		}
	})
	t.Run("early exit", func(t *testing.T) {
		service, _, _, _ := newPreviewTestService(t, "5m", "#!/bin/sh\nexit 0\n", nil)
		if !service.Preview() {
			t.Fatal("Preview request was not queued")
		}
		snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionMessage == "Preview session ended." || s.ActionError != "" })
		if snap.Previewing {
			t.Fatalf("early child exit retained Preview session: %+v", snap)
		}
	})
	t.Run("spawn failure", func(t *testing.T) {
		service, _, _, _ := newPreviewTestService(t, "5m", "", nil)
		service.deps.startPreview = func(string, []string) (*previewChild, error) { return nil, errors.New("spawn denied") }
		if !service.Preview() {
			t.Fatal("Preview request was not queued")
		}
		snap := waitSnapshot(t, service, func(s Snapshot) bool { return s.ActionError != "" })
		if snap.Previewing || !strings.Contains(snap.PreviewError, "spawn denied") {
			t.Fatalf("spawn failure result = %+v", snap)
		}
	})
}

func waitFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return data
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", path)
		}
	}
}

func TestConfigureIdleDoesNotEnableAfterTimeoutFailure(t *testing.T) {
	service, command, paths := newActionService(t, "disabled", "inactive", "dead", nil)
	command.mu.Lock()
	command.results = map[string]commandResult{paths["sysc-walls-client"] + " set timeout 3m": {err: errors.New("denied")}}
	command.mu.Unlock()
	if err := service.ConfigureIdle(true, "3m"); err == nil {
		t.Fatal("failed timeout reported success")
	}
	for _, call := range command.callsCopy() {
		if slices.Contains(call, "enable") {
			t.Fatalf("enabled after timeout failure: %v", call)
		}
	}
}
func TestConfigureIdleReturnsUnitFailure(t *testing.T) {
	service, _, _ := newActionService(t, "enabled", "active", "running", map[string]commandResult{"systemctl --user disable --now sysc-walls.service": {err: errors.New("denied")}})
	if err := service.ConfigureIdle(false, ""); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("disable result: %v", err)
	}
}
