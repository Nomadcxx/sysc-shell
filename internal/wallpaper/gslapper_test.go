package wallpaper

import (
	"slices"
	"strings"
	"testing"
)

func TestHelpSupports(t *testing.T) {
	modern := []byte("--ipc-socket PATH  --transition-type TYPE  --cache-size MB")
	if !helpSupports(modern) {
		t.Error("gSlapper 1.5 help must probe as supported")
	}
	old := []byte("--ipc-socket PATH  --transition-type TYPE")
	if helpSupports(old) {
		t.Error("help without --cache-size is a pre-1.5 build")
	}
	if helpSupports(nil) {
		t.Error("empty help must not probe as supported")
	}
}

func defaultSettings() Settings {
	return Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone}
}

// argAfter returns the value following flag, or "" when the flag is absent.
func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func TestLaunchArgs(t *testing.T) {
	const socket = "/run/user/1000/sysc-shell/gslapper-DP-1.sock"
	args := launchArgs(defaultSettings(), socket, "DP-1", "/tmp/a.mp4")

	if argAfter(args, "-I") != socket {
		t.Errorf("-I = %q, want the owned socket", argAfter(args, "-I"))
	}
	if !slices.Contains(args, "--no-save-state") {
		t.Error("--no-save-state keeps gSlapper from restoring a wallpaper we did not assign")
	}
	if got := argAfter(args, "-o"); got != "fill no-audio loop" {
		t.Errorf("-o = %q, want scale, no-audio, and loop", got)
	}
	if got := argAfter(args, "-r"); got != "30" {
		t.Errorf("-r = %q, want the short form with 30", got)
	}
	if slices.Contains(args, "--fps-cap") {
		t.Error("the tests pin the short -r form")
	}
	// The connector and the path are positional and last, in that order.
	if len(args) < 2 || args[len(args)-2] != "DP-1" || args[len(args)-1] != "/tmp/a.mp4" {
		t.Errorf("tail = %v, want the connector then the path", args[len(args)-2:])
	}
	if slices.Contains(args, "*") {
		t.Error("all-outputs is a fan-out, never gSlapper's wildcard (D14)")
	}
}

func TestLaunchArgsHidden(t *testing.T) {
	cases := []struct {
		hidden string
		want   string
	}{
		{HiddenNone, ""},
		{HiddenAutoPause, "--auto-pause"},
		{HiddenAutoStop, "--auto-stop"},
	}
	for _, c := range cases {
		set := defaultSettings()
		set.Hidden = c.hidden
		args := launchArgs(set, "/s.sock", "DP-1", "/tmp/a.mp4")
		pause := slices.Contains(args, "--auto-pause")
		stop := slices.Contains(args, "--auto-stop")
		if pause && stop {
			t.Fatalf("hidden %q emitted each flag; they are exclusive", c.hidden)
		}
		if c.want == "" && (pause || stop) {
			t.Errorf("hidden none emitted %v", args)
		}
		if c.want != "" && !slices.Contains(args, c.want) {
			t.Errorf("hidden %q did not emit %s", c.hidden, c.want)
		}
	}
}

func TestLaunchArgsFade(t *testing.T) {
	off := launchArgs(defaultSettings(), "/s.sock", "DP-1", "/tmp/a.png")
	if slices.Contains(off, "--transition-type") || slices.Contains(off, "--transition-duration") {
		t.Error("fade off must emit no transition flags; gSlapper already defaults to none")
	}
	set := defaultSettings()
	set.Fade, set.FadeDuration = true, 0.5
	on := launchArgs(set, "/s.sock", "DP-1", "/tmp/a.png")
	if argAfter(on, "--transition-type") != "fade" {
		t.Errorf("--transition-type = %q", argAfter(on, "--transition-type"))
	}
	if argAfter(on, "--transition-duration") != "0.5" {
		t.Errorf("--transition-duration = %q", argAfter(on, "--transition-duration"))
	}
	if slices.Contains(on, "--fade") {
		t.Error("there is no --fade flag on the binary")
	}
}

func TestLaunchArgsNoLoop(t *testing.T) {
	set := defaultSettings()
	set.Loop = false
	if got := argAfter(launchArgs(set, "/s.sock", "DP-1", "/a.mp4"), "-o"); strings.Contains(got, "loop") {
		t.Errorf("-o = %q, want no loop", got)
	}
}

func TestVideoChangeNeedsRestart(t *testing.T) {
	// gSlapper documents --auto-stop as required for video IPC changes, so the
	// hidden setting decides whether a video swap is a `change` or a relaunch.
	if !videoChangeNeedsRestart(HiddenNone) {
		t.Error("hidden none cannot take the change path")
	}
	if !videoChangeNeedsRestart(HiddenAutoPause) {
		t.Error("auto-pause cannot take the change path either")
	}
	if videoChangeNeedsRestart(HiddenAutoStop) {
		t.Error("auto-stop is exactly what makes a video change work")
	}
}
