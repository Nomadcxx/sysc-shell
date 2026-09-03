package wallpaper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ThumbWidth and ThumbHeight are the cached preview size. They are the tile's
// own aspect, not a separate one: paintImage scales a raster to fill its box
// with no aspect preservation, so the crop has to happen here, once, off the
// Wayland owner. A cache at a different ratio would show every wallpaper
// subtly stretched.
const (
	ThumbWidth  = 210
	ThumbHeight = 96
)

// CacheDir is $XDG_CACHE_HOME/sysc-shell/wallpaper.
func CacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "sysc-shell", "wallpaper")
}

// cacheName keys a preview by path and modification time, so replacing a file
// with a different image at the same path produces a different entry rather
// than a stale thumbnail.
func cacheName(path string, modUnix int64, size int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", path, modUnix, size)))
	return hex.EncodeToString(sum[:16]) + ".jpg"
}

// cachedStillPath is where the preview for path lives, whether or not it has
// been generated yet. A missing source file has no cache entry.
func cachedStillPath(path string) string {
	dir := CacheDir()
	if dir == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, cacheName(path, info.ModTime().Unix(), info.Size()))
}
