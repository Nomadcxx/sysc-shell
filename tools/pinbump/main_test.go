package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReadBumpFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bump.txt")
	content := "# a comment\n\ngithub.com/Nomadcxx/sysc-terminal=v0.3.0\ngithub.com/Nomadcxx/sysc-Go=v1.0.10\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("the bump file was not written: %v", err)
	}
	bumps, err := readBumpFile(path)
	if err != nil {
		t.Fatalf("readBumpFile failed: %v", err)
	}
	if len(bumps) != 2 {
		t.Fatalf("readBumpFile read %d bumps, want 2", len(bumps))
	}
	if bumps[0].Module != "github.com/Nomadcxx/sysc-terminal" || bumps[0].Version != "v0.3.0" {
		t.Fatalf("the first bump is %+v", bumps[0])
	}
}

func TestReadBumpFileRefusesALineItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bump.txt")
	if err := os.WriteFile(path, []byte("no-equals-sign\n"), 0o644); err != nil {
		t.Fatalf("the bump file was not written: %v", err)
	}
	if _, err := readBumpFile(path); err == nil {
		t.Fatalf("readBumpFile accepted a line it cannot read")
	}
}

func TestPlanSkipsHeldAndPseudoPins(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/!nomadcxx/sysc-terminal/@v/list":
			fmt.Fprint(w, "v0.2.0\nv0.3.0\n")
		case "/github.com/!nomadcxx/sysc-wayland/@v/list":
			fmt.Fprint(w, "v0.3.2\n")
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer proxy.Close()

	original := `module example.com/x

require (
	github.com/Nomadcxx/sysc-terminal v0.2.0
	// hold: the bump waits for the grouped move (see sysc-1733)
	github.com/Nomadcxx/sysc-wayland v0.3.2
	github.com/Nomadcxx/sysc-clipboard v0.1.4-0.20261019192617-18b64bffd9e7
)
`
	bumps, err := plan(context.Background(), config{prefix: "github.com/Nomadcxx/sysc", proxy: proxy.URL}, original)
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(bumps) != 1 {
		t.Fatalf("plan returned %d bumps, want 1: %v", len(bumps), bumps)
	}
	if bumps[0].Module != "github.com/Nomadcxx/sysc-terminal" || bumps[0].Version != "v0.3.0" {
		t.Fatalf("the bump is %+v", bumps[0])
	}
}

func TestVerifyBuildList(t *testing.T) {
	before := map[string]string{"example.com/a": "v1.0.0", "example.com/b": "v1.0.0"}
	cases := []struct {
		name    string
		after   map[string]string
		bumps   []Bump
		wantErr bool
	}{
		{
			name:  "nothing moved",
			after: map[string]string{"example.com/a": "v1.0.0", "example.com/b": "v1.0.0"},
		},
		{
			name:  "the bump moved the module it names",
			after: map[string]string{"example.com/a": "v1.1.0", "example.com/b": "v1.0.0"},
			bumps: []Bump{{Module: "example.com/a", Version: "v1.1.0"}},
		},
		{
			name:    "another module moved",
			after:   map[string]string{"example.com/a": "v1.1.0", "example.com/b": "v1.0.0"},
			bumps:   []Bump{{Module: "example.com/b", Version: "v1.0.0"}},
			wantErr: true,
		},
		{
			name:    "a module was pulled in",
			after:   map[string]string{"example.com/a": "v1.0.0", "example.com/b": "v1.0.0", "example.com/c": "v1.0.0"},
			wantErr: true,
		},
		{
			name:    "a module was dropped",
			after:   map[string]string{"example.com/a": "v1.0.0"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		err := verifyBuildList(before, c.after, c.bumps)
		if c.wantErr && err == nil {
			t.Errorf("%s: verifyBuildList accepted a build list that moved", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: verifyBuildList refused a sound build list: %v", c.name, err)
		}
	}
}
