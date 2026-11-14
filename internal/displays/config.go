// Package displays owns Niri display configuration: the native KDL graph the
// compositor reads, the single file the shell is allowed to write, and the
// bytes it publishes there. Nothing outside this package may rewrite those
// files, and the shell JSON configuration stores at most the path to the root
// it manages — never a mode, scale or VRR policy.
package displays

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"strings"

	kdl "github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// ManagedFileName is the one file this shell owns. It lives beside the
// compositor's root so the root's relative include bases stay intact, and it
// holds complete native output blocks rather than a subset of fields.
const ManagedFileName = "sysc-shell-displays.kdl"

// includeLinePrefix marks the owned include. A comment marks it in the
// bootstrap stage, when the file is deliberately empty, so an interrupted
// first run cannot change output geometry.
const includeLinePrefix = "include "

// Resource bounds. Niri itself has no size limits, so these are the product's
// own limits; a graph that is native-valid but exceeds them is refused with an
// explanation rather than read into memory.
const (
	maxFileBytes    = 1 << 20
	maxGraphBytes   = 8 << 20
	maxGraphFiles   = 128
	maxIncludeDepth = 10
)

// includeDepthError marks the depth refusal so callers can tell the product's
// bound from a native parse failure.
var errResourceLimit = errors.New("displays: configuration graph exceeds the shell's resource limit")

// Name identifies an output for matching, mirroring niri's own OutputName. A
// missing make, model or serial is absent, not empty, because niri treats
// "Unknown" as a real comparison value and an empty string as no value at all.
type Name struct {
	Connector string
	Make      string
	Model     string
	Serial    string
}

// Matches implements niri's OutputName::matches against one configuration node
// name. Connector comparison is case-insensitive and tried first. Otherwise a
// query with no make, model or serial cannot match anything, and a query with
// some of them is rendered as "make model serial" with Unknown for the parts it
// lacks and compared segment by segment against single spaces.
func (n Name) Matches(configName string) bool {
	if strings.EqualFold(configName, n.Connector) {
		return true
	}
	if n.Make == "" && n.Model == "" && n.Serial == "" {
		return false
	}
	make, model, serial := "Unknown", "Unknown", "Unknown"
	if n.Make != "" {
		make = n.Make
	}
	if n.Model != "" {
		model = n.Model
	}
	if n.Serial != "" {
		serial = n.Serial
	}
	rest, ok := cutPrefixFold(configName, make)
	if !ok {
		return false
	}
	rest, ok = cutPrefixFold(rest, " "+model+" ")
	if !ok {
		return false
	}
	return strings.EqualFold(rest, serial)
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) {
		return "", false
	}
	if !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}

// FileRecord is one file in the resolved graph. Resolved is the symlink target,
// which is what identifies content across a replaced link.
type FileRecord struct {
	Path     string
	Resolved string
	Optional bool
	Missing  bool
	SHA256   string
}

// OutputNode is one active output node, with the file that contributed it.
type OutputNode struct {
	Node *document.Node
	File string
	Name string
}

// Graph is a resolved configuration root and everything it includes, in native
// order. Outputs are spliced where their include appeared, because that order
// is what decides which node wins.
type Graph struct {
	Root    FileRecord
	Files   []FileRecord
	Outputs []OutputNode
}

// Find returns the first output node niri would use for name, as a whole
// block: niri's Outputs::find returns the first match and does not merge
// fields from later nodes.
func (g *Graph) Find(name Name) (OutputNode, bool) {
	for _, out := range g.Outputs {
		if name.Matches(out.Name) {
			return out, true
		}
	}
	return OutputNode{}, false
}

// Fingerprint is the identity of the whole resolved graph: every file's path,
// optionality, presence, resolved target and content.
func (g *Graph) Fingerprint() string { return g.fingerprint("") }

// UserFingerprint is the identity of the graph with the owned file left out.
//
// This is the fingerprint that must not change across a publication. The
// managed file is the shell's own output, so publishing necessarily changes
// it; what must not change is every other file the user's configuration
// depends on. Its own content is tracked separately by hash, because the
// managed file is written in place rather than renamed.
func (g *Graph) UserFingerprint(managed string) string { return g.fingerprint(managed) }

func (g *Graph) fingerprint(skip string) string {
	h := sha256.New()
	for _, f := range g.Files {
		if skip != "" && f.Path == skip {
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00%d\x00%s\n", f.Path, b2i(f.Optional), f.Resolved, b2i(f.Missing), f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// LoadGraph resolves root and everything it includes, in native order.
func LoadGraph(root string) (*Graph, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("displays: resolve %s: %w", root, err)
	}
	g := &Graph{}
	budget := graphBudget{}
	stack := []string{abs}
	if err := g.walk(abs, filepath.Dir(abs), false, 0, &stack, &budget); err != nil {
		return nil, err
	}
	return g, nil
}

type graphBudget struct {
	bytes int
	files int
}

// walk reads one file and splices its includes in place. base is the
// containing file's directory, which is what relative include paths resolve
// against; stack is niri's include stack, so a cycle is a cycle.
func (g *Graph) walk(path, base string, optional bool, depth int, stack *[]string, budget *graphBudget) error {
	if depth >= maxIncludeDepth {
		return fmt.Errorf("%w: includes cannot be more than %d levels deep", errResourceLimit, maxIncludeDepth)
	}
	budget.files++
	if budget.files > maxGraphFiles {
		return fmt.Errorf("%w: more than %d files", errResourceLimit, maxGraphFiles)
	}
	rec, doc, err := readGraphFile(path, optional, budget)
	if err != nil {
		return err
	}
	g.Files = append(g.Files, rec)
	if rec.Missing {
		return nil
	}
	if depth == 0 {
		g.Root = rec
	}
	for _, node := range doc.Nodes {
		if text(node.Name) != "include" {
			if text(node.Name) == "output" && len(node.Arguments) == 1 {
				g.Outputs = append(g.Outputs, OutputNode{Node: node, File: rec.Path, Name: text(node.Arguments[0])})
			}
			continue
		}
		inc, err := includeTarget(node, base)
		if err != nil {
			return fmt.Errorf("displays: %s: %w", rec.Path, err)
		}
		if slices.Contains(*stack, inc.path) {
			return fmt.Errorf("displays: %s: recursive include (file includes itself)", rec.Path)
		}
		*stack = append(*stack, inc.path)
		err = g.walk(inc.path, filepath.Dir(inc.path), inc.optional, depth+1, stack, budget)
		*stack = (*stack)[:len(*stack)-1]
		if err != nil {
			return err
		}
	}
	return nil
}

type include struct {
	path     string
	optional bool
}

// includeTarget reads niri's include node: exactly one path argument, an
// optional boolean property, and no children. Anything else is a hard error in
// niri too, so the shell refuses the graph instead of guessing.
func includeTarget(node *document.Node, base string) (include, error) {
	if len(node.Arguments) != 1 {
		return include{}, fmt.Errorf("include needs exactly one path argument, found %d", len(node.Arguments))
	}
	if len(node.Children) != 0 {
		return include{}, fmt.Errorf("include cannot have child nodes")
	}
	raw, ok := node.Arguments[0].Value.(string)
	if !ok || raw == "" {
		return include{}, fmt.Errorf("include path is not a string")
	}
	inc := include{optional: false}
	if v, ok := node.Properties.Get("optional"); ok {
		b, ok := v.Value.(bool)
		if !ok {
			return include{}, fmt.Errorf("include optional=%v is not a boolean", v.Value)
		}
		inc.optional = b
	}
	for _, k := range propertyKeys(node) {
		if k != "optional" {
			return include{}, fmt.Errorf("include has unexpected property %q", k)
		}
	}
	// Niri expands a leading ~ against the home directory and resolves
	// everything else against the containing file's directory.
	if rest, ok := strings.CutPrefix(raw, "~"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return include{}, fmt.Errorf("include %q needs a home directory: %w", raw, err)
		}
		inc.path = filepath.Join(home, rest)
		return inc, nil
	}
	if filepath.IsAbs(raw) {
		inc.path = filepath.Clean(raw)
		return inc, nil
	}
	inc.path = filepath.Join(base, raw)
	return inc, nil
}

func readGraphFile(path string, optional bool, budget *graphBudget) (FileRecord, *document.Document, error) {
	rec := FileRecord{Path: path, Optional: optional}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return rec, nil, fmt.Errorf("displays: resolve %s: %w", path, err)
		}
		if !optional {
			return rec, nil, fmt.Errorf("displays: read %s: %w", path, err)
		}
		// A missing optional include is a warning in niri, not a failure, and
		// it still belongs in the fingerprint: a file appearing there changes
		// the graph.
		rec.Missing = true
		return rec, nil, nil
	}
	rec.Resolved = target
	f, err := os.Open(target)
	if err != nil {
		return rec, nil, fmt.Errorf("displays: open %s: %w", target, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return rec, nil, fmt.Errorf("displays: stat %s: %w", target, err)
	}
	if info.Size() > maxFileBytes {
		return rec, nil, fmt.Errorf("%w: %s is %d bytes, over the %d byte file limit", errResourceLimit, target, info.Size(), maxFileBytes)
	}
	budget.bytes += int(info.Size())
	if budget.bytes > maxGraphBytes {
		return rec, nil, fmt.Errorf("%w: the graph is over the %d byte total", errResourceLimit, maxGraphBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return rec, nil, fmt.Errorf("displays: read %s: %w", target, err)
	}
	sum := sha256.Sum256(data)
	rec.SHA256 = hex.EncodeToString(sum[:])
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return rec, nil, fmt.Errorf("displays: parse %s: %w", target, err)
	}
	return rec, doc, nil
}

func propertyKeys(n *document.Node) []string {
	all := n.Properties.Unordered()
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// text reads a KDL value as the string it denotes. The document package's own
// accessors all re-quote a string, which is right for rendering and wrong for
// reading a name or a path back.
func text(v *document.Value) string {
	if v == nil {
		return ""
	}
	s, _ := v.Value.(string)
	return s
}

// ManagedPath is the owned file's path beside root. root must already be
// absolute, so the managed file lands in the directory the compositor reads
// from even when the root itself is a symlink.
func ManagedPath(root string) string {
	return filepath.Join(filepath.Dir(root), ManagedFileName)
}

// IncludeLine renders the single owned include line with the KDL encoder
// rather than string quoting, so a path needing quotes gets them.
func IncludeLine(managed string) ([]byte, error) {
	doc := document.New()
	node := document.NewNode()
	node.SetName("include")
	node.AddArgument(managed, "")
	doc.AddNode(node)
	var buf bytes.Buffer
	if err := kdl.Generate(doc, &buf); err != nil {
		return nil, fmt.Errorf("displays: render include: %w", err)
	}
	return buf.Bytes(), nil
}

// Hash is the content identity used for ownership and drift checks.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
