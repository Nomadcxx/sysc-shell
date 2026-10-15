package main

import (
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
		{"testdata/bad/kdl-unknown-method.md", []string{":4:"}},
		{"testdata/bad/kdl-unknown-panel.md", []string{":4:"}},
		{"testdata/bad/shell-unknown-method.md", []string{":4:"}},
		{"testdata/bad/shell-unknown-panel.md", []string{":4:"}},
		{"testdata/bad/kdl-bad-params.md", []string{":4:"}},
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
