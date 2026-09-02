package ui

import "fmt"

// Animated reports whether the surface animator tracks this node. Only chrome
// that resolves interaction state carries a transition: buttons and segments
// always, and a capsule only when it is clickable, so the bar's CPU and memory
// display groups stay static.
func Animated(n *Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case KindButton, KindSegmented:
		return true
	case KindCapsule:
		return n.Action != ""
	default:
		return false
	}
}

// ValidateKeys reports whether every animated node in the tree carries a
// distinct, non-empty StableKey. The animator holds resolved visual state in a
// map across rebuilds, so a duplicate key makes two controls share one
// transition and an empty key silently drops the node's animation. Both are
// composition errors in the caller, not conditions to recover from at paint
// time, so they surface here rather than as a missing hover.
func ValidateKeys(root *Node) error {
	seen := map[string]bool{}
	var walk func(*Node) error
	walk = func(n *Node) error {
		if n == nil {
			return nil
		}
		if Animated(n) {
			key := n.StableKey()
			if key == "" {
				return fmt.Errorf("ui: animated %s needs a Key or Action to keep its state across rebuilds", describeNode(n))
			}
			if seen[key] {
				return fmt.Errorf("ui: animated key %q is used twice; two controls would share one transition", key)
			}
			seen[key] = true
		}
		if n.Kind == KindVirtualList && n.Item != nil {
			for i := 0; i < n.ItemCount; i++ {
				if err := walk(n.Item(i)); err != nil {
					return err
				}
			}
			return nil
		}
		for _, c := range n.Children {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

// describeNode names a node for an error a caller has to act on. Text is the
// only identifying content an unkeyed node has.
func describeNode(n *Node) string {
	if n.Text != "" {
		return fmt.Sprintf("button %q", n.Text)
	}
	if n.Icon != "" {
		return fmt.Sprintf("icon button %q", n.Icon)
	}
	return fmt.Sprintf("node of kind %d", n.Kind)
}
