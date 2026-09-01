package shell

import (
	"slices"
	"testing"
)

func TestRootChainReplacesTheWholeChainOnAnUnrelatedRoot(t *testing.T) {
	t.Parallel()
	var chain rootChain
	var order []string

	first := chain.openRoot(panelRoot(PanelClock))
	chain.onClose(first, func() { order = append(order, "owner:clock") })
	if !chain.attach(first, rootID{kind: rootPanel, key: 99}) {
		t.Fatal("attach was refused for the current generation")
	}
	chain.onChildClose(first, func() { order = append(order, "child:clock") })

	second := chain.openRoot(panelRoot(PanelSession))
	if second == first {
		t.Fatal("a replacement kept the old generation")
	}
	// The child is released before its owner, and both before the new owner
	// becomes current.
	if !slices.Equal(order, []string{"child:clock", "owner:clock"}) {
		t.Fatalf("cleanup order = %v", order)
	}
	if _, ok := chain.currentChild(); ok {
		t.Fatal("the replaced chain kept its child")
	}
	if !chain.owns(panelRoot(PanelSession)) {
		t.Fatal("the new root is not the chain owner")
	}
}

func TestRootChainRunsCleanupExactlyOnce(t *testing.T) {
	t.Parallel()
	var chain rootChain
	var owner, child int

	generation := chain.openRoot(panelRoot(PanelClock))
	chain.onClose(generation, func() { owner++ })
	chain.attach(generation, rootID{kind: rootPanel, key: 99})
	chain.onChildClose(generation, func() { child++ })

	if !chain.closeRoot(generation) {
		t.Fatal("closeRoot refused the current generation")
	}
	// Every later close names a chain that is already gone.
	if chain.closeRoot(generation) {
		t.Fatal("closeRoot ran twice for one chain")
	}
	if chain.closeChild(generation) {
		t.Fatal("closeChild ran after the chain was released")
	}
	chain.release()
	if owner != 1 || child != 1 {
		t.Fatalf("cleanup ran owner=%d child=%d, want 1 each", owner, child)
	}
}

func TestRootChainCleanupRunsInReverseOrder(t *testing.T) {
	t.Parallel()
	var chain rootChain
	var order []string

	generation := chain.openRoot(panelRoot(PanelSettings))
	for _, step := range []string{"keyboard", "text-input", "serial", "tooltip"} {
		chain.onClose(generation, func() { order = append(order, step) })
	}
	chain.closeRoot(generation)

	want := []string{"tooltip", "serial", "text-input", "keyboard"}
	if !slices.Equal(order, want) {
		t.Fatalf("cleanup order = %v, want %v", order, want)
	}
}

func TestRootChainClosesAChildWithoutClosingItsOwner(t *testing.T) {
	t.Parallel()
	var chain rootChain
	var owner, child int

	generation := chain.openRoot(panelRoot(PanelMonitor))
	chain.onClose(generation, func() { owner++ })
	chain.attach(generation, rootID{kind: rootPanel, key: 5})
	chain.onChildClose(generation, func() { child++ })

	if !chain.closeChild(generation) {
		t.Fatal("closeChild refused the current generation")
	}
	if child != 1 || owner != 0 {
		t.Fatalf("closing a child ran owner=%d child=%d", owner, child)
	}
	if !chain.owns(panelRoot(PanelMonitor)) {
		t.Fatal("closing a child released its owner")
	}
	if _, ok := chain.currentChild(); ok {
		t.Fatal("the child survived its own close")
	}
}

func TestRootChainIgnoresStaleGenerations(t *testing.T) {
	t.Parallel()
	var chain rootChain
	var first, second int

	stale := chain.openRoot(panelRoot(PanelClock))
	chain.onClose(stale, func() { first++ })
	fresh := chain.openRoot(panelRoot(PanelSession))
	chain.onClose(fresh, func() { second++ })

	if first != 1 {
		t.Fatalf("the replaced chain ran cleanup %d times", first)
	}
	// A close that was in flight when the chain changed must do nothing.
	if chain.closeRoot(stale) {
		t.Fatal("a stale close released the current chain")
	}
	if chain.attach(stale, rootID{kind: rootPanel, key: 1}) {
		t.Fatal("a stale generation attached a child")
	}
	if chain.onClose(stale, func() { t.Fatal("stale cleanup ran") }) {
		t.Fatal("a stale generation registered cleanup")
	}
	if second != 0 {
		t.Fatal("a stale close released the current chain's cleanup")
	}
	if !chain.owns(panelRoot(PanelSession)) {
		t.Fatal("a stale close changed the chain owner")
	}
}

func TestRootChainRefusesWorkWithNoOwner(t *testing.T) {
	t.Parallel()
	var chain rootChain

	if _, _, ok := chain.current(); ok {
		t.Fatal("an empty chain reported an owner")
	}
	if chain.closeRoot(0) || chain.closeChild(0) {
		t.Fatal("an empty chain accepted a close")
	}
	if chain.attach(0, panelRoot(PanelClock)) {
		t.Fatal("an empty chain accepted a child")
	}
	if chain.onClose(0, func() {}) {
		t.Fatal("an empty chain accepted cleanup")
	}
	chain.release()
}
