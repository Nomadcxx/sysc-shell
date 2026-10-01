package shell

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestScreenshotBarItemIsKnownButNotDefault(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	for _, zone := range [][]config.Item{cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right} {
		for _, item := range zone {
			if item.ID == "screenshot" {
				t.Fatal("the default bar layout must not change; the glyph is opt-in")
			}
		}
	}
	if _, err := config.Parse([]byte(`{"bar":{"items":{"right":[{"id":"screenshot"}]}}}`)); err != nil {
		t.Fatalf("a configured screenshot item must load: %v", err)
	}
}

func TestScreenshotWidgetOpensItsPanel(t *testing.T) {
	t.Parallel()
	w := buildScreenshotWidget()
	if w.node.Action != panelScreenshotAction || w.node.Name != "Screenshot" || w.node.Role != "button" {
		t.Fatalf("node = action %q name %q role %q", w.node.Action, w.node.Name, w.node.Role)
	}
	if w.tooltip != "Screenshot" {
		t.Fatalf("tooltip = %q, want Screenshot", w.tooltip)
	}
}

func newScreenshotHost(t *testing.T, dir string) (*Registry, *PanelHost) {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	reg.screenshotDir = func() string { return dir }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelScreenshot, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	return reg, reg.panelHosts[PanelScreenshot]
}

func TestScreenshotPanelListsThreeModesInOrder(t *testing.T) {
	t.Parallel()
	_, h := newScreenshotHost(t, "/home/u/Pictures/Screenshots")
	got := focusableNames(h.root)
	want := []string{"Region", "Window", "Screen"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if h.roving.Index() != 0 {
		t.Fatalf("focus starts on row %d, want Region (0)", h.roving.Index())
	}
}

func TestScreenshotPanelNamesItsPanelAndSize(t *testing.T) {
	t.Parallel()
	if PanelScreenshot.String() != "screenshot" {
		t.Fatalf("String = %q", PanelScreenshot.String())
	}
	if id, err := parsePanelName("screenshot"); err != nil || id != PanelScreenshot {
		t.Fatalf("parsePanelName = %v, %v", id, err)
	}
	if got := panelTargetSize(PanelScreenshot); got.W != 360 || got.H != 240 {
		t.Fatalf("size = %+v, want 360x240", got)
	}
}

func TestShortenPath(t *testing.T) {
	t.Parallel()
	long := "/home/ユーザー/Pictures/Screenshots/with/a/very/deep/tree/of/folders"
	tests := []struct {
		name, in string
		max      int
		want     string
	}{
		{"fits", "/home/u/Pictures", 40, "/home/u/Pictures"},
		{"exact", "abcdef", 6, "abcdef"},
		{"middle", "/home/u/Pictures/Screenshots", 16, "/home/u…eenshots"},
	}
	for _, tc := range tests {
		if got := shortenPath(tc.in, tc.max); got != tc.want {
			t.Errorf("%s: shortenPath(%q,%d) = %q, want %q", tc.name, tc.in, tc.max, got, tc.want)
		}
	}
	got := shortenPath(long, 24)
	if r := []rune(got); len(r) != 24 || !strings.Contains(got, "…") || !utf8.ValidString(got) {
		t.Errorf("non-ASCII path shortened to %q (%d runes), want 24 valid runes with an ellipsis", got, len([]rune(got)))
	}
}

func TestScreenshotCaptionShortensButNamesTheWholePath(t *testing.T) {
	t.Parallel()
	dir := "/home/someone/Pictures/Screenshots/with/a/very/deep/tree/of/folders"
	_, h := newScreenshotHost(t, dir)
	var found *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n.Name == dir {
			found = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if found == nil {
		t.Fatal("no node carries the full save directory as its name")
	}
	if len([]rune(found.Text)) > screenshotPathRunes {
		t.Fatalf("caption %q is longer than %d runes", found.Text, screenshotPathRunes)
	}
}
