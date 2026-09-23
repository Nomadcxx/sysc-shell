package plugin

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
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
	if converted.Kind != want && !(view == v1.ViewPanel && converted.Kind == ui.KindScroll) {
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

// wireFillKinds is the set the validator lets carry a fill: containers and a
// button. It restates v1's unexported rule so the converter rejects the same
// trees instead of trusting the validator to have caught them first.
var wireFillKinds = map[v1.NodeKind]bool{
	v1.KindRow:      true,
	v1.KindColumn:   true,
	v1.KindList:     true,
	v1.KindDropZone: true,
	v1.KindButton:   true,
}

// iconNode turns a plugin's icon name into a node the painter can draw.
// The material subset and the project font are both this shell's catalogue,
// and which of the two holds a given glyph is not something a plugin should
// have to know.
//
// The project font is asked first, so a name both carry keeps painting the
// glyph it always has -- the weather widget's WMO symbols are shared that
// way. A name only the subset holds becomes a real icon node, which the
// painter rasterises from the subset. A name in neither fails here rather
// than painting a missing-glyph box the user would have to interpret.
func iconNode(name, path string) (*ui.Node, error) {
	if glyph, ok := render.IconByName(name); ok {
		return &ui.Node{Kind: ui.KindText, Text: string(glyph)}, nil
	}
	if render.ValidMaterialIcon(name) {
		return &ui.Node{Kind: ui.KindIcon, Icon: name}, nil
	}
	return nil, fmt.Errorf("plugin: %s: no icon named %q; this shell has %v and %v",
		path, name, render.IconNames(), render.MaterialIconNames())
}

var wireFills = map[string]ui.Fill{
	"surface":         ui.FillNone,
	"accent":          ui.FillAccent,
	"container":       ui.FillContainer,
	"error":           ui.FillError,
	"soft":            ui.FillSoft,
	"card":            ui.FillContainerHigh,
	"outline":         ui.FillOutline,
	"chip":            ui.FillContainerHighest,
	"error-container": ui.FillErrorContainer,
}

var wireSizes = map[string]theme.TextRole{
	"body":     theme.RoleBody,
	"caption":  theme.RoleCaption,
	"label":    theme.RoleLabel,
	"title":    theme.RoleTitle,
	"headline": theme.RoleHeadline,
	"display":  theme.RoleDisplay,
	"mono":     theme.RoleMono,
}

var wireShapes = map[string]ui.Shape{
	"circle":  ui.ShapeCircle,
	"stadium": ui.ShapeStadium,
	"small":   ui.ShapeSmall,
	"medium":  ui.ShapeMedium,
	"large":   ui.ShapeLarge,
	"card":    ui.ShapeCard,
	"panel":   ui.ShapePanel,
}

func convertNode(n *v1.Node, path string) (*ui.Node, error) {
	out := &ui.Node{
		// The path travels with the node so a rejection downstream can name
		// it: layout runs long after the converter stopped holding it.
		Path:     path,
		Padding:  n.Padding,
		Gap:      n.Gap,
		Width:    n.Width,
		Height:   n.Height,
		MaxWidth: n.MaxWidth,
		Tabular:  n.Tabular,
		Bold:     n.Bold,
		CenterX:  n.CenterX,
		PinEnd:   n.PinEnd,
		Name:     n.Name,
		Role:     n.Role,
		Tooltip:  n.Tooltip,
		Absent:   n.Absent,
	}
	switch n.Tone {
	case v1.ToneError:
		out.Tone = ui.ToneError
	case v1.ToneSubtle:
		out.Tone = ui.ToneSubtle
	case v1.ToneAccent:
		out.Tone = ui.ToneAccent
	}

	if n.Fill != "" {
		fill, ok := wireFills[n.Fill]
		if !ok {
			return nil, fmt.Errorf("plugin: %s: unknown fill %q", path, n.Fill)
		}
		if !wireFillKinds[n.Kind] {
			return nil, fmt.Errorf("plugin: %s: %s cannot carry a fill", path, n.Kind)
		}
		out.Fill = fill
	}
	if n.Radius != 0 {
		out.Radius = n.Radius
	}
	if n.Size != "" {
		role, ok := wireSizes[n.Size]
		if !ok {
			return nil, fmt.Errorf("plugin: %s: unknown size %q", path, n.Size)
		}
		if n.Kind != v1.KindText {
			return nil, fmt.Errorf("plugin: %s: %s cannot carry a size", path, n.Kind)
		}
		out.TextRole = role
	}
	if n.Shape != "" {
		shape, ok := wireShapes[n.Shape]
		if !ok {
			return nil, fmt.Errorf("plugin: %s: unknown shape %q", path, n.Shape)
		}
		out.Shape = shape
	}
	if n.Stroke != 0 || n.StrokeFill != "" {
		strokeFill := ui.FillOutline
		if n.StrokeFill != "" {
			fill, ok := wireFills[n.StrokeFill]
			if !ok {
				return nil, fmt.Errorf("plugin: %s: unknown stroke fill %q", path, n.StrokeFill)
			}
			strokeFill = fill
		}
		out.Stroke, out.StrokeFill = n.Stroke, strokeFill
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
		icon, err := iconNode(n.Icon, path)
		if err != nil {
			return nil, err
		}
		out.Kind, out.Text, out.Icon = icon.Kind, icon.Text, icon.Icon
	case v1.KindProgress:
		out.Kind = ui.KindMeter
		out.Value = n.Value
		out.Key, out.Animate = n.Key, n.Animate
	case v1.KindGauge:
		out.Kind = ui.KindRadialGauge
		out.Value = n.Value
		out.ValueText = n.ValueText
		out.Icon = n.Icon
		out.Key, out.Animate = n.Key, n.Animate
	case v1.KindGraph:
		out.Kind = ui.KindGraph
		out.Values = n.Values
	case v1.KindSeparator:
		out.Kind = ui.KindSeparator
	case v1.KindImage:
		out.Kind = ui.KindImage
		out.ImagePath = n.Path
		out.ImageSize = n.ImageSize
		out.ImageW = n.ImageW
		out.ImageH = n.ImageH
		out.Background = n.Background
	case v1.KindButton:
		out.Kind = ui.KindButton
		out.Text = n.Text
		if n.Icon != "" {
			icon, err := iconNode(n.Icon, path)
			if err != nil {
				return nil, err
			}
			switch {
			case n.Text == "" && icon.Kind == ui.KindText:
				// A project glyph is a character, so the button can carry it
				// as its own label rather than as a child.
				out.Text = icon.Text
			case n.Text == "":
				out.Children = []*ui.Node{icon}
			default:
				// An icon beside a label is the noctalia bar-widget shape:
				// one control, glyph and text together. The button carries
				// them as children so both paint inside its hit target.
				out.Children = []*ui.Node{icon,
					{Kind: ui.KindText, Text: n.Text, Tabular: n.Tabular}}
				out.Text = ""
			}
		}
		// The node id becomes the action, which is how a hit finds its way
		// back to the node the plugin addressed.
		out.Action = n.ID
		out.Focusable = true
		if n.Disabled {
			out.AriaDisabled = true
			out.Action = "" // no action route: activation is blocked
		}
		if n.Tone == v1.ToneError && n.Text != "" {
			out.Fill = ui.FillError
			if out.Padding == 0 {
				out.Padding = 4
			}
		}
	case v1.KindTextInput:
		out.Kind = ui.KindTextField
		out.Text = n.Text
		out.Action = n.ID
		out.Key = n.Key
		out.Focusable = true
		if n.Disabled {
			out.AriaDisabled = true
			out.Action = "" // no action route: activation is blocked
		}
		out.Multiline = n.Multiline
		out.SubmitOnEnter = n.SubmitOnEnter
		out.Reseed = n.Reseed
	case v1.KindList:
		out.Kind = ui.KindScroll
		if n.Height > 0 {
			out.Height = n.Height
		}
		// A scroll fills the row when no width is set; a master/detail
		// panel needs two sized panes side by side, so an explicit width
		// must survive the wire.
		if n.Width > 0 {
			out.Width = n.Width
		}
	case v1.KindDragSource:
		out.Kind = ui.KindDragSource
		out.Text = n.Text
		out.Action = n.ID
		out.Focusable = true
		if n.Disabled {
			out.AriaDisabled = true
			out.Action = "" // no action route: activation is blocked
		}
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
			out.Children[i] = card(child)
		}
	}
	return out, nil
}

// card gives a filled container the chrome that paints it. It is applied to
// children rather than to a root, whose kind the view contract fixes and
// whose surface already carries the panel's own chrome.
//
// The painter draws a fill for a capsule and for a button, not for a bare
// row or column, so a plugin that asked for a card got its padding and its
// radius and no colour at all -- the fill was accepted by validation and
// then silently dropped. Wrapping the container in the capsule the bar
// already uses for its own cards keeps the promise the vocabulary makes,
// and leaves the container itself to lay the children out.
func card(out *ui.Node) *ui.Node {
	switch out.Kind {
	case ui.KindRow, ui.KindColumn:
	default:
		return out
	}
	if out.Fill == ui.FillNone && out.Stroke == 0 {
		return out
	}
	wrapper := &ui.Node{Kind: ui.KindCapsule, Fill: out.Fill, Radius: out.Radius,
		Stroke: out.Stroke, StrokeFill: out.StrokeFill, Children: []*ui.Node{out}}
	out.Fill, out.Radius = ui.FillNone, 0
	out.Stroke, out.StrokeFill = 0, ui.FillNone
	return wrapper
}
