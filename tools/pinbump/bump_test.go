package main

import (
	"strings"
	"testing"
)

// original is a go.mod shaped like the shell's.
const original = `module github.com/Nomadcxx/sysc-shell

// The module calls x509.Certificate.Verify and http.Client.Do, so it needs a
// standard library without GO-2026-6809 (fixed in go1.26.10).
go 1.26.10

require (
	github.com/Nomadcxx/sysc-terminal v0.2.0
	golang.org/x/sys v0.47.0
)

require github.com/Nomadcxx/sysc-Go v1.0.7 // indirect

ignore ./docs
`

// TestVerify is the focused check of the tool: a bump may change the require
// line it names and nothing else.
func TestVerify(t *testing.T) {
	bumped := strings.Replace(original,
		"github.com/Nomadcxx/sysc-terminal v0.2.0",
		"github.com/Nomadcxx/sysc-terminal v0.3.0", 1)
	bumps := []Bump{{Module: "github.com/Nomadcxx/sysc-terminal", Version: "v0.3.0"}}
	cases := []struct {
		name    string
		updated string
		bumps   []Bump
		wantErr string
	}{
		{
			name:    "the require line the bump names",
			updated: bumped,
			bumps:   bumps,
		},
		{
			name:    "the go directive was lowered",
			updated: strings.Replace(bumped, "go 1.26.10", "go 1.26.4", 1),
			bumps:   bumps,
			wantErr: "go directive",
		},
		{
			name:    "the comment above the go directive was dropped",
			updated: strings.Replace(bumped, "// standard library without GO-2026-6809 (fixed in go1.26.10).\n", "", 1),
			bumps:   bumps,
			wantErr: "comment above the go directive",
		},
		{
			name:    "the ignore directive was deleted",
			updated: strings.Replace(bumped, "\nignore ./docs\n", "\n", 1),
			bumps:   bumps,
			wantErr: "ignore directives",
		},
		{
			name:    "another requirement moved as well",
			updated: strings.Replace(bumped, "golang.org/x/sys v0.47.0", "golang.org/x/sys v0.49.0", 1),
			bumps:   bumps,
			wantErr: "without being asked to",
		},
		{
			name:    "a requirement was added",
			updated: bumped + "\nrequire example.com/added v1.0.0\n",
			bumps:   bumps,
			wantErr: "added a requirement",
		},
		{
			name:    "a requirement was removed",
			updated: strings.Replace(bumped, "require github.com/Nomadcxx/sysc-Go v1.0.7 // indirect\n", "", 1),
			bumps:   bumps,
			wantErr: "removed the requirement",
		},
		{
			name:    "the comment on the requirement changed",
			updated: strings.Replace(bumped, "v1.0.7 // indirect", "v1.0.7 // indirect, pinned", 1),
			bumps:   bumps,
			wantErr: "comment on the requirement",
		},
		{
			name:    "the bump names a module the go.mod does not require",
			updated: original,
			bumps:   []Bump{{Module: "example.com/missing", Version: "v1.0.0"}},
			wantErr: "does not require",
		},
		{
			name:    "the bump did not move the requirement",
			updated: original,
			bumps:   bumps,
			wantErr: "did not move",
		},
	}
	for _, c := range cases {
		err := Verify(original, c.updated, c.bumps)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: Verify failed on a clean bump: %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: Verify accepted a bump it should have refused", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: Verify said %q, want it to name %q", c.name, err, c.wantErr)
		}
	}
}

func TestParseBump(t *testing.T) {
	bump, err := ParseBump("github.com/Nomadcxx/sysc-terminal=v0.3.0")
	if err != nil {
		t.Fatalf("ParseBump failed: %v", err)
	}
	if bump.Module != "github.com/Nomadcxx/sysc-terminal" || bump.Version != "v0.3.0" {
		t.Fatalf("ParseBump = %+v", bump)
	}
	if got := bump.String(); got != "github.com/Nomadcxx/sysc-terminal=v0.3.0" {
		t.Errorf("Bump.String = %q", got)
	}
	if _, err := ParseBump("no-equals-sign"); err == nil {
		t.Errorf("ParseBump accepted a pair it cannot read")
	}
}
