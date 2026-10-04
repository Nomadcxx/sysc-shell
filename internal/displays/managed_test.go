package displays

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ptrScale(f float64) *float64 { return &f }

// BuildManaged must clone the whole effective block and touch nothing but the
// five fields it controls. A dropped layout or hot-corners setting would
// silently change the user's desktop the moment a preview is applied.
func TestBuildManagedPreservesTheWholeBlock(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "output \"DP-1\" {\n    scale 1\n    transform \"Normal\"\n    mode \"1920x1080@60.000\" custom=false\n    layout {\n        gaps 4\n        center-focused-column \"never\"\n    }\n    hot-corners {\n        top-left\n        bottom-right\n    }\n    backdrop-color \"#00000099\"\n    focus-at-startup\n}\n",
	})
	root := filepath.Join(dir, "config.kdl")
	g, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{
		"DP-1": {Scale: ptrScale(1.5)},
	})
	if err != nil {
		t.Fatalf("BuildManaged: %v", err)
	}
	got := string(candidate)

	for _, want := range []string{
		"gaps 4",
		`center-focused-column "never"`,
		"top-left",
		"bottom-right",
		`backdrop-color "#00000099"`,
		"focus-at-startup",
		"scale 1.5",
		`output "DP-1"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("candidate is missing %q:\n%s", want, got)
		}
	}
	// The scale must be a bare number: niri reads a quoted one as a string.
	if strings.Contains(got, `scale "1.5"`) {
		t.Errorf("scale was quoted, which niri rejects:\n%s", got)
	}
	if strings.Contains(got, "scale 1\n") {
		t.Errorf("the old scale survived:\n%s", got)
	}
	// A nil field means automatic, so the child's absence is the claim.
	if strings.Contains(got, "transform") {
		t.Errorf("an unset transform was written out:\n%s", got)
	}
	if !strings.HasPrefix(got, "// "+ManagedFileName) {
		t.Error("the generated file carries no header saying the shell wrote it")
	}
}

// Rewriting a field in place keeps the block readable, because the shell
// replaces a child rather than appending a second copy of it.
func TestBuildManagedReplacesInPlace(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "output \"DP-1\" {\n    scale 1\n    layout {\n        gaps 4\n    }\n    mode \"1920x1080@60.000\"\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{
		"DP-1": {Scale: ptrScale(2), Mode: &ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 60030}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(candidate)
	if n := strings.Count(got, "scale "); n != 1 {
		t.Errorf("%d scale children, want exactly one:\n%s", n, got)
	}
	if n := strings.Count(got, "mode "); n != 1 {
		t.Errorf("%d mode children, want exactly one:\n%s", n, got)
	}
	if strings.Contains(got, "custom=true") {
		t.Errorf("custom was invented for a plain mode:\n%s", got)
	}
	if !strings.Contains(got, `mode "1920x1080@60.030"`) {
		t.Errorf("mode was not rewritten in millihertz form:\n%s", got)
	}
}

func TestBuildManagedNamesTheExactConnector(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "output \"DP-1\" {\n    scale 1\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatal(err)
	}
	// An output with no block of its own still gets a node, named by its
	// connector, so the file never claims an output by make and model.
	candidate, err := BuildManaged(g, []string{"HDMI-A-1"}, map[string]Policy{
		"HDMI-A-1": {Scale: ptrScale(1.25)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(candidate)
	if !strings.Contains(got, `output "HDMI-A-1"`) {
		t.Fatalf("no node for the unmanaged output:\n%s", got)
	}
	if strings.Contains(got, "Unknown") {
		t.Errorf("the node was named by make and model instead of the connector:\n%s", got)
	}
}

func TestBuildManagedValidatesBeforeWriting(t *testing.T) {
	dir := writeTree(t, map[string]string{"config.kdl": "output \"DP-1\" {\n    scale 1\n}\n"})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatal(err)
	}
	bad := 99.0
	if _, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{"DP-1": {Scale: &bad}}); err == nil {
		t.Fatal("BuildManaged wrote a scale niri refuses")
	}
	if _, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{
		"DP-1": {Transform: ptrString("45")},
	}); err == nil {
		t.Fatal("BuildManaged accepted a transform niri does not have")
	}
	if _, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{
		"DP-1": {Mode: &ModePolicy{Width: 1920, Height: 1080}},
	}); err == nil {
		t.Fatal("BuildManaged wrote a mode with no refresh rate")
	}
}

func TestParseTransformNative(t *testing.T) {
	for _, s := range Transforms {
		if err := ParseTransformNative(s); err != nil {
			t.Errorf("ParseTransformNative(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "45", "Flip", "flipped180"} {
		if err := ParseTransformNative(s); err == nil {
			t.Errorf("ParseTransformNative(%q) = nil, want an error", s)
		}
	}
}

func TestStageWrapperOrdersCandidateBeforeRoot(t *testing.T) {
	dir := writeTree(t, map[string]string{"config.kdl": "output \"DP-1\" {\n    scale 1\n}\n"})
	root := filepath.Join(dir, "config.kdl")
	candidate := []byte("output \"DP-1\" {\n    scale 1.5\n}\n")

	wrapper, cleanup, err := StageWrapper(dir, candidate, root)
	if err != nil {
		t.Fatalf("StageWrapper: %v", err)
	}
	defer cleanup()
	body, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	// The candidate must come first, or the user's own block would win and
	// the staged graph would not be the one that is about to be published.
	if strings.Index(got, "include "+filepath.Join(dir, ManagedFileName)) > strings.Index(got, "include "+root) {
		t.Fatalf("wrapper includes the candidate after the root:\n%s", got)
	}
	if !strings.Contains(got, root) || !strings.Contains(got, ManagedFileName) {
		t.Fatalf("wrapper does not mention both files:\n%s", got)
	}

	// The staged candidate is the bytes that were handed in, unmodified.
	staged, err := os.ReadFile(filepath.Join(dir, ManagedFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(staged, candidate) {
		t.Fatalf("staged candidate = %q, want %q", staged, candidate)
	}

	if _, _, err := StageWrapper(dir, candidate, "relative/config.kdl"); err == nil {
		t.Fatal("StageWrapper accepted a relative root; a relative include would resolve elsewhere")
	}
}

func TestStageWrapperRefusals(t *testing.T) {
	dir := writeTree(t, map[string]string{"config.kdl": "output \"DP-1\" {\n}\n"})
	root := filepath.Join(dir, "config.kdl")
	if _, _, err := StageWrapper(dir, nil, filepath.Join(dir, "absent.kdl")); err == nil {
		t.Fatal("StageWrapper accepted a root that does not exist")
	}
	if _, _, err := StageWrapper(filepath.Join(dir, "no-such-dir"), []byte("x"), root); err == nil {
		t.Fatal("StageWrapper accepted a directory that does not exist")
	}
}

func TestStageAndValidateUsesTheSuppliedValidator(t *testing.T) {
	dir := writeTree(t, map[string]string{"config.kdl": "output \"DP-1\" {\n    scale 1\n}\n"})
	root := filepath.Join(dir, "config.kdl")
	var seen string
	validate := func(_ context.Context, wrapper string) error {
		seen = wrapper
		return nil
	}
	candidate := []byte("output \"DP-1\" {\n    scale 1.5\n}\n")
	if err := StageAndValidate(context.Background(), validate, t.TempDir(), candidate, root); err != nil {
		t.Fatalf("StageAndValidate: %v", err)
	}
	if seen == "" {
		t.Fatal("the validator was not called with a wrapper path")
	}
	// The staged directory must not survive the call.
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Errorf("staged wrapper %s outlived the call", seen)
	}

	boom := errors.New("niri rejected it")
	if err := StageAndValidate(context.Background(), func(context.Context, string) error { return boom }, t.TempDir(), candidate, root); !errors.Is(err, boom) {
		t.Fatalf("StageAndValidate = %v, want the validator's error", err)
	}
}

// The reader has to agree with niri on the files the shell writes. This is the
// check the plan requires before the reader is depended on: the generated
// managed file and a wrapper including a real user config are handed to the
// niri binary and niri itself decides.
func TestNiriValidateAcceptsWhatTheShellGenerates(t *testing.T) {
	validate, err := NiriValidator()
	if err != nil {
		t.Skipf("no niri binary to check against: %v", err)
	}
	dir := writeTree(t, map[string]string{
		"config.kdl": "output \"DP-1\" {\n    scale 1\n    layout {\n        gaps 4\n    }\n    hot-corners {\n        top-left\n    }\n    focus-at-startup\n}\n",
		"extra.kdl":  "output \"HDMI-A-1\" {\n    scale 2\n    transform \"90\"\n}\n",
	})
	root := filepath.Join(dir, "config.kdl")
	if err := os.WriteFile(root, []byte("include \"./extra.kdl\"\n"+mustRead(t, root)), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildManaged(g, []string{"DP-1", "HDMI-A-1"}, map[string]Policy{
		"DP-1":     {Scale: ptrScale(1.5), Position: &Position{X: 0, Y: 0}, VRR: &VRRPolicy{OnDemand: true}},
		"HDMI-A-1": {Scale: ptrScale(1.25), Transform: ptrString("90"), Position: &Position{X: 2560, Y: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := StageAndValidate(context.Background(), validate, t.TempDir(), candidate, root); err != nil {
		t.Fatalf("niri refused the generated file:\n%s\n---\n%s", err, candidate)
	}
}

// The same oracle, pointed at a candidate that violates the schema, so the
// test above cannot pass for the wrong reason.
func TestNiriValidateRejectsAGeneratedScaleNiriRefuses(t *testing.T) {
	validate, err := NiriValidator()
	if err != nil {
		t.Skipf("no niri binary to check against: %v", err)
	}
	dir := writeTree(t, map[string]string{"config.kdl": "output \"DP-1\" {\n    scale 1\n}\n"})
	root := filepath.Join(dir, "config.kdl")
	// A quoted scale is what the reader produced when the value was handed
	// over wrongly. niri reads it as a string and must refuse it.
	quoted := "output \"DP-1\" {\n    scale \"1.5\"\n}\n"
	if err := StageAndValidate(context.Background(), validate, t.TempDir(), []byte(quoted), root); err == nil {
		t.Fatal("niri accepted a quoted scale; the oracle proves nothing then")
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func ptrString(s string) *string { return &s }
