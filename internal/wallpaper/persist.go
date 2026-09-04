package wallpaper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// wireAssignment is the on-disk shape. The enums are written as names rather
// than as numbers so the file survives a reordering of the constants and stays
// legible to whoever opens it.
type wireAssignment struct {
	Kind            string `json:"kind"`
	Path            string `json:"path"`
	PreviewPath     string `json:"preview_path,omitempty"`
	DesiredPlayback string `json:"desired_playback"`
}

var (
	kindNames     = map[Kind]string{KindImage: "image", KindVideo: "video"}
	playbackNames = map[State]string{StateStatic: "static", StatePlaying: "playing", StatePaused: "paused"}
)

func lookupName[K comparable](names map[K]string, want string) (K, bool) {
	for key, name := range names {
		if name == want {
			return key, true
		}
	}
	var zero K
	return zero, false
}

// AssignmentsPath is $XDG_STATE_HOME/sysc-shell/wallpaper/assignments.json.
// What is on which output is state rather than configuration: it changes
// without the user editing anything, so it does not belong in their config
// file (D19).
func AssignmentsPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "sysc-shell", "wallpaper", "assignments.json")
}

// checkPath rejects a media path that cannot safely reach an engine.
//
// A newline would smuggle a second command onto a line-oriented control
// socket, and a non-UTF-8 path cannot be carried through the JSON file or the
// panel. Both are checked on the way out and on the way in, because the file
// is editable between the two.
func checkPath(path string) error {
	if path == "" {
		return fmt.Errorf("wallpaper: empty media path")
	}
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("wallpaper: path %q contains a newline", path)
	}
	if !utf8.ValidString(path) {
		return fmt.Errorf("wallpaper: path is not valid UTF-8")
	}
	return nil
}

// SaveAssignments writes the assignment table atomically at mode 0600. A
// refused entry fails the whole write and leaves the previous file in place:
// half a table would silently drop an output's wallpaper.
func SaveAssignments(path string, assignments map[string]Assignment) error {
	wire := make(map[string]wireAssignment, len(assignments))
	for connector, a := range assignments {
		if err := checkPath(a.Path); err != nil {
			return err
		}
		if a.PreviewPath != "" {
			if err := checkPath(a.PreviewPath); err != nil {
				return err
			}
		}
		kind, ok := kindNames[a.Kind]
		if !ok {
			return fmt.Errorf("wallpaper: %s has no media kind", connector)
		}
		playback, ok := playbackNames[a.DesiredPlayback]
		if !ok {
			playback = playbackNames[StateStatic]
		}
		wire[connector] = wireAssignment{
			Kind: kind, Path: a.Path, PreviewPath: a.PreviewPath, DesiredPlayback: playback,
		}
	}

	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return fmt.Errorf("wallpaper: encode assignments: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("wallpaper: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".assignments-*.tmp")
	if err != nil {
		return fmt.Errorf("wallpaper: create temp: %w", err)
	}
	name := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("wallpaper: chmod temp: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("wallpaper: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("wallpaper: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("wallpaper: close temp: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("wallpaper: replace %s: %w", path, err)
	}
	committed = true
	return nil
}

// LoadAssignments reads the assignment table. A missing file is not an error:
// the first run has nothing assigned yet.
func LoadAssignments(path string) (map[string]Assignment, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]Assignment{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("wallpaper: read %s: %w", path, err)
	}
	var wire map[string]wireAssignment
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("wallpaper: decode %s: %w", path, err)
	}

	out := make(map[string]Assignment, len(wire))
	for connector, w := range wire {
		if err := checkPath(w.Path); err != nil {
			return nil, err
		}
		if w.PreviewPath != "" {
			if err := checkPath(w.PreviewPath); err != nil {
				return nil, err
			}
		}
		kind, ok := lookupName(kindNames, w.Kind)
		if !ok {
			return nil, fmt.Errorf("wallpaper: %s has unknown kind %q", connector, w.Kind)
		}
		playback, ok := lookupName(playbackNames, w.DesiredPlayback)
		if !ok {
			return nil, fmt.Errorf("wallpaper: %s has unknown playback %q", connector, w.DesiredPlayback)
		}
		out[connector] = Assignment{
			Kind: kind, Path: w.Path, PreviewPath: w.PreviewPath, DesiredPlayback: playback,
		}
	}
	return out, nil
}
