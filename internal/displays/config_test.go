package displays

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sblinch/kdl-go/document"
)

// writeTree lays a map of relative paths to contents under a fresh directory.
// Keys ending in "/" are created as empty directories.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func graphNames(g *Graph) []string {
	names := make([]string, 0, len(g.Outputs))
	for _, o := range g.Outputs {
		names = append(names, o.Name)
	}
	return names
}

// niri keeps the first matching output block whole, so the shell has to see
// the same order or it will clone the wrong block.
func TestLoadGraphSplicesIncludesInPlace(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "output \"DP-1\" {\n    scale 1\n}\ninclude \"./extra.kdl\"\noutput \"HDMI-A-1\" {\n    scale 2\n}\n",
		"extra.kdl":  "output \"DP-1\" {\n    scale 9\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	want := []string{"DP-1", "DP-1", "HDMI-A-1"}
	if got := graphNames(g); !equalStrings(got, want) {
		t.Fatalf("output order = %v, want %v", got, want)
	}
	if len(g.Files) != 2 {
		t.Fatalf("graph recorded %d files, want 2 (root and the include)", len(g.Files))
	}
}

// First match wins as a whole block: the shadowed block's scale is 9, and
// Find must never merge the two into one answer.
func TestLoadGraphFirstMatchWins(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "include \"./extra.kdl\"\noutput \"DP-1\" {\n    scale 2\n}\n",
		"extra.kdl":  "output \"DP-1\" {\n    scale 1\n    transform \"90\"\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	node, ok := g.Find(Name{Connector: "DP-1"})
	if !ok {
		t.Fatal("Find did not match the connector")
	}
	if got := argText(t, node, "scale"); got != "1" {
		t.Errorf("winning block scale = %q, want 1 from the include (first match)", got)
	}
	if childNames(node.Node)[0] != "scale" {
		t.Errorf("winning block kept %v, want it to start at scale", childNames(node.Node))
	}
	if len(childNames(node.Node)) != 2 {
		t.Errorf("winning block has children %v, want exactly 2: niri does not merge blocks", childNames(node.Node))
	}
}

func TestLoadGraphResolvesHomeAndAbsoluteIncludes(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl":  "include \"" + "sub/abs.kdl" + "\"\n",
		"sub/abs.kdl": "output \"DP-1\" {\n    scale 1\n}\n",
	})
	// An absolute include is used verbatim.
	root := writeTree(t, map[string]string{
		"config.kdl": "include \"" + filepath.Join(dir, "sub/abs.kdl") + "\"\n",
	})
	g, err := LoadGraph(filepath.Join(root, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if got := graphNames(g); !equalStrings(got, []string{"DP-1"}) {
		t.Fatalf("outputs = %v, want DP-1 from the absolute include", got)
	}
}

// A `~` include must expand against the user's home directory, because that is
// what niri does with the same string.
func TestLoadGraphExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "cfg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "cfg/home.kdl"), []byte("output \"DP-1\" {\n    scale 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := writeTree(t, map[string]string{"config.kdl": "include \"~/cfg/home.kdl\"\n"})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if got := graphNames(g); !equalStrings(got, []string{"DP-1"}) {
		t.Fatalf("outputs = %v, want the output from the ~ include", got)
	}
}

func TestLoadGraphIncludeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			"a missing required include",
			map[string]string{"config.kdl": "include \"./absent.kdl\"\n"},
			"absent.kdl",
		},
		{
			"an include with two paths",
			map[string]string{"config.kdl": "include \"./a.kdl\" \"./b.kdl\"\n", "a.kdl": "", "b.kdl": ""},
			"one path",
		},
		{
			"an include with an unknown property",
			map[string]string{"config.kdl": "include \"./a.kdl\" optional=false watch=true\n", "a.kdl": ""},
			"watch",
		},
		{
			"an include with children",
			map[string]string{"config.kdl": "include \"./a.kdl\" {\n    nested\n}\n", "a.kdl": ""},
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, tc.files)
			_, err := LoadGraph(filepath.Join(dir, "config.kdl"))
			if err == nil {
				t.Fatal("LoadGraph accepted a config niri rejects")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A missing optional include is a warning in niri, and it must stay a warning
// here, or the shell would refuse to work with a user's untouched config.
func TestLoadGraphMissingOptionalIncludeIsNotAnError(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "include \"./absent.kdl\" optional=true\noutput \"DP-1\" {\n    scale 1\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	var missing int
	for _, f := range g.Files {
		if f.Missing {
			missing++
		}
	}
	if missing != 1 {
		t.Fatalf("graph recorded %d missing files, want 1", missing)
	}
	if got := graphNames(g); !equalStrings(got, []string{"DP-1"}) {
		t.Fatalf("outputs = %v, want DP-1", got)
	}
}

func TestLoadGraphRejectsCyclesAndDepth(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		dir := writeTree(t, map[string]string{
			"config.kdl": "include \"./a.kdl\"\n",
			"a.kdl":      "include \"./config.kdl\"\n",
		})
		if _, err := LoadGraph(filepath.Join(dir, "config.kdl")); err == nil ||
			!strings.Contains(err.Error(), "recursive") {
			t.Fatalf("LoadGraph = %v, want a recursive-include error", err)
		}
	})

	t.Run("depth", func(t *testing.T) {
		files := map[string]string{"config.kdl": "include \"./f1.kdl\"\n"}
		for i := 1; i <= maxIncludeDepth+1; i++ {
			files["f"+strconv.Itoa(i)+".kdl"] = "include \"./f" + strconv.Itoa(i+1) + ".kdl\"\n"
		}
		files["f"+strconv.Itoa(maxIncludeDepth+2)+".kdl"] = "output \"DP-1\" {\n    scale 1\n}\n"
		dir := writeTree(t, files)
		if _, err := LoadGraph(filepath.Join(dir, "config.kdl")); err == nil {
			t.Fatal("LoadGraph accepted an include chain deeper than niri allows")
		}
	})
}

func TestLoadGraphResourceLimits(t *testing.T) {
	t.Run("file too large", func(t *testing.T) {
		big := strings.Repeat("// filler line to grow the file\n", maxFileBytes/23+16)
		dir := writeTree(t, map[string]string{"config.kdl": big})
		if _, err := LoadGraph(filepath.Join(dir, "config.kdl")); !isResourceLimit(err) {
			t.Fatalf("LoadGraph = %v, want a resource-limit error", err)
		}
	})

	t.Run("too many files", func(t *testing.T) {
		files := map[string]string{"config.kdl": ""}
		for i := 0; i <= maxGraphFiles; i++ {
			files["f"+strconv.Itoa(i)+".kdl"] = ""
			files["config.kdl"] += "include \"./f" + strconv.Itoa(i) + ".kdl\"\n"
		}
		dir := writeTree(t, files)
		if _, err := LoadGraph(filepath.Join(dir, "config.kdl")); !isResourceLimit(err) {
			t.Fatalf("LoadGraph = %v, want a resource-limit error", err)
		}
	})
}

// A root reached through a symlink is the file niri reads, and the graph must
// report the resolved path so ownership and fingerprinting agree with the
// file's identity rather than the link's.
func TestLoadGraphResolvesASymlinkedRoot(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"real/config.kdl": "output \"DP-1\" {\n    scale 1\n}\n",
	})
	link := filepath.Join(dir, "link.kdl")
	if err := os.Symlink(filepath.Join(dir, "real/config.kdl"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	g, err := LoadGraph(link)
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if g.Root.Resolved != filepath.Join(dir, "real/config.kdl") {
		t.Fatalf("root resolved to %q, want the symlink target", g.Root.Resolved)
	}
}

func TestNameMatches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		query  Name
		config string
		want   bool
	}{
		{"exact connector", Name{Connector: "DP-1"}, "DP-1", true},
		{"connector case-insensitive", Name{Connector: "DP-1"}, "dp-1", true},
		{"a different connector", Name{Connector: "DP-1"}, "HDMI-A-1", false},
		{"a make model serial triple", Name{Connector: "DP-1", Make: "Dell Inc.", Model: "DELL U2720Q", Serial: "ABC"}, "Dell Inc. DELL U2720Q ABC", true},
		{"the triple is case-insensitive", Name{Connector: "DP-1", Make: "Dell Inc.", Model: "DELL U2720Q", Serial: "ABC"}, "dell inc. dell u2720q abc", true},
		{"a triple with the wrong serial", Name{Connector: "DP-1", Make: "Dell Inc.", Model: "DELL U2720Q", Serial: "ABC"}, "Dell Inc. DELL U2720Q XYZ", false},
		{"a triple with the wrong model", Name{Connector: "DP-1", Make: "Dell Inc.", Model: "DELL U2720Q"}, "Acme 999 XYZ", false},
		{"no identity at all never matches", Name{Connector: "DP-1"}, "Unknown Unknown Unknown", false},
		{"the prefix has to be the whole make and model", Name{Connector: "DP-1", Make: "Dell", Model: "U2720Q"}, "Dell Inc. U2720Q ABC", false},
		{"a doubled space is not a separator", Name{Connector: "DP-1", Make: "Dell", Model: "U2720Q", Serial: "ABC"}, "Dell  U2720Q ABC", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.query.Matches(tc.config); got != tc.want {
				t.Fatalf("Matches(%q) = %v, want %v", tc.config, got, tc.want)
			}
		})
	}
}

// The reader drops knuffel's inactive `/-` forms. The graph must not pretend
// otherwise, so this test pins the limitation instead of hiding it.
func TestLoadGraphDropsInactiveNodes(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "/-output \"DP-1\" {\n    scale 1\n}\noutput \"DP-1\" {\n    scale 2\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if got := graphNames(g); !equalStrings(got, []string{"DP-1"}) {
		t.Fatalf("outputs = %v, want only the active block: the reader drops /- nodes", got)
	}
}

func TestUserFingerprintIgnoresOnlyTheManagedFile(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "include \"./extra.kdl\"\ninclude \"./" + ManagedFileName + "\"\n",
		"extra.kdl":  "output \"DP-1\" {\n    scale 1\n}\n",
	})
	root := filepath.Join(dir, "config.kdl")
	managed := filepath.Join(dir, ManagedFileName)
	if err := os.WriteFile(managed, []byte("output \"DP-1\" {\n    scale 2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.UserFingerprint(managed) == "" {
		t.Fatal("UserFingerprint returned nothing")
	}

	// Rewriting the managed file is the shell's own job and must not look
	// like a user edit.
	if err := os.WriteFile(managed, []byte("output \"DP-1\" {\n    scale 3\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterManaged, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.UserFingerprint(managed) != afterManaged.UserFingerprint(managed) {
		t.Fatal("rewriting the managed file changed the user fingerprint")
	}
	if before.Fingerprint() == afterManaged.Fingerprint() {
		t.Fatal("the full fingerprint ignored a real change to the managed file")
	}

	// Editing any other file the user's config depends on must show up.
	if err := os.WriteFile(filepath.Join(dir, "extra.kdl"), []byte("output \"DP-1\" {\n    scale 4\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterUser, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterManaged.UserFingerprint(managed) == afterUser.UserFingerprint(managed) {
		t.Fatal("editing an included file did not change the user fingerprint")
	}
}

// A user file carrying comments, raw strings and braces inside them must parse
// into the same block niri would see, so the clone is not silently reshaped.
func TestLoadGraphHandlesCommentsAndRawStrings(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "/* a block comment with a brace } in it */\noutput \"DP-1\" {\n    // scale 3 is the one I wanted\n    scale 1\n    modeline \"pulseaudio-output-default-lfe-on\"\n    backdrop-color \"#00000099\"\n}\n",
	})
	g, err := LoadGraph(filepath.Join(dir, "config.kdl"))
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if got := graphNames(g); !equalStrings(got, []string{"DP-1"}) {
		t.Fatalf("outputs = %v, want DP-1", got)
	}
	node, _ := g.Find(Name{Connector: "DP-1"})
	if got := argText(t, node, "scale"); got != "1" {
		t.Errorf("scale = %q, want 1: the commented-out scale must not win", got)
	}
}

// valueText renders an argument the way niri would see it. text() only handles
// strings, so a bare number needs the general path.
func valueText(v *document.Value) string {
	if s, ok := v.Value.(string); ok {
		return s
	}
	return fmt.Sprint(v.Value)
}

// childNames lists an output block's child node names in document order.
func childNames(node *document.Node) []string {
	names := make([]string, 0, len(node.Children))
	for _, c := range node.Children {
		names = append(names, c.Name.Value.(string))
	}
	return names
}

// argText returns a child node's first argument as plain text. Value.String
// would re-quote it, which is why this reads the payload directly.
func argText(t *testing.T, out OutputNode, name string) string {
	t.Helper()
	for _, c := range out.Node.Children {
		if c.Name.Value.(string) == name {
			if len(c.Arguments) == 0 {
				return ""
			}
			return valueText(c.Arguments[0])
		}
	}
	return ""
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isResourceLimit(err error) bool {
	return err != nil && strings.Contains(err.Error(), errResourceLimit.Error())
}
