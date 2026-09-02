package plugin

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Convert turns a wire tree into a fresh shell-owned tree.
//
// This is the only path from plugin JSON into anything the shell will lay out
// or paint, so it validates first and copies everything it reads. The wire
// tree belongs to the goroutine that decoded it and may be reused or mutated
// after this returns; a converted tree that aliased any of it could change
// under a frame the shell had already published.
//
// Nothing crosses that the shell cannot own. There is no field for arranged
// bounds, no callback, no focus index, no scroll offset, and no virtual-list
// item function: a plugin describes what it means, and the host decides what
// that looks like.
func Convert(root *v1.Node, view v1.ViewKind) (*ui.Node, error) {
	if err := v1.Validate(root, view); err != nil {
		return nil, fmt.Errorf("plugin: %w", err)
	}
	// A bar strip is arranged as a row and a panel or tooltip as a column. A
	// root of the other kind has no layout to run, so refusing it here gives
	// the plugin author the reason instead of a layout error further on.
	want := ui.KindColumn
	if view == v1.ViewBar {
		want = ui.KindRow
	}
	converted, err := convertNode(root, "root")
	if err != nil {
		return nil, err
	}
	if converted.Kind != want {
		return nil, fmt.Errorf("plugin: a %s view needs a %s root, not %s", view, rootName(want), root.Kind)
	}
	return converted, nil
}

// ViewTree is one view's current immutable wire tree and revision.
//
// Patches apply only against a matching base. Any rejected patch leaves this
// tree unchanged and asks for a snapshot once, until that snapshot arrives.
type ViewTree struct {
	View     v1.ViewKind
	Revision uint64
	Root     *v1.Node
	awaiting bool
}

// ApplySnapshot replaces the tree and clears a pending resync.
func (t *ViewTree) ApplySnapshot(rev uint64, root *v1.Node) error {
	if err := v1.Validate(root, t.View); err != nil {
		return err
	}
	t.Root = cloneNode(root)
	t.Revision = rev
	t.awaiting = false
	return nil
}

// ApplyPatch tries keyed replacements. resync is true only the first time a
// patch is dropped until the next snapshot.
func (t *ViewTree) ApplyPatch(p *v1.ViewPatch) (resync bool, err error) {
	err = t.apply(p)
	if err == nil {
		return false, nil
	}
	if t.awaiting {
		return false, err
	}
	t.awaiting = true
	return true, err
}

func (t *ViewTree) apply(p *v1.ViewPatch) error {
	if p == nil || t.Root == nil {
		return fmt.Errorf("plugin: no tree to patch")
	}
	if p.Base != t.Revision {
		if p.Base < t.Revision {
			return fmt.Errorf("plugin: stale patch base %d, have %d", p.Base, t.Revision)
		}
		return fmt.Errorf("plugin: patch base %d does not match revision %d", p.Base, t.Revision)
	}
	if p.Revision <= p.Base {
		return fmt.Errorf("plugin: patch revision %d does not advance base %d", p.Revision, p.Base)
	}
	seen := make(map[string]bool, len(p.Replacements))
	for _, r := range p.Replacements {
		if r.Key == "" {
			return fmt.Errorf("plugin: replacement has no key")
		}
		if seen[r.Key] {
			return fmt.Errorf("plugin: duplicate replacement target %q", r.Key)
		}
		seen[r.Key] = true
		if r.Node == nil {
			return fmt.Errorf("plugin: replacement %q has no node", r.Key)
		}
		if dup := firstDupKey(r.Node); dup != "" {
			return fmt.Errorf("plugin: replacement %q reuses key %q", r.Key, dup)
		}
		if !hasKey(t.Root, r.Key) {
			return fmt.Errorf("plugin: no node keyed %q", r.Key)
		}
	}
	next := cloneNode(t.Root)
	for _, r := range p.Replacements {
		var ok bool
		next, ok = replaceKey(next, r.Key, cloneNode(r.Node))
		if !ok {
			return fmt.Errorf("plugin: no node keyed %q", r.Key)
		}
	}
	if err := v1.Validate(next, t.View); err != nil {
		return err
	}
	t.Root = next
	t.Revision = p.Revision
	return nil
}

func cloneNode(n *v1.Node) *v1.Node {
	if n == nil {
		return nil
	}
	out := *n
	if n.Events != nil {
		out.Events = append([]v1.EventKind(nil), n.Events...)
	}
	if n.Accept != nil {
		out.Accept = append([]string(nil), n.Accept...)
	}
	if len(n.Children) > 0 {
		out.Children = make([]*v1.Node, len(n.Children))
		for i, c := range n.Children {
			out.Children[i] = cloneNode(c)
		}
	}
	return &out
}

func hasKey(n *v1.Node, key string) bool {
	if n == nil {
		return false
	}
	if n.Key == key {
		return true
	}
	for _, c := range n.Children {
		if hasKey(c, key) {
			return true
		}
	}
	return false
}

func replaceKey(n *v1.Node, key string, repl *v1.Node) (*v1.Node, bool) {
	if n == nil {
		return nil, false
	}
	if n.Key == key {
		return repl, true
	}
	for i, c := range n.Children {
		next, ok := replaceKey(c, key, repl)
		if ok {
			n.Children[i] = next
			return n, true
		}
	}
	return n, false
}

func firstDupKey(n *v1.Node) string {
	seen := map[string]bool{}
	var dup string
	var walk func(*v1.Node)
	walk = func(n *v1.Node) {
		if n == nil || dup != "" {
			return
		}
		if n.Key != "" {
			if seen[n.Key] {
				dup = n.Key
				return
			}
			seen[n.Key] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(n)
	return dup
}

func rootName(k ui.Kind) string {
	if k == ui.KindRow {
		return "row"
	}
	return "column"
}

func convertNode(n *v1.Node, path string) (*ui.Node, error) {
	out := &ui.Node{
		Padding:  n.Padding,
		Gap:      n.Gap,
		Width:    n.Width,
		MaxWidth: n.MaxWidth,
		Tabular:  n.Tabular,
		Name:     n.Name,
		Role:     n.Role,
	}
	if n.Tone == v1.ToneError {
		out.Tone = ui.ToneError
	}

	switch n.Kind {
	case v1.KindRow:
		out.Kind = ui.KindRow
	case v1.KindColumn:
		out.Kind = ui.KindColumn
	case v1.KindText:
		out.Kind = ui.KindText
		out.Text = n.Text
	case v1.KindIcon:
		// A plugin names a symbol; the shell decides which glyphs exist. A
		// name the font has no glyph for fails here rather than painting a
		// missing-glyph box the user would have to interpret.
		glyph, ok := render.IconByName(n.Icon)
		if !ok {
			return nil, fmt.Errorf("plugin: %s: no icon named %q; this shell has %v", path, n.Icon, render.IconNames())
		}
		out.Kind = ui.KindText
		out.Text = string(glyph)
	case v1.KindProgress:
		out.Kind = ui.KindMeter
		out.Value = n.Value
	case v1.KindButton:
		out.Kind = ui.KindButton
		out.Text = n.Text
		// The node id becomes the action, which is how a hit finds its way
		// back to the node the plugin addressed.
		out.Action = n.ID
		out.Focusable = true
	case v1.KindTextInput:
		out.Kind = ui.KindTextField
		out.Text = n.Text
		out.Action = n.ID
		out.Key = n.Key
		out.Focusable = true
		out.Multiline = n.Multiline
		out.SubmitOnEnter = n.SubmitOnEnter
		out.Reseed = n.Reseed
	case v1.KindList:
		out.Kind = ui.KindScroll
		if n.Height > 0 {
			out.Height = n.Height
		}
	case v1.KindDragSource:
		out.Kind = ui.KindDragSource
		out.Text = n.Text
		out.Action = n.ID
		out.Focusable = true
		out.DragType = n.DragType
		out.Payload = n.Payload
	case v1.KindDropZone:
		out.Kind = ui.KindDropZone
		out.Action = n.ID
		out.Accept = append([]string(nil), n.Accept...)
	default:
		return nil, fmt.Errorf("plugin: %s: no shell element for %q", path, n.Kind)
	}

	if len(n.Children) > 0 {
		out.Children = make([]*ui.Node, len(n.Children))
		for i, c := range n.Children {
			child, err := convertNode(c, fmt.Sprintf("%s.children[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out.Children[i] = child
		}
	}
	return out, nil
}
