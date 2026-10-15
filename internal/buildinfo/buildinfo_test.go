package buildinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestReleaseWorkflowStampsTheStampOwner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, want := range []string{
		"-X github.com/Nomadcxx/sysc-shell/internal/buildinfo.Version=",
		"-X github.com/Nomadcxx/sysc-shell/internal/buildinfo.Commit=",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release.yml does not stamp %q", want)
		}
	}
}
