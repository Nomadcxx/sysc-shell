package shell

import (
	"os/user"
	"strconv"
	"sync"
)

// usernameCache resolves process owners for the monitor's USER column. It
// has its own lock because the tree reads it under Registry.mu, and a lookup
// can reach NSS; each uid is resolved once for the shell's lifetime.
type usernameCache struct {
	mu     sync.Mutex
	names  map[uint32]string
	lookup func(uid string) (string, error)
}

func newUsernameCache(lookup func(uid string) (string, error)) *usernameCache {
	if lookup == nil {
		lookup = func(uid string) (string, error) {
			u, err := user.LookupId(uid)
			if err != nil {
				return "", err
			}
			return u.Username, nil
		}
	}
	return &usernameCache{names: map[uint32]string{}, lookup: lookup}
}

// Name returns the user name, or the numeric id when the lookup fails.
func (c *usernameCache) Name(uid uint32) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name, ok := c.names[uid]; ok {
		return name
	}
	id := strconv.FormatUint(uint64(uid), 10)
	name, err := c.lookup(id)
	if err != nil || name == "" {
		name = id
	}
	c.names[uid] = name
	return name
}

// usernameCache returns the registry's cache, building it on first use.
// Caller holds r.mu.
func (r *Registry) usernameCache() *usernameCache {
	if r.usernames == nil {
		r.usernames = newUsernameCache(nil)
	}
	return r.usernames
}
