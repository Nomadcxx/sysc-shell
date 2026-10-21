package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModPinsReadsTheRequireBlock(t *testing.T) {
	pins, err := goModPins()
	if err != nil {
		t.Fatal(err)
	}
	version, ok := pins["github.com/Nomadcxx/sysc-wayland"]
	if !ok {
		t.Fatalf("pins = %v, want the wayland module", pins)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "github.com/Nomadcxx/sysc-wayland "+version) {
		t.Fatalf("version %q does not match the go.mod text", version)
	}
}

func TestCheckPinsReportsARestatedVersion(t *testing.T) {
	pins := map[string]string{"github.com/Nomadcxx/sysc-wayland": "v0.3.2"}
	findings := checkPins("testdata/bad/pinned-module.md", pins)
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one", findings)
	}
	for _, want := range []string{"pinned-module.md:4:", "github.com/Nomadcxx/sysc-wayland at v0.2.2", "v0.3.2"} {
		if !strings.Contains(findings[0], want) {
			t.Fatalf("finding %q does not contain %q", findings[0], want)
		}
	}
}

func TestCheckPinsLeavesADocumentThatNamesNoVersion(t *testing.T) {
	pins := map[string]string{"github.com/Nomadcxx/sysc-wayland": "v0.3.2"}
	if findings := checkPins("testdata/good/config-object.md", pins); len(findings) != 0 {
		t.Fatalf("findings = %v, want none", findings)
	}
}

func TestCheckPinsLeavesAModuleTheRepositoryDoesNotRequire(t *testing.T) {
	other := map[string]string{"github.com/other/tool": "v1.0.0"}
	if findings := checkPins("testdata/bad/pinned-module.md", other); len(findings) != 0 {
		t.Fatalf("findings = %v, want none", findings)
	}
}

// TestTheContributorGuideNamesNoModuleVersion is the check that keeps a rule
// from going stale: the guide used to name the wayland module at a version
// the repository had moved past, and a rule that names a version reads as a
// constraint the next session would try to satisfy.
func TestTheContributorGuideNamesNoModuleVersion(t *testing.T) {
	pins, err := goModPins()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoRoot(), "AGENTS.md")
	if findings := checkPins(path, pins); len(findings) != 0 {
		t.Fatalf("findings = %v, want none", findings)
	}
}
