package theming

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

var procRoot = "/proc"

type applyJob struct {
	home    string
	enabled func(string) bool
	tok     theme.Tokens
	// terminalOpacity is the percentage written into each enabled terminal's
	// config; 100 leaves the terminals' own settings alone.
	terminalOpacity int
	done            chan applyResult
}

var (
	applyMu     sync.Mutex
	applyBusy   bool
	applyQueued *applyJob
)

var ErrApplySuperseded = errors.New("theming: queued apply superseded by a newer apply")

type applyResult struct {
	outcomes map[string]error
	results  map[string]error
	adopted  []string
	err      error
}

// ApplyEnabled renders every enabled template for home. It returns the
// per-template failures keyed by template name, the templates it adopted over
// the user's own setting (their file backed up once to <file>.bak), and the
// first error overall. While another apply runs, the latest queued job
// replaces its predecessor and this call returns nil outcomes; use
// ApplyEnabledAndWait when this caller needs results.
//
// An enabled template is always applied (owner decision, 2026-10-09: the
// shell themes everything it lists as themeable). The guarded write runs
// first; only a refusal over the user's own edit falls through to the
// adopting write, so a file the shell already owns is never backed up again.
func ApplyEnabled(home string, enabled func(string) bool, tok theme.Tokens, terminalOpacity int) (map[string]error, []string, error) {
	if home == "" || enabled == nil {
		return nil, nil, nil
	}
	job := applyJob{home: home, enabled: enabled, tok: tok, terminalOpacity: terminalOpacity}
	applyMu.Lock()
	if applyBusy {
		queueApplyLocked(job)
		applyMu.Unlock()
		return nil, nil, nil
	}
	applyBusy = true
	applyMu.Unlock()
	result := runApply(job)
	return result.outcomes, result.adopted, result.err
}

// ApplyEnabledAndWait returns outcomes for templates actually attempted, with
// nil values for success, and the templates adopted, even when another apply
// is already running. As with ApplyEnabled, only the newest queued job runs; a
// queued request replaced before it starts returns ErrApplySuperseded.
func ApplyEnabledAndWait(home string, enabled func(string) bool, tok theme.Tokens, terminalOpacity int) (map[string]error, []string, error) {
	if home == "" || enabled == nil {
		return nil, nil, nil
	}
	job := applyJob{home: home, enabled: enabled, tok: tok, terminalOpacity: terminalOpacity, done: make(chan applyResult, 1)}
	applyMu.Lock()
	if applyBusy {
		queueApplyLocked(job)
		applyMu.Unlock()
		result := <-job.done
		return result.results, result.adopted, result.err
	}
	applyBusy = true
	applyMu.Unlock()
	go runApply(job)
	result := <-job.done
	return result.results, result.adopted, result.err
}

func queueApplyLocked(job applyJob) {
	if applyQueued != nil && applyQueued.done != nil {
		finishApplyWaiter(applyQueued.done, applyResult{err: ErrApplySuperseded})
	}
	applyQueued = &job
}

func finishApplyWaiter(done chan applyResult, result applyResult) {
	if done == nil {
		return
	}
	done <- result
	close(done)
}

func runApply(job applyJob) applyResult {
	current := job
	var result applyResult
	for {
		result.results, result.adopted, result.err = applyOnce(current.home, current.enabled, current.tok, current.terminalOpacity)
		result.outcomes = applyFailures(result.results)
		finishApplyWaiter(current.done, result)
		applyMu.Lock()
		if applyQueued == nil {
			applyBusy = false
			applyMu.Unlock()
			return result
		}
		current = *applyQueued
		applyQueued = nil
		applyMu.Unlock()
	}
}

func applyFailures(results map[string]error) map[string]error {
	failures := make(map[string]error)
	for name, err := range results {
		if err != nil {
			failures[name] = err
		}
	}
	return failures
}

func applyOnce(home string, enabled func(string) bool, tok theme.Tokens, terminalOpacity int) (map[string]error, []string, error) {
	// D6: a template body in $XDG_CONFIG_HOME/sysc-shell/theming-templates
	// replaces the embedded one for its name; the write targets, the
	// Complete() gate and the user-modified guard are unchanged.
	cat := Catalog().WithOverlay()
	results := map[string]error{}
	var first error
	record := func(name string, err error) {
		results[name] = err
		if err != nil && first == nil {
			first = err
		}
	}
	var adopted []string
	// adopt runs the guarded write, and over a refusal caused by the user's
	// own edit, the adopting one. A failure of the adopting write is reported
	// as itself.
	adopt := func(name string, apply func(force bool) error) error {
		err := apply(false)
		if !errors.Is(err, ErrUserModified) {
			return err
		}
		if err := apply(true); err != nil {
			return err
		}
		adopted = append(adopted, name)
		return nil
	}
	for _, name := range cat.Names() {
		rendered := Render(cat.Template(name), tok)
		// GH #7: a stub template rendered onto a live app config neuters the
		// app. An incomplete template behaves as off everywhere, which also
		// removes a stub written by an older release.
		on := enabled(name) && Complete(name)
		// D6: an overlay body that does not parse renders empty, and an empty
		// render must never replace a live app config. Report and skip.
		if on && rendered == "" {
			record(name, fmt.Errorf("theming: %s renders empty", name))
			continue
		}
		switch name {
		case "niri":
			cfg := filepath.Join(home, ".config", "niri", "config.kdl")
			gen := filepath.Join(home, ".config", "niri", "sysc-shell.kdl")
			if !on {
				record(name, UnapplyNiri(cfg, gen))
				continue
			}
			if _, err := os.Stat(cfg); err != nil {
				continue
			}
			record(name, adopt(name, func(force bool) error { return applyNiri(cfg, gen, rendered, force) }))
		default:
			if !on {
				// A disabled template has nothing to theme; its guard keeps
				// a line the shell did not write.
				record(name, applyTemplateTarget(name, home, false, rendered, false, terminalOpacity))
				continue
			}
			record(name, adopt(name, func(force bool) error {
				return applyTemplateTarget(name, home, true, rendered, force, terminalOpacity)
			}))
		}
	}
	return results, adopted, first
}

func writeTarget(home, name string) string {
	switch name {
	case "alacritty":
		return filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	case "foot":
		return filepath.Join(home, ".config", "foot", "foot.ini")
	case "ghostty":
		return filepath.Join(home, ".config", "ghostty", "config")
	case "kitty":
		return filepath.Join(home, ".config", "kitty", "kitty.conf")
	case "wezterm":
		return filepath.Join(home, ".config", "wezterm", "wezterm.lua")
	default:
		return ""
	}
}

// procPIDs lists the processes under root whose comm is name.
func procPIDs(root, name string) []int {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(b)) == name {
			pids = append(pids, pid)
		}
	}
	return pids
}

// signalComm sends sig to every process named name.
func signalComm(root, name string, sig syscall.Signal) error {
	var first error
	for _, pid := range procPIDs(root, name) {
		p, err := os.FindProcess(pid)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := p.Signal(sig); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func signalKitty(root string) error { return signalComm(root, "kitty", syscall.SIGUSR1) }

// signalGhostty asks ghostty to reload its config. ghostty 1.2+ handles
// SIGUSR2; 1.3.1 was checked to log the reload and keep running.
func signalGhostty(root string) error { return signalComm(root, "ghostty", syscall.SIGUSR2) }

// templateTarget is the sidecar+directive model for one application: we own
// a generated file, and manage exactly one include-ish line in the user's
// real config. Names not in the table still use the guarded whole-file path
// until their port task adds a row.
//
// ponytail: directive matching uses the target's key prefix rather than an
// app-config parser; add format-aware parsing only when a supported target
// needs syntax beyond one managed line in a named section.
// block applications get no sidecar: the rendered body is managed in place
// between marker lines inside the user's own config file.
type templateTarget struct {
	sidecar    func(home string) []string
	directives func(home string) []directive
	signal     func(root string) error
	block      func(home string) (file, open, close string)
	// opacity builds the terminal's owned opacity lines for a value such as
	// "0.85". Nil for applications without a background opacity.
	opacity func(home, value string) []valuedLine
}

// targetFiles hashes every file the target writes, keyed by path; a missing
// file hashes as empty.
func targetFiles(tgt templateTarget, home string) map[string]string {
	paths := append([]string(nil), tgt.sidecar(home)...)
	for _, d := range tgt.directives(home) {
		paths = append(paths, d.file)
	}
	if tgt.opacity != nil {
		for _, v := range tgt.opacity(home, "") {
			paths = append(paths, v.file)
		}
	}
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			out[p] = hash(b)
		} else {
			out[p] = ""
		}
	}
	return out
}

// opacityValue is a percentage as the fraction terminals read.
func opacityValue(percent int) string {
	return fmt.Sprintf("%.2f", float64(percent)/100)
}

const (
	blockOpen  = "# >>> sysc-shell >>>"
	blockClose = "# <<< sysc-shell <<<"
)

func joined(home string, elems ...string) string {
	return filepath.Join(append([]string{home}, elems...)...)
}

var templateTargets = map[string]templateTarget{
	"alacritty": {
		sidecar: func(h string) []string {
			return []string{joined(h, ".config", "alacritty", "themes", "sysc-shell.toml")}
		},
		directives: func(h string) []directive {
			p := joined(h, ".config", "alacritty", "themes", "sysc-shell.toml")
			return []directive{{
				file:    joined(h, ".config", "alacritty", "alacritty.toml"),
				line:    `import = ["` + p + `"]`,
				key:     "import",
				section: "general",
				seed:    "[general]\n" + `import = ["` + p + `"]` + "\n",
				create:  true,
			}}
		},
		opacity: func(h, v string) []valuedLine {
			return []valuedLine{{file: joined(h, ".config", "alacritty", "alacritty.toml"), key: "opacity=", line: "opacity = " + v, section: "window"}}
		},
	},
	"foot": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "foot", "themes", "sysc-shell")} },
		directives: func(h string) []directive {
			// foot holds many includes. One into its themes directory is a
			// competing theme; any other is the user's own and stays.
			line := "include=~/.config/foot/themes/sysc-shell"
			return []directive{{
				file:    joined(h, ".config", "foot", "foot.ini"),
				line:    line,
				key:     "include=",
				themes:  "foot/themes/",
				section: "main",
				seed:    "[main]\n" + line + "\n",
				create:  true,
			}}
		},
		// foot 1.28 reads alpha in the [colors-dark] section the template writes.
		opacity: func(h, v string) []valuedLine {
			return []valuedLine{{file: joined(h, ".config", "foot", "foot.ini"), key: "alpha=", line: "alpha=" + v, section: "colors-dark"}}
		},
	},
	"ghostty": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "ghostty", "themes", "sysc-shell")} },
		directives: func(h string) []directive {
			return []directive{{
				file:   joined(h, ".config", "ghostty", "config"),
				line:   "theme = sysc-shell",
				key:    "theme",
				create: true,
			}}
		},
		// The "=" suffix keeps background-opacity-cells out of the match.
		opacity: func(h, v string) []valuedLine {
			return []valuedLine{{file: joined(h, ".config", "ghostty", "config"), key: "background-opacity=", line: "background-opacity = " + v}}
		},
		signal: signalGhostty,
	},
	"kitty": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "kitty", "themes", "sysc-shell.conf")} },
		directives: func(h string) []directive {
			// kitty configs hold many includes; only one naming our own file is
			// ours or a conflicting copy of it. A bare "include" key refused to
			// wire the theme beside every ordinary tab or colour include.
			return []directive{{
				file:   joined(h, ".config", "kitty", "kitty.conf"),
				line:   "include themes/sysc-shell.conf",
				key:    "include themes/sysc-shell.conf",
				create: true,
			}}
		},
		signal: signalKitty,
		opacity: func(h, v string) []valuedLine {
			// kitty applies a reloaded background_opacity only to windows
			// started with dynamic_background_opacity on, so the shell turns
			// it on while it manages the opacity.
			conf := joined(h, ".config", "kitty", "kitty.conf")
			return []valuedLine{
				{file: conf, key: "background_opacity", line: "background_opacity " + v},
				{file: conf, key: "dynamic_background_opacity", line: "dynamic_background_opacity yes"},
			}
		},
	},
	"helix": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "helix", "themes", "sysc-shell.toml")} },
		directives: func(h string) []directive {
			return []directive{{
				file: joined(h, ".config", "helix", "config.toml"),
				line: `theme = "sysc-shell"`,
				key:  "theme",
				top:  true,
			}}
		},
	},
	"cava": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "cava", "themes", "sysc-shell")} },
		directives: func(h string) []directive {
			return []directive{{
				file:    joined(h, ".config", "cava", "config"),
				line:    `theme = "sysc-shell"`,
				key:     "theme",
				section: "color",
			}}
		},
	},
	"btop": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "btop", "themes", "sysc-shell.theme")} },
		directives: func(h string) []directive {
			return []directive{{
				file: joined(h, ".config", "btop", "btop.conf"),
				line: `color_theme = "sysc-shell"`,
				key:  "color_theme",
			}}
		},
	},
	"kcolorscheme": {
		sidecar: func(h string) []string {
			return []string{joined(h, ".local", "share", "color-schemes", "sysc-shell.colors")}
		},
		directives: func(h string) []directive {
			return []directive{{
				file:    joined(h, ".config", "kdeglobals"),
				line:    "ColorSchemeName=sysc-shell",
				key:     "ColorSchemeName",
				section: "General",
				seed:    "[General]\nColorSchemeName=sysc-shell\n",
				create:  true,
			}}
		},
	},
	"starship": {
		block: func(h string) (string, string, string) {
			return joined(h, ".config", "starship.toml"), blockOpen, blockClose
		},
		directives: func(h string) []directive {
			return []directive{{
				file:   joined(h, ".config", "starship.toml"),
				line:   `palette = "sysc-shell"`,
				key:    "palette",
				top:    true,
				create: true,
			}}
		},
	},
	"qt": {
		sidecar: func(h string) []string {
			return []string{
				joined(h, ".config", "qt5ct", "colors", "sysc-shell.conf"),
				joined(h, ".config", "qt6ct", "colors", "sysc-shell.conf"),
			}
		},
		directives: func(h string) []directive {
			mk := func(ct, path string) directive {
				return directive{
					file:    joined(h, ".config", ct, ct+".conf"),
					line:    "color_scheme_path=" + path,
					key:     "color_scheme_path",
					section: "General",
				}
			}
			return []directive{
				mk("qt5ct", joined(h, ".config", "qt5ct", "colors", "sysc-shell.conf")),
				mk("qt6ct", joined(h, ".config", "qt6ct", "colors", "sysc-shell.conf")),
			}
		},
	},
	"wezterm": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "wezterm", "colors", "sysc-shell.toml")} },
		directives: func(h string) []directive {
			return []directive{{
				file:         joined(h, ".config", "wezterm", "wezterm.lua"),
				line:         `config.color_scheme = "sysc-shell"`,
				key:          "config.color_scheme",
				returnConfig: true,
			}}
		},
		opacity: func(h, v string) []valuedLine {
			return []valuedLine{{
				file: joined(h, ".config", "wezterm", "wezterm.lua"), key: "config.window_background_opacity",
				line: "config.window_background_opacity = " + v, returnConfig: true,
			}}
		},
	},
}

// applyTemplateTarget is the per-app dispatch: sidecar+directive for table
// members, and a guarded whole-file write for the rest.
func applyTemplateTarget(name, home string, on bool, rendered string, force bool, terminalOpacity int) error {
	tgt, known := templateTargets[name]
	if !known {
		target := writeTarget(home, name)
		if target == "" {
			return nil
		}
		if on {
			return applyWrite(target, rendered, force)
		}
		return UnapplyWrite(target)
	}
	if tgt.block != nil {
		file, open, close := tgt.block(home)
		if on {
			if err := ManageBlock(file, open, close, rendered, force); err != nil {
				return err
			}
			for _, dir := range tgt.directives(home) {
				if err := ensureDirective(dir, force); err != nil {
					return err
				}
			}
			return nil
		}
		for _, dir := range tgt.directives(home) {
			if err := RemoveDirective(dir); err != nil {
				return err
			}
		}
		return RemoveBlock(file, open, close, force)
	}
	sidecars := tgt.sidecar(home)
	if on {
		before := targetFiles(tgt, home)
		for _, sidecar := range sidecars {
			if err := applySidecar(sidecar, rendered, force); err != nil {
				return err
			}
		}
		for _, dir := range tgt.directives(home) {
			if err := ensureDirective(dir, force); err != nil {
				return err
			}
		}
		var opacityErr error
		if tgt.opacity != nil {
			for _, v := range tgt.opacity(home, opacityValue(terminalOpacity)) {
				// Zero is a config that never set the field, not a request for
				// invisible terminals; it is unmanaged like 100.
				if terminalOpacity <= 0 || terminalOpacity >= 100 {
					opacityErr = errors.Join(opacityErr, removeValuedLine(v))
					continue
				}
				// Stop at a refusal: the adopting pass backs the file up,
				// and it must not hold a line this pass already wrote.
				if opacityErr = ensureValuedLine(v, force); opacityErr != nil {
					break
				}
			}
		}
		// A guarded pass refused over the user's own opacity line is retried
		// as an adopting write, which sends the reload; sending one here too
		// would announce two.
		if !force && errors.Is(opacityErr, ErrUserModified) {
			return opacityErr
		}
		// The colours above are written whatever the opacity step says, so a
		// failed opacity write must not keep them from the running terminal.
		// An apply that changed nothing sends no reload: every shell config
		// reload runs one, and ghostty announces each reload it is sent.
		var signalErr error
		if tgt.signal != nil && !maps.Equal(before, targetFiles(tgt, home)) {
			signalErr = tgt.signal(procRoot)
		}
		return errors.Join(opacityErr, signalErr)
	}
	if tgt.opacity != nil {
		// Removal matches the recorded line, so the value is irrelevant here.
		for _, v := range tgt.opacity(home, "") {
			if err := removeValuedLine(v); err != nil {
				return err
			}
		}
	}
	for _, dir := range tgt.directives(home) {
		if err := RemoveDirective(dir); err != nil {
			return err
		}
	}
	for _, sidecar := range sidecars {
		if err := UnapplyWrite(sidecar); err != nil {
			return err
		}
	}
	return nil
}

// Backups lists the <file>.bak copies that exist for a template's targets:
// where the user's own file went when the template was adopted over it.
func Backups(home, name string) []string {
	files := []string{writeTarget(home, name)}
	if name == "niri" {
		files = append(files, filepath.Join(home, ".config", "niri", "config.kdl"))
	}
	if tgt, ok := templateTargets[name]; ok {
		if tgt.sidecar != nil {
			files = append(files, tgt.sidecar(home)...)
		}
		if tgt.directives != nil {
			for _, d := range tgt.directives(home) {
				files = append(files, d.file)
			}
		}
		if tgt.block != nil {
			file, _, _ := tgt.block(home)
			files = append(files, file)
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range files {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		if _, err := os.Stat(f + ".bak"); err == nil {
			out = append(out, f+".bak")
		}
	}
	return out
}
