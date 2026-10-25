package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Floors is the committed per-package coverage floor file. A package either
// carries a floor or an exclusion with the reason it has none. The file is
// data, not configuration: it records what the suite measured inside the CI
// runner so a later run can fail when a package loses the coverage it had.
type Floors struct {
	Note       string             `json:"note"`
	Packages   map[string]float64 `json:"packages"`
	Exclusions map[string]string  `json:"exclusions"`
}

// profileLine is one block of a Go coverage profile:
//
//	file.go:12.34,56.78 3 1
//
// The last two fields are the statement count of the block and how often the
// block ran. A block that ran is covered.
type profileLine struct {
	file       string
	statements int
	count      int
}

// coverage is the statement coverage of one package.
type coverage struct {
	total   int
	covered int
}

// percent is the covered share of the package's statements.
func (c coverage) percent() float64 {
	if c.total == 0 {
		return 0
	}
	return float64(c.covered) * 100 / float64(c.total)
}

// parseProfile reads a Go coverage profile.
func parseProfile(r io.Reader) ([]profileLine, error) {
	var out []profileLine
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		// The profile opens with the coverage mode: "mode: atomic".
		if strings.HasPrefix(text, "mode:") {
			continue
		}
		// The block descriptor follows the last colon; the range itself
		// carries no colon, so the split is unambiguous.
		sep := strings.LastIndex(text, ":")
		if sep < 0 {
			return nil, fmt.Errorf("line %d: %q has no block descriptor", n, text)
		}
		fields := strings.Fields(text[sep+1:])
		if len(fields) != 3 {
			return nil, fmt.Errorf("line %d: %q does not carry a range and two counts", n, text)
		}
		statements, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: %q has a statement count of %q", n, text, fields[1])
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("line %d: %q has a run count of %q", n, text, fields[2])
		}
		out = append(out, profileLine{file: text[:sep], statements: statements, count: count})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// aggregate sums the profile blocks per package. The key function maps a
// source file to the package that holds it; a file it declines is left out.
func aggregate(lines []profileLine, key func(string) (string, bool)) map[string]coverage {
	out := map[string]coverage{}
	for _, line := range lines {
		pkg, ok := key(line.file)
		if !ok {
			continue
		}
		c := out[pkg]
		c.total += line.statements
		if line.count > 0 {
			c.covered += line.statements
		}
		out[pkg] = c
	}
	return out
}

// packageKey maps a source file from the profile to the module relative
// directory that holds it: internal/render for .../internal/render/paint.go.
//
// The profile names a file either by its import path
// (github.com/Nomadcxx/sysc-shell/internal/render/paint.go) or by a path on
// disk (internal/render/paint.go, or an absolute one). Both name the same
// directory. A file outside the module is declined.
func packageKey(file, root, module string) (string, bool) {
	name := filepath.ToSlash(file)
	if module != "" {
		if name == module {
			return ".", true
		}
		if strings.HasPrefix(name, module+"/") {
			return dirOfPath(strings.TrimPrefix(name, module+"/")), true
		}
	}
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return dirOfPath(rel), true
}

// dirOfPath names the directory that holds a file, relative to the module.
func dirOfPath(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(filepath.ToSlash(rel)))
	if dir == "." {
		return "."
	}
	return dir
}

// normalizePackage turns an entry of the package list into a module relative
// directory: github.com/Nomadcxx/sysc-shell/internal/render and internal/render
// both name internal/render.
func normalizePackage(entry, modulePath string) string {
	e := strings.TrimSpace(entry)
	if e == "" {
		return ""
	}
	e = strings.TrimSuffix(e, "/")
	if modulePath != "" && (e == modulePath || strings.HasPrefix(e, modulePath+"/")) {
		e = strings.TrimPrefix(e, modulePath)
		e = strings.TrimPrefix(e, "/")
	}
	if e == "" {
		return "."
	}
	return e
}

// reason names why a package did not pass.
type reason string

const (
	reasonBelow      reason = "below"
	reasonNoEntry    reason = "no entry"
	reasonNoEntryRaw reason = "no entry without tests"
	reasonNoCoverage reason = "no coverage"
)

// finding is one package that did not meet its floor.
type finding struct {
	pkg      string
	why      reason
	floor    float64
	measured float64
}

// String is the line the tool prints for a finding. It names the package, the
// floor and the measured value so a reader does not have to open the profile.
func (f finding) String() string {
	switch f.why {
	case reasonBelow:
		return fmt.Sprintf("%s: floor %.2f%%, measured %.2f%%", f.pkg, f.floor, f.measured)
	case reasonNoEntry:
		return fmt.Sprintf("%s: no floor is committed", f.pkg)
	case reasonNoEntryRaw:
		return fmt.Sprintf("%s: no floor is committed and no exclusion explains it; a package without tests needs an exclusion with a reason", f.pkg)
	case reasonNoCoverage:
		return fmt.Sprintf("%s: floor %.2f%%, measured 0.00%% (no statement of the package was measured)", f.pkg, f.floor)
	default:
		return fmt.Sprintf("%s: %s", f.pkg, f.why)
	}
}

// judge compares every package that has to be judged with its floor. The
// judged set is the packages named by the list, the packages with a floor and
// the packages the profile measured, so a new package cannot arrive without an
// entry. A package named by an exclusion is left alone.
func judge(floors Floors, measured map[string]coverage, packages []string) []finding {
	judged := map[string]bool{}
	for _, p := range packages {
		if p != "" {
			judged[p] = true
		}
	}
	for p := range floors.Packages {
		judged[p] = true
	}
	for p := range measured {
		judged[p] = true
	}
	var out []finding
	for pkg := range judged {
		if _, excluded := floors.Exclusions[pkg]; excluded {
			continue
		}
		floor, ok := floors.Packages[pkg]
		if !ok {
			why := reasonNoEntry
			if measured[pkg].total == 0 {
				why = reasonNoEntryRaw
			}
			out = append(out, finding{pkg: pkg, why: why})
			continue
		}
		c := measured[pkg]
		if c.total == 0 {
			out = append(out, finding{pkg: pkg, why: reasonNoCoverage, floor: floor})
			continue
		}
		if c.percent()+tolerance < floor {
			out = append(out, finding{pkg: pkg, why: reasonBelow, floor: floor, measured: c.percent()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pkg < out[j].pkg })
	return out
}

// tolerance is the wobble a measurement may have before the gate calls it a
// regression. A package has to fall further than this below its floor before
// the build fails, so a rounding difference between two runs is not a drop.
//
// Two runs of the same tree measure a few packages differently: a branch that
// depends on a timeout, or on what the machine already has, lands on one side
// of the line in one run and on the other side in the next. The wobble
// measured on this tree was up to 0.18 points, so the band is half a point.
// A gate that reds on noise is a gate that gets ignored.
const tolerance = 0.5

// floorTo truncates a measured percentage to the committed precision, so a
// floor never sits above what was measured.
func floorTo(percent float64) float64 {
	return math.Floor(percent*100) / 100
}

// measuredFloors turns a measured coverage into the floor file that records
// it. Exclusions are carried over untouched: the reason a package has no
// floor is a decision, not a measurement.
func measuredFloors(floors Floors, measured map[string]coverage) Floors {
	out := Floors{
		Note:       floors.Note,
		Packages:   map[string]float64{},
		Exclusions: map[string]string{},
	}
	for pkg, reason := range floors.Exclusions {
		out.Exclusions[pkg] = reason
	}
	for pkg, c := range measured {
		if c.total == 0 {
			continue
		}
		// An excluded package keeps its reason and carries no floor. A floor
		// of zero next to an exclusion would read as a demand the package
		// cannot meet.
		if _, excluded := floors.Exclusions[pkg]; excluded {
			continue
		}
		out.Packages[pkg] = floorTo(c.percent())
	}
	return out
}

// loadFloors reads the committed floor file.
func loadFloors(path string) (Floors, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Floors{}, err
	}
	var f Floors
	if err := json.Unmarshal(data, &f); err != nil {
		return Floors{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.Packages == nil {
		f.Packages = map[string]float64{}
	}
	if f.Exclusions == nil {
		f.Exclusions = map[string]string{}
	}
	return f, nil
}

// saveFloors writes the floor file.
func saveFloors(path string, f Floors) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// readPackages reads a list of packages, one per line. Blank lines and lines
// that start with a hash are left out.
func readPackages(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		out = append(out, text)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// moduleRoot walks up from dir to the directory that holds go.mod.
func moduleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		abs = parent
	}
}

// modulePath reads the module path from go.mod.
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("no module path in %s", filepath.Join(root, "go.mod"))
}
