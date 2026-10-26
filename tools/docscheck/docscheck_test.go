package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONExamplesAreChecked(t *testing.T) {
	cases := []struct {
		file      string
		wantLines []string // empty means the document passes
	}{
		{"testdata/good/config-object.md", nil},
		{"testdata/good/item-array.md", nil},
		{"testdata/good/skipped-tag.md", nil},
		{"testdata/bad/config-unknown-field.md", []string{":4:"}},
		{"testdata/bad/item-unknown-id.md", []string{":4:"}},
		{"testdata/bad/item-missing-id.md", []string{":4:"}},
		{"testdata/bad/item-unknown-panel.md", []string{":4:"}},
		{"testdata/bad/scalar-block.md", []string{":4:"}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			findings := checkFile(tc.file)
			if len(findings) != len(tc.wantLines) {
				t.Fatalf("findings = %v, want %d", findings, len(tc.wantLines))
			}
			for i, want := range tc.wantLines {
				if !strings.Contains(findings[i], want) {
					t.Fatalf("finding %d = %q, want it to contain %q", i, findings[i], want)
				}
			}
		})
	}
}

func TestIPCExamplesAreChecked(t *testing.T) {
	cases := []struct {
		file      string
		wantLines []string
	}{
		{"testdata/good/kdl-binds.md", nil},
		{"testdata/good/shell-commands.md", nil},
		{"testdata/good/kdl-unknown-escape.md", nil},
		{"testdata/bad/kdl-unknown-escape-bad-method.md", []string{":4:"}},
		{"testdata/bad/kdl-unknown-method.md", []string{":4:"}},
		{"testdata/bad/kdl-unknown-panel.md", []string{":4:"}},
		{"testdata/bad/shell-unknown-method.md", []string{":4:"}},
		{"testdata/bad/shell-unknown-panel.md", []string{":4:"}},
		{"testdata/bad/kdl-bad-params.md", []string{":4:"}},
		{"testdata/good/shell-subscribe.md", nil},
		{"testdata/bad/shell-subscribe-unknown-topic.md", []string{":4:"}},
		{"testdata/bad/shell-subscribe-bad-seconds.md", []string{":4:"}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			findings := checkFile(tc.file)
			if len(findings) != len(tc.wantLines) {
				t.Fatalf("findings = %v, want %d", findings, len(tc.wantLines))
			}
			for i, want := range tc.wantLines {
				if !strings.Contains(findings[i], want) {
					t.Fatalf("finding %d = %q, want it to contain %q", i, findings[i], want)
				}
			}
		})
	}
}

func TestDefaultInputListAndExitCodes(t *testing.T) {
	// The tool is invoked from the repository root, so the contract is the
	// document set as seen from there.
	t.Chdir(filepath.Join("..", ".."))
	paths := defaultPaths()
	want := []string{"README.md", "AGENTS.md", "CONTRIBUTING.md"}
	matches, err := filepath.Glob(filepath.Join("docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		want = append(want, filepath.Join("docs", filepath.Base(m)))
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestRunExitCodes(t *testing.T) {
	if code := run([]string{"testdata/nonexistent.md"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("missing file exit = %d, want 2", code)
	}
	if code := run([]string{"testdata/good/item-array.md"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("good document exit = %d, want 0", code)
	}
	if code := run([]string{"testdata/bad/scalar-block.md"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("bad document exit = %d, want 1", code)
	}
}

func TestRunPrintsSummaryAndFindingPrefix(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"testdata/good/item-array.md"}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	if got, want := out.String(), "checked 1 blocks in 1 files\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"testdata/bad/scalar-block.md"}, &out, &errOut); code != 1 {
		t.Fatalf("code = %d", code)
	}
	if want := "docs/examples: testdata/bad/scalar-block.md:4: "; !strings.HasPrefix(errOut.String(), want) {
		t.Fatalf("stderr = %q, want prefix %q", errOut.String(), want)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty when there are findings", out.String())
	}
}

func TestRunListsSkippedBlocksWhenAsked(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-v", "testdata/good/skipped-tag.md"}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "skipped-tag.md:4") || !strings.Contains(out.String(), "tag text") {
		t.Fatalf("stdout = %q, want the skipped block and its reason", out.String())
	}
}
