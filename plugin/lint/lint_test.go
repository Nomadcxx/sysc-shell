package lint_test

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/plugin/lint"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// paddedRow is the shape that reached a user: an 8-padded row whose declared
// Height leaves its children less room than they measure.
func paddedRow(height int, kids ...*v1.Node) *v1.Node {
	return &v1.Node{Kind: v1.KindRow, Height: height, Padding: 8, Children: kids}
}

func panelRoot(children ...*v1.Node) *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Children: children}
}

func TestTreeAcceptsAFittingView(t *testing.T) {
	// 42 = 26 of content for a 16-tall text plus the 2x8 the row insets.
	root := panelRoot(paddedRow(42, &v1.Node{Kind: v1.KindText, Text: "Avg 70%", Width: 56}))
	if f := lint.Tree(root, v1.ViewPanel, 290, 200); len(f) != 0 {
		t.Fatalf("findings = %v, want none", f)
	}
}

func TestTreeNamesThePaddedRowThatCannotFit(t *testing.T) {
	root := panelRoot(paddedRow(28, &v1.Node{Kind: v1.KindText, Text: "Avg 70%", Width: 56}))
	findings := lint.Tree(root, v1.ViewPanel, 290, 200)
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one", findings)
	}
	f := findings[0]
	if f.Path != "root.children[0].children[0]" {
		t.Errorf("path = %q, want the text inside the padded row", f.Path)
	}
	for _, want := range []string{"root.children[0]", `text "Avg 70%"`, "does not fit in 274x12"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("%q missing %q", f.Message, want)
		}
	}
}

func TestTreeReportsTheWrongRootKind(t *testing.T) {
	// The converter fixes the root kind per view; Validate is blind to it.
	root := panelRoot(&v1.Node{Kind: v1.KindText, Text: "hi"})
	if f := lint.Tree(root, v1.ViewBar, lint.BarWidth, lint.BarHeight); len(f) == 0 {
		t.Fatal("a bar view with a column root must be reported")
	}
}

func TestTreeReportsANonPositiveSlot(t *testing.T) {
	root := panelRoot(&v1.Node{Kind: v1.KindText, Text: "hi"})
	findings := lint.Tree(root, v1.ViewPanel, 0, 430)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "not positive") {
		t.Fatalf("findings = %v, want a size complaint", findings)
	}
}

func TestFindingStringCarriesThePath(t *testing.T) {
	f := lint.Finding{Path: "root.children[0]", Message: "ui: boom"}
	if f.String() != "root.children[0]: ui: boom" {
		t.Fatalf("String() = %q", f.String())
	}
	if (lint.Finding{Message: "ui: boom"}).String() != "ui: boom" {
		t.Fatal("a finding without a path must read as its message")
	}
}
