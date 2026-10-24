package sockpath

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// CheckSocketPaths reports the unix socket binds in the test files under dir
// whose path comes from a temporary directory that can be long.
//
// t.TempDir and os.MkdirTemp derive their path from TMPDIR, and t.TempDir
// adds the test's own name to it, so a socket bound there overruns sun_path
// under a long TMPDIR. The bind then fails with "invalid argument", and the
// test reports a broken component instead of its own failure.
//
// The check reads the test sources, so it holds under any load and binds no
// socket of its own.
func CheckSocketPaths(dir string) error {
	var problems []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			long := longVars(fn)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				socket, ok := socketPath(call)
				if !ok {
					return true
				}
				if source := longSource(socket, long); source != "" {
					problems = append(problems, fmt.Sprintf(
						"%s: the socket path comes from %s, whose path can overrun the %d byte limit under a long TMPDIR; use sockpath.Dir(t, ...)",
						fset.Position(call.Pos()), source, Limit))
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "\n"))
}

// socketPath returns the expression that names the socket a call binds, and
// whether the call binds a unix socket at all.
func socketPath(call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "net" {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Listen":
		// net.Listen("unix", path)
		if len(call.Args) != 2 || !isString(call.Args[0], "unix") {
			return nil, false
		}
		return call.Args[1], true
	case "ListenUnix":
		// net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if len(call.Args) != 2 || !isString(call.Args[0], "unix") {
			return nil, false
		}
		addr, ok := call.Args[1].(*ast.UnaryExpr)
		if !ok {
			return nil, false
		}
		lit, ok := addr.X.(*ast.CompositeLit)
		if !ok {
			return nil, false
		}
		for _, element := range lit.Elts {
			kv, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if ok && key.Name == "Name" {
				return kv.Value, true
			}
		}
	}
	return nil, false
}

// longVars names the variables that hold a long temporary directory, directly
// or through a path built from one.
func longVars(fn *ast.FuncDecl) map[string]bool {
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

// longSource names where a long temporary directory enters the expression, or
// returns an empty string when it does not.
func longSource(expr ast.Expr, names map[string]bool) string {
	source := ""
	ast.Inspect(expr, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if isTempDirCall(n) {
				source = "a temporary directory"
			}
		case *ast.Ident:
			if source == "" && names[n.Name] {
				source = n.Name
			}
		}
		return true
	})
	return source
}

// isTempDirCall reports whether the call makes a temporary directory that
// inherits TMPDIR.
func isTempDirCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "TempDir":
		// t.TempDir()
		recv, ok := sel.X.(*ast.Ident)
		return ok && recv.Name == "t"
	case "MkdirTemp":
		// os.MkdirTemp("", pattern) inherits TMPDIR; a named base does not.
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "os" || len(call.Args) == 0 {
			return false
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			// A base that is not a literal may be TMPDIR.
			return true
		}
		return lit.Kind == token.STRING && unquote(lit.Value) == ""
	}
	return false
}

// isString reports whether the expression is the given string literal.
func isString(expr ast.Expr, want string) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && unquote(lit.Value) == want
}

// unquote returns the value of a string literal.
func unquote(lit string) string {
	value, err := strconv.Unquote(lit)
	if err != nil {
		return lit
	}
	return value
}
