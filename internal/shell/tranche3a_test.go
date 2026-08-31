package shell

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The initial event burst must give each output its own title.
func TestInitialWindowStateTitlesEachOutput(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.UpdateNiri(niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true, Focused: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
				ActiveWindowID: 81, HasActiveWindow: true},
		},
		Windows: []niri.Window{
			{ID: 80, Title: "Fixture One"},
			{ID: 81, Title: "Fixture Two"},
		},
	})

	if got := reg.bars[1].left[1].inner.Text; got != "Fixture One" {
		t.Fatalf("DP-9 title = %q, want its own window", got)
	}
	if got := reg.bars[2].left[1].inner.Text; got != "Fixture Two" {
		t.Fatalf("HDMI-A-9 title = %q, want its own window", got)
	}
}

// A bar with no Niri state must still render a stable fallback rather than
// disappearing or showing a stale value.
func TestUnavailableNiriStateRendersAStableFallback(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if got := reg.bars[1].left[0].inner.Text; got != noWorkspace {
		t.Fatalf("workspace before any snapshot = %q, want %q", got, noWorkspace)
	}
	if got := reg.bars[1].left[1].inner.Text; got != "" {
		t.Fatalf("title before any snapshot = %q, want empty", got)
	}
	// The bar still has its three sections and can lay out.
	if len(reg.bars[1].sections()) != 3 {
		t.Fatal("the bar lost a section with no Niri state")
	}
}

// An unbounded title must not exceed its cap or push its neighbour out.
func TestALongTitleStaysWithinItsCapAndKeepsItsNeighbour(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	// Mixed scripts on purpose: titles are the first unbounded user text, so
	// truncation has to hold across a per-rune face change, not just ASCII.
	// The non-Latin run sits beyond the cap, where the cut lands.
	long := strings.Repeat("Fixture Title Segment ", 20) +
		strings.Repeat("こんにちは世界 ", 10) +
		strings.Repeat("مرحبا بالعالم ", 10)

	bar := reg.bars[1]
	// Live order: the owner configures while the title is still empty, then
	// the first window arrives. Layout must follow the text, not the other way.
	if err := bar.Configure(1920, 44, 120); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	reg.UpdateNiri(niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
		},
		Windows: []niri.Window{{ID: 80, Title: long}},
	})
	if err := bar.Render(make([]byte, 1920*44*4), 1920, 44, 1920*4); err != nil {
		t.Fatalf("Render: %v", err)
	}

	workspace := bar.left[0].node
	title := bar.left[1].inner
	if title.Bounds.W > title.MaxWidth {
		t.Fatalf("title width %d exceeds its cap %d", title.Bounds.W, title.MaxWidth)
	}
	if workspace.Bounds.W <= 0 {
		t.Fatal("a long title squeezed the workspace widget to zero width")
	}
	// The title must not run past the bar's content band.
	if right := title.Bounds.X + title.Bounds.W; right > 1920 {
		t.Fatalf("title right edge %d exceeds the surface width", right)
	}
	// The truncated text must remain valid UTF-8: a cut inside a multi-byte
	// rune would render replacement characters.
	if !utf8.ValidString(title.Text) {
		t.Fatal("truncation split a multi-byte rune")
	}
}

// A window moving to another workspace changes which output shows it. Only the
// affected bars may be reported.
func TestMovingAWindowInvalidatesOnlyTheOutputsItLeavesAndJoins(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9", 3: "DP-8"})

	base := niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true},
			{ID: 7, Name: "idle", Output: "DP-8", Active: true},
		},
		Windows: []niri.Window{{ID: 80, Title: "Fixture One", WorkspaceID: 5, HasWorkspace: true}},
	}
	reg.UpdateNiri(base)

	// The window moves from workspace 5 (DP-9) to workspace 6 (HDMI-A-9).
	moved := niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 7, Name: "idle", Output: "DP-8", Active: true},
		},
		Windows: []niri.Window{{ID: 80, Title: "Fixture One", WorkspaceID: 6, HasWorkspace: true}},
	}
	changed := reg.UpdateNiri(moved)

	seen := map[uint32]bool{}
	for _, g := range changed {
		seen[g] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("changed = %v, want the output it left (1) and the one it joined (2)", changed)
	}
	if seen[3] {
		t.Fatalf("changed = %v, want the untouched output 3 excluded", changed)
	}
	if got := reg.bars[1].left[1].inner.Text; got != "" {
		t.Fatalf("DP-9 title = %q, want empty after the window left", got)
	}
	if got := reg.bars[2].left[1].inner.Text; got != "Fixture One" {
		t.Fatalf("HDMI-A-9 title = %q, want the window it gained", got)
	}
}

// Focus, title and close must invalidate only the bar whose text changed, the
// same way a workspace switch and a move already do.
func TestFocusTitleAndCloseInvalidateOnlyTheAffectedBar(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	base := niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
				ActiveWindowID: 81, HasActiveWindow: true},
		},
		Windows: []niri.Window{
			{ID: 80, Title: "Fixture One", WorkspaceID: 5, HasWorkspace: true},
			{ID: 81, Title: "Fixture Two", WorkspaceID: 6, HasWorkspace: true},
			{ID: 82, Title: "Fixture Three", WorkspaceID: 5, HasWorkspace: true},
		},
	}

	cases := []struct {
		name string
		next niri.Snapshot
	}{
		{
			name: "focus",
			next: niri.Snapshot{
				Workspaces: []niri.Workspace{
					{ID: 5, Name: "code", Output: "DP-9", Active: true,
						ActiveWindowID: 82, HasActiveWindow: true},
					{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
						ActiveWindowID: 81, HasActiveWindow: true},
				},
				Windows: base.Windows,
			},
		},
		{
			name: "title",
			next: niri.Snapshot{
				Workspaces: base.Workspaces,
				Windows: []niri.Window{
					{ID: 80, Title: "Fixture One renamed", WorkspaceID: 5, HasWorkspace: true},
					base.Windows[1],
					base.Windows[2],
				},
			},
		},
		{
			name: "close",
			next: niri.Snapshot{
				Workspaces: []niri.Workspace{
					{ID: 5, Name: "code", Output: "DP-9", Active: true},
					{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
						ActiveWindowID: 81, HasActiveWindow: true},
				},
				Windows: []niri.Window{base.Windows[1], base.Windows[2]},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg.UpdateNiri(base)
			changed := reg.UpdateNiri(tc.next)
			seen := map[uint32]bool{}
			for _, g := range changed {
				seen[g] = true
			}
			if !seen[1] {
				t.Fatalf("changed = %v, want global 1 for a %s on DP-9", changed, tc.name)
			}
			if seen[2] {
				t.Fatalf("changed = %v, want HDMI-A-9 excluded for a %s on DP-9", changed, tc.name)
			}
		})
	}
}

// Every goroutine this package starts must be gone once the registry closes.
func TestClosingTheRegistryLeavesNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	reg := NewRegistry(config.Default())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})
	reg.Close()

	// The clock goroutine exits synchronously inside Close, but the scheduler
	// may not have reaped it yet.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines = %d after Close, want at most the starting %d", got, before)
	}
}

// Every state change must reach exactly one invalidation for its bar, with no
// bar's redraw lost. This is the invariant Task 0 Step 4 could not confirm
// statically.
func TestEveryChangedBarIsReportedWhenManyChangeAtOnce(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)

	hosts := map[uint32]string{}
	workspaces := []niri.Workspace{}
	for i := uint32(1); i <= 12; i++ {
		connector := "DP-" + string(rune('a'+i-1))
		hosts[i] = connector
		workspaces = append(workspaces, niri.Workspace{
			ID: uint64(i), Name: "ws", Output: connector, Active: true,
		})
	}
	newHosts(t, reg, hosts)

	// Drain concurrently: publish blocks rather than dropping, and more bars
	// change here than the channel can buffer. That is the point — a
	// non-blocking send would silently lose the overflow.
	received := make(chan uint32, 4*len(hosts))
	done := make(chan struct{})
	go func() {
		for {
			select {
			case inv := <-reg.Invalidations():
				received <- inv.Global
			case <-done:
				return
			}
		}
	}()

	changed := reg.UpdateNiri(niri.Snapshot{Workspaces: workspaces})
	if len(changed) != len(hosts) {
		t.Fatalf("changed %d bars, want all %d; an invalidation was dropped",
			len(changed), len(hosts))
	}

	seen := make(map[uint32]int, len(changed))
	for _, global := range changed {
		seen[global]++
	}
	for global, count := range seen {
		if count != 1 {
			t.Fatalf("global %d reported %d times, want exactly one", global, count)
		}
	}

	// Every changed bar must also reach the transport. Returning the globals
	// is not enough: the channel is what actually drives a frame.
	onTransport := make(map[uint32]bool, len(hosts))
	deadline := time.After(2 * time.Second)
	for len(onTransport) < len(hosts) {
		select {
		case global := <-received:
			onTransport[global] = true
		case <-deadline:
			t.Fatalf("only %d of %d invalidations reached the channel; the rest were lost",
				len(onTransport), len(hosts))
		}
	}
	close(done)
}

// The live sequence is Configure first, with empty clock text and no title, and
// the first tick only afterwards. apply must therefore re-layout: without it
// the first clock tick and the first window title measure into a zero-width box
// and never appear, until an unrelated output configure happens to run.
func TestAppliedTextIsLaidOutWithoutASecondConfigure(t *testing.T) {
	t.Parallel()
	bar, err := New("DP-9")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Configure while every widget is still empty, as the owner does.
	if err := bar.Configure(600, BarHeight, int(ui.ScaleUnit)); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, w := range bar.center {
		if got := w.node.Bounds.W; got != 0 {
			t.Fatalf("an empty clock reserved %d, want a zero-width box", got)
		}
	}

	// The first tick arrives, and the owner repaints. No second Configure.
	if !bar.apply(barView{Now: time.Date(2026, 8, 31, 15, 4, 0, 0, time.UTC)}) {
		t.Fatal("the first tick did not change the bar")
	}
	renderOnce(t, bar)
	for _, w := range bar.center {
		if w.inner.Text == "" {
			continue
		}
		if got := w.node.Bounds.W; got <= 0 {
			t.Fatalf("clock %q laid out %d wide, so it paints into nothing", w.inner.Text, got)
		}
	}
}

// A title that grows must be measured again. Without re-layout it keeps the
// width it was given at configure time, which is the empty-state zero, so it
// paints into nothing however long it gets.
func TestAGrowingTitleIsMeasuredAgain(t *testing.T) {
	t.Parallel()
	bar, err := New("DP-9")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := bar.Configure(600, BarHeight, int(ui.ScaleUnit)); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	title := bar.left[1].node
	bar.apply(barView{Title: "short"})
	renderOnce(t, bar)
	short := title.Bounds.W
	if short <= 0 {
		t.Fatalf("a title of %q laid out %d wide", title.Text, short)
	}

	bar.apply(barView{Title: "a considerably longer focused window title than before"})
	renderOnce(t, bar)
	if long := title.Bounds.W; long <= short {
		t.Fatalf("the longer title laid out %d wide, no wider than the short one at %d",
			long, short)
	}
}

// The centre is pinned to the band centre without reference to its neighbours,
// so it is its own width that moves it. That only happens if apply re-lays out.
func TestTheCentreRecentresAsItsOwnTextChanges(t *testing.T) {
	t.Parallel()
	bar, err := New("DP-9")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := bar.Configure(600, BarHeight, int(ui.ScaleUnit)); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	empty := bar.center[0].node.Bounds.X
	bar.apply(barView{Now: time.Date(2026, 8, 31, 15, 4, 0, 0, time.UTC)})
	renderOnce(t, bar)
	if filled := bar.center[0].node.Bounds.X; filled >= empty {
		t.Fatalf("the centre sat at %d empty and %d with text; a widened centre must move left",
			empty, filled)
	}
}

// renderOnce drives one owner-side repaint, which is where a stale arrangement
// is brought up to date. Reading bounds without it reads the arrangement as it
// stood before the change, which is what the shell itself would paint.
func renderOnce(t *testing.T, bar *Bar) {
	t.Helper()
	const width, height = 600, BarHeight
	if err := bar.Render(make([]byte, width*height*4), width, height, width*4); err != nil {
		t.Fatalf("Render: %v", err)
	}
}
