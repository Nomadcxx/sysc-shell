// Package files is the shell file browser's path jail, directory listing and
// view tree. The host panel and the plugin host call both go through here so
// a plugin cannot name a path the panel would refuse.
package files

import (
	"fmt"
	"path/filepath"
	"strings"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Contain resolves path and reports the EvalSymlinks result if and only if it
// stays under root. Both arguments must be absolute.
func Contain(root, path string) (string, error) {
	rootRes, err := resolveAbs(root)
	if err != nil {
		return "", fmt.Errorf("files: root: %w", err)
	}
	pathRes, err := resolveAbs(path)
	if err != nil {
		return "", fmt.Errorf("files: path: %w", err)
	}
	rel, err := filepath.Rel(rootRes, pathRes)
	if err != nil {
		return "", fmt.Errorf("files: %s is outside %s", pathRes, rootRes)
	}
	if pathRes != rootRes && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("files: %s is outside %s", pathRes, rootRes)
	}
	return pathRes, nil
}

func resolveAbs(path string) (string, error) {
	if path == "" || len(path) > v1.MaxPathBytes {
		return "", fmt.Errorf("path is empty or longer than %d bytes", v1.MaxPathBytes)
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains NUL")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q is not absolute", path)
	}
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("path %q resolved relative", path)
	}
	return resolved, nil
}
