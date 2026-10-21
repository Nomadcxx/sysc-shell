package shell

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The mono role's verdict rests on a scan of the system fonts, and the first
// scan in a process costs real time. Every theme resolution runs under the
// registry lock, so a scan that happened there would stall every other
// registry user for the length of the scan.
//
// The property is about the first probe in a process, so the check runs in a
// child process: a child that runs this test alone has no other caller, and
// the first probe is the one this test controls.
const fontScanProbeEnv = "SYSC_FONTSCAN_PROBE"

func TestFontScanDoesNotRunUnderTheRegistryLock(t *testing.T) {
	if os.Getenv(fontScanProbeEnv) == "1" {
		fontScanProbeChild(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFontScanDoesNotRunUnderTheRegistryLock$")
	cmd.Env = append(os.Environ(), fontScanProbeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the font scan probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "fontscan-live=false") {
		t.Fatalf("the first font scan ran while the registry was live, so it ran under the registry lock:\n%s", out)
	}
}

// fontScanProbeChild reports whether the first monospace probe in this
// process ran before or after the registry existed.
func fontScanProbeChild(t *testing.T) {
	live := false
	firstLive := ""
	monospaceFamily.Swap(func(family, fallback string) string {
		if firstLive == "" {
			if live {
				firstLive = "true"
			} else {
				firstLive = "false"
			}
		}
		return fallback
	})
	reg := newPanelRegistry(t)
	live = true
	// A theme resolution runs under the registry lock, the way the OSD, the
	// toast host and the panel hosts resolve their theme.
	reg.mu.Lock()
	_ = reg.panelTheme()
	reg.mu.Unlock()
	if firstLive == "" {
		t.Fatal("the monospace probe was never called")
	}
	fmt.Printf("fontscan-live=%s\n", firstLive)
}
