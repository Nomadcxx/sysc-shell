package shell

import (
	"bufio"
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/compositor"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

func TestInputPageWritesInclude(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		kind          compositor.Kind
		want          bool
	}{
		{"sysc", "", compositor.SyscCompositor, true}, {"niri-current", "26.04", compositor.Niri, true}, {"niri-old", "25.11", compositor.Niri, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.kdl")
			if err := os.WriteFile(path, []byte("input { touchpad { tap-button-map \"left-middle-right\"; }; }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			state := loadInputSettings(dir, tc.kind, tc.version, t.TempDir())
			cfg := config.Default()
			err := applyInputSettings(&cfg, []settingsWriteChange{{path: "input.touchpad.tap", value: "true"}}, state)
			if tc.want {
				if err != nil {
					t.Fatal(err)
				}
				b, err := os.ReadFile(filepath.Join(dir, "sysc-shell-input.kdl"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(b), "tap-button-map") || !strings.Contains(string(b), "tap\n") {
					t.Fatalf("settings lost: %s", b)
				}
				if cfg.Input.Touchpad.Tap == nil || !*cfg.Input.Touchpad.Tap {
					t.Fatal("shell setting not updated")
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "Needs Niri 26.04 or sysc-compositor") {
					t.Fatalf("old version error=%v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "sysc-shell-input.kdl")); !os.IsNotExist(err) {
					t.Fatal("old Niri config written")
				}
			}
		})
	}
}

func TestInputPageParseErrorWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.kdl"), []byte("input { touchpad {"), 0600); err != nil {
		t.Fatal(err)
	}
	state := loadInputSettings(dir, compositor.SyscCompositor, "", t.TempDir())
	cfg := config.Default()
	if err := applyInputSettings(&cfg, []settingsWriteChange{{path: "input.mouse.natural-scroll", value: "true"}}, state); err == nil {
		t.Fatal("bad config accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "sysc-shell-input.kdl")); !os.IsNotExist(err) {
		t.Fatal("invalid config written")
	}
}

func TestInputPageSeparatesGeneralAndDisablesUnavailableControls(t *testing.T) {
	h := newSettingsHost()
	h.section, h.settingsPage = "Session", "Mouse & touchpad"
	h.set = settings.DefaultFor(h.draft, settings.WithInput(config.Input{}, true, "Needs Niri 26.04 or sysc-compositor"))
	tree := settingsTree(nil, h)
	if findText(tree, "Tap to click") == nil {
		t.Fatal("input page is missing")
	}
	if findText(tree, "Authentication agent") != nil {
		t.Fatal("General settings leaked into input page")
	}
	e := h.set.ByPath("input.touchpad.tap")
	row := settingsEntryRow(h, *e, 600)
	for _, node := range walk(row) {
		if strings.Contains(node.Action, "input.touchpad.tap") && (node.Focusable || !node.AriaDisabled) {
			t.Fatalf("unavailable input control active: %+v", node)
		}
	}
	h.settingsPage = "General"
	tree = settingsTree(nil, h)
	if findText(tree, "Tap to click") != nil {
		t.Fatal("input settings leaked into General page")
	}
}

func TestInputPagePersistsInDetectedConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SYSC_SOCKET", "/run/user/1000/sysc.sock")
	t.Setenv("NIRI_SOCKET", "/run/user/1000/sysc-compositor.wayland-1.7.sock")
	dir := filepath.Join(home, ".config", "sysc-compositor")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.kdl"), []byte("input { touchpad { tap-button-map \"left-middle-right\"; }; }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	path := filepath.Join(home, "shell.json")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	r := newSettingsRegistry(t, cfg)
	r.BindPersist(path, nil)
	h := newSettingsHost()
	applied, err := r.writeSettingsChanges(h, []settingsWriteChange{{path: "input.touchpad.tap", value: "true"}}, 0, path, cfg, nil)
	if err != nil || !applied {
		t.Fatalf("persist=%t,%v", applied, err)
	}
	got, err := config.LoadRequired(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Input.Touchpad.Tap == nil || !*got.Input.Touchpad.Tap {
		t.Fatal("input setting not persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, "sysc-shell-input.kdl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "niri")); !os.IsNotExist(err) {
		t.Fatal("Niri config touched")
	}
}

func TestInputPageVersionReadDoesNotBlockOwnerEdits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SYSC_SOCKET", "")
	listener, err := net.Listen("unix", filepath.Join(sockpath.Dir(t, "input-version"), "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("NIRI_SOCKET", listener.Addr().String())
	requested, release := make(chan struct{}), make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		close(requested)
		<-release
		_, _ = fmt.Fprintln(conn, `{"Ok":{"Version":"26.04"}}`)
	}()
	cfg := config.Default()
	r := newSettingsRegistry(t, cfg)
	h := newSettingsHost()
	path := filepath.Join(t.TempDir(), "shell.json")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	r.BindPersist(path, nil)
	finished := make(chan bool, 1)
	go func() {
		applied, _ := r.writeSettingsChanges(h, []settingsWriteChange{{path: "input.mouse.natural-scroll", value: "true"}}, 0, path, cfg, nil)
		finished <- applied
	}()
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("version request did not start")
	}
	edited := make(chan struct{})
	go func() {
		r.mu.Lock()
		r.settingsWriteMu.Lock()
		h.settingsWriteRevision++
		h.settingsWriteChanges = []settingsWriteChange{{path: "input.mouse.natural-scroll", value: "false"}}
		r.settingsWriteMu.Unlock()
		r.mu.Unlock()
		close(edited)
	}()
	select {
	case <-edited:
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-finished
		<-edited
		t.Fatal("compositor version read held the settings lock needed by the owner")
	}
	close(release)
	if applied := <-finished; applied {
		t.Fatal("write invalidated by a newer edit was applied")
	}
	r.settingsWriteMu.Lock()
	pending := len(h.settingsWriteChanges)
	r.settingsWriteMu.Unlock()
	if pending != 1 {
		t.Fatal("newer edit was cleared")
	}
}
