package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMarqueeResolveOnlyRunsOnOverflow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		text    string
		width   int
		reduced bool
		motion  bool
	}{
		{name: "fits", text: "short", width: 200},
		{name: "overflow", text: strings.Repeat("long title ", 6), width: 60, motion: true},
		{name: "reduced", text: strings.Repeat("long title ", 6), width: 60, reduced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bar := newTestBar(t)
			t.Cleanup(bar.stopAnimation)
			bar.mu.Lock()
			bar.anim = newAnimator(nil, tc.reduced, bar.theme.Motion)
			root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
				Kind: ui.KindText, Key: "media-title", Text: tc.text, MaxWidth: tc.width,
				Marquee: true, Bounds: ui.Rect{W: tc.width, H: BarHeight},
			}}}
			bar.resolveMediaMotionLocked(root)
			title := root.Children[0]
			active := bar.anim.has("media-title", animSweep) && !bar.anim.Settled()
			bar.mu.Unlock()

			if active != tc.motion {
				t.Fatalf("sweep active = %v, want %v", active, tc.motion)
			}
			if tc.motion && !title.Marquee {
				t.Fatal("overflow title lost marquee intent")
			}
			if !tc.motion && title.Marquee {
				t.Fatal("static/reduced title kept marquee paint path")
			}
		})
	}
}
