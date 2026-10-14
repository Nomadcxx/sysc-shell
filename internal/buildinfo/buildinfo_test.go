package buildinfo

import "testing"

func TestLineRendersEveryBuildShape(t *testing.T) {
	origVersion, origCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = origVersion, origCommit })

	cases := []struct {
		name            string
		version, commit string
		want            string
	}{
		{"tagged release", "0.1.2", "1a2b3c4", "sysc-shell 0.1.2 (1a2b3c4)"},
		{"version only", "0.1.2", "unknown", "sysc-shell 0.1.2 (unknown)"},
		{"unstamped", "development", "unknown", "sysc-shell development (unknown)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Version, Commit = tc.version, tc.commit
			if got := Line(); got != tc.want {
				t.Fatalf("Line() = %q, want %q", got, tc.want)
			}
		})
	}
}
