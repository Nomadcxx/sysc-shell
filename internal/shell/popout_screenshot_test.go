package shell

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestScreenshotRowsClosePanelThenStartTheirMode(t *testing.T) {
	for _, tc := range []struct{ row, mode string }{
		{"Region", "region"}, {"Window", "window"}, {"Screen", "screen"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			reg, h := newScreenshotHost(t, t.TempDir())
			started := make(chan string, 1)
			reg.mu.Lock()
			reg.startScreenshot = func(mode string) error { started <- mode; return nil }
			reg.mu.Unlock()
			activateNamed(h, reg, tc.row)
			reg.mu.Lock()
			open := reg.panelOpenLocked(PanelScreenshot)
			reg.mu.Unlock()
			if open {
				t.Fatal("the panel must close before the capture starts, or it is in the shot")
			}
			select {
			case got := <-started:
				if got != tc.mode {
					t.Fatalf("started %q, want %q", got, tc.mode)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("capture never started")
			}
		})
	}
}

func TestScreenshotRowWhileASelectorIsOpenKeepsThePanelAndShowsWhy(t *testing.T) {
	reg, h := newScreenshotHost(t, t.TempDir())
	started := make(chan string, 1)
	reg.mu.Lock()
	reg.startScreenshot = func(mode string) error { started <- mode; return nil }
	reg.selector = &regionSelector{}
	reg.mu.Unlock()
	activateNamed(h, reg, "Region")
	reg.mu.Lock()
	open := reg.panelOpenLocked(PanelScreenshot)
	label := h.errLabel
	reg.mu.Unlock()
	if !open {
		t.Fatal("the panel must stay open so choosing again is the retry")
	}
	if !strings.Contains(label, "region selector is already open") {
		t.Fatalf("errLabel = %q, want the selector-open cause", label)
	}
	select {
	case got := <-started:
		t.Fatalf("started %q while a selector was open", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestScreenshotBarClickOpensThePanel(t *testing.T) {
	reg := newPanelRegistry(t)
	bar := &Bar{right: []textWidget{{node: &ui.Node{
		Action: panelScreenshotAction, Bounds: ui.Rect{X: 1200, Y: 0, W: 40, H: 44},
	}}}}
	bar.setOutputSize(1536, 864)
	reg.mu.Lock()
	reg.bars[7] = bar
	reg.mu.Unlock()
	reg.bindBarPanelActionsLocked(7, bar)
	target := bar.actionBounds(panelScreenshotAction)
	drainAuxQueue(reg)
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonLeft) {
		t.Fatal("left-click on the screenshot item did not activate")
	}
	_ = drainAux(t, reg, 2)
	if reg.panelHosts[PanelScreenshot] == nil {
		t.Fatal("the screenshot panel did not open")
	}
}
