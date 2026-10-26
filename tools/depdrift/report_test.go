package main

import (
	"strings"
	"testing"
)

// TestFindings is the focused check of the reporter: a pin that is behind
// with no reason beside it is a finding, and a pin with a reason is a
// decision.
func TestFindings(t *testing.T) {
	rep := &report{
		Owner: "Nomadcxx",
		Repos: []repoReport{
			{
				Name: "sysc-shell",
				Modules: []moduleRow{
					{Module: "github.com/Nomadcxx/sysc-terminal", Pinned: "v0.2.0", Latest: "v0.3.0", Behind: 2},
					{Module: "github.com/Nomadcxx/sysc-Go", Pinned: "v1.0.7", Latest: "v1.0.10", Behind: 3, Hold: "settled across its consumers (see sysc-1691)"},
					{Module: "github.com/Nomadcxx/sysc-wayland", Pinned: "v0.3.2", Latest: "v0.3.2", Behind: 0},
					{Module: "golang.org/x/sys", Pinned: "v0.47.0", Latest: "v0.49.0", Behind: 1},
					{Module: "golang.org/x/net", Pinned: "v0.0.0-20211118161319-6a13c67c3ce4", Latest: "v0.61.0", Behind: -1, Note: "no release target; cut a tag first"},
				},
				Refs: []refRow{
					{Repository: "Nomadcxx/sysc-plugins", Pinned: "0cea6f74", Head: "04fa71c9", Behind: 46},
					{Repository: "Nomadcxx/sysc-lock", Pinned: "df54457a", Head: "6550d7b9", Behind: 11, Hold: "a grouped pin bump (see sysc-1733)"},
				},
			},
		},
	}
	findings := rep.findings()
	if len(findings) != 2 {
		t.Fatalf("the report found %d findings, want 2: %v", len(findings), findings)
	}
	// The module pin that is behind and the workflow pin that is behind are
	// findings. The held pins are decisions and are not.
	for _, found := range findings {
		if found.Repo != "sysc-shell" {
			t.Errorf("the finding names the repository %q", found.Repo)
		}
	}
	if findings[0].Pin != "Nomadcxx/sysc-plugins" || findings[0].Drift != "46 commits behind" {
		t.Errorf("the first finding is %+v", findings[0])
	}
	if findings[1].Pin != "github.com/Nomadcxx/sysc-terminal" || findings[1].Drift != "2 releases behind" {
		t.Errorf("the second finding is %+v", findings[1])
	}
}

func TestMarkdown(t *testing.T) {
	rep := &report{
		Owner:     "Nomadcxx",
		Excluded:  []string{"`sysc-walls` — out of scope"},
		Uncovered: []string{"sysc-new"},
		Repos: []repoReport{
			{
				Name:          "sysc-shell",
				Branch:        "main",
				Toolchain:     "go 1.26.10",
				ToolchainNote: "the floor here is the floor everywhere",
				Modules: []moduleRow{
					{Module: "github.com/Nomadcxx/sysc-Go", Pinned: "v1.0.7", Latest: "v1.0.10", Behind: 3, Hold: "settled across its consumers (see sysc-1691)"},
					{Module: "golang.org/x/net", Pinned: "v0.0.0-20211118161319-6a13c67c3ce4", Latest: "v0.61.0", Behind: -1, Note: "no release target; cut a tag first"},
				},
				Refs: []refRow{
					{Repository: "Nomadcxx/sysc-plugins", Pinned: "0cea6f74a0c9", Head: "04fa71c9", Behind: 46},
				},
			},
		},
	}
	markdown := rep.Markdown()
	for _, want := range []string{
		"# Dependency drift",
		"Repositories walked: 1",
		"`sysc-walls` — out of scope",
		"`sysc-new`",
		"go 1.26.10",
		"the floor here is the floor everywhere",
		"no release target; cut a tag first",
		"3 releases behind",
		"46 commits behind",
		"(see sysc-1691)",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("the report does not carry %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "3 releases behind | —") {
		t.Errorf("a held pin was reported as a finding")
	}
}
