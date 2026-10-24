package wallpaper

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// unixSocketPathLimit is the length of sun_path on Linux. A socket path
// longer than this cannot be bound: the bind fails with "invalid argument",
// which reads as a broken engine rather than a broken test.
const unixSocketPathLimit = 107

// shortTempDir makes a temporary directory whose path stays short enough to
// hold a unix socket.
//
// t.TempDir names its directory after the test, so a long test name under a
// long TMPDIR overruns the socket limit. The gate runs with GOTMPDIR pointed
// at /home, where the path is already long before the test name is added.
func shortTempDir(t *testing.T) string {
	t.Helper()
	// /dev/shm is short and a Linux session has it; the temporary directory
	// is the fallback when it does not.
	for _, base := range []string{"/dev/shm", os.TempDir()} {
		dir, err := os.MkdirTemp(base, "wc")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Fatal("no writable directory for a short temporary path")
	return ""
}

// TestShortTempDirLeavesRoomForASocket pins the property the helper exists
// for: the directory it returns, and the sockets the engine binds under it,
// are short enough for the limit, whatever TMPDIR says.
func TestShortTempDirLeavesRoomForASocket(t *testing.T) {
	dir := shortTempDir(t)
	for _, path := range []string{
		terminalSocketPath(dir, "DP-1"),
		socketPath(dir, "DP-1"),
		filepath.Join(dir, "replacement.sock"),
		filepath.Join(dir, "g.sock"),
	} {
		if len(path) > unixSocketPathLimit {
			t.Fatalf("socket path is %d bytes: %s; the limit is %d", len(path), path, unixSocketPathLimit)
		}
	}
	h := newEngineHarness(t)
	for _, path := range []string{h.socket("DP-1"), terminalSocketPath(h.eng.dir, "DP-1")} {
		if len(path) > unixSocketPathLimit {
			t.Fatalf("harness socket path is %d bytes: %s; the limit is %d", len(path), path, unixSocketPathLimit)
		}
	}
}

// TestEngineSocketDirIsNotInTheTestTempRoot pins where the engine's socket
// directory may live.
//
// t.TempDir names its directory after the test, so the test's own name sits
// in every socket path the engine binds. A long test name under a long
// TMPDIR overruns the socket limit. A directory outside the test's temp root
// keeps that name out of the path.
func TestEngineSocketDirIsNotInTheTestTempRoot(t *testing.T) {
	root := filepath.Dir(t.TempDir())
	h := newEngineHarness(t)
	if strings.HasPrefix(h.eng.dir, root) {
		t.Fatalf("the engine's socket dir is %s, inside the test's temp root %s; use shortTempDir(t)", h.eng.dir, root)
	}
}

// TestSocketListensAvoidLongTempDirs fails a test that binds a unix socket
// under a directory t.TempDir made. That directory is named after the test,
// so the path overruns the socket limit under a long TMPDIR and the bind
// fails with "invalid argument" instead of the test's own failure.
//
// The check reads the package's own test sources, so it holds under any load
// and needs no socket of its own.
func TestSocketListensAvoidLongTempDirs(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	checked := 0
	for _, file := range files {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			longDirs := longTempDirVars(fn)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !bindsUnixSocket(call) {
					return true
				}
				checked++
				if bad := socketPathSource(call, longDirs); bad != "" {
					t.Errorf("%s: the socket path comes from %s, whose path overruns the %d byte limit under a long TMPDIR; use shortTempDir(t)",
						fset.Position(call.Pos()), bad, unixSocketPathLimit)
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("checked no socket bind: the check does not see the package's own tests")
	}
}

// longTempDirVars names the variables a function holds a long temporary
// directory in, directly or through a path built from one.
func longTempDirVars(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	for {
		grew := false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				if i >= len(assign.Lhs) {
					continue
				}
				ident, ok := assign.Lhs[i].(*ast.Ident)
				if !ok || names[ident.Name] {
					continue
				}
				if reachesLongDir(rhs, names) {
					names[ident.Name] = true
					grew = true
				}
			}
			return true
		})
		if !grew {
			return names
		}
	}
}

// reachesLongDir reports whether the expression reaches a long temporary
// directory, either by naming one or by making one.
func reachesLongDir(expr ast.Expr, names map[string]bool) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			if names[n.Name] {
				found = true
			}
		case *ast.CallExpr:
			if isTempDirCall(n) {
				found = true
			}
		}
		return true
	})
	return found
}

// isTempDirCall reports whether the call is t.TempDir().
func isTempDirCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "TempDir" {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	return ok && recv.Name == "t"
}

// bindsUnixSocket reports whether the call listens on a unix socket.
func bindsUnixSocket(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Listen":
		// net.Listen("unix", path)
		if len(call.Args) < 2 {
			return false
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		return ok && lit.Kind == token.STRING && unquote(lit.Value) == "unix"
	case "ListenUnix":
		return true
	}
	return false
}

// socketPathSource names the long temporary directory a socket bind reaches,
// or an empty string when the path does not come from one.
func socketPathSource(call *ast.CallExpr, longDirs map[string]bool) string {
	found := ""
	ast.Inspect(call, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			if longDirs[n.Name] && found == "" {
				found = n.Name
			}
		case *ast.CallExpr:
			if isTempDirCall(n) && found == "" {
				found = "t.TempDir()"
			}
		}
		return true
	})
	return found
}

// unquote unquotes a string literal.
func unquote(lit string) string {
	out, err := strconv.Unquote(lit)
	if err != nil {
		return lit
	}
	return out
}
