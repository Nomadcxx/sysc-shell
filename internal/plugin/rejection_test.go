package plugin

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// A view the host cannot render used to disappear with no account of why: a
// plugin author saw a blank panel and could not tell a dead process from a
// rejected tree (sysc-1936). These pin the reason a rejection carries, which
// is what the operator's journal and the failure label both quote.

func TestPreparerRejectsARootOfTheWrongKindAndSaysWhy(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	// A bar is laid out as a row. A text root has no row to run.
	p.Submit(barJob("v1", 3, &v1.Node{Kind: v1.KindText, Text: "hello"}))
	got := await(t, p)
	if got.Err == nil {
		t.Fatal("a bar accepted a text root")
	}
	// Plugin and view travel beside the error, so the host can name both.
	if got.ViewID != "v1" || got.Plugin != "org.sysc.timer" {
		t.Fatalf("result = %+v, want the plugin and view named alongside the error", got)
	}
	for _, want := range []string{"bar view", "row root", "text"} {
		if !strings.Contains(got.Err.Error(), want) {
			t.Errorf("reason %q does not name %q", got.Err.Error(), want)
		}
	}
}

// panelJob is a column-rooted job: the tree budgets are independent of which
// view the tree is for, and a panel takes a column root.
func panelJob(viewID string, revision uint64, root *v1.Node) Job {
	return Job{
		ViewID:   viewID,
		Plugin:   "org.sysc.timer",
		View:     v1.ViewPanel,
		Revision: revision,
		Root:     root,
		Bounds:   ui.Rect{W: 400, H: 400},
	}
}

// treeOf builds a tree of exactly total nodes, root included, with no node
// holding more than v1.MaxChildren children. Both protocol ceilings apply to
// one tree, so a test of one has to stay under the other.
func treeOf(total int) *v1.Node {
	root := &v1.Node{Kind: v1.KindColumn}
	if total <= 1 {
		return root
	}
	queue := []*v1.Node{root}
	made := 1
	for made < total {
		parent := queue[0]
		child := &v1.Node{Kind: v1.KindColumn}
		parent.Children = append(parent.Children, child)
		queue = append(queue, child)
		if len(parent.Children) >= v1.MaxChildren {
			queue = queue[1:]
		}
		made++
	}
	return root
}

// The tree budget is a ceiling, not a warning: a plugin that counts its nodes
// has to be able to rely on where the line is.
func TestPreparerEnforcesTheTreeNodeBudget(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	p.Submit(panelJob("fat", 1, treeOf(v1.MaxNodes+1)))
	got := await(t, p)
	if got.Err == nil {
		t.Fatalf("a tree of %d nodes was accepted", v1.MaxNodes+1)
	}
	if want := fmt.Sprintf("more than %d nodes", v1.MaxNodes); !strings.Contains(got.Err.Error(), want) {
		t.Errorf("reason %q does not name the budget it broke (%q)", got.Err.Error(), want)
	}
	if got.Root != nil {
		t.Error("a rejected tree published a root")
	}

	p.Submit(panelJob("ok", 1, treeOf(v1.MaxNodes)))
	got = await(t, p)
	if got.Err != nil {
		t.Fatalf("a tree of exactly %d nodes was rejected: %v", v1.MaxNodes, got.Err)
	}
	if got.Root == nil || got.Root.Kind != ui.KindColumn {
		t.Fatalf("root = %+v, want a laid-out column", got.Root)
	}
}

// The per-node child ceiling is separate from the tree total and hits first.
// A flat list is what a plugin actually writes, so this is the one a plugin
// author meets before the tree budget ever comes into it.
func TestPreparerEnforcesThePerNodeChildBudget(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	flat := func(n int) *v1.Node {
		root := &v1.Node{Kind: v1.KindColumn}
		for range n {
			root.Children = append(root.Children, &v1.Node{Kind: v1.KindText, Text: "x"})
		}
		return root
	}

	p.Submit(panelJob("flat", 1, flat(v1.MaxChildren+1)))
	got := await(t, p)
	if got.Err == nil {
		t.Fatalf("a node with %d children was accepted", v1.MaxChildren+1)
	}
	if want := fmt.Sprintf("%d children, more than the %d allowed", v1.MaxChildren+1, v1.MaxChildren); !strings.Contains(got.Err.Error(), want) {
		t.Errorf("reason %q does not name the budget it broke (%q)", got.Err.Error(), want)
	}

	p.Submit(panelJob("ok", 1, flat(v1.MaxChildren)))
	got = await(t, p)
	if got.Err != nil {
		t.Fatalf("a node with exactly %d children was rejected: %v", v1.MaxChildren, got.Err)
	}
}
