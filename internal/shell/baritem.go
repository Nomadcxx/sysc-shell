package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// hasBarItem reports whether a bar layout places an item anywhere, including
// inside a group.
func hasBarItem(policy config.Bar, id string) bool {
	return hasItemIn(policy.Left, id) || hasItemIn(policy.Center, id) || hasItemIn(policy.Right, id)
}

func hasItemIn(items []config.Item, id string) bool {
	for _, item := range items {
		if item.ID == id || (item.ID == "group" && hasItemIn(item.Items, id)) {
			return true
		}
	}
	return false
}
