package shell

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// replaceKeyedScroll swaps the scroll with key for replacement and returns a
// function that puts the original back.
func replaceKeyedScroll(root *ui.Node, key string, replacement *ui.Node) func() {
	var walk func(*ui.Node) func()
	walk = func(n *ui.Node) func() {
		for i, c := range n.Children {
			if c == nil {
				continue
			}
			if c.Key == key && c.Kind == ui.KindScroll {
				n.Children[i] = replacement
				return func() { n.Children[i] = c }
			}
			if restore := walk(c); restore != nil {
				return restore
			}
		}
		return nil
	}
	return walk(root)
}

// TestSettingsApplicationsVirtualListMatchesFullBuild is the byte-identity
// proof for the mixed-height virtual list: the Applications page must paint
// exactly what the full column of rows paints, down to the row positions and
// the scroll extent.
func TestSettingsApplicationsVirtualListMatchesFullBuild(t *testing.T) {
	apps := syntheticAppCatalog(24)
	reg, spec := openSyntheticApps(t, apps)
	h := reg.panelHosts[PanelSettings]
	scale := ui.Scale120(240)
	pw, ph := scale.Physical(int(spec.Width)), scale.Physical(int(spec.Height))
	render := func() []byte {
		t.Helper()
		pixels := make([]byte, pw*ph*4)
		if err := spec.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	virtualFrame := render()

	reg.mu.Lock()
	body := keyedScroll(h.root, settingsBodyKey)
	if body == nil {
		reg.mu.Unlock()
		t.Fatal("the Applications page has no settings body scroll")
	}
	list := findKey(h.root, "applications-installed")
	if list == nil || list.Kind != ui.KindVirtualList {
		reg.mu.Unlock()
		t.Fatal("the installed applications list is not a virtual list")
	}
	if len(list.Children) == 0 || len(list.Children) >= list.ItemCount {
		reg.mu.Unlock()
		t.Fatalf("virtual list laid out %d of %d rows", len(list.Children), list.ItemCount)
	}
	virtualBounds := rowBoundsByAction(list)
	virtualContent := body.ContentH

	full := applicationsSettingsPage(reg, h, false)
	full.Height = body.Height
	restore := replaceKeyedScroll(h.root, settingsBodyKey, full)
	if restore == nil {
		reg.mu.Unlock()
		t.Fatal("could not find the settings body to replace")
	}
	reg.mu.Unlock()
	t.Cleanup(func() {
		reg.mu.Lock()
		restore()
		reg.mu.Unlock()
	})

	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), 240); err != nil {
		t.Fatal(err)
	}
	fullBody := keyedScroll(h.root, settingsBodyKey)
	if fullBody == nil {
		t.Fatal("the full Applications page has no settings body")
	}
	if fullBody.ContentH != virtualContent {
		t.Fatalf("content height: virtual %d, full %d", virtualContent, fullBody.ContentH)
	}
	for action, want := range virtualBounds {
		got := findAction(h.root, action)
		if got == nil {
			t.Fatalf("full build has no node for %q", action)
		}
		if got.Bounds != want {
			t.Fatalf("%s bounds: virtual %+v, full %+v", action, want, got.Bounds)
		}
	}
	fullFrame := render()
	if !bytes.Equal(virtualFrame, fullFrame) {
		first := 0
		for i := range virtualFrame {
			if virtualFrame[i] != fullFrame[i] {
				first = i
				break
			}
		}
		t.Fatalf("virtual and full frames differ, first byte %d (offset %d,%d)", first, (first/4)%pw, (first/4)/pw)
	}
}

// rowBoundsByAction records the bounds of each application row action under
// root, keyed by the action the interaction layer resolves.
func rowBoundsByAction(root *ui.Node) map[string]ui.Rect {
	out := make(map[string]ui.Rect)
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if len(n.Action) > len("applications:favourite:") && n.Action[:len("applications:favourite:")] == "applications:favourite:" {
			out[n.Action] = n.Bounds
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// findFirstAction returns the first action under n, for diagnostics.
func findFirstAction(n *ui.Node) string {
	if n == nil {
		return ""
	}
	if n.Action != "" {
		return n.Action
	}
	for _, c := range n.Children {
		if a := findFirstAction(c); a != "" {
			return a
		}
	}
	return ""
}

// findKey returns the first node with key under root.
func findKey(root *ui.Node, key string) *ui.Node {
	var found *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || found != nil {
			return
		}
		if n.Key == key {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

// TestSettingsApplicationsVirtualListResolvesPointer proves the materialised
// rows are the real interactive nodes: hover resolves onto a visible row, and a
// row far down the list is absent until the scroll reaches it.
func TestSettingsApplicationsVirtualListResolvesPointer(t *testing.T) {
	apps := syntheticAppCatalog(40)
	reg, spec := openSyntheticApps(t, apps)
	h := reg.panelHosts[PanelSettings]

	reg.mu.Lock()
	body := keyedScroll(h.root, settingsBodyKey)
	list := findKey(h.root, "applications-installed")
	if body == nil || list == nil {
		reg.mu.Unlock()
		t.Fatal("the Applications page has no virtualised body")
	}
	sorted := append([]launcher.Entry(nil), apps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := strings.ToLower(sorted[i].Name), strings.ToLower(sorted[j].Name)
		if left != right {
			return left < right
		}
		return sorted[i].ID < sorted[j].ID
	})
	firstAction := "applications:favourite:" + sorted[0].ID
	toggle := findAction(list, firstAction)
	if toggle == nil || toggle.Bounds.W <= 0 {
		var actions []string
		for _, c := range list.Children {
			actions = append(actions, findFirstAction(c))
		}
		reg.mu.Unlock()
		t.Fatalf("the first installed row was not materialised (children %d, list %+v, body %+v, actions %v, want %s)", len(list.Children), list.Bounds, body.Bounds, actions, firstAction)
	}
	x := toggle.Bounds.X + toggle.Bounds.W/2
	y := toggle.Bounds.Y + toggle.Bounds.H/2
	key := hoverKeyAt(h.root, x, y)
	if key != firstAction {
		reg.mu.Unlock()
		t.Fatalf("hover key at the first toggle = %q, want %q", key, firstAction)
	}
	h.pointer.setHover(key)
	h.pointer.apply(h.root, nil)
	hovered := toggle.State.Has(ui.StateHovered)
	h.pointer.clear()
	h.pointer.apply(h.root, nil)
	cleared := !toggle.State.Has(ui.StateHovered)
	lastAction := "applications:favourite:" + sorted[len(sorted)-1].ID
	last := findAction(list, lastAction)
	reg.mu.Unlock()

	if !hovered {
		t.Fatal("a visible virtual row did not take hover state")
	}
	if !cleared {
		t.Fatal("hover state survived the pointer leaving")
	}
	if last != nil {
		t.Fatal("a row far below the viewport was materialised")
	}

	reg.mu.Lock()
	ui.ScrollBy(body, 1<<30)
	reg.mu.Unlock()
	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), 240); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	list = findKey(h.root, "applications-installed")
	last = findAction(list, lastAction)
	reg.mu.Unlock()
	if last == nil || last.Bounds.W <= 0 {
		t.Fatal("the last row did not materialise after scrolling to the end")
	}
}
