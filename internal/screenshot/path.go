// Package screenshot holds the screenshot pipeline's pure parts: where a shot
// is saved, how it is named and encoded, and the region selector's state.
package screenshot

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir is the directory screenshots are saved to: Screenshots under the
// user's XDG pictures directory.
func Dir() string {
	home, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return dir(home, configHome, os.ReadFile)
}

func dir(home, configHome string, read func(string) ([]byte, error)) string {
	return filepath.Join(PicturesDir(home, configHome, read), "Screenshots")
}

// PicturesDir reads XDG_PICTURES_DIR from user-dirs.dirs.
func PicturesDir(home, configHome string, read func(string) ([]byte, error)) string {
	return userDir("XDG_PICTURES_DIR", "Pictures", home, configHome, read)
}

// DownloadsDir reads XDG_DOWNLOAD_DIR from user-dirs.dirs, falling back to
// ~/Downloads.
func DownloadsDir(home, configHome string, read func(string) ([]byte, error)) string {
	return userDir("XDG_DOWNLOAD_DIR", "Downloads", home, configHome, read)
}

// UserDownloadsDir is DownloadsDir for the running user.
func UserDownloadsDir() string {
	home, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return DownloadsDir(home, configHome, os.ReadFile)
}

// userDir reads one key from user-dirs.dirs. The file holds shell assignments
// whose value is either absolute or starts with $HOME/; anything else, and a
// missing file or key, falls back to ~/<fallbackName>.
func userDir(key, fallbackName, home, configHome string, read func(string) ([]byte, error)) string {
	fallback := filepath.Join(home, fallbackName)
	data, err := read(filepath.Join(configHome, "user-dirs.dirs"))
	if err != nil {
		return fallback
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		value, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), key+"=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		if rest, ok := strings.CutPrefix(value, "$HOME/"); ok {
			if rest == "" {
				// The spec's way of saying the directory is disabled.
				return fallback
			}
			return filepath.Join(home, rest)
		}
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		return fallback
	}
	return fallback
}

// NextPath names a new screenshot taken at now inside dir, adding -2, -3 and
// so on while exists reports the name taken.
func NextPath(dir string, now time.Time, exists func(string) bool) string {
	stem := filepath.Join(dir, "screenshot_"+now.Format("2006-01-02_15-04-05"))
	path := stem + ".png"
	for n := 2; exists(path); n++ {
		path = fmt.Sprintf("%s-%d.png", stem, n)
	}
	return path
}

// Exists is NextPath's probe against the filesystem.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
