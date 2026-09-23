// Package lint checks a plugin view tree against the host's layout rules
// before the tree is ever sent to a shell, using the host's own pipeline and
// text metric: v1.Validate, the converter, and the arithmetic that lays a view
// out. It exists because v1.Validate is geometry-blind and the host's layout
// stops at the first rejection, so a plugin can ship a view that is
// grammatically perfect and cannot be drawn — the host then shows the user a
// failure card with Close, Retry and Disable where the panel should be.
//
// The rules it enforces, in the host's own terms:
//
//   - A bar view's root is a row; a panel's root is a column (or a list); a
//     tooltip's root is a column. The converter refuses anything else.
//   - Height includes padding. A node's content box is Height − 2×Padding, and
//     a row is refused when a child cannot live in it. A 28-tall row with
//     Padding 8 has twelve pixels of content.
//   - A row's children must fit its content width too. Text is clipped at the
//     row's edge, but a control (button, capsule, segmented control, menu,
//     drag source) that overruns it refuses the row. PinEnd reserves the
//     trailing child's width before the leading text is clipped.
//   - A column never refuses: a child taller or wider than its column
//     truncates or overflows in silence. Budget a column's children yourself.
//   - A capsule in a row fills the row's content height, so a fixed-size disc
//     needs a row content height that matches the disc.
//   - Text measures len(bytes)×8 wide and 16 tall, whatever its role, on a bar
//     slot of BarWidth×BarHeight and on the panel box the manifest declares.
//
// Tree reports every violation of those rules at once. A plugin's test suite
// should call it for each view it can build, at the sizes the host uses:
//
//	for _, f := range lint.Tree(tree, v1.ViewPanel, panelW, panelH) {
//		t.Errorf("panel: %s", f)
//	}
package lint

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// The slots a host opens a view with. A panel's box comes from the plugin's
// own manifest; these are the sizes a bar widget and a tooltip are given.
const (
	BarWidth      = 240
	BarHeight     = 32
	TooltipWidth  = 280
	TooltipHeight = 200
)

// Finding is one reason a host would refuse this view. Path is the wire path
// of the offending node ("root.children[1].children[0]") when the pipeline got
// far enough to name one; Message is the wording a rejection carries, or the
// validator's own error when the tree never reached layout.
type Finding struct {
	Path    string
	Message string
}

// String renders a finding the way a test failure should read: the node, then
// the reason.
func (f Finding) String() string {
	if f.Path == "" {
		return f.Message
	}
	return f.Path + ": " + f.Message
}

// Tree reports every reason the host would refuse this view at this size. An
// empty result means the view lays out.
func Tree(root *v1.Node, view v1.ViewKind, width, height int) []Finding {
	if width <= 0 || height <= 0 {
		return []Finding{{Message: fmt.Sprintf("view size %dx%d is not positive", width, height)}}
	}
	if err := v1.Validate(root, view); err != nil {
		return []Finding{{Message: err.Error()}}
	}
	converted, err := plugin.Convert(root, view)
	if err != nil {
		return []Finding{{Message: err.Error()}}
	}
	problems := ui.CheckFit(converted, ui.Rect{W: width, H: height}, plugin.Measure)
	out := make([]Finding, 0, len(problems))
	for _, p := range problems {
		out = append(out, Finding{Path: p.Path, Message: p.Message})
	}
	return out
}
