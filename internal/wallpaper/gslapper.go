package wallpaper

import (
	"bytes"
	"strconv"
	"strings"
)

// Hidden names what gSlapper does when the wallpaper is occluded.
//
// It is a CPU-versus-continuity preference, and it also decides whether a
// video-to-video apply can use `change`: gSlapper documents --auto-stop as
// required for video IPC changes. HiddenNone is the default, so the common
// path for a video swap is stop-and-relaunch rather than `change`.
const (
	HiddenNone      = "none"
	HiddenAutoPause = "auto-pause"
	HiddenAutoStop  = "auto-stop"
)

// Settings is the configured gSlapper playback behaviour (D19).
type Settings struct {
	// Scale is one of fill, stretch, original, or panscan, forwarded to
	// GStreamer through -o verbatim.
	Scale string
	Loop  bool
	// FPS is the frame cap: 30, 60, or 100.
	FPS          int
	Fade         bool
	FadeDuration float64
	Hidden       string
}

// probeFlags are the flags that mark a gSlapper new enough to drive. The cache
// flag is the 1.5 marker: an older build has the socket and the transition but
// not the cache, and its IPC behaviour is the pre-1.5 shape this slice does
// not support.
var probeFlags = []string{"--ipc-socket", "--transition-type", "--cache-size"}

// helpSupports reports whether `gslapper --help` describes a build we can own.
func helpSupports(help []byte) bool {
	for _, flag := range probeFlags {
		if !bytes.Contains(help, []byte(flag)) {
			return false
		}
	}
	return true
}

// gstOptions builds the -o value: the scaling mode, audio off because a
// wallpaper is never the thing you are listening to, and loop when configured.
func gstOptions(s Settings) string {
	opts := make([]string, 0, 3)
	if s.Scale != "" {
		opts = append(opts, s.Scale)
	}
	opts = append(opts, "no-audio")
	if s.Loop {
		opts = append(opts, "loop")
	}
	return strings.Join(opts, " ")
}

// launchArgs builds the argv for one owned gSlapper instance.
//
// The connector and the path are positional and last. There is one instance
// per output and never a `*`: a single instance owning every output blanks the
// others the moment a second wallpaper is assigned (D14).
func launchArgs(s Settings, socket, connector, path string) []string {
	args := []string{"gslapper", "-I", socket, "--no-save-state"}
	if opts := gstOptions(s); opts != "" {
		args = append(args, "-o", opts)
	}
	if s.FPS > 0 {
		args = append(args, "-r", strconv.Itoa(s.FPS))
	}
	switch s.Hidden {
	case HiddenAutoPause:
		args = append(args, "--auto-pause")
	case HiddenAutoStop:
		args = append(args, "--auto-stop")
	}
	if s.Fade {
		args = append(args, "--transition-type", "fade",
			"--transition-duration", strconv.FormatFloat(s.FadeDuration, 'f', -1, 64))
	}
	return append(args, connector, path)
}

// videoChangeNeedsRestart reports whether a video-to-video apply has to stop
// and relaunch instead of sending `change`.
func videoChangeNeedsRestart(hidden string) bool {
	return hidden != HiddenAutoStop
}
