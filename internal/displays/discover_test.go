package displays

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/compositor"
)

func TestDiscoverNativeProcessPolicy(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"niri", "sysc-compositor"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "config.kdl"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name      string
		argv, env []string
		want      string
	}{
		{"niri default", []string{"niri"}, []string{"XDG_CONFIG_HOME=" + dir}, filepath.Join(dir, "niri/config.kdl")},
		{"sysc default", []string{"sysc-compositor"}, []string{"XDG_CONFIG_HOME=" + dir, "NIRI_CONFIG=/wrong"}, filepath.Join(dir, "sysc-compositor/config.kdl")},
		{"sysc explicit environment", []string{"sysc-compositor"}, []string{"SYSC_COMPOSITOR_CONFIG=" + filepath.Join(dir, "sysc-compositor/config.kdl")}, filepath.Join(dir, "sysc-compositor/config.kdl")},
		{"relative argument", []string{"sysc-compositor", "-c", "sysc-compositor/config.kdl"}, nil, filepath.Join(dir, "sysc-compositor/config.kdl")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := discoverProcess(tc.argv, tc.env, dir)
			if err != nil || got.Root != tc.want || got.ReadOnly {
				t.Fatalf("discovery = %+v, %v; want %s writable", got, err, tc.want)
			}
		})
	}
}

func TestDiscoverUsesExistingNativeFallback(t *testing.T) {
	dir := t.TempDir()
	got, err := discoverProcess([]string{"sysc-compositor"}, []string{"XDG_CONFIG_HOME=" + dir}, dir)
	if err != nil || got.Root != "/etc/sysc-compositor/config.kdl" || !got.ReadOnly {
		t.Fatalf("discovery = %+v, %v", got, err)
	}
}

func TestConfigFromArgv(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
		ok   bool
	}{
		{"short flag", []string{"niri", "-c", "/etc/niri.conf.kdl"}, "/etc/niri.conf.kdl", true},
		{"long flag", []string{"niri", "--config", "/etc/niri.conf.kdl"}, "/etc/niri.conf.kdl", true},
		{"joined value", []string{"niri", "--config=/etc/niri.conf.kdl"}, "/etc/niri.conf.kdl", true},
		{"later argument", []string{"niri", "--session", "x", "-c", "/a.kdl", "focused"}, "/a.kdl", true},
		// Everything after -- belongs to a command niri was asked to run.
		{"after the separator", []string{"niri", "--", "msg", "-c", "/a.kdl"}, "", false},
		{"before the separator", []string{"niri", "-c", "/a.kdl", "--", "msg"}, "/a.kdl", true},
		{"missing value", []string{"niri", "-c"}, "", false},
		{"empty value", []string{"niri", "--config="}, "", false},
		{"no flag", []string{"niri", "wayland"}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := configFromArgv(tc.argv)
			if got != tc.want || ok != tc.ok {
				t.Errorf("configFromArgv(%q) = %q, %v; want %q, %v", tc.argv, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestEnvValue(t *testing.T) {
	env := []string{"PATH=/bin", "NIRI_CONFIG=/a.kdl", "NIRI_CONFIGURE_ONLY=1"}
	if got, ok := envValue(env, "NIRI_CONFIG"); !ok || got != "/a.kdl" {
		t.Errorf("NIRI_CONFIG = %q, %v; want /a.kdl, true", got, ok)
	}
	// A prefix match is not a match: NIRI_CONFIG must not find
	// NIRI_CONFIGURE_ONLY.
	if _, ok := envValue(env, "NIRI_CONFIGURE_ONLY"); !ok {
		t.Error("NIRI_CONFIGURE_ONLY was not found")
	}
	if _, ok := envValue(env, "NIRI_SOCKET"); ok {
		t.Error("NIRI_SOCKET was invented")
	}
}

func TestProposalFlagsRelativeAndSystemRoots(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "config.kdl")
	if err := os.WriteFile(mine, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// An absolute writable root needs no confirmation: it was read from the
	// compositor's own arguments.
	d, err := proposal(mine, "the compositor's -c argument", false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Confirm || d.ReadOnly || d.Root != mine {
		t.Errorf("proposal(%q) = %+v, want the path, unconfirmed and writable", mine, d)
	}

	// A relative path is read by the compositor against its own working
	// directory, which the shell cannot see, so it stays a guess.
	d, err = proposal("config.kdl", "the compositor's home directory", false)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Confirm || d.ReadOnly || !strings.Contains(d.Note, "relative") {
		t.Errorf("proposal(relative) = %+v, want a confirmed relative guess", d)
	}

	// The system configuration is niri's, not the user's.
	d, err = proposal(systemConfigPath, "niri's system configuration", true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReadOnly || !strings.Contains(d.Note, "system-wide") {
		t.Errorf("proposal(system) = %+v, want read-only", d)
	}

	// A file this user cannot write is never edited by the shell.
	locked := filepath.Join(dir, "locked.kdl")
	if err := os.WriteFile(locked, []byte("\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0400 file")
	}
	d, err = proposal(locked, "NIRI_CONFIG in the compositor's environment", false)
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReadOnly || !strings.Contains(d.Note, "not writable") {
		t.Errorf("proposal(unwritable) = %+v, want read-only with a reason", d)
	}
}

func TestNotWritable(t *testing.T) {
	dir := t.TempDir()
	if os.Geteuid() == 0 {
		t.Skip("root writes everything")
	}

	// A file that does not exist yet is writable when its directory is: niri
	// creates it, and Setup refuses to bootstrap one it cannot write.
	missing := filepath.Join(dir, "absent.kdl")
	if readOnly, why := notWritable(missing); readOnly {
		t.Errorf("notWritable(absent in a writable dir) = %q, want writable", why)
	}

	readOnlyDir := filepath.Join(dir, "locked")
	if err := os.Mkdir(readOnlyDir, 0o500); err != nil {
		t.Fatal(err)
	}
	if readOnly, why := notWritable(filepath.Join(readOnlyDir, "absent.kdl")); !readOnly {
		t.Error("notWritable(absent in an unwritable dir) said writable")
	} else if !strings.Contains(why, "not writable") {
		t.Errorf("reason = %q, want it to say the directory is not writable", why)
	}
}

func TestDiscoverReadsTheCompositorItFound(t *testing.T) {
	fake := newFakeNiri(t, filepath.Join(t.TempDir(), "niri.log"))
	d, err := Discover(fake.start(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// The fake runs in this process, so its own environment is what is read.
	// The environment of a process cannot be changed after exec, so the
	// branches above it are covered by the table tests instead of by faking
	// /proc. What this proves is the part that cannot be table tested: the
	// peer credentials of the socket identify the process to read.
	if !d.Confirm {
		t.Errorf("Discover = %+v, want a proposal that needs confirming", d)
	}
	if !filepath.IsAbs(d.Root) || !strings.HasSuffix(d.Root, filepath.Join("niri", "config.kdl")) {
		t.Errorf("Discover.Root = %q, want an absolute niri/config.kdl", d.Root)
	}
	if d.Root != envOr(t, "XDG_CONFIG_HOME")+"/niri/config.kdl" && d.Root != envOr(t, "HOME")+"/.config/niri/config.kdl" {
		t.Errorf("Discover.Root = %q, want it derived from this process's own environment", d.Root)
	}
}

func envOr(t *testing.T, key string) string {
	t.Helper()
	if v, ok := envValue(os.Environ(), key); ok {
		return v
	}
	return ""
}

// Each compositor reads only its own confirmed root.
func TestSavedRootIsPerCompositor(t *testing.T) {
	saved := config.Displays{NiriConfigPath: "/n/config.kdl"}
	if got := SavedRoot(compositor.SyscCompositor, saved); got != "" {
		t.Errorf("sysc-compositor reused the Niri root %q", got)
	}
	saved.SyscConfigPath = "/s/config.kdl"
	if got := SavedRoot(compositor.SyscCompositor, saved); got != "/s/config.kdl" {
		t.Errorf("sysc-compositor root = %q", got)
	}
	if got := SavedRoot(compositor.Niri, saved); got != "/n/config.kdl" {
		t.Errorf("niri root = %q", got)
	}
}
