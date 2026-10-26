package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	clipboardclient "github.com/Nomadcxx/sysc-clipboard/client"
	"github.com/Nomadcxx/sysc-shell/internal/buildinfo"
	"github.com/Nomadcxx/sysc-shell/internal/ipc"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	"github.com/godbus/dbus/v5"
)

// doctorLevel is the verdict for one subsystem. A warning is a degraded but
// usable desktop; a failure is something the shell needs. The exit status is
// built from these, so the words in the table and the status a script reads
// cannot disagree.
type doctorLevel string

const (
	doctorOK   doctorLevel = "ok"
	doctorWarn doctorLevel = "warn"
	doctorFail doctorLevel = "fail"
)

// doctorCheck is one subsystem's report. Dependency is what the subsystem
// needs, Present says whether it was found, Version is what was found, Detail
// explains the finding, and Fix is the next action when the level is not ok.
// Nothing in here is user content: paths are redacted when printed.
type doctorCheck struct {
	Subsystem  string      `json:"subsystem"`
	Dependency string      `json:"dependency"`
	Present    bool        `json:"present"`
	Version    string      `json:"version"`
	Detail     string      `json:"detail"`
	Fix        string      `json:"fix,omitempty"`
	Level      doctorLevel `json:"status"`
}

// runDoctor implements `sysc-shell doctor`. It opens short-lived probe
// connections but starts no service and needs no shell instance, so it can be
// run from a terminal when the desktop is broken. Exit codes are a contract:
// 0 no failures, 1 one or more failures, 2 usage. With --strict a warning also
// exits 1.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	asJSON, strict := false, false
	for _, arg := range args {
		switch arg {
		case "--json":
			asJSON = true
		case "--strict":
			strict = true
		case "-h", "--help":
			fmt.Fprintln(stdout, "usage: sysc-shell doctor [--json] [--strict]")
			fmt.Fprintln(stdout, "reports the state of the compositor, Wayland protocols, daemon sockets, plugins, fonts, locale and D-Bus")
			return 0
		default:
			fmt.Fprintf(stderr, "doctor: unknown argument %q\n", arg)
			fmt.Fprintln(stderr, "usage: sysc-shell doctor [--json] [--strict]")
			return 2
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	checks := collectDoctor(ctx)
	redact := doctorRedactor()
	for i := range checks {
		checks[i].Subsystem = redact(checks[i].Subsystem)
		checks[i].Dependency = redact(checks[i].Dependency)
		checks[i].Version = redact(checks[i].Version)
		checks[i].Detail = redact(checks[i].Detail)
		checks[i].Fix = redact(checks[i].Fix)
	}

	if asJSON {
		doc := struct {
			Shell  string        `json:"shell"`
			Checks []doctorCheck `json:"checks"`
		}{Shell: buildinfo.Line(), Checks: checks}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			fmt.Fprintf(stderr, "doctor: encode report: %v\n", err)
			return 2
		}
		return doctorExit(checks, strict)
	}

	if err := printDoctor(stdout, checks); err != nil {
		fmt.Fprintf(stderr, "doctor: write report: %v\n", err)
		return 2
	}
	return doctorExit(checks, strict)
}

// doctorExit maps the report to a support-script status.
func doctorExit(checks []doctorCheck, strict bool) int {
	for _, check := range checks {
		if check.Level == doctorFail || (strict && check.Level == doctorWarn) {
			return 1
		}
	}
	return 0
}

func printDoctor(w io.Writer, checks []doctorCheck) error {
	if _, err := fmt.Fprintf(w, "%s doctor\n\n", buildinfo.Line()); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "STATUS\tSUBSYSTEM\tPRESENT\tDEPENDENCY\tVERSION\tDETAIL"); err != nil {
		return err
	}
	ok, warn, fail := 0, 0, 0
	for _, check := range checks {
		switch check.Level {
		case doctorOK:
			ok++
		case doctorWarn:
			warn++
		case doctorFail:
			fail++
		}
		present := "no"
		if check.Present {
			present = "yes"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			check.Level, check.Subsystem, present, check.Dependency, check.Version, check.Detail); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if ok+warn+fail > 0 {
		if _, err := fmt.Fprintf(w, "\n%d ok, %d warn, %d fail\n", ok, warn, fail); err != nil {
			return err
		}
	}
	var fixes []doctorCheck
	for _, check := range checks {
		if check.Level != doctorOK && check.Fix != "" {
			fixes = append(fixes, check)
		}
	}
	if len(fixes) > 0 {
		if _, err := fmt.Fprintln(w, "\nfixes:"); err != nil {
			return err
		}
		for _, check := range fixes {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", check.Subsystem, check.Fix); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectDoctor runs every probe. Each probe is bounded internally so one
// silent service cannot hold the whole report; the outer context is the final
// backstop.
func collectDoctor(ctx context.Context) []doctorCheck {
	checks := make([]doctorCheck, 0, 12)
	checks = append(checks, compositorCheck(ctx))
	checks = append(checks, waylandCheck(ctx))
	checks = append(checks, daemonChecks()...)
	checks = append(checks, pluginCheck())
	checks = append(checks, fontCheck())
	checks = append(checks, localeCheck())
	checks = append(checks, tzdataCheck())
	checks = append(checks, dbusCheck(ctx))
	return checks
}

func compositorCheck(ctx context.Context) doctorCheck {
	check := doctorCheck{Subsystem: "compositor", Dependency: "NIRI_SOCKET", Version: "n/a"}
	socket := os.Getenv("NIRI_SOCKET")
	if socket == "" {
		check.Level = doctorFail
		check.Detail = "NIRI_SOCKET is not set"
		check.Fix = "start sysc-shell from a Niri session; the compositor sets NIRI_SOCKET"
		return check
	}
	if err := inspectCompositorSocket(socket, uint32(os.Geteuid())); err != nil {
		check.Level = doctorFail
		check.Detail = "NIRI_SOCKET does not name a usable socket: " + err.Error()
		check.Fix = "restart the compositor, then start sysc-shell from that session"
		return check
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	windows, err := niri.GetWindows(probeCtx, socket)
	if err != nil {
		check.Level = doctorFail
		check.Detail = "the socket exists but Niri did not answer: " + err.Error()
		check.Fix = "check the compositor log and confirm NIRI_SOCKET belongs to the running Niri instance"
		return check
	}
	check.Present = true
	check.Level = doctorOK
	check.Detail = fmt.Sprintf("connected; %d windows", len(windows))
	return check
}

func waylandCheck(ctx context.Context) doctorCheck {
	check := doctorCheck{Subsystem: "wayland", Dependency: "WAYLAND_DISPLAY", Version: "n/a"}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		check.Level = doctorFail
		check.Detail = "WAYLAND_DISPLAY is not set"
		check.Fix = "start sysc-shell from the graphical session so the compositor's Wayland socket is exported"
		return check
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	report, err := wayland.ProbeProtocols(probeCtx)
	if err != nil {
		check.Level = doctorFail
		check.Detail = "could not read the compositor's registry: " + err.Error()
		check.Fix = "check that WAYLAND_DISPLAY names the running compositor's socket and that it accepts clients"
		return check
	}
	requiredFound := 0
	var required []string
	for _, protocol := range report.Protocols {
		if protocol.Required {
			requiredFound++
			required = append(required, fmt.Sprintf("%s v%d", protocol.Name, protocol.Version))
		}
	}
	requiredTotal := requiredFound + len(report.Missing)
	if len(report.Missing) > 0 {
		check.Level = doctorFail
		check.Detail = fmt.Sprintf("%d globals; missing required %s", report.Globals, strings.Join(report.Missing, ", "))
		check.Fix = "use a Niri release that implements the missing interfaces"
	} else {
		check.Present = true
		check.Level = doctorOK
		check.Detail = fmt.Sprintf("%d globals; %d/%d required interfaces present", report.Globals, requiredFound, requiredTotal)
	}
	check.Version = strings.Join(required, ", ")
	if check.Version == "" {
		check.Version = "n/a"
	}
	return check
}

// daemonChecks reports the shell's own socket and the three companion
// presenters. The shell's socket is required: if it is absent the shell is not
// running, which is the first thing a support report should say. The companion
// daemons degrade one feature each, so their absence is a warning.
func daemonChecks() []doctorCheck {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	uid := uint32(os.Geteuid())
	checks := make([]doctorCheck, 0, 4)
	checks = append(checks, daemonCheck("ipc", ipc.DefaultSocket(), "sysc-shell", true, runtimeDir, uid))
	checks = append(checks, daemonCheck("notify", notifyclient.SocketPath(runtimeDir), "sysc-notify.service", false, runtimeDir, uid))
	checks = append(checks, daemonCheck("tray", trayclient.SocketPath(runtimeDir), "sysc-tray.service", false, runtimeDir, uid))
	clipboard, err := clipboardclient.DefaultSocketPath()
	if err != nil {
		checks = append(checks, daemonCheck("clipboard", "", "sysc-clipboard.service", false, runtimeDir, uid))
	} else {
		checks = append(checks, daemonCheck("clipboard", clipboard, "sysc-clipboard.service", false, runtimeDir, uid))
	}
	return checks
}

func daemonCheck(name, path, unit string, required bool, runtimeDir string, uid uint32) doctorCheck {
	check := doctorCheck{Subsystem: name, Dependency: path, Version: "protocol 1"}
	level := doctorWarn
	if required {
		level = doctorFail
	}
	if runtimeDir == "" {
		check.Level = level
		check.Detail = "XDG_RUNTIME_DIR is not set, so no daemon socket can exist"
		check.Fix = "start sysc-shell from the graphical session; it exports XDG_RUNTIME_DIR"
		check.Dependency = "$XDG_RUNTIME_DIR"
		return check
	}
	if path == "" {
		check.Level = level
		check.Detail = "the socket path could not be derived"
		check.Fix = "start " + unit + " in the graphical session"
		return check
	}
	if err := inspectSocket(path, uid); err != nil {
		check.Level = level
		check.Detail = err.Error()
		check.Fix = "start it with: systemctl --user start " + unit
		return check
	}
	check.Present = true
	check.Level = doctorOK
	check.Detail = fmt.Sprintf("socket owned by uid %d", uid)
	return check
}

// inspectSocket checks the properties the companion clients themselves demand:
// a real socket, not a symlink, owned by this user and not reachable by other
// users.
func inspectSocket(path string, uid uint32) error {
	return inspectSocketLayout(path, uid, true)
}

// inspectCompositorSocket checks what a compositor's IPC socket promises: a
// real socket, not a symlink, owned by this user. Niri creates its IPC socket
// with the process umask rather than the 0600 layout the shell's own daemons
// use, so requiring that mode here would fail every real session.
func inspectCompositorSocket(path string, uid uint32) error {
	return inspectSocketLayout(path, uid, false)
}

func inspectSocketLayout(path string, uid uint32, private bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("socket not found")
		}
		return fmt.Errorf("inspect socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("the path is a symlink")
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("the path is not a Unix socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && stat.Uid != uid {
		return fmt.Errorf("the socket is owned by uid %d, want %d", stat.Uid, uid)
	}
	if private {
		if perm := info.Mode().Perm(); perm != 0o600 {
			return fmt.Errorf("the socket mode %04o is not 0600", perm)
		}
	}
	return nil
}

func pluginCheck() doctorCheck {
	check := doctorCheck{Subsystem: "plugins", Dependency: "Landlock", Version: "n/a"}
	abi := plugin.LandlockABI()
	check.Version = fmt.Sprintf("ABI %d", abi)
	catalog, err := plugin.Discover(plugin.DefaultRoots("/usr/share/sysc-shell/plugins")...)
	if err != nil {
		check.Level = doctorFail
		check.Detail = "could not scan plugin directories: " + err.Error()
		check.Fix = "check that the plugin directories are readable"
		return check
	}
	startable, rejected, missing := 0, 0, 0
	for _, candidate := range catalog.Plugins {
		switch {
		case candidate.Err != nil:
			rejected++
		case candidate.ShadowedBy != "":
			// A user copy intentionally overrides the managed one.
		case len(candidate.MissingCommands) != 0:
			missing++
		default:
			startable++
		}
	}
	check.Detail = fmt.Sprintf("%d plugins, %d startable, %d rejected, %d missing commands",
		len(catalog.Plugins), startable, rejected, missing)
	switch {
	case abi < 1:
		check.Level = doctorFail
		check.Detail += "; this kernel offers no Landlock confinement"
		check.Fix = "run a kernel with Landlock enabled; without it plugins run with the user's full file access"
	case abi < 6:
		check.Level = doctorWarn
		check.Detail += "; files are confined, process scopes are not"
		check.Fix = "Landlock ABI 6 or newer confines abstract sockets and signals; update the kernel"
	case abi < 9:
		check.Level = doctorWarn
		check.Detail += "; files and scopes are confined, pathname sockets are not"
		check.Fix = "Landlock ABI 9 or newer confines pathname sockets; update the kernel"
	case rejected > 0 || missing > 0:
		check.Level = doctorWarn
		check.Fix = "open Settings > Plugins to review the rejected plugins and their missing dependencies"
	default:
		check.Level = doctorOK
	}
	check.Present = abi >= 1
	return check
}

func fontCheck() doctorCheck {
	check := doctorCheck{Subsystem: "fonts", Dependency: "fontconfig", Version: "n/a"}
	// fontscan logs fontconfig misses with absolute paths under $HOME. A
	// doctor report is meant to be pasted into an issue, so keep its scan out
	// of the report's stderr.
	previous := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(previous)
	fontMap, err := render.NewSystemFontMap("", render.DefaultFontCacheDir())
	if err != nil {
		check.Level = doctorFail
		check.Detail = "no usable system fonts: " + err.Error()
		check.Fix = "install a text font package and fontconfig, then run fc-cache"
		return check
	}
	if fontMap.Primary() == nil {
		check.Level = doctorFail
		check.Detail = "fontconfig found no primary face"
		check.Fix = "install a text font package and run fc-cache"
		return check
	}
	check.Present = true
	check.Level = doctorOK
	if family := fontMap.Primary().Describe().Family; family != "" {
		check.Version = family
	}
	check.Detail = "primary face resolved; fallback glyphs available"
	return check
}

func localeCheck() doctorCheck {
	check := doctorCheck{Subsystem: "locale", Dependency: "LC_ALL/LC_MESSAGES/LANG", Version: "unset"}
	value := firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"))
	if value == "" {
		check.Level = doctorWarn
		check.Detail = "no locale is set; the shell falls back to English"
		check.Fix = "set LANG to a UTF-8 locale, for example en_US.UTF-8"
		return check
	}
	check.Version = value
	check.Present = true
	if !strings.Contains(strings.ToUpper(value), "UTF-8") && !strings.Contains(strings.ToUpper(value), "UTF8") {
		check.Level = doctorWarn
		check.Detail = "the locale is not UTF-8; non-ASCII labels and text may not render"
		check.Fix = "set LANG to a UTF-8 locale, for example en_US.UTF-8"
		return check
	}
	check.Level = doctorOK
	check.Detail = "UTF-8 locale"
	return check
}

func tzdataCheck() doctorCheck {
	check := doctorCheck{Subsystem: "tzdata", Dependency: "IANA zone database", Version: zoneDataVersion()}
	location, err := time.LoadLocation("Local")
	if err != nil {
		check.Level = doctorFail
		check.Detail = "the local time zone could not be loaded: " + err.Error()
		check.Fix = "set TZ to a valid zone name, or update the tzdata package"
		return check
	}
	check.Present = true
	check.Level = doctorOK
	check.Detail = "local zone " + location.String()
	return check
}

// zoneDataVersion names the system zone database when one is installed. The
// binary also embeds a fallback, so a missing file is not a failure.
func zoneDataVersion() string {
	data, err := os.ReadFile("/usr/share/zoneinfo/tzdata.zi")
	if err != nil {
		return "embedded"
	}
	line, _, _ := strings.Cut(string(data), "\n")
	line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
	if line == "" {
		return "embedded"
	}
	return line
}

func dbusCheck(ctx context.Context) doctorCheck {
	check := doctorCheck{Subsystem: "dbus", Dependency: "session and system buses", Version: "n/a"}
	session, sessionErr := dbus.ConnectSessionBus()
	if sessionErr == nil {
		defer session.Close()
	}
	system, systemErr := dbus.ConnectSystemBus()
	if systemErr == nil {
		defer system.Close()
	}

	var details []string
	present := 0
	if sessionErr != nil {
		details = append(details, "session bus unavailable")
	} else {
		present++
		details = append(details, "session bus up")
		if hasOwner(ctx, session, "org.freedesktop.Notifications") {
			details = append(details, "notifications yes")
		} else {
			details = append(details, "notifications no")
		}
		if hasOwner(ctx, session, "org.freedesktop.portal.Desktop") {
			details = append(details, "portal yes")
		} else {
			details = append(details, "portal no")
		}
	}
	if systemErr != nil {
		details = append(details, "system bus unavailable")
	} else {
		present++
		details = append(details, "system bus up")
		if hasOwner(ctx, system, "org.freedesktop.PolicyKit1") {
			details = append(details, "polkit yes")
		} else {
			details = append(details, "polkit no")
		}
	}
	check.Detail = strings.Join(details, "; ")
	check.Present = present > 0
	switch {
	case systemErr != nil:
		check.Level = doctorFail
		check.Fix = "start the system bus; polkit prompts need org.freedesktop.PolicyKit1"
	case sessionErr != nil:
		check.Level = doctorWarn
		check.Fix = "start the session bus inside the graphical session"
	default:
		check.Level = doctorOK
	}
	return check
}

func hasOwner(ctx context.Context, conn *dbus.Conn, name string) bool {
	var owned bool
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned); err != nil {
		return false
	}
	return owned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// doctorRedactor removes the home and runtime-directory prefixes from every
// printed field, so a report can be pasted into an issue without exposing the
// user's directory layout.
func doctorRedactor() func(string) string {
	home, _ := os.UserHomeDir()
	return doctorRedactorFor(home, os.Getenv("XDG_RUNTIME_DIR"))
}

func doctorRedactorFor(home, runtimeDir string) func(string) string {
	replacements := make([]struct{ prefix, text string }, 0, 2)
	for _, pair := range []struct{ prefix, text string }{
		{runtimeDir, "$XDG_RUNTIME_DIR"},
		{home, "~"},
	} {
		prefix := strings.TrimSuffix(pair.prefix, string(os.PathSeparator))
		if prefix == "" || prefix == string(os.PathSeparator) {
			continue
		}
		replacements = append(replacements, struct{ prefix, text string }{prefix, pair.text})
	}
	// Longest first so a runtime directory inside home still becomes the more
	// specific placeholder.
	sort.Slice(replacements, func(i, j int) bool {
		return len(replacements[i].prefix) > len(replacements[j].prefix)
	})
	return func(value string) string {
		for _, replacement := range replacements {
			value = strings.ReplaceAll(value, replacement.prefix, replacement.text)
		}
		return value
	}
}
