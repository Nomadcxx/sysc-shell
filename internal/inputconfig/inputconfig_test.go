package inputconfig

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestReadSectionsFollowsIncludes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	put(t, path, `input { touchpad { tap; drag-lock; }; }; include "in.kdl"`)
	put(t, filepath.Join(dir, "in.kdl"), `input { touchpad { tap; tap-button-map "left-middle-right"; }; }`)
	touch, mouse, err := ReadSections(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(touch, Section{`tap`, `tap-button-map "left-middle-right"`}) || len(mouse) != 0 {
		t.Fatalf("%q %q", touch, mouse)
	}
}
func TestRenderKeepsUnshownSettings(t *testing.T) {
	off, speed := false, 0.3
	got := Render(Section{`tap`, `tap-button-map "left-middle-right"`, `natural-scroll`}, config.Pointer{NaturalScroll: &off, AccelSpeed: &speed}, "touchpad")
	want := "touchpad {\n    tap\n    tap-button-map \"left-middle-right\"\n    accel-speed 0.3\n}"
	if got != want {
		t.Fatalf("%q want %q", got, want)
	}
}
func TestWriteAddsIncludeLastOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	original := "// keep my comment\ninput { touchpad { tap; drag-lock; }; }\n"
	put(t, path, original)
	on := true
	for i := 0; i < 2; i++ {
		if err := Write(dir, config.Input{Touchpad: config.Pointer{Tap: &on}}, Section{`drag-lock`}, nil); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	if string(b) != original+includeLine+"\n" {
		t.Fatalf("config changed: %s", b)
	}
	b, _ = os.ReadFile(filepath.Join(dir, managedFile))
	if strings.Contains(string(b), "mouse {") || !strings.Contains(string(b), "drag-lock") {
		t.Fatalf("%s", b)
	}
}
func TestReadSectionsReportsParseErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	put(t, path, `input {`)
	if _, _, err := ReadSections(path); err == nil {
		t.Fatal("accepted invalid KDL")
	}
	put(t, path, `include "config.kdl"`)
	if _, _, err := ReadSections(path); err == nil {
		t.Fatal("accepted cycle")
	}
	put(t, path, `include optional=true "missing.kdl"`)
	if _, _, err := ReadSections(path); err != nil {
		t.Fatal(err)
	}
}
func TestWriteRefusesInvalidConfigWithoutChangingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	put(t, path, `input {`)
	on := true
	if err := Write(dir, config.Input{Mouse: config.Pointer{NaturalScroll: &on}}, nil, nil); err == nil {
		t.Fatal("accepted invalid config")
	}
	if _, err := os.Stat(filepath.Join(dir, managedFile)); !os.IsNotExist(err) {
		t.Fatalf("sidecar written: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != `input {` {
		t.Fatal("changed invalid config")
	}
}
func TestEffectiveAndVersion(t *testing.T) {
	in, err := Effective(Section{`tap`, `accel-speed -0.4`, `scroll-factor 2`, `tap-button-map "left-middle-right"`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !*in.Touchpad.Tap || *in.Touchpad.AccelSpeed != -0.4 || *in.Touchpad.ScrollFactor != 2 {
		t.Fatalf("%+v", in)
	}
	for _, v := range []string{"26.04", "niri 26.04 (abc)", "26.4.1", "27.01"} {
		if !SupportedVersion(v) {
			t.Errorf("rejected %s", v)
		}
	}
	for _, v := range []string{"25.11", "26.03", "unknown", "niri 26.04-dev"} {
		if SupportedVersion(v) {
			t.Errorf("accepted %s", v)
		}
	}
}
func TestHasTouchpad(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "c13:1"), "E:ID_INPUT_TOUCHPAD=1\n")
	if !HasTouchpad(dir) {
		t.Fatal("no touchpad")
	}
	put(t, filepath.Join(dir, "c13:1"), "E:ID_INPUT_TOUCHPAD=10\n")
	if HasTouchpad(dir) {
		t.Fatal("false positive")
	}
}

func TestWriteRefreshesSectionsBeforeSaving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	put(t, path, `input { touchpad { tap-button-map "left-middle-right"; }; }`)
	old, mouse, err := ReadSections(path)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, `input { touchpad { drag-lock; }; }`)
	on := true
	if err := Write(dir, config.Input{Touchpad: config.Pointer{Tap: &on}}, old, mouse); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, managedFile))
	if !strings.Contains(string(b), "drag-lock") || strings.Contains(string(b), "tap-button-map") {
		t.Fatalf("saved stale device section: %s", b)
	}
}
func TestWriteKeepsSymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "dotfile.kdl")
	put(t, target, "// keep\ninput {}\n")
	path := filepath.Join(dir, "config.kdl")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	on := true
	if err := Write(dir, config.Input{Mouse: config.Pointer{NaturalScroll: &on}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v", err)
	}
	b, _ := os.ReadFile(target)
	if !strings.HasSuffix(string(b), includeLine+"\n") {
		t.Fatalf("target not updated: %s", b)
	}
}
func TestIncludeLastLeavesCommentedDirectivesAlone(t *testing.T) {
	for _, src := range []string{"/*\n" + includeLine + "\n*/\n", "/-\n" + includeLine + "\n", "input {\n" + includeLine + "\n}\n"} {
		got, err := includeLast(src)
		if err != nil {
			t.Fatal(err)
		}
		if got != src+includeLine+"\n" {
			t.Fatalf("changed inactive or nested directive: %q", got)
		}
	}
}

func TestWriteRefusesManagedSymlink(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.kdl")
	source := filepath.Join(dir, "user.kdl")
	original := `input { touchpad { drag-lock; }; }; binds { Mod+Q { close-window; }; }`
	put(t, source, original)
	put(t, root, "include \"user.kdl\"\n"+includeLine+"\n")
	if err := os.Symlink(source, filepath.Join(dir, managedFile)); err != nil {
		t.Fatal(err)
	}
	on := true
	if err := Write(dir, config.Input{Touchpad: config.Pointer{Tap: &on}}, nil, nil); err == nil {
		t.Fatal("accepted managed symlink")
	}
	got, _ := os.ReadFile(source)
	if string(got) != original {
		t.Fatal("overwrote user config through managed symlink")
	}
}
func TestAxisScrollFactorSurvivesUnrelatedChange(t *testing.T) {
	for _, factor := range []string{`scroll-factor horizontal=2 vertical=0.5`, `scroll-factor 2 vertical=0.5`, `scroll-factor horizontal=2 vertical=-1`} {
		t.Run(factor, func(t *testing.T) {
			dir := t.TempDir()
			put(t, filepath.Join(dir, "config.kdl"), "input { touchpad { "+factor+"; drag-lock; }; }")
			touch, mouse, err := ReadSections(filepath.Join(dir, "config.kdl"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Effective(touch, mouse); err != nil {
				t.Fatal(err)
			}
			on := true
			if err := Write(dir, config.Input{Touchpad: config.Pointer{Tap: &on}}, touch, mouse); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(dir, managedFile))
			if !strings.Contains(string(b), "horizontal=2") && !strings.Contains(string(b), "scroll-factor 2") {
				t.Fatalf("lost axis factor: %s", b)
			}
			if !strings.Contains(string(b), "vertical=0.5") && !strings.Contains(string(b), "vertical=-1") {
				t.Fatalf("lost vertical factor: %s", b)
			}
		})
	}
}
