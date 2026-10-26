package dep

import (
	"strings"
	"testing"
)

// goModSample is a go.mod shaped like the shell's: a comment above the go
// directive, require blocks, a pseudo-version, an indirect requirement and a
// hold reason above a requirement.
const goModSample = `module github.com/Nomadcxx/sysc-shell

// The module calls x509.Certificate.Verify and http.Client.Do, so it needs a
// standard library without GO-2026-6809.
go 1.26.10

require (
	github.com/Nomadcxx/sysc-clipboard v0.1.4-0.20261019192617-18b64bffd9e7
	github.com/Nomadcxx/sysc-wayland v0.3.2
	golang.org/x/sys v0.47.0
)

require (
	github.com/Nomadcxx/sysc-terminal v0.2.0
	github.com/Nomadcxx/sysc-Go v1.0.7 // indirect
)

// hold: the version pin is settled across its five consumers (see sysc-1691)
require github.com/Nomadcxx/sysc-metrics v0.7.3

ignore ./docs
`

func TestParseGoMod(t *testing.T) {
	pins := ParseGoMod(goModSample)
	if len(pins) != 6 {
		t.Fatalf("ParseGoMod found %d pins, want 6", len(pins))
	}
	byModule := map[string]Pin{}
	for _, pin := range pins {
		byModule[pin.Module] = pin
	}
	if pin := byModule["github.com/Nomadcxx/sysc-Go"]; !pin.Indirect {
		t.Errorf("the sysc-Go requirement is not marked indirect")
	}
	if pin := byModule["github.com/Nomadcxx/sysc-metrics"]; pin.Hold == "" {
		t.Errorf("the requirement with a hold comment above it carries no reason")
	} else if !strings.Contains(pin.Hold, "sysc-1691") {
		t.Errorf("the hold reason does not name the issue: %q", pin.Hold)
	}
	if pin := byModule["github.com/Nomadcxx/sysc-wayland"]; pin.Hold != "" {
		t.Errorf("a requirement with no comment beside it carries a reason: %q", pin.Hold)
	}
	if pin := byModule["github.com/Nomadcxx/sysc-clipboard"]; !pin.Pseudo() {
		t.Errorf("the pseudo-version pin is not recognised as one")
	}
}

func TestParseGoModTrailingHold(t *testing.T) {
	content := "module example.com/x\n\nrequire example.com/y v1.0.0 // hold: pinned until the migration lands (see sysc-1733)\n"
	pins := ParseGoMod(content)
	if len(pins) != 1 {
		t.Fatalf("ParseGoMod found %d pins, want 1", len(pins))
	}
	if !strings.Contains(pins[0].Hold, "sysc-1733") {
		t.Errorf("the trailing hold reason was not read: %q", pins[0].Hold)
	}
}

func TestGoDirective(t *testing.T) {
	line, ok := GoDirectiveLine(goModSample)
	if !ok || line != "go 1.26.10" {
		t.Fatalf("GoDirectiveLine = %q, %v", line, ok)
	}
	comment := GoDirectiveComment(goModSample)
	if len(comment) != 2 {
		t.Fatalf("GoDirectiveComment found %d lines, want 2", len(comment))
	}
	if !strings.Contains(strings.Join(comment, "\n"), "GO-2026-6809") {
		t.Errorf("the comment above the directive is not the one that explains it: %q", comment)
	}
}

func TestIgnoreLines(t *testing.T) {
	got := IgnoreLines(goModSample)
	if len(got) != 1 || got[0] != "ignore ./docs" {
		t.Fatalf("IgnoreLines = %v, want [ignore ./docs]", got)
	}
}

func TestSkeleton(t *testing.T) {
	skeleton := Skeleton(goModSample)
	joined := strings.Join(skeleton, "\n")
	if strings.Contains(joined, "golang.org/x/sys v0.47.0") {
		t.Errorf("the skeleton still carries a require line")
	}
	if !strings.Contains(joined, "go 1.26.10") {
		t.Errorf("the skeleton lost the go directive")
	}
	if !strings.Contains(joined, "ignore ./docs") {
		t.Errorf("the skeleton lost the ignore directive")
	}
	if !strings.Contains(joined, "sysc-1691") {
		t.Errorf("the skeleton lost the comment above a requirement")
	}
}

func TestSetRequire(t *testing.T) {
	cases := []struct {
		name    string
		module  string
		version string
		want    string
		wantErr bool
	}{
		{name: "in a block", module: "github.com/Nomadcxx/sysc-wayland", version: "v0.3.3", want: "\tgithub.com/Nomadcxx/sysc-wayland v0.3.3\n"},
		{name: "keeps the indirect comment", module: "github.com/Nomadcxx/sysc-Go", version: "v1.0.10", want: "\tgithub.com/Nomadcxx/sysc-Go v1.0.10 // indirect\n"},
		{name: "single line", module: "github.com/Nomadcxx/sysc-metrics", version: "v0.7.4", want: "require github.com/Nomadcxx/sysc-metrics v0.7.4\n"},
		{name: "not required", module: "example.com/missing", version: "v1.0.0", wantErr: true},
	}
	for _, c := range cases {
		got, err := SetRequire(goModSample, c.module, c.version)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: SetRequire accepted a module the go.mod does not require", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: SetRequire failed: %v", c.name, err)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: SetRequire wrote %q, want it to hold %q", c.name, got, c.want)
		}
		if !strings.Contains(got, "go 1.26.10") {
			t.Errorf("%s: SetRequire changed the go directive", c.name)
		}
		if !strings.Contains(got, "ignore ./docs") {
			t.Errorf("%s: SetRequire dropped the ignore directive", c.name)
		}
	}
}

func TestHoldReason(t *testing.T) {
	cases := []struct {
		comment string
		want    bool
	}{
		{comment: "// hold: the floor here is the floor everywhere (see sysc-1731)", want: true},
		{comment: "// grouped pin bump (sysc-1733)", want: true},
		{comment: "// Recorder integration tests build the plugin from this pinned checkout.", want: false},
		{comment: "", want: false},
		{comment: "//", want: false},
	}
	for _, c := range cases {
		got := HoldReason(c.comment) != ""
		if got != c.want {
			t.Errorf("HoldReason(%q) reads a reason = %v, want %v", c.comment, got, c.want)
		}
	}
}
