package shell

// The process allows one interactive root at a time. A root is whatever holds
// keyboard focus and the pointer grab for a modal surface: today a panel,
// during Milestone 5 also the notification centre and the tray drawer. A root
// may own one attached child, such as a tray popup opened from a drawer.
//
// Opening an unrelated root replaces the whole chain. The replaced chain's
// cleanup runs before the new owner is published, so keyboard interactivity,
// text input, saved serials, and tooltip visibility are released while the old
// owner is still the current one and can never be released twice.
//
// Every method is called with Registry.mu held.

// rootKind names the sort of surface that can own the chain.
type rootKind uint8

const (
	rootNone rootKind = iota
	rootPanel
	rootTrayMenu
	rootTrayDrawer
)

// rootID identifies one interactive root. Two roots are the same only when
// both the kind and the key match.
type rootID struct {
	kind rootKind
	key  uint64
}

func panelRoot(id PanelID) rootID { return rootID{kind: rootPanel, key: uint64(id)} }

// trayMenuRoot keys a tray menu root by the wl_registry global of the output
// it is open on. One chain exists at a time, so one menu per output is enough
// to make a replacement unambiguous.
func trayMenuRoot(outputGlobal uint32) rootID {
	return rootID{kind: rootTrayMenu, key: uint64(outputGlobal)}
}

func trayDrawerRoot(outputGlobal uint32) rootID {
	return rootID{kind: rootTrayDrawer, key: uint64(outputGlobal)}
}

// rootChain is the single interactive root and its optional attached child.
//
// generation advances on every open. A close request naming an older
// generation describes a chain that has already gone and does nothing, which
// is what makes a late close from a dwell timer or a compositor event safe.
type rootChain struct {
	generation uint64
	open       bool

	owner        rootID
	ownerCleanup []func()

	child        rootID
	hasChild     bool
	childCleanup []func()
}

// openRoot publishes id as the chain owner and returns its generation. Any
// chain already open is released first, child before owner.
func (c *rootChain) openRoot(id rootID) uint64 {
	c.release()
	c.generation++
	c.open = true
	c.owner = id
	return c.generation
}

// attach binds a child to the current owner. It reports false when there is no
// owner or the generation has moved on, so a child can never outlive the root
// it was opened from.
func (c *rootChain) attach(generation uint64, id rootID) bool {
	if !c.open || generation != c.generation {
		return false
	}
	c.releaseChild()
	c.child = id
	c.hasChild = true
	return true
}

// onClose registers cleanup for the current owner. Cleanup runs in reverse
// registration order, so a later step is undone before the step it built on.
func (c *rootChain) onClose(generation uint64, fn func()) bool {
	if !c.open || generation != c.generation || fn == nil {
		return false
	}
	c.ownerCleanup = append(c.ownerCleanup, fn)
	return true
}

// onChildClose registers cleanup for the attached child.
func (c *rootChain) onChildClose(generation uint64, fn func()) bool {
	if !c.open || !c.hasChild || generation != c.generation || fn == nil {
		return false
	}
	c.childCleanup = append(c.childCleanup, fn)
	return true
}

// closeRoot releases the whole chain when the generation still matches.
func (c *rootChain) closeRoot(generation uint64) bool {
	if !c.open || generation != c.generation {
		return false
	}
	c.release()
	return true
}

// closeChild releases only the attached child, leaving its owner open.
func (c *rootChain) closeChild(generation uint64) bool {
	if !c.open || !c.hasChild || generation != c.generation {
		return false
	}
	c.releaseChild()
	return true
}

// current reports the chain owner and its generation.
func (c *rootChain) current() (rootID, uint64, bool) {
	if !c.open {
		return rootID{}, 0, false
	}
	return c.owner, c.generation, true
}

// currentChild reports the attached child.
func (c *rootChain) currentChild() (rootID, bool) {
	if !c.open || !c.hasChild {
		return rootID{}, false
	}
	return c.child, true
}

// owns reports whether id is the chain owner.
func (c *rootChain) owns(id rootID) bool { return c.open && c.owner == id }

// release runs the whole chain's cleanup exactly once, child first.
func (c *rootChain) release() {
	if !c.open {
		return
	}
	c.releaseChild()
	cleanup := c.ownerCleanup
	c.ownerCleanup = nil
	c.open = false
	c.owner = rootID{}
	runReverse(cleanup)
}

func (c *rootChain) releaseChild() {
	if !c.hasChild {
		return
	}
	cleanup := c.childCleanup
	c.childCleanup = nil
	c.hasChild = false
	c.child = rootID{}
	runReverse(cleanup)
}

// runReverse undoes steps in the opposite order to the one that built them.
func runReverse(cleanup []func()) {
	for i := len(cleanup) - 1; i >= 0; i-- {
		cleanup[i]()
	}
}
