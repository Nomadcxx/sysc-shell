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
		out.Focusable = true
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
