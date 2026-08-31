package theming

import (
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

func TestApplyEnabledWritesAlacrittyUnderXDG(t *testing.T) {
	home := t.TempDir()
	only := func(name string) bool { return name == "alacritty" }
	if err := ApplyEnabled(home, only, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), marker) {
		t.Fatalf("missing marker in %s", p)
	}
	sand := filepath.Join(home, ".config", "sysc-shell", "themes", "alacritty.conf")
	if _, err := os.Stat(sand); !os.IsNotExist(err) {
		t.Fatalf("wrote sandbox path %s", sand)
	}
}

func TestApplyEnabledSkipsForeignKitty(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".config", "kitty", "kitty.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("font_size 12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "kitty" }
	if err := ApplyEnabled(home, only, theme.Fallback); err == nil {
		t.Fatal("foreign kitty.conf must be reported")
	}
	got, _ := os.ReadFile(p)
	if string(got) != "font_size 12\n" {
		t.Fatalf("rewrote user kitty.conf: %q", got)
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
	home := t.TempDir()
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
	if err := ApplyEnabled(home, only, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("kitty process did not receive SIGUSR1")
	}
}

func TestApplyEnabledSingleFlight(t *testing.T) {
	home := t.TempDir()
	only := func(name string) bool { return name == "alacritty" }
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_ = ApplyEnabled(home, only, theme.Fallback)
		}()
	}
	wg.Wait()
	p := filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), marker) {
		t.Fatalf("interleaved write: %q", b)
	}
}

func TestApplyEnabledReportsFirstError(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "alacritty" }
	err := ApplyEnabled(home, only, theme.Fallback)
	if err == nil {
		t.Fatal("expected skip error")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyEnabledSupersedeUsesLatestHome(t *testing.T) {
	home1 := t.TempDir()
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
	go func() { done <- ApplyEnabled(home1, first, theme.Fallback) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first apply did not reach alacritty")
	}
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- ApplyEnabled(home2, func(name string) bool { return name == "alacritty" }, theme.Fallback)
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
