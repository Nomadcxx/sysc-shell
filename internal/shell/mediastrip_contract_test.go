package shell

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMediaStripTestsUseTheOpenResult checks that every call to openLocked in
// a media strip test uses its result.
//
// The call reports whether the strip opened, so a test that discards it
// cannot tell an open strip from one that never opened. The assertion that
// follows then blames the wrong thing: a closed pinned strip reads as a hover
// grace that closed it, and a strip that never opened reads as a clean close.
// The media state is the one input a strip needs, and the media relay can
// replace it, so this is the difference between a failure that names its
// cause and one that sends the next reader after the wrong code.
//
// A file is a media strip test when it names the strip host or the strip
// fixture. The check reads the package's own test sources, so it holds under
// any load.
func TestMediaStripTestsUseTheOpenResult(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !namesMediaStrip(file) {
			continue
		}
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			stmt, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "openLocked" {
				return true
			}
			t.Errorf("%s: the result of openLocked is discarded, so a strip that never opened reads as a different failure",
				fset.Position(stmt.Pos()))
			return true
		})
	}
	if checked == 0 {
		t.Fatal("checked no media strip test file")
	}
	t.Logf("checked %d media strip test files", checked)
}

// namesMediaStrip reports whether a file names the media strip host or the
// media strip fixture.
func namesMediaStrip(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if ok && (ident.Name == "mediaStripHost" || ident.Name == "stripTestRegistry") {
			found = true
		}
		return true
	})
	return found
}
