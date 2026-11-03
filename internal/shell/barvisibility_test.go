package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// The bar of the focused output goes down while the focused window covers that
// output. The compositor reports it with the is_fullscreen flag; a compositor
// without the flag leaves the bar alone.
func TestBarVisibilityFollowsTheFocusedFullscreenWindow(t *testing.T) {
	cases := []struct {
		name     string
		snapshot niri.Snapshot
		focused  string
		wantHide bool
	}{
		{
			name: "a fullscreen window hides the bar of its output",
			snapshot: niri.Snapshot{Windows: []niri.Window{
				{ID: 5, Focused: true, Fullscreen: true, HasFullscreen: true},
			}},
			focused:  "DP-1",
			wantHide: true,
		},
		{
			name: "a window that is not fullscreen keeps the bar",
			snapshot: niri.Snapshot{Windows: []niri.Window{
				{ID: 5, Focused: true, Fullscreen: false, HasFullscreen: true},
			}},
			focused:  "DP-1",
			wantHide: false,
		},
		{
			name: "a compositor without the flag leaves the bar alone",
			snapshot: niri.Snapshot{Windows: []niri.Window{
				{ID: 5, Focused: true},
			}},
			focused:  "DP-1",
			wantHide: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Registry{
				bars:         map[uint32]*Bar{1: {conn: "DP-1"}},
				barHidden:    map[uint32]bool{},
				focused:      tc.focused,
				niriSnapshot: tc.snapshot,
			}
			changes := r.barVisibilityLocked()
			if tc.wantHide {
				if len(changes) != 1 {
					t.Fatalf("changes = %d, want 1", len(changes))
				}
				change := changes[0]
				if change.Global != 1 || change.Hidden == nil || *change.Hidden != tc.wantHide {
					t.Fatalf("change = %+v, want global 1 hidden %v", change, tc.wantHide)
				}
			} else if len(changes) != 0 {
				t.Fatalf("changes = %d, want 0", len(changes))
			}
			// The same state must not ask the owner again.
			if again := r.barVisibilityLocked(); len(again) != 0 {
				t.Fatalf("second pass changes = %d, want 0", len(again))
			}
		})
	}
}

// A bar on another output stays visible while the focused output has a
// fullscreen window.
func TestBarVisibilityOnlyHidesTheBarOfTheFocusedOutput(t *testing.T) {
	r := &Registry{
		bars:      map[uint32]*Bar{1: {conn: "DP-1"}, 2: {conn: "DP-2"}},
		barHidden: map[uint32]bool{},
		focused:   "DP-1",
		niriSnapshot: niri.Snapshot{Windows: []niri.Window{
			{ID: 5, Focused: true, Fullscreen: true, HasFullscreen: true},
		}},
	}
	changes := r.barVisibilityLocked()
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	if changes[0].Global != 1 || changes[0].Hidden == nil || !*changes[0].Hidden {
		t.Fatalf("change = %+v, want global 1 hidden", changes[0])
	}
}
