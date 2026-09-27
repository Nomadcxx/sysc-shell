package theming

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// markTemplatesComplete lets mechanism tests exercise the write path of a
// template the release has not verified yet (GH #7 gate).
func markTemplatesComplete(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if !completeTemplates[n] {
			completeTemplates[n] = true
			t.Cleanup(func() { delete(completeTemplates, n) })
		}
	}
}

func TestApplyEnabledReportsRefusalsPerTemplate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "cava")
	target := filepath.Join(home, ".config", "cava", "config")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "cava" }
	outcomes, _ := ApplyEnabled(home, only, theme.Fallback, nil)
	if !errors.Is(outcomes["cava"], ErrUserModified) {
		t.Fatalf("outcomes = %v, want a cava refusal", outcomes)
	}

	outcomes, err := ApplyEnabled(home, only, theme.Fallback, only)
	if err != nil {
		t.Fatalf("forced apply: %v", err)
	}
	if len(outcomes) != 0 {
		t.Fatalf("forced outcomes = %v", outcomes)
	}
	if got, _ := os.ReadFile(target); !strings.Contains(string(got), marker) {
		t.Fatalf("forced write = %q", got)
	}
	if _, err := os.Stat(target + ".bak"); err != nil {
		t.Fatal("the refused bytes were not backed up")
	}
}

func TestApplyEnabledWritesAlacrittyUnderXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "alacritty")
	only := func(name string) bool { return name == "alacritty" }
	if _, err := ApplyEnabled(home, only, theme.Fallback, nil); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(home, ".config", "alacritty", "themes", "sysc-shell.toml")
	b, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), marker) {
		t.Fatalf("missing marker in %s", sidecar)
	}
	cfg, err := os.ReadFile(filepath.Join(home, ".config", "alacritty", "alacritty.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `import = ["`+sidecar+`"]`) {
		t.Fatalf("import directive missing: %q", cfg)
	}
}

func TestApplyEnabledSkipsForeignKitty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	p := filepath.Join(home, ".config", "kitty", "kitty.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	user := "font_size 12\ninclude themes/user.conf\n"
	if err := os.WriteFile(p, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "kitty" }
	if _, err := ApplyEnabled(home, only, theme.Fallback, nil); err == nil {
		t.Fatal("user-written include must be reported")
	}
	got, _ := os.ReadFile(p)
	if string(got) != user {
		t.Fatalf("rewrote user kitty.conf: %q", got)
	}
}

func TestApplyEnabledForceReplacesDirectiveWithBackup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "ghostty")
	config := filepath.Join(home, ".config", "ghostty", "config")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "theme = catppuccin\nfont-size = 12\n"
	if err := os.WriteFile(config, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "ghostty" }
	outcomes, err := ApplyEnabled(home, only, theme.Fallback, only)
	if err != nil || len(outcomes) != 0 {
		t.Fatalf("forced apply = %v, %v", outcomes, err)
	}
	got, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(got), "theme = sysc-shell") || !strings.Contains(string(got), "font-size = 12") {
		t.Fatalf("config after force = %q, %v", got, err)
	}
	backup, err := os.ReadFile(config + ".bak")
	if err != nil || string(backup) != original {
		t.Fatalf("config backup = %q, %v", backup, err)
	}
	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestKittyPIDsFromProc(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeComm := func(pid, comm string) {
		dir := filepath.Join(root, pid)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeComm("100", "kitty")
	writeComm("101", "niri")
	writeComm("102", "kitty")
	got := kittyPIDs(root)
	if len(got) != 2 || got[0] != 100 || got[1] != 102 {
		t.Fatalf("kittyPIDs = %v, want [100 102]", got)
	}
}

func TestApplyEnabledSignalsKitty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	got := make(chan os.Signal, 1)
	signal.Notify(got, syscall.SIGUSR1)
	t.Cleanup(func() { signal.Stop(got) })
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(os.Getpid()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("kitty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = prev })
	only := func(name string) bool { return name == "kitty" }
	if _, err := ApplyEnabled(home, only, theme.Fallback, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("kitty process did not receive SIGUSR1")
	}
}

func TestApplyEnabledSingleFlight(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "alacritty")
	only := func(name string) bool { return name == "alacritty" }
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = ApplyEnabled(home, only, theme.Fallback, nil)
		}()
	}
	wg.Wait()
	p := filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "import = [") != 1 {
		t.Fatalf("interleaved write: %q", b)
	}
	sidecar, _ := os.ReadFile(filepath.Join(home, ".config", "alacritty", "themes", "sysc-shell.toml"))
	if !strings.Contains(string(sidecar), marker) {
		t.Fatalf("sidecar missing marker: %q", sidecar)
	}
}

func TestApplyEnabledReportsFirstError(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "alacritty")
	p := filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`import = ["user.toml"]`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "alacritty" }
	_, err := ApplyEnabled(home, only, theme.Fallback, nil)
	if err == nil {
		t.Fatal("expected skip error")
	}
	if !errors.Is(err, ErrUserModified) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyEnabledSupersedeUsesLatestHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home1 := t.TempDir()
	markTemplatesComplete(t, "alacritty")
	home2 := t.TempDir()
	started := make(chan struct{})
	block := make(chan struct{})
	first := func(name string) bool {
		if name != "alacritty" {
			return false
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-block
		return true
	}
	done := make(chan error, 1)
	go func() { _, err := ApplyEnabled(home1, first, theme.Fallback, nil); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first apply did not reach alacritty")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := ApplyEnabled(home2, func(name string) bool { return name == "alacritty" }, theme.Fallback, nil)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	p2 := filepath.Join(home2, ".config", "alacritty", "alacritty.toml")
	if _, err := os.Stat(p2); err != nil {
		t.Fatalf("latest home missing alacritty.toml: %v", err)
	}
}

func TestApplyEnabledGatesIncompleteTemplates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	on := func(name string) bool { return true }
	if _, err := ApplyEnabled(home, on, theme.Fallback, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range Catalog().Names() {
		if Complete(name) {
			continue
		}
		targets := []string{writeTarget(home, name)}
		if tgt, ok := templateTargets[name]; ok {
			targets = append(targets, tgt.sidecar(home))
			for _, dir := range tgt.directives(home) {
				targets = append(targets, dir.file)
			}
		}
		for _, p := range targets {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("incomplete template %s wrote %s (GH #7)", name, p)
			}
		}
	}
}
