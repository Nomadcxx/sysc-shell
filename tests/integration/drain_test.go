package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/shell"
)

// keepInvalidationsDrained consumes the registry's invalidation queue the
// way the production Wayland bridge does. A registry blocks its publisher
// once the bounded queue is full, and a test owns no bridge to receive those
// invalidations, so a fixture that neither drains the queue nor consumes it
// blocks its publisher for good (sysc-1553).
func keepInvalidationsDrained(t *testing.T, reg *shell.Registry) {
	t.Helper()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-reg.Invalidations():
			case <-stop:
				return
			}
		}
	}()
}

// TestPanelFixturesDrainTheirInvalidations pins the fixture contract that
// keeps this package from hanging the suite. A test that builds a Registry
// and opens a panel publishes into a queue nobody reads unless it drains the
// queue itself, and a blocked publish holds the test for good: a blocked
// publisher is a package timeout in CI, not a test failure.
//
// The check reads the package's own test sources, so a new fixture that
// forgets the drain fails the suite instead of waiting for a loaded runner
// to hang on it.
func TestPanelFixturesDrainTheirInvalidations(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Body == nil {
				continue
			}
			builds, opens, drains, consumes := fixtureCalls(fn)
			if !builds || !opens {
				continue
			}
			checked++
			if drains || consumes {
				continue
			}
			t.Errorf("%s: %s opens a panel on a Registry it built without a consumer for the invalidation queue; "+
				"call keepInvalidationsDrained(t, reg) or consume reg.Invalidations()",
				fileSet.Position(fn.Pos()), fn.Name.Name)
		}
	}
	if checked == 0 {
		t.Fatal("no panel fixture was checked; the scan no longer sees this package's tests")
	}
	t.Logf("checked %d panel fixtures", checked)
}

// fixtureCalls reports what a test does with the Registry it builds: whether
// it creates one, whether it opens a panel on it, whether it drains the
// invalidation queue, and whether it consumes the queue itself.
func fixtureCalls(fn *ast.FuncDecl) (builds, opens, drains, consumes bool) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if fun.Name == "NewRegistry" {
				builds = true
			}
			if fun.Name == "keepInvalidationsDrained" {
				drains = true
			}
		case *ast.SelectorExpr:
			switch fun.Sel.Name {
			case "NewRegistry":
				builds = true
			case "OpenPanel":
				opens = true
			case "Invalidations":
				consumes = true
			}
		}
		return true
	})
	return builds, opens, drains, consumes
}
