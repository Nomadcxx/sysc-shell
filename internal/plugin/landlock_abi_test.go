package plugin

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

// TestLandlockABIRecorded asserts and records the kernel's Landlock ABI so
// CI reports it where the confinement tests would otherwise skip out of
// sight. The ABI decides what can run: filesystem confinement needs 1,
// socket scoping needs scopeABI, and resolved unix socket paths need
// resolveUnixABI. A runner below those levels is a finding to file, not
// grounds for a job that is always red; the assertion fails only when the
// kernel offers no Landlock at all. SYSC_REQUIRE_LANDLOCK=<n> raises the
// floor to what a runner is known to clear.
func TestLandlockABIRecorded(t *testing.T) {
	abi := LandlockABI()
	t.Logf("landlock ABI %d (filesystem confinement: 1, socket scoping: %d, unix socket paths: %d)",
		abi, scopeABI, resolveUnixABI)

	summary := fmt.Sprintf("Landlock ABI on this runner: %d\n", abi)
	if abi < scopeABI {
		summary += fmt.Sprintf("- unrun: `TestAConfinedPluginIsIsolatedFromAbstractSocketsAndOtherProcesses` needs ABI %d\n", scopeABI)
	}
	if abi < resolveUnixABI {
		summary += fmt.Sprintf("- unrun: `TestAConfinedPluginReachesOnlyTheSocketsItDeclared` needs ABI %d\n", resolveUnixABI)
	}
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("open step summary: %v", err)
		}
		defer f.Close()
		if _, err := f.WriteString(summary + "\n"); err != nil {
			t.Fatalf("write step summary: %v", err)
		}
	}

	required := 1
	if raw := os.Getenv("SYSC_REQUIRE_LANDLOCK"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("SYSC_REQUIRE_LANDLOCK: %v", err)
		}
		required = n
	}
	if abi < required {
		t.Fatalf("landlock ABI %d is below the required %d; file a finding for the runner kernel", abi, required)
	}
	if required == 1 && abi < resolveUnixABI && os.Getenv("GITHUB_ACTIONS") != "" {
		fmt.Printf("::warning title=Landlock ABI gap::runner kernel offers ABI %d; the socket confinement tests need %d and are recorded unrun. File a finding.\n", abi, resolveUnixABI)
	}
}
