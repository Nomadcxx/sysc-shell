// Command coveragefloor fails when a package's statement coverage drops below
// the floor committed for it. The floors are measured inside the CI runner, so
// a result is a property of the tree rather than of the machine that ran it.
//
// Usage:
//
//	coveragefloor [-profile coverage.out] [-floors floors.json]
//	              [-packages packages.txt] [-module-root dir] [-update] [-out file]
//
// With -update the tool writes the measured coverage into the floor file
// instead of judging it. A package the profile did not measure keeps its
// exclusion and its reason.
//
// Exit codes: 0 every package is at or above its floor, 1 a package failed the
// floor, 2 a file could not be read or written.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// defaultFloorsPath is the committed floor file. It sits with the tool that
// reads it, so the tool needs no flag to do its job.
const defaultFloorsPath = "tools/coveragefloor/floors.json"

// defaultNote describes the floor file in the file itself.
const defaultNote = "Per-package statement coverage floors, measured in the CI runner. " +
	"Regenerate with `go run ./tools/coveragefloor -update` after a coverage run. " +
	"A package without tests carries an exclusion and the reason it has none. " +
	"A package fails when it falls more than 0.05 points below its floor."

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole command, so a test can drive every path through it.
func run(args []string, stdout, stderr io.Writer) int {
	var (
		profilePath  string
		floorsPath   string
		packagesPath string
		rootFlag     string
		update       bool
		outPath      string
	)
	fs := flag.NewFlagSet("coveragefloor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&profilePath, "profile", "coverage.out", "coverage profile to read")
	fs.StringVar(&floorsPath, "floors", defaultFloorsPath, "floor file to read or write")
	fs.StringVar(&packagesPath, "packages", "", "file naming every package of the module, one per line")
	fs.StringVar(&rootFlag, "module-root", ".", "directory that holds go.mod")
	fs.BoolVar(&update, "update", false, "write the measured coverage into the floor file")
	fs.StringVar(&outPath, "out", "", "file to write the measured floors into; defaults to the floor file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "coveragefloor: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	root, err := moduleRoot(rootFlag)
	if err != nil {
		fmt.Fprintf(stderr, "coveragefloor: %v\n", err)
		return 2
	}
	module, err := modulePath(root)
	if err != nil {
		fmt.Fprintf(stderr, "coveragefloor: %v\n", err)
		return 2
	}
	file, err := os.Open(profilePath)
	if err != nil {
		fmt.Fprintf(stderr, "coveragefloor: cannot read the profile %s: %v\n", profilePath, err)
		return 2
	}
	lines, err := parseProfile(file)
	file.Close()
	if err != nil {
		fmt.Fprintf(stderr, "coveragefloor: %s: %v\n", profilePath, err)
		return 2
	}
	measured := aggregate(lines, func(name string) (string, bool) {
		return packageKey(name, root, module)
	})
	floors, err := loadFloors(floorsPath)
	if err != nil {
		// A first run has no floor file to update.
		if !update || !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "coveragefloor: cannot read the floors %s: %v\n", floorsPath, err)
			return 2
		}
		floors = Floors{Note: defaultNote, Packages: map[string]float64{}, Exclusions: map[string]string{}}
	}
	if update && outPath == "" {
		outPath = floorsPath
	}
	if update {
		written := measuredFloors(floors, measured)
		if err := saveFloors(outPath, written); err != nil {
			fmt.Fprintf(stderr, "coveragefloor: cannot write the floors %s: %v\n", outPath, err)
			return 2
		}
		fmt.Fprintf(stdout, "coveragefloor: wrote %d package floors into %s\n", len(written.Packages), outPath)
		return 0
	}
	var packages []string
	if packagesPath != "" {
		raw, err := readPackages(packagesPath)
		if err != nil {
			fmt.Fprintf(stderr, "coveragefloor: cannot read the package list %s: %v\n", packagesPath, err)
			return 2
		}
		for _, entry := range raw {
			if pkg := normalizePackage(entry, module); pkg != "" {
				packages = append(packages, pkg)
			}
		}
	}
	findings := judge(floors, measured, packages)
	for _, f := range findings {
		fmt.Fprintln(stderr, "coveragefloor: "+f.String())
	}
	if len(findings) > 0 {
		fmt.Fprintf(stderr, "coveragefloor: %d package(s) did not meet the committed floor\n", len(findings))
		return 1
	}
	fmt.Fprintf(stdout, "coveragefloor: %d package(s) measured, %d excluded, all at or above their floors\n",
		len(measured), len(floors.Exclusions))
	return 0
}
