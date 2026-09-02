package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// swipeSlop is the horizontal travel that cancels a press without committing
// a swipe: below the commit fraction a drag returns the card instead of
// invoking whatever the press landed on.
const swipeSlop = 8

// swipeCommitFraction is the design's swipe threshold: a drag that reaches 35
// percent of the card width commits to dismiss; anything shorter returns.
const swipeCommitFraction = 0.35

// notifyActions is the sink a resolver drives. The production wiring turns
// these into notifyclient commands; tests capture them.
type notifyActions interface {
	invoke(id uint32, key string)
	dismiss(id uint32)
	reply(id uint32, text string)
	hover(id uint32, on bool)
	openLink(href string)
}

// notifyResolver turns pointer sequences over a card tree into notification
// intents. It holds only the in-flight press and the open inline reply; the
// service owns every record.
type notifyResolver struct {
	actions notifyActions

	pressed   *ui.Node
	pressX    int
	pressY    int
	cardWidth int

	replyID uint32 // zero when no reply is open
}

func newNotifyResolver(a notifyActions) *notifyResolver {
	return &notifyResolver{actions: a}
}

// hitAt returns the deepest node under (x, y) that carries an action.
func hitAt(n *ui.Node, x, y int) *ui.Node {
	if n == nil {
		return nil
	}
	inside := x >= n.Bounds.X && x < n.Bounds.X+n.Bounds.W &&
		y >= n.Bounds.Y && y < n.Bounds.Y+n.Bounds.H
	if !inside {
		return nil
	}
	for _, c := range n.Children {
		if hit := hitAt(c, x, y); hit != nil {
			return hit
		}
	}
	if n.Action != "" {
		return n
	}
	return nil
}

func (r *notifyResolver) press(root *ui.Node, x, y int) {
	r.pressed = hitAt(root, x, y)
	r.pressX, r.pressY = x, y
	r.cardWidth = root.Bounds.W
}

// release matches the press. A release on the pressed node activates it; a
// release far enough away commits a swipe; anything else returns the card.
func (r *notifyResolver) release(root *ui.Node, x, y int) {
	pressed := r.pressed
	r.pressed = nil

	// A horizontal drag past the threshold dismisses regardless of which node
	// the press landed on, so swiping a button never invokes it.
	if dx := r.pressX - x; r.cardWidth > 0 && float64(dx) > swipeCommitFraction*float64(r.cardWidth) {
		if id, ok := cardID(root); ok {
			r.actions.dismiss(id)
		}
		return
	}
	// A drag short of the threshold but past the slop returns the card: it
	// was a swipe that did not commit, not a click on the pressed node.
	if dx := r.pressX - x; dx > swipeSlop || dx < -swipeSlop {
		return
	}
	if pressed == nil {
		return
	}
	if hitAt(root, x, y) != pressed {
		return
	}
	r.activate(pressed)
}

// activate dispatches one matched node. Action strings carry the intent
// because Task 4 wrote them there; this is the one place that parses them.
func (r *notifyResolver) activate(n *ui.Node) {
	id, rest, ok := parseCardAction(n.Action)
	if !ok {
		return
	}
	switch rest[0] {
	case "default":
		r.actions.invoke(id, "default")
	case "action":
		if len(rest) == 2 {
			r.actions.invoke(id, rest[1])
		}
	case "dismiss":
		r.actions.dismiss(id)
	case "link":
		if len(rest) == 2 {
			r.actions.openLink(rest[1])
		}
	}
}

// cardID reads the record id off any node in the card tree.
func cardID(n *ui.Node) (uint32, bool) {
	if id, _, ok := parseCardAction(n.Action); ok {
		return id, true
	}
	for _, c := range n.Children {
		if id, ok := cardID(c); ok {
			return id, true
		}
	}
	return 0, false
}

// parseCardAction splits "notify:<id>:<kind>[:<arg>]" into its parts. A link
// href can itself contain colons, so the split is bounded.
func parseCardAction(action string) (uint32, []string, bool) {
	rest, ok := strings.CutPrefix(action, "notify:")
	if !ok {
		return 0, nil, false
	}
	head, tail, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, nil, false
	}
	id, err := strconv.ParseUint(head, 10, 32)
	if err != nil {
		return 0, nil, false
	}
	kind, arg, hasArg := strings.Cut(tail, ":")
	parts := []string{kind}
	if hasArg {
		parts = append(parts, arg)
	}
	return uint32(id), parts, true
}

// Inline reply. Only one reply is open at a time; a second begin is refused
// so two cards never share the keyboard.

func (r *notifyResolver) beginReply(id uint32) bool {
	if r.replyID != 0 {
		return false
	}
	r.replyID = id
	return true
}

func (r *notifyResolver) submitReply(id uint32, text string) {
	if r.replyID != id {
		return
	}
	r.replyID = 0
	if text == "" {
		return
	}
	r.actions.reply(id, text)
}

func (r *notifyResolver) cancelReply() { r.replyID = 0 }

// recordClosed ends a reply whose record the service closed.
func (r *notifyResolver) recordClosed(id uint32) {
	if r.replyID == id {
		r.replyID = 0
	}
}

func (r *notifyResolver) replying() bool { return r.replyID != 0 }

// hoverAt feeds presentation aggregation. It never issues a command.
func (r *notifyResolver) hoverAt(root *ui.Node, x, y int, on bool) {
	if id, ok := cardID(root); ok {
		r.actions.hover(id, on)
	}
}

var _ = fmt.Sprintf
