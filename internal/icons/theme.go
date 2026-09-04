// Package icons resolves XDG icon names to files and decodes them into bounded
// rasters. It is shared: notifications, the tray, and anything else that needs
// an application icon use this one path.
//
// Milestone 5 is raster-only. No SVG rasterizer is pinned, so a theme that
// offers only SVG for a name yields no file and the caller falls back to its
// own placeholder. Adding SVG later means extending resolve, not a second path.
package icons

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// rasterExtensions are the formats searched for by name inside an icon theme,
// in preference order. Icon themes ship PNG and XPM; looking for anything else
// would be wasted stats on every miss.
var rasterExtensions = []string{".png", ".xpm"}

// decodableExtensions are the formats the worker can decode when it is handed
// an exact file rather than asked to find one. A caller passing an absolute
// path has already chosen the file, so theme-search preference does not apply:
// the only question is whether the decoder understands it. Wallpaper previews
// are JPEG, and gating them on the theme list rejected every one of them.
var decodableExtensions = []string{".png", ".xpm", ".jpg", ".jpeg", ".gif", ".bmp"}

// maxInheritDepth bounds theme inheritance. A cycle in index.theme files would
// otherwise walk forever.
const maxInheritDepth = 8

// Resolver finds icon files in the XDG icon themes.
type Resolver struct {
	theme string
	dirs  []string
}

// NewResolver builds a resolver over the standard search path. An empty theme
// name means hicolor, which every compliant theme inherits anyway.
func NewResolver(theme string, dirs []string) *Resolver {
	if theme == "" {
		theme = "hicolor"
	}
	if dirs == nil {
		dirs = SearchDirs()
	}
	return &Resolver{theme: theme, dirs: dirs}
}

// SearchDirs reports the icon directories in XDG precedence order.
func SearchDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".icons"))
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		dirs = append(dirs, filepath.Join(data, "icons"))
	}
	shared := os.Getenv("XDG_DATA_DIRS")
	if shared == "" {
		shared = "/usr/local/share:/usr/share"
	}
	for _, dir := range strings.Split(shared, ":") {
		if dir != "" {
			dirs = append(dirs, filepath.Join(dir, "icons"))
		}
	}
	return append(dirs, "/usr/share/pixmaps")
}

// Resolve reports the best file for an icon name at a wanted logical size.
//
// An absolute path is taken as given, which is what the freedesktop
// notification spec allows an application to send. Otherwise the configured
// theme is searched, then everything it inherits, then hicolor, then the
// unthemed pixmap directory.
func (r *Resolver) Resolve(name string, size int) (string, bool) {
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) {
		if isDecodableFile(name) {
			return name, true
		}
		return "", false
	}
	// A name may not escape into another directory.
	if strings.ContainsRune(name, filepath.Separator) {
		return "", false
	}
	for _, theme := range r.chain() {
		if path, ok := r.findInTheme(theme, name, size); ok {
			return path, true
		}
	}
	for _, dir := range r.dirs {
		for _, extension := range rasterExtensions {
			candidate := filepath.Join(dir, name+extension)
			if isRasterFile(candidate) {
				return candidate, true
			}
		}
	}
	return "", false
}

// chain reports the theme and everything it inherits, hicolor last.
func (r *Resolver) chain() []string {
	seen := map[string]struct{}{}
	var order []string
	var walk func(theme string, depth int)
	walk = func(theme string, depth int) {
		if theme == "" || depth > maxInheritDepth {
			return
		}
		if _, done := seen[theme]; done {
			return
		}
		seen[theme] = struct{}{}
		order = append(order, theme)
		for _, parent := range r.inherits(theme) {
			walk(parent, depth+1)
		}
	}
	walk(r.theme, 0)
	walk("hicolor", 0)
	return order
}

// inherits reads the Inherits key from a theme's index.theme.
func (r *Resolver) inherits(theme string) []string {
	for _, dir := range r.dirs {
		file, err := os.Open(filepath.Join(dir, theme, "index.theme"))
		if err != nil {
			continue
		}
		parents := parseInherits(file)
		_ = file.Close()
		if len(parents) > 0 {
			return parents
		}
	}
	return nil
}

func parseInherits(file *os.File) []string {
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		value, ok := strings.CutPrefix(line, "Inherits=")
		if !ok {
			continue
		}
		var parents []string
		for _, parent := range strings.Split(value, ",") {
			if parent = strings.TrimSpace(parent); parent != "" {
				parents = append(parents, parent)
			}
		}
		return parents
	}
	return nil
}

// findInTheme picks the closest size a theme offers for a name. An exact match
// wins; otherwise the smallest icon at least as large as the request, because
// scaling down keeps more detail than scaling up.
func (r *Resolver) findInTheme(theme, name string, size int) (string, bool) {
	type candidate struct {
		path string
		size int
	}
	var found []candidate
	for _, dir := range r.dirs {
		root := filepath.Join(dir, theme)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			at := directorySize(entry.Name())
			for _, category := range subdirectories(filepath.Join(root, entry.Name())) {
				for _, extension := range rasterExtensions {
					path := filepath.Join(category, name+extension)
					if isRasterFile(path) {
						found = append(found, candidate{path: path, size: at})
					}
				}
			}
		}
	}
	if len(found) == 0 {
		return "", false
	}
	sort.SliceStable(found, func(i, j int) bool {
		return betterSize(found[i].size, found[j].size, size)
	})
	return found[0].path, true
}

// betterSize orders candidates: exact first, then the smallest that is large
// enough, then the largest of those that are too small.
func betterSize(a, b, want int) bool {
	if a == want {
		return true
	}
	if b == want {
		return false
	}
	aBig, bBig := a >= want, b >= want
	switch {
	case aBig && bBig:
		return a < b
	case aBig != bBig:
		return aBig
	default:
		return a > b
	}
}

func subdirectories(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			paths = append(paths, filepath.Join(path, entry.Name()))
		}
	}
	return paths
}

// directorySize reads the leading pixel size of a theme directory such as
// "48x48" or "32". A scalable directory reports zero: with no SVG support it
// can hold nothing this resolver accepts.
func directorySize(name string) int {
	if index := strings.IndexByte(name, 'x'); index > 0 {
		name = name[:index]
	}
	size, err := strconv.Atoi(name)
	if err != nil {
		return 0
	}
	return size
}

func isRasterFile(path string) bool { return hasReadableExtension(path, rasterExtensions) }

// isDecodableFile reports whether an exact path names a file the decoder can
// read.
func isDecodableFile(path string) bool { return hasReadableExtension(path, decodableExtensions) }

func hasReadableExtension(path string, allowed []string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	extension := strings.ToLower(filepath.Ext(path))
	for _, candidate := range allowed {
		if extension == candidate {
			return true
		}
	}
	return false
}
