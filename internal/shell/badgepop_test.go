package shell

import (
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestScaleRectAboutKeepsTheCentre(t *testing.T) {
	got := ui.ScaleRectAbout(ui.Rect{X: 10, Y: 20, W: 40, H: 20}, 1.5)
	if got != (ui.Rect{X: 0, Y: 15, W: 60, H: 30}) {
		t.Fatalf("got %+v", got)
	}
}

func TestBadgePopScalePeaksMidway(t *testing.T) {
	if badgePopScale(0) != 1 || math.Abs(badgePopScale(1)-1) > 1e-9 || math.Abs(badgePopScale(0.5)-1.15) > 1e-9 {
		t.Fatalf("scale 0/0.5/1 = %v %v %v", badgePopScale(0), badgePopScale(0.5), badgePopScale(1))
	}
}

func TestBadgePopsOnlyWhenTheCountRises(t *testing.T) {
	h := &PanelHost{anim: newAnimator(nil, false, DefaultTheme().Motion)}
	badge := func(count string) *ui.Node {
		return &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{{
			Kind: ui.KindCapsule, Key: badgeKeyPrefix + "app", Children: []*ui.Node{{Kind: ui.KindText, Text: count}},
		}}}
	}
	h.noteBadges(badge("2"))
	if !h.anim.Settled() {
		t.Fatal("first sighting popped")
	}
	h.noteBadges(badge("3"))
	if h.anim.Settled() {
		t.Fatal("rise from 2 to 3 did not pop")
	}
	h.anim = newAnimator(nil, false, DefaultTheme().Motion)
	h.noteBadges(badge("1"))
	if !h.anim.Settled() {
		t.Fatal("a fall popped")
	}
}

func TestBadgePopSurvivesProgressResolutionUntilBadgeLeaves(t *testing.T) {
	h := &PanelHost{anim: newAnimator(nil, false, DefaultTheme().Motion)}
	badge := func(count string) *ui.Node {
		return &ui.Node{Kind: ui.KindCapsule, Key: badgeKeyPrefix + "app", Children: []*ui.Node{{Kind: ui.KindText, Text: count}}}
	}
	key := badgeKeyPrefix + "app"
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{badge("2")}}
	h.noteBadges(root)
	root.Children[0] = badge("3")
	h.noteBadges(root)

	resolveProgressMotion(h.anim, root)
	if !h.anim.has(key, animProgress) {
		t.Fatal("progress resolver dropped a live badge pop")
	}
	resolveProgressMotion(h.anim, &ui.Node{Kind: ui.KindColumn})
	if h.anim.has(key, animProgress) {
		t.Fatal("progress resolver kept a pop after its badge left")
	}
}
