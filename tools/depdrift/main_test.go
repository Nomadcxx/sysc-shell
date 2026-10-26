package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFillModule(t *testing.T) {
	published := []string{"v0.2.0", "v0.3.0", "v0.4.0-rc.1"}
	cases := []struct {
		name       string
		pinned     string
		wantBehind int
		wantNote   string
		wantLatest string
	}{
		{name: "current", pinned: "v0.3.0", wantBehind: 0, wantLatest: "v0.3.0"},
		{name: "behind", pinned: "v0.2.0", wantBehind: 1, wantLatest: "v0.3.0"},
		{name: "a pseudo-version has no release target", pinned: "v0.0.0-20211118161319-6a13c67c3ce4", wantBehind: -1, wantNote: "no release target; cut a tag first", wantLatest: "v0.3.0"},
	}
	for _, c := range cases {
		row := moduleRow{Module: "example.com/x", Pinned: c.pinned}
		fillModule(&row, published)
		if row.Behind != c.wantBehind {
			t.Errorf("%s: Behind = %d, want %d", c.name, row.Behind, c.wantBehind)
		}
		if row.Note != c.wantNote {
			t.Errorf("%s: Note = %q, want %q", c.name, row.Note, c.wantNote)
		}
		if row.Latest != c.wantLatest {
			t.Errorf("%s: Latest = %q, want %q", c.name, row.Latest, c.wantLatest)
		}
	}
}

func TestFillModuleWithoutAPublishedVersion(t *testing.T) {
	row := moduleRow{Module: "example.com/x", Pinned: "v1.0.0"}
	fillModule(&row, nil)
	if row.Behind != -1 || row.Note != "no published version" {
		t.Fatalf("the row says %+v", row)
	}
}

func TestDedupeRefs(t *testing.T) {
	refs := []refRow{
		{Repository: "Nomadcxx/sysc-plugins", Pinned: "0cea6f74", Behind: 46},
		{Repository: "Nomadcxx/sysc-plugins", Pinned: "0cea6f74", Behind: 46, Hold: "a reason"},
		{Repository: "Nomadcxx/sysc-lock", Pinned: "df54457a", Behind: 11},
	}
	got := dedupeRefs(refs)
	if len(got) != 2 {
		t.Fatalf("dedupeRefs returned %d rows, want 2", len(got))
	}
	if got[0].Repository != "Nomadcxx/sysc-plugins" || got[0].Hold != "a reason" {
		t.Fatalf("the merged row is %+v", got[0])
	}
	if got[1].Repository != "Nomadcxx/sysc-lock" {
		t.Fatalf("the second row is %+v", got[1])
	}
}

func TestIsWorkflow(t *testing.T) {
	if !isWorkflow("ci.yml") || !isWorkflow("release.yaml") {
		t.Errorf("a workflow file was not recognised")
	}
	if isWorkflow("README.md") {
		t.Errorf("a document was read as a workflow")
	}
}

func TestRunRefusesAnArgumentItCannotRead(t *testing.T) {
	var out, errOut strings.Builder
	if code := run([]string{"-nonsense"}, &out, &errOut); code != 2 {
		t.Fatalf("run returned %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "-nonsense") {
		t.Fatalf("the error does not name the argument: %q", errOut.String())
	}
}

func TestRunRefusesANamelessBump(t *testing.T) {
	var out, errOut strings.Builder
	if code := run([]string{"-ref", "origin/main"}, &out, &errOut); code != 2 {
		t.Fatalf("run returned %d, want 2", code)
	}
}

func TestWriteSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "summary.md")
	if err := writeSummary(path, "# report\n"); err != nil {
		t.Fatalf("writeSummary failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the summary was not written: %v", err)
	}
	if string(data) != "# report\n" {
		t.Fatalf("the summary holds %q", data)
	}
}
