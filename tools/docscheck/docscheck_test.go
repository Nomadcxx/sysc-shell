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