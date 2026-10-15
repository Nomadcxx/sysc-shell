package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunConfigCheckUsageRefusals(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"too many paths", []string{"a.json", "b.json"}, 2, "usage: sysc-shell config check [path]"},
		{"flag is refused", []string{"--json"}, 2, "usage: sysc-shell config check [path]"},
		{"directory", []string{"."}, 2, "config: read ."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := runConfigCheck(tc.args, &out, &errOut)
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tc.wantCode, errOut.String())
			}
			if !strings.Contains(errOut.String(), tc.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", errOut.String(), tc.wantErr)
			}
			if out.Len() != 0 {
				t.Fatalf("stdout = %q, want empty on refusal", out.String())
			}
		})
	}
}