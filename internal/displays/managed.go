package displays

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// BuildManaged renders the complete text of the owned file after a Keep.
//
// The rule that shapes this function: the owned block is a clone of the whole
// effective node, not a five-field summary. Layout, backdrop, hot corners,
// focus-at-startup and every other active child survive an edit, because the
// alternative is silently dropping a setting the user can see working. Only
// the fields the draft actually changed are rewritten; the rest keep the
// spellings the user already had.
//
// A connector with no matching node anywhere in the graph gets native
// defaults, which is the honest starting point: the shell does not invent a
// layout it never observed.
//
// The scale, transform and mode-refresh bounds are checked here because the
// graph alone can prove them wrong. Whether a mode was ever advertised is not
// knowable from a config file, so the caller validates the policy against the
// output it discovered with ValidateMode before it gets here.
func BuildManaged(graph *Graph, order []string, policies map[string]Policy) ([]byte, error) {
	names := make([]Name, len(order))
	for i, connector := range order {
		names[i] = Name{Connector: connector}
	}
	return buildManaged(graph, names, policies)
}

func buildManaged(graph *Graph, order []Name, policies map[string]Policy) ([]byte, error) {
	doc := document.New()
	changed := make(map[string]bool, len(order))
	for _, name := range order {
		connector := name.Connector
		want, ok := policies[connector]
		if !ok {
			return nil, fmt.Errorf("displays: no policy for %s", connector)
		}
		node, err := managedNode(graph.Find, name, want)
		if err != nil {
			return nil, err
		}
		doc.AddNode(node)
		changed[connector] = true
	}
	// Managed connectors that are disconnected or untouched keep their whole
	// existing block, including options this editor does not expose.
	managed := ManagedPath(graph.Root.Resolved)
	for _, source := range graph.Outputs {
		if source.File == managed && !changed[source.Name] {
			doc.AddNode(source.Node)
		}
	}
	var buf bytes.Buffer
	if err := kdl.Generate(doc, &buf); err != nil {
		return nil, fmt.Errorf("displays: render managed file: %w", err)
	}
	// The header is prepended rather than built as nodes because the KDL
	// library has no comment kind: a comment is not a node in the AST. The
	// managed file is entirely the shell's own text, so prepending is not a
	// preservation problem.
	return append([]byte(managedHeader), buf.Bytes()...), nil
}

// managedHeader explains the file to anyone who opens it in an editor,
// including a future reader who wonders why the user's own block seems to
// have stopped working.
const managedHeader = `// ` + ManagedFileName + ` is written by sysc-shell. Do not edit while a
// preview is running: the shell compares these bytes before it writes and
// will refuse to overwrite an edit it did not make.
//
// Each block below is a complete clone of the output block that was active
// when the setting was kept, with only the changed fields rewritten. Layout,
// backdrop, hot corners and focus-at-startup are carried over rather than
// dropped. The original block is untouched and is now shadowed for this
// connector, because the first matching block in the resolved graph wins.
`

// findFunc is the graph lookup, named so BuildManaged can take a function
// rather than reach into a graph in tests.
type findFunc func(Name) (OutputNode, bool)

func managedNode(find findFunc, name Name, want Policy) (*document.Node, error) {
	if err := validatePolicy(name, want); err != nil {
		return nil, err
	}
	source, ok := find(name)
	node := document.NewNode()
	if ok {
		// ShallowCopy keeps children, properties and arguments; the Name
		// pointer is replaced because the generated block must match the
		// connector exactly rather than the name the user typed.
		clone := source.Node.ShallowCopy()
		clone.Children = slices.Clone(source.Node.Children)
		node = clone
		setArgument(node, name.Connector)
	} else {
		setName(node, "output")
		node.AddArgument(name.Connector, "")
	}
	applyPolicy(node, want)
	return node, nil
}

func validatePolicy(name Name, want Policy) error {
	if want.Scale != nil {
		if err := ValidateScale(*want.Scale); err != nil {
			return fmt.Errorf("displays: %s: %w", name.Connector, err)
		}
	}
	if want.Mode != nil && (want.Mode.Width == 0 || want.Mode.Height == 0) {
		return fmt.Errorf("displays: %s: mode has an invalid size", name.Connector)
	}
	if want.Transform != nil {
		if err := ParseTransformNative(*want.Transform); err != nil {
			return fmt.Errorf("displays: %s: %w", name.Connector, err)
		}
	}
	return nil
}

// applyPolicy rewrites exactly the non-nil fields. A nil field is automatic,
// which niri spells as an absent node, so a field that becomes automatic has
// its node removed rather than set to a default value.
func applyPolicy(node *document.Node, want Policy) {
	if want.Mode != nil {
		replaceChild(node, modeNode(want.Mode))
	} else {
		removeChild(node, "mode")
	}
	if want.Scale != nil {
		if err := ValidateScale(*want.Scale); err != nil {
			// Validated in managedNode; unreachable here.
			panic("displays: unvalidated scale reached applyPolicy")
		}
		replaceChild(node, childNode("scale", *want.Scale))
	} else {
		removeChild(node, "scale")
	}
	if want.Transform != nil && *want.Transform != "Normal" {
		replaceChild(node, childNode("transform", nativeTransforms[slices.Index(Transforms, *want.Transform)]))
	} else {
		removeChild(node, "transform")
	}
	if want.Position != nil {
		pos := document.NewNode()
		pos.SetName("position")
		pos.AddProperty("x", int64(want.Position.X), "")
		pos.AddProperty("y", int64(want.Position.Y), "")
		replaceChild(node, pos)
	} else {
		removeChild(node, "position")
	}
	if want.VRR != nil {
		vrr := document.NewNode()
		vrr.SetName("variable-refresh-rate")
		if want.VRR.OnDemand {
			vrr.AddProperty("on-demand", true, "")
		}
		replaceChild(node, vrr)
	} else {
		removeChild(node, "variable-refresh-rate")
	}
}

// modeNode renders niri's mode node: one string argument plus a custom flag.
// The flag is omitted when false because that is the native default, and an
// absent property is easier to recognise in a diff than custom=false.
func modeNode(m *ModePolicy) *document.Node {
	n := childNode("mode", m.String())
	if m.Custom {
		n.AddProperty("custom", true, "")
	}
	return n
}

func childNode(name string, args ...any) *document.Node {
	n := document.NewNode()
	n.SetName(name)
	for _, a := range args {
		n.AddArgument(a, "")
	}
	return n
}

// replaceChild swaps a child in place when it already exists, so a clone keeps
// the field's original position, and appends otherwise. Position inside the
// block is not semantically meaningful to niri, but keeping it makes the
// generated diff readable against the block it came from.
func replaceChild(node *document.Node, child *document.Node) {
	if idx := indexChild(node, text(child.Name)); idx >= 0 {
		node.Children[idx] = child
		return
	}
	node.AddNode(child)
}

func indexChild(node *document.Node, name string) int {
	for i, c := range node.Children {
		if n, ok := c.Name.Value.(string); ok && n == name {
			return i
		}
	}
	return -1
}

func removeChild(node *document.Node, name string) {
	kept := node.Children[:0]
	for _, c := range node.Children {
		if n, ok := c.Name.Value.(string); ok && n == name {
			continue
		}
		kept = append(kept, c)
	}
	node.Children = kept
}

// setName gives a node its name as a bare Go string. A node name is not an
// argument, so the encoder leaves it unquoted when it can.
func setName(node *document.Node, name string) {
	val := &document.Value{}
	val.Value = name
	node.Name = val
}

// setArgument replaces the node's single name argument, which for an output
// block is the exact connector.
func setArgument(node *document.Node, arg string) {
	node.Arguments = nil
	node.AddArgument(arg, "")
}

// Transforms is the set of native transform spellings. The KDL string and the
// IPC name are the same word in niri 26.04, so one set serves both.
var Transforms = []string{
	"Normal", "90", "180", "270",
	"Flipped", "Flipped90", "Flipped180", "Flipped270",
}

// Native KDL and IPC use different spellings. Normal is encoded by omission
// because Niri's native parser has no explicit normal transform.
var nativeTransforms = []string{"normal", "90", "180", "270", "flipped", "flipped-90", "flipped-180", "flipped-270"}

// ParseTransformNative reports whether s is a transform niri will accept. The
// native form is the same token the IPC uses, so the check is a set
// membership rather than a second table that could drift.
func ParseTransformNative(s string) error {
	if !slices.Contains(Transforms, s) {
		return fmt.Errorf("displays: %q is not a niri transform (one of %s)", s, strings.Join(Transforms, ", "))
	}
	return nil
}
