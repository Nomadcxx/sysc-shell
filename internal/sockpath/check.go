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
	problems, _, err := socketBinds(dir)
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "\n"))
}

// CountSocketBinds returns the number of unix socket binds the check sees in
// the test files under dir.
//
// A bind the check no longer recognises would pass the check for the wrong
// reason, so a package that expects binds compares the count against the
// binds it knows it has.
func CountSocketBinds(dir string) (int, error) {
	_, count, err := socketBinds(dir)
	return count, err
}

// socketBinds returns the problems the check found and the number of binds it
// saw.
func socketBinds(dir string) ([]string, int, error) {
	var problems []string
	count := 0
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
				socket, ok := socketPathOf(node, parsed.Name.Name)
				if !ok {
					return true
				}
				count++
				if source := longSource(socket, long); source != "" {
					problems = append(problems, fmt.Sprintf(
						"%s: the socket path comes from %s, whose path can overrun the %d byte limit under a long TMPDIR; use sockpath.Dir(t, ...)",
						fset.Position(node.Pos()), source, Limit))
				}
				return true
			})
		}
		return nil
	})
	return problems, count, err
}

// socketPathOf returns the expression that names the socket a node binds, and
// whether the node binds a unix socket at all.
func socketPathOf(node ast.Node, pkgName string) (ast.Expr, bool) {
	switch n := node.(type) {
	case *ast.CallExpr:
		return socketPath(n, pkgName)
	case *ast.BinaryExpr:
		// A bus address names a socket without binding it here.
		return dbusSocketPath(n)
	}
	return nil, false
}

// dbusSocketPath returns the path a "unix:path=" address names.
func dbusSocketPath(expr *ast.BinaryExpr) (ast.Expr, bool) {
	if expr.Op != token.ADD {
		return nil, false
	}
	left, ok := expr.X.(*ast.BasicLit)
	if !ok || left.Kind != token.STRING || unquote(left.Value) != "unix:path=" {
		return nil, false
	}
	return expr.Y, true
}

// socketPath returns the expression that names the socket a call binds, and
// whether the call binds a unix socket at all.
//
// pkgName is the package the file belongs to, which decides whether a call
// to NewServer is the ipc server's constructor.
func socketPath(call *ast.CallExpr, pkgName string) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		// A constructor called without a package qualifier, such as
		// NewServer(path) from inside the ipc package.
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "NewServer" || pkgName != "ipc" || len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	switch pkg.Name {
	case "net":
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
	case "ipc":
		// ipc.NewServer(path, handlers) binds the instance socket at path.
		// The bind happens inside the package, so a check that only reads
		// net.Listen calls would miss it.
		if sel.Sel.Name == "NewServer" && len(call.Args) != 0 {
			return call.Args[0], true
		}
	}
	if sel.Sel.Name == "listenUnix" && len(call.Args) != 0 {
		// A test helper that binds a unix socket at the path it is given.
		return call.Args[len(call.Args)-1], true
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
			switch n := node.(type) {
			case *ast.AssignStmt:
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) {
						continue
					}
					ident, ok := n.Lhs[i].(*ast.Ident)
					if !ok || names[ident.Name] {
						continue
					}
					if reachesLongDir(rhs, names) {
						names[ident.Name] = true
						grew = true
					}
				}
			case *ast.GenDecl:
				// var sock = filepath.Join(t.TempDir(), "s.sock")
				if n.Tok != token.VAR {
					break
				}
				for _, spec := range n.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, rhs := range value.Values {
						if i >= len(value.Names) {
							continue
						}
						ident := value.Names[i]
						if names[ident.Name] {
							continue
						}
						if reachesLongDir(rhs, names) {
							names[ident.Name] = true
							grew = true
						}
					}
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
