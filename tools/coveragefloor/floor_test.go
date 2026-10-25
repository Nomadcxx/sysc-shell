package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    []profileLine
		wantErr string
	}{
		{
			name:  "a block carries a statement count and a run count",
			input: "internal/render/paint.go:12.34,56.78 3 1\n",
			want:  []profileLine{{file: "internal/render/paint.go", statements: 3, count: 1}},
		},
		{
			name:  "a block that never ran is counted as uncovered",
			input: "internal/render/paint.go:12.34,56.78 3 0\n",
			want:  []profileLine{{file: "internal/render/paint.go", statements: 3, count: 0}},
		},
		{
			name:  "the mode header is skipped",
			input: "mode: atomic\ninternal/render/paint.go:12.34,56.78 3 1\n",
			want:  []profileLine{{file: "internal/render/paint.go", statements: 3, count: 1}},
		},
		{
			name:  "blank lines are skipped",
			input: "\ninternal/render/paint.go:1.1,2.2 1 1\n\n",
			want:  []profileLine{{file: "internal/render/paint.go", statements: 1, count: 1}},
		},
		{
			name:    "a line without a block descriptor is an error",
			input:   "internal/render/paint.go\n",
			wantErr: "no block descriptor",
		},
		{
			name:    "a line without counts is an error",
			input:   "internal/render/paint.go:12.34,56.78 3\n",
			wantErr: "does not carry a range and two counts",
		},
		{
			name:    "a count that is not a number is an error",
			input:   "internal/render/paint.go:12.34,56.78 x 1\n",
			wantErr: "statement count",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProfile(strings.NewReader(tc.input))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseProfile returned no error, wanted %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseProfile error %q does not name %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProfile: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parseProfile returned %d lines, wanted %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("line %d is %+v, wanted %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPackageKey(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "home", "runner", "work", "sysc-shell")
	cases := []struct {
		name string
		file string
		want string
		ok   bool
	}{
		{
			name: "a file inside the module names its directory",
			file: filepath.Join(root, "internal", "render", "paint.go"),
			want: "internal/render",
			ok:   true,
		},
		{
			name: "a relative file is read against the module root",
			file: filepath.Join("internal", "render", "paint.go"),
			want: "internal/render",
			ok:   true,
		},
		{
			name: "a file named by its import path loses the module prefix",
			file: "github.com/Nomadcxx/sysc-shell/internal/render/paint.go",
			want: "internal/render",
			ok:   true,
		},
		{
			name: "a file of the module itself is the root package",
			file: "github.com/Nomadcxx/sysc-shell/main.go",
			want: ".",
			ok:   true,
		},
		{
			name: "a file outside the module is declined",
			file: filepath.Join(string(filepath.Separator), "usr", "lib", "go", "src", "fmt", "print.go"),
			want: "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := packageKey(tc.file, root, "github.com/Nomadcxx/sysc-shell")
			if ok != tc.ok {
				t.Fatalf("packageKey(%q) ok is %v, wanted %v", tc.file, ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("packageKey(%q) is %q, wanted %q", tc.file, got, tc.want)
			}
		})
	}
}

func TestNormalizePackage(t *testing.T) {
	const module = "github.com/Nomadcxx/sysc-shell"
	cases := []struct {
		name  string
		entry string
		want  string
	}{
		{name: "an import path loses the module prefix", entry: module + "/internal/render", want: "internal/render"},
		{name: "a relative directory is kept", entry: "internal/render", want: "internal/render"},
		{name: "the module itself is the root package", entry: module, want: "."},
		{name: "a trailing slash is dropped", entry: "internal/render/", want: "internal/render"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizePackage(tc.entry, module); got != tc.want {
				t.Fatalf("normalizePackage(%q) is %q, wanted %q", tc.entry, got, tc.want)
			}
		})
	}
}

func TestFloorTo(t *testing.T) {
	cases := []struct {
		name    string
		percent float64
		want    float64
	}{
		{name: "a measured value keeps its floor below it", percent: 41.839, want: 41.83},
		{name: "a whole value stays whole", percent: 100, want: 100},
		{name: "nothing measured stays zero", percent: 0, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := floorTo(tc.percent); got != tc.want {
				t.Fatalf("floorTo(%v) is %v, wanted %v", tc.percent, got, tc.want)
			}
		})
	}
}

func TestJudge(t *testing.T) {
	cases := []struct {
		name     string
		floors   Floors
		measured map[string]coverage
		packages []string
		want     []finding
	}{
		{
			name: "a package under its floor is a finding",
			floors: Floors{Packages: map[string]float64{
				"internal/render": 50,
				"internal/shell":  40,
				"tools/wlspy":     28.2,
			}},
			measured: map[string]coverage{
				"internal/render": {total: 100, covered: 40},
				"internal/shell":  {total: 100, covered: 60},
				"tools/wlspy":     {total: 10, covered: 2},
			},
			packages: []string{"internal/render", "internal/shell", "tools/wlspy"},
			want: []finding{
				{pkg: "internal/render", why: reasonBelow, floor: 50, measured: 40},
				{pkg: "tools/wlspy", why: reasonBelow, floor: 28.2, measured: 20},
			},
		},
		{
			name:     "a package at its floor passes",
			floors:   Floors{Packages: map[string]float64{"internal/shell": 40}},
			measured: map[string]coverage{"internal/shell": {total: 100, covered: 60}},
			packages: []string{"internal/shell"},
			want:     nil,
		},
		{
			name:     "an excluded package is left alone",
			floors:   Floors{Exclusions: map[string]string{"tools/emojigen": "a one-shot generator with no runtime surface"}},
			measured: map[string]coverage{},
			packages: []string{"tools/emojigen"},
			want:     nil,
		},
		{
			name:     "a package without an entry is a finding",
			floors:   Floors{Packages: map[string]float64{}},
			measured: map[string]coverage{"internal/new": {total: 10, covered: 10}},
			packages: []string{"internal/new"},
			want:     []finding{{pkg: "internal/new", why: reasonNoEntry}},
		},
		{
			name:     "a package without tests needs an exclusion",
			floors:   Floors{Packages: map[string]float64{}},
			measured: map[string]coverage{},
			packages: []string{"tools/emojigen2"},
			want:     []finding{{pkg: "tools/emojigen2", why: reasonNoEntryRaw}},
		},
		{
			name:     "a package with a floor but no measurement is a finding",
			floors:   Floors{Packages: map[string]float64{"internal/quiet": 12}},
			measured: map[string]coverage{},
			packages: []string{"internal/quiet"},
			want:     []finding{{pkg: "internal/quiet", why: reasonNoCoverage, floor: 12}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := judge(tc.floors, tc.measured, tc.packages)
			if len(got) != len(tc.want) {
				t.Fatalf("judge returned %d findings, wanted %d: %v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("finding %d is %+v, wanted %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestJudgeNamesFloorAndMeasured(t *testing.T) {
	floors := Floors{Packages: map[string]float64{"internal/render": 50}}
	measured := map[string]coverage{"internal/render": {total: 100, covered: 42}}
	got := judge(floors, measured, []string{"internal/render"})
	if len(got) != 1 {
		t.Fatalf("judge returned %d findings, wanted 1", len(got))
	}
	line := got[0].String()
	for _, want := range []string{"internal/render", "50.00", "42.00"} {
		if !strings.Contains(line, want) {
			t.Fatalf("finding %q does not name %q", line, want)
		}
	}
}

func TestMeasuredFloorsKeepsExclusions(t *testing.T) {
	floors := Floors{
		Note:       "a note",
		Packages:   map[string]float64{"internal/old": 10},
		Exclusions: map[string]string{"tools/emojigen": "a one-shot generator"},
	}
	measured := map[string]coverage{
		"internal/render": {total: 3, covered: 2},
		"tools/empty":     {total: 0, covered: 0},
	}
	got := measuredFloors(floors, measured)
	if len(got.Packages) != 1 {
		t.Fatalf("measuredFloors kept %d packages, wanted 1", len(got.Packages))
	}
	if got.Packages["internal/render"] != 66.66 {
		t.Fatalf("internal/render floor is %v, wanted 66.66", got.Packages["internal/render"])
	}
	if got.Exclusions["tools/emojigen"] != "a one-shot generator" {
		t.Fatalf("the exclusion was not carried over: %v", got.Exclusions)
	}
	if got.Note != "a note" {
		t.Fatalf("the note was not carried over: %q", got.Note)
	}
}

func TestRunFailsWhenAPackageDropsBelowItsFloor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/shell\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(dir, "coverage.out")
	// A block that ran covers its whole statement count, so the package sits
	// at 100 of 250 statements.
	body := "internal/render/paint.go:1.1,2.2 150 0\ninternal/render/mask.go:1.1,2.2 100 1\ninternal/shell/panel.go:1.1,2.2 100 1\n"
	if err := os.WriteFile(profile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	floors := filepath.Join(dir, "floors.json")
	if err := os.WriteFile(floors, []byte(`{"packages":{"internal/render":50,"internal/shell":50}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := run([]string{"-profile", profile, "-floors", floors, "-module-root", dir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run returned %d, wanted 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"internal/render", "50.00", "40.00"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr %q does not name %q", stderr.String(), want)
		}
	}
}

func TestRunPassesWhenEveryPackageMeetsItsFloor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/shell\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(dir, "coverage.out")
	body := "internal/render/paint.go:1.1,2.2 100 1\n"
	if err := os.WriteFile(profile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	floors := filepath.Join(dir, "floors.json")
	if err := os.WriteFile(floors, []byte(`{"packages":{"internal/render":50}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := run([]string{"-profile", profile, "-floors", floors, "-module-root", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d, wanted 0\nstderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "at or above their floors") {
		t.Fatalf("stdout %q does not report the result", stdout.String())
	}
}

func TestRunUpdatesTheFloorFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/shell\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(dir, "coverage.out")
	body := "internal/render/paint.go:1.1,2.2 100 0\ninternal/render/mask.go:1.1,2.2 150 1\n"
	if err := os.WriteFile(profile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	floors := filepath.Join(dir, "floors.json")
	if err := os.WriteFile(floors, []byte(`{"note":"n","packages":{"internal/old":10},"exclusions":{"tools/emojigen":"a one-shot generator"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := run([]string{"-profile", profile, "-floors", floors, "-module-root", dir, "-update"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d, wanted 0\nstderr: %s", code, stderr.String())
	}
	got, err := loadFloors(floors)
	if err != nil {
		t.Fatal(err)
	}
	if got.Packages["internal/render"] != 60 {
		t.Fatalf("the written floor is %v, wanted 60", got.Packages["internal/render"])
	}
	if _, ok := got.Packages["internal/old"]; ok {
		t.Fatal("a package the profile did not measure kept its floor")
	}
	if got.Exclusions["tools/emojigen"] != "a one-shot generator" {
		t.Fatalf("the exclusion was not carried over: %v", got.Exclusions)
	}
}

func TestRunUpdatesWhenTheFloorFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/shell\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(dir, "coverage.out")
	body := "internal/render/paint.go:1.1,2.2 100 1\n"
	if err := os.WriteFile(profile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	floors := filepath.Join(dir, "floors.json")
	var stdout, stderr strings.Builder
	code := run([]string{"-profile", profile, "-floors", floors, "-module-root", dir, "-update"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d, wanted 0\nstderr: %s", code, stderr.String())
	}
	got, err := loadFloors(floors)
	if err != nil {
		t.Fatal(err)
	}
	if got.Packages["internal/render"] != 100 {
		t.Fatalf("the written floor is %v, wanted 100", got.Packages["internal/render"])
	}
	if got.Note == "" {
		t.Fatal("the written floor file carries no note")
	}
}

// A measurement that wobbles by less than the tolerance is not a regression.
func TestJudgeToleratesTheWobble(t *testing.T) {
	floors := Floors{Packages: map[string]float64{"internal/render": 50}}
	measured := map[string]coverage{
		"internal/render": {total: 10000, covered: 4996}, // 49.96, just under the floor
	}
	if got := judge(floors, measured, []string{"internal/render"}); len(got) != 0 {
		t.Fatalf("a drop of 0.04 points must pass, got %v", got)
	}
	measured["internal/render"] = coverage{total: 10000, covered: 4994} // 49.94
	if got := judge(floors, measured, []string{"internal/render"}); len(got) != 1 {
		t.Fatalf("a drop of 0.06 points must fail, got %v", got)
	}
}
