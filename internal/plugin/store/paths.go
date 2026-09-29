package store

import (
	"os"
	"path/filepath"
)

// CacheRoot holds one git clone per source. It is disposable: deleting it
// costs one re-clone.
func CacheRoot() string {
	return filepath.Join(cacheBase(), "sources")
}

// MediaRoot holds sha256-addressed screenshots and READMEs.
func MediaRoot() string {
	return filepath.Join(cacheBase(), "media")
}

func cacheBase() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(base, "sysc-shell")
}
