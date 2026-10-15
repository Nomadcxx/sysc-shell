package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ContentDigest is the sha256 of what a user approves when they approve a
// plugin: its manifest, which carries the asks, and its resolved entry point,
// which is the code. Nothing else in the directory is covered. State, caches
// and the store's records change on every run, and digesting them would make a
// stable approval impossible.
//
// Each file contributes "path \0 length \0 bytes", sorted by its slash-separated
// path inside the plugin directory, so the result does not depend on the order
// files are read in and bytes cannot move from one entry into the next.
func ContentDigest(m Manifest) (string, error) {
	realDir, err := filepath.EvalSymlinks(m.Dir)
	if err != nil {
		return "", fmt.Errorf("plugin: digest %s: %w", m.Dir, err)
	}
	paths := []string{filepath.Join(realDir, ManifestName)}
	realExec, err := filepath.EvalSymlinks(m.ExecPath)
	if err != nil {
		return "", fmt.Errorf("plugin: digest %s: %w", m.ID, err)
	}
	if !slices.Contains(paths, realExec) {
		paths = append(paths, realExec)
	}
	entries := make([]digestEntry, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(realDir, path)
		if err != nil {
			return "", fmt.Errorf("plugin: digest %s: %w", m.ID, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("plugin: digest %s: %w", m.ID, err)
		}
		entries = append(entries, digestEntry{path: filepath.ToSlash(rel), data: data})
	}
	return contentDigestOf(entries), nil
}

type digestEntry struct {
	path string
	data []byte
}

func contentDigestOf(entries []digestEntry) string {
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b digestEntry) int { return strings.Compare(a.path, b.path) })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.path))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(e.data))))
		h.Write([]byte{0})
		h.Write(e.data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
