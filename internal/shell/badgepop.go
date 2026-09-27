package shell

import (
	"math"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const badgeKeyPrefix = "notify-badge:"

func badgePopScale(v float64) float64 { return 1 + 0.15*math.Sin(math.Pi*v) }

func badgeCount(n *ui.Node) (int, bool) {
	for _, c := range n.Children {
		if c.Kind == ui.KindText {
			v, err := strconv.Atoi(strings.TrimPrefix(c.Text, "+"))
			return v, err == nil
		}
	}
	return 0, false
}

func walkBadges(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	if strings.HasPrefix(n.Key, badgeKeyPrefix) {
		fn(n)
	}
	for _, c := range n.Children {
		walkBadges(c, fn)
	}
}

// noteBadges runs after a rebuild. A badge whose count rose since the last
// rebuild starts one 0-to-1 run on its progress channel; the first sighting
// and a fall do not.
func (h *PanelHost) noteBadges(root *ui.Node) {
	if h.anim == nil {
		return
	}
	if h.badgeCounts == nil {
		h.badgeCounts = map[string]int{}
	}
	seen := map[string]bool{}
	walkBadges(root, func(n *ui.Node) {
		count, ok := badgeCount(n)
		if !ok {
			return
		}
		seen[n.Key] = true
		if prev, had := h.badgeCounts[n.Key]; had && count > prev {
			h.anim.Reset(n.Key, animProgress)
			h.anim.Target(n.Key, animProgress, 1)
		}
		h.badgeCounts[n.Key] = count
	})
	for k := range h.badgeCounts {
		if !seen[k] {
			delete(h.badgeCounts, k)
		}
	}
}

// applyBadgePop swells a popping badge on the paint copy. Layout is
// untouched: only the drawn rectangles grow about their centre.
func (h *PanelHost) applyBadgePop(root *ui.Node) {
	if h.anim == nil {
		return
	}
	walkBadges(root, func(n *ui.Node) {
		v := h.anim.Value(n.Key, animProgress)
		if v <= 0 || v >= 1 {
			return
		}
		s := badgePopScale(v)
		n.Bounds = ui.ScaleRectAbout(n.Bounds, s)
		for _, c := range n.Children {
			c.Bounds = ui.ScaleRectAbout(c.Bounds, s)
		}
	})
}
