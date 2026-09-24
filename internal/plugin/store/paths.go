package store

import (
	"os"
	"path/filepath"
)

// CacheRoot holds one git clone per source. It is disposable: deleting it
// costs one re-clone.
func CacheRoot() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(base, "sysc-shell", "sources")
}
