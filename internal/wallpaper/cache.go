package wallpaper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ThumbWidth and ThumbHeight are the preview's aspect and its nominal tile
// size. paintImage scales a raster to fill its box with no aspect
// preservation, so the crop has to happen here, once, off the Wayland owner. A
// cache at a different ratio would show every wallpaper subtly stretched.
//
// PreviewWidth and PreviewHeight are what is cached: twice the tile, so an
// output at 1.25 or 2 downsamples the preview instead of blowing it up.
const (
	ThumbWidth    = 210
	ThumbHeight   = 96
	PreviewWidth  = 2 * ThumbWidth
	PreviewHeight = 2 * ThumbHeight
)

// cacheVersion changes when the cached preview changes shape, so older
// entries stop matching and are regenerated rather than shown. Version 2 is
// the 2x preview.
const cacheVersion = 2

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
	sum := sha256.Sum256([]byte(fmt.Sprintf("v%d\x00%s\x00%d\x00%d", cacheVersion, path, modUnix, size)))
	return hex.EncodeToString(sum[:16]) + ".jpg"
}

// CachedStillPath is where the preview for path lives, whether or not it has
// been generated yet. A missing source file has no cache entry.
func CachedStillPath(path string) string {
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

// PreviewFailed reports that the generator recorded this version of path as
// impossible to preview. A changed file has a different key and reads false.
func PreviewFailed(path string) bool {
	still := CachedStillPath(path)
	if still == "" {
		return false
	}
	_, err := os.Stat(failMarker(still))
	return err == nil
}
