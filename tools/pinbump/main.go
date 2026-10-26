// Command pinbump performs the recorded pin-bump procedure.
//
// The procedure exists because `go get` does more than change a version: it
// rewrites the metadata around the requirement. It lowered the go directive
// from 1.26.10 to 1.26.4, which reopened two known flaws in the standard
// library, and it deleted the ignore directive that keeps the docs directory
// out of the module's package list. CI reads the directive for the toolchain
// it installs, so the damage would have made CI install a flawed toolchain.
//
// The procedure is: take go.mod and go.sum from the base ref, hand-edit only
// the require line, then run `go mod download` for the module. The tool does
// exactly that, and then it checks that the file it produced is the file the
// procedure says it should be.
//
// Usage:
//
//	pinbump [-ref origin/main] [-bump module=version]... [-plan] [-dir .]
//
// With -plan the command prints the grouped bump it would make and changes
// nothing. Without it, the command applies the bumps named on the command
// line and leaves the result in the working tree.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Nomadcxx/sysc-shell/tools/internal/dep"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// config is what the command was asked to do.
type config struct {
	ref    string
	dir    string
	bumps  []Bump
	plan   bool
	prefix string
	proxy  string
}

// run is the whole command, so a test can drive every path through it.
func run(args []string, stdout, stderr io.Writer) int {
	cfg := config{ref: "origin/main", dir: ".", prefix: "github.com/Nomadcxx/sysc"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-plan":
			cfg.plan = true
		case arg == "-ref" && i+1 < len(args):
			i++
			cfg.ref = args[i]
		case arg == "-dir" && i+1 < len(args):
			i++
			cfg.dir = args[i]
		case arg == "-prefix" && i+1 < len(args):
			i++
			cfg.prefix = args[i]
		case arg == "-proxy" && i+1 < len(args):
			i++
			cfg.proxy = args[i]
		case arg == "-bump" && i+1 < len(args):
			i++
			bump, err := ParseBump(args[i])
			if err != nil {
				fmt.Fprintf(stderr, "pinbump: %v\n", err)
				return 2
			}
			cfg.bumps = append(cfg.bumps, bump)
		case arg == "-bump-file" && i+1 < len(args):
			i++
			bumps, err := readBumpFile(args[i])
			if err != nil {
				fmt.Fprintf(stderr, "pinbump: %v\n", err)
				return 2
			}
			cfg.bumps = append(cfg.bumps, bumps...)
		case arg == "-help" || arg == "-h":
			fmt.Fprintln(stdout, "usage: pinbump [-ref origin/main] [-bump module=version]... [-bump-file path] [-plan] [-dir .]")
			return 0
		default:
			fmt.Fprintf(stderr, "pinbump: cannot read the argument %q\n", arg)
			return 2
		}
	}

	original, err := gitShow(cfg.dir, cfg.ref, "go.mod")
	if err != nil {
		fmt.Fprintf(stderr, "pinbump: %v\n", err)
		return 2
	}

	if cfg.plan {
		bumps, err := plan(context.Background(), cfg, original)
		if err != nil {
			fmt.Fprintf(stderr, "pinbump: %v\n", err)
			return 2
		}
		for _, bump := range bumps {
			fmt.Fprintln(stdout, bump.String())
		}
		return 0
	}

	if len(cfg.bumps) == 0 {
		fmt.Fprintln(stderr, "pinbump: no bump was named. Use -plan to see the grouped bump, or -bump module=version.")
		return 2
	}

	result, err := apply(cfg, original)
	if err != nil {
		fmt.Fprintf(stderr, "pinbump: %v\n", err)
		return 1
	}
	for _, line := range result {
		fmt.Fprintln(stdout, line)
	}
	return 0
}

// readBumpFile reads the bumps from a file: one module=version pair per
// line. A blank line and a line that starts with a hash are left out.
func readBumpFile(path string) ([]Bump, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Bump
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		bump, err := ParseBump(line)
		if err != nil {
			return nil, err
		}
		out = append(out, bump)
	}
	return out, nil
}

// plan returns the grouped bump the tree asks for.
//
// A module is in the group when it is behind, carries no reason for being
// held, and has a release to move to. A pin that names a commit rather than
// a release is left out: there is nothing to move it to, and the report
// says so.
func plan(ctx context.Context, cfg config, original string) ([]Bump, error) {
	client := dep.NewHTTPClient()
	var out []Bump
	for _, pin := range dep.ParseGoMod(original) {
		if !strings.HasPrefix(pin.Module, cfg.prefix) {
			continue
		}
		if pin.Hold != "" {
			continue
		}
		if dep.IsPseudoVersion(pin.Version) {
			continue
		}
		published, err := dep.Versions(ctx, client, cfg.proxy, pin.Module)
		if err != nil {
			return nil, err
		}
		target := ""
		for _, v := range published {
			parsed, perr := dep.ParseVersion(v)
			if perr != nil || parsed.Pre != "" {
				continue
			}
			if dep.CompareVersions(v, pin.Version) > 0 {
				target = v
			}
		}
		if target == "" {
			continue
		}
		out = append(out, Bump{Module: pin.Module, Version: target})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out, nil
}

// apply performs the procedure and checks the result.
func apply(cfg config, original string) ([]string, error) {
	originalSum, err := gitShow(cfg.dir, cfg.ref, "go.sum")
	if err != nil {
		return nil, err
	}
	before, err := buildList(original, originalSum)
	if err != nil {
		// The tree as it stands may not resolve; the comparison is a
		// check, not the procedure. The procedure continues without it.
		before = nil
	}
	if err := writeFile(filepath.Join(cfg.dir, "go.mod"), original); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(cfg.dir, "go.sum"), originalSum); err != nil {
		return nil, err
	}

	updated := original
	for _, bump := range cfg.bumps {
		updated, err = dep.SetRequire(updated, bump.Module, bump.Version)
		if err != nil {
			return nil, err
		}
	}
	if err := writeFile(filepath.Join(cfg.dir, "go.mod"), updated); err != nil {
		return nil, err
	}
	for _, bump := range cfg.bumps {
		if err := download(cfg.dir, bump.Module); err != nil {
			return nil, err
		}
	}

	final, err := readFile(filepath.Join(cfg.dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	if err := Verify(original, final, cfg.bumps); err != nil {
		return nil, err
	}
	after, err := buildList(final, readFileOr(filepath.Join(cfg.dir, "go.sum"), ""))
	if err != nil {
		return nil, err
	}
	if err := verifyBuildList(before, after, cfg.bumps); err != nil {
		return nil, err
	}

	lines := []string{
		fmt.Sprintf("bumped %d requirement(s) and checked the result", len(cfg.bumps)),
	}
	for _, bump := range cfg.bumps {
		lines = append(lines, "  "+bump.String())
	}
	return lines, nil
}

// verifyBuildList checks that the build list changed only in the modules the
// bump names.
//
// The requirement lines can look right while the build list is wrong: a
// requirement can pull a different set of modules into the graph without
// changing any line of go.mod. The build list is what the compiler sees.
func verifyBuildList(before, after map[string]string, bumps []Bump) error {
	if before == nil {
		return nil
	}
	wanted := map[string]bool{}
	for _, b := range bumps {
		wanted[b.Module] = true
	}
	for module, version := range after {
		old, ok := before[module]
		if !ok {
			if wanted[module] {
				continue
			}
			return fmt.Errorf("the bump pulled %s %s into the build list", module, version)
		}
		if old != version && !wanted[module] {
			return fmt.Errorf("the bump moved %s from %s to %s in the build list", module, old, version)
		}
	}
	for module, version := range before {
		if _, ok := after[module]; !ok {
			return fmt.Errorf("the bump dropped %s %s out of the build list", module, version)
		}
	}
	return nil
}

// buildList reads the module versions the tree resolves to.
//
// It runs in a directory of its own, so the read does not depend on the
// working tree it is asked about.
func buildList(goMod, goSum string) (map[string]string, error) {
	scratch, err := os.MkdirTemp("", "pinbump")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	if err := writeFile(filepath.Join(scratch, "go.mod"), goMod); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(scratch, "go.sum"), goSum); err != nil {
		return nil, err
	}
	// The read runs with -mod=mod because a bumped requirement can pull a
	// version of another module into the graph, and the read-only mode
	// refuses to answer rather than resolve it. The scratch copy is
	// throwaway: the tree itself is never rewritten by the read.
	out, err := runCommandEnv(scratch, []string{"GOFLAGS=-mod=mod"}, "go", "list", "-m", "all")
	if err != nil {
		return nil, err
	}
	list := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		list[fields[0]] = fields[1]
	}
	return list, nil
}

// download fetches one module and records its hashes, the way the recorded
// procedure says.
//
// The go command is never asked to resolve the whole graph: `go get` does
// that, and `go get` is what rewrites the metadata.
func download(dir, module string) error {
	cmd := exec.Command("go", "mod", "download", module)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod download %s: %v: %s", module, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitShow reads one file from a ref.
func gitShow(dir, ref, path string) (string, error) {
	out, err := runCommand(dir, "git", "show", ref+":"+path)
	if err != nil {
		return "", fmt.Errorf("git show %s:%s: %v", ref, path, err)
	}
	return out, nil
}

// runCommand makes one command in a directory and returns its output.
func runCommand(dir string, name string, args ...string) (string, error) {
	return runCommandEnv(dir, nil, name, args...)
}

// runCommandEnv makes one command in a directory with extra environment
// entries and returns its output.
func runCommandEnv(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// readFile reads a file.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// readFileOr reads a file, or returns a fallback when it cannot be read.
func readFileOr(path, fallback string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	return string(data)
}

// writeFile writes a file.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
