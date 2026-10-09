package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (stdout []byte, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = run(args, strings.NewReader(stdin), &out, &errOut)
	return out.Bytes(), err
}

func TestRunWritesAPNGToTheNamedFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "panel.png")
	if _, err := runCLI(t, sampleTree, "-width", "360", "-height", "240", "-o", out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("output is not a PNG: %v", err)
	}
	if cfg.Width != 540 || cfg.Height != 360 {
		t.Fatalf("PNG is %dx%d, want 540x360", cfg.Width, cfg.Height)
	}
}

func TestRunWritesToStandardOutputWithoutO(t *testing.T) {
	out, err := runCLI(t, sampleTree, "-width", "200", "-height", "120", "-scale", "120")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("stdout does not start with the PNG signature: %q", out[:min(8, len(out))])
	}
}

func TestRunReadsATreeFromAFile(t *testing.T) {
	tree := filepath.Join(t.TempDir(), "tree.json")
	if err := os.WriteFile(tree, []byte(sampleTree), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "", "-width", "200", "-height", "120", tree); err != nil {
		t.Fatal(err)
	}
}

func TestRunIsDeterministic(t *testing.T) {
	a, err := runCLI(t, sampleTree, "-width", "300", "-height", "200")
	if err != nil {
		t.Fatal(err)
	}
	b, err := runCLI(t, sampleTree, "-width", "300", "-height", "200")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two runs of one tree differ")
	}
}

func TestRunLeavesNoFileWhenTheRenderFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "panel.png")
	// A text root is not a valid panel, so the render fails.
	if _, err := runCLI(t, `{"kind":"text","text":"x"}`, "-width", "200", "-height", "100", "-o", out); err == nil {
		t.Fatal("an invalid tree rendered")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("a failed render left %s behind (stat err = %v)", out, err)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	size := []string{"-width", "200", "-height", "100"}
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no size", sampleTree, nil, "outside 1.."},
		{"not json", "this is not json", size, "tree"},
		{"unknown field", `{"kind":"column","bogus":1}`, size, "tree"},
		{"trailing data", sampleTree + ` {"kind":"column"}`, size, "after the tree"},
		{"two files", sampleTree, append(append([]string{}, size...), "a.json", "b.json"), "at most one"},
		{"missing file", "", append(append([]string{}, size...), "/nonexistent/tree.json"), "no such file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runCLI(t, tc.stdin, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
			if len(out) != 0 {
				t.Errorf("wrote %d bytes of output despite the error", len(out))
			}
		})
	}
}
