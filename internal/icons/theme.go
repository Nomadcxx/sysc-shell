// Package icons resolves XDG icon names to files and decodes them into bounded
// rasters. It is shared: notifications, the tray, and anything else that needs
// an application icon use this one path.
//
// Resolution is tiered: an exact-size raster beats a vector, which beats a
// nearest raster. A broken vector falls back through ResolveRaster so one bad
// document never leaves an icon empty.
package icons

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rasterExtensions are the formats searched for by name inside an icon theme,
// in preference order. Icon themes ship PNG and XPM; looking for anything else
// would be wasted stats on every miss.
var rasterExtensions = []string{".png", ".xpm"}

// vectorExtensions are the formats the worker can rasterise at any size. They
// are a tier of their own, never folded into the raster scan: an exact-size
// raster beats a vector, which beats a scaled raster.
var vectorExtensions = []string{".svg"}

// decodableExtensions are the formats the worker can decode when it is handed
// an exact file rather than asked to find one. A caller passing an absolute
// path has already chosen the file, so theme-search preference does not apply:
// the only question is whether the decoder understands it. Wallpaper previews
// are JPEG, and gating them on the theme list rejected every one of them.
var decodableExtensions = []string{".png", ".xpm", ".jpg", ".jpeg", ".gif", ".bmp", ".svg"}

// maxInheritDepth bounds theme inheritance. A cycle in index.theme files would
// otherwise walk forever.
const maxInheritDepth = 8

// themeRecheckInterval bounds how often the index revalidates itself. A theme
// installed, updated or removed while the shell runs is noticed within this
// window, while a lookup pays nothing for it.
const themeRecheckInterval = 5 * time.Second

// Resolver finds icon files in the XDG icon themes.
//
// The theme tree is indexed once and reused: opening the launcher asks for
// every visible row, and a walk of the whole tree per row cost milliseconds and
// roughly 1,400 stats each time on a real theme.
type Resolver struct {
	theme string
	dirs  []string

	// mu guards the index and what it was built from. The index is immutable
	// once built; a rebuild replaces it, so a resolve can take a snapshot and
	// finish without the lock.
	mu         sync.Mutex
	themes     []string
	index      map[string]*themeIndex
	watched    []watchedDirectory
	checkedAt  time.Time
	generation uint64
	now        func() time.Time
}

// iconCandidate is one file the index found for an icon name.
type iconCandidate struct {
	path   string
	size   int
	vector bool
}

// themeIndex maps an icon name to every file a theme offers for it, in search
// directory order.
type themeIndex struct {
	byName map[string][]iconCandidate
}

// watchedDirectory is a directory whose modification time invalidates the
// index. A file added to a category changes that category's time, not the
// theme root's, so every directory read while indexing is remembered.
type watchedDirectory struct {
	path string
	mod  time.Time
}

// NewResolver builds a resolver over the standard search path. An empty theme
// name uses the configured desktop icon theme, falling back to hicolor, which
// every compliant theme inherits anyway.
func NewResolver(theme string, dirs []string) *Resolver {
	if theme == "" {
		theme = configuredIconTheme()
	}
	if theme == "" {
		theme = "hicolor"
	}
	if dirs == nil {
		dirs = SearchDirs()
	}
	return &Resolver{theme: theme, dirs: dirs, now: time.Now}
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

// configuredIconTheme reads gtk-icon-theme-name from the GTK settings files,
// preferring the newest version the desktop writes. The shell keeps no icon
// theme key of its own; this is the same value GTK applications use.
func configuredIconTheme() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, version := range []string{"gtk-4.0", "gtk-3.0"} {
		contents, err := os.ReadFile(filepath.Join(home, ".config", version, "settings.ini"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(contents), "\n") {
			value, ok := strings.CutPrefix(strings.TrimSpace(line), "gtk-icon-theme-name=")
			if ok {
				return strings.Trim(strings.TrimSpace(value), `"`)
			}
		}
	}
	return ""
}

// FileResolver resolves absolute filesystem paths and nothing else. Plugin
// images name files the host decodes with the user's privileges; a relative
// path would resolve against the shell's working directory, which the plugin
// cannot know, so it fails rather than guessing.
type FileResolver struct{}

// Resolve reports the path itself when it names a file the decoder can read.
func (FileResolver) Resolve(name string, size int) (string, bool) {
	if !filepath.IsAbs(name) || !isDecodableFile(name) {
		return "", false
	}
	return name, true
}

// Resolve reports the best file for an icon name at a wanted logical size,
// preferring an exact raster, then a vector, then a nearest raster.
//
// An absolute path is taken as given, which is what the freedesktop
// notification spec allows an application to send. Otherwise the configured
// theme is searched, then everything it inherits, then hicolor, then the
// unthemed pixmap directory.
func (r *Resolver) Resolve(name string, size int) (string, bool) {
	return r.resolve(name, size, true)
}

// ResolveRaster is Resolve without the vector tier. The worker calls it after
// an SVG fails to rasterise, so a broken document falls through to the
// nearest raster instead of leaving the icon empty.
func (r *Resolver) ResolveRaster(name string, size int) (string, bool) {
	return r.resolve(name, size, false)
}

func (r *Resolver) resolve(name string, size int, vectors bool) (string, bool) {
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) {
		if isDecodableFile(name) && (vectors || !isVectorFile(name)) {
			return name, true
		}
		return "", false
	}
	// A name may not escape into another directory.
	if strings.ContainsRune(name, filepath.Separator) {
		return "", false
	}
	if path, ok := r.resolveThemed(name, size, vectors); ok {
		return path, true
	}
	for _, dir := range r.dirs {
		for _, extension := range rasterExtensions {
			candidate := filepath.Join(dir, name+extension)
			if isRasterFile(candidate) {
				return candidate, true
			}
		}
		if vectors {
			for _, extension := range vectorExtensions {
				candidate := filepath.Join(dir, name+extension)
				if isVectorFile(candidate) {
					return candidate, true
				}
			}
		}
	}
	return "", false
}

// resolveThemed answers from the index: the chain, then one map lookup per
// theme.
func (r *Resolver) resolveThemed(name string, size int, vectors bool) (string, bool) {
	r.mu.Lock()
	r.ensureIndexLocked()
	themes, index := r.themes, r.index
	r.mu.Unlock()
	for _, theme := range themes {
		if themed := index[theme]; themed != nil {
			if path, ok := themed.resolve(name, size, vectors); ok {
				return path, true
			}
		}
	}
	return "", false
}

// Invalidate drops the index, so the next resolve re-reads the theme tree. A
// worker over this resolver forgets its remembered misses at the same time.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.index = nil
	r.themes = nil
	r.watched = nil
	r.generation++
	r.mu.Unlock()
}

// GenerationReporter is a resolver whose index can be rebuilt. A caller that
// remembers a failure records the generation it saw, so a rebuild retries what
// it remembered instead of serving a stale miss.
type GenerationReporter interface {
	Generation() uint64
}

// Generation counts index rebuilds.
func (r *Resolver) Generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation
}

func (r *Resolver) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// ensureIndexLocked rebuilds the index when it is missing, or when one of the
// directories it was built from has changed since the last recheck.
func (r *Resolver) ensureIndexLocked() {
	if r.index != nil {
		if r.clock().Sub(r.checkedAt) < themeRecheckInterval {
			return
		}
		if r.unchangedLocked() {
			r.checkedAt = r.clock()
			return
		}
	}
	r.rebuildLocked()
}

func (r *Resolver) unchangedLocked() bool {
	for _, watched := range r.watched {
		info, err := os.Stat(watched.path)
		if err != nil || !info.ModTime().Equal(watched.mod) {
			return false
		}
	}
	return true
}

func (r *Resolver) rebuildLocked() {
	themes := r.computeChain()
	index := make(map[string]*themeIndex, len(themes))
	watched := make(map[string]time.Time, len(r.dirs))
	watch := func(path string) {
		if _, done := watched[path]; done {
			return
		}
		if info, err := os.Stat(path); err == nil {
			watched[path] = info.ModTime()
		}
	}
	// The search directories are watched so a theme installed or removed while
	// the shell runs is noticed even when its root did not exist before.
	for _, dir := range r.dirs {
		watch(dir)
	}
	for _, theme := range themes {
		index[theme] = r.indexTheme(theme, watch)
	}
	stamps := make([]watchedDirectory, 0, len(watched))
	for path, mod := range watched {
		stamps = append(stamps, watchedDirectory{path: path, mod: mod})
	}
	r.themes = themes
	r.index = index
	r.watched = stamps
	r.checkedAt = r.clock()
	r.generation++
}

// indexTheme records every icon file a theme offers. Each directory is read
// once, rather than statting a name in every size and category directory for
// every lookup.
func (r *Resolver) indexTheme(theme string, watch func(string)) *themeIndex {
	index := &themeIndex{byName: map[string][]iconCandidate{}}
	for _, dir := range r.dirs {
		root := filepath.Join(dir, theme)
		watch(root)
		sizes, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, size := range sizes {
			if !size.IsDir() {
				continue
			}
			at := directorySize(size.Name())
			sizeDir := filepath.Join(root, size.Name())
			watch(sizeDir)
			categories, err := os.ReadDir(sizeDir)
			if err != nil {
				continue
			}
			for _, category := range categories {
				if !category.IsDir() {
					continue
				}
				categoryDir := filepath.Join(sizeDir, category.Name())
				watch(categoryDir)
				files, err := os.ReadDir(categoryDir)
				if err != nil {
					continue
				}
				for _, file := range files {
					if file.IsDir() {
						continue
					}
					name, vector, ok := iconName(file.Name())
					if !ok {
						continue
					}
					index.byName[name] = append(index.byName[name], iconCandidate{
						path:   filepath.Join(categoryDir, file.Name()),
						size:   at,
						vector: vector,
					})
				}
			}
		}
	}
	return index
}

// iconName splits an icon file into the name a lookup uses and whether the file
// is a vector. Anything the decoder cannot read is not an icon.
func iconName(file string) (string, bool, bool) {
	extension := strings.ToLower(filepath.Ext(file))
	switch {
	case contains(rasterExtensions, extension):
		return strings.TrimSuffix(file, filepath.Ext(file)), false, true
	case contains(vectorExtensions, extension):
		return strings.TrimSuffix(file, filepath.Ext(file)), true, true
	default:
		return "", false, false
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// resolve picks the best file the theme offers and confirms it is still
// readable. The index records names without statting every file, so a zero-byte
// or newly removed candidate is skipped here and the next one is tried.
func (idx *themeIndex) resolve(name string, size int, vectors bool) (string, bool) {
	candidates := idx.byName[name]
	if len(candidates) == 0 {
		return "", false
	}
	skip := map[string]struct{}(nil)
	for {
		candidate, ok := idx.pick(candidates, size, vectors, skip)
		if !ok {
			return "", false
		}
		if candidate.vector {
			if isVectorFile(candidate.path) {
				return candidate.path, true
			}
		} else if isRasterFile(candidate.path) {
			return candidate.path, true
		}
		if skip == nil {
			skip = make(map[string]struct{}, 1)
		}
		skip[candidate.path] = struct{}{}
	}
}

// pick applies the tiers unchanged: an exact-size raster, then a vector, then
// the raster nearest the request.
func (idx *themeIndex) pick(candidates []iconCandidate, size int, vectors bool, skip map[string]struct{}) (iconCandidate, bool) {
	var best iconCandidate
	haveBest := false
	firstVector := -1
	for i := range candidates {
		candidate := candidates[i]
		if _, skipped := skip[candidate.path]; skipped {
			continue
		}
		if candidate.vector {
			if vectors && firstVector < 0 {
				firstVector = i
			}
			continue
		}
		if !haveBest || (candidate.size != best.size && betterSize(candidate.size, best.size, size)) {
			best, haveBest = candidate, true
		}
	}
	if haveBest && best.size == size {
		return best, true
	}
	if firstVector >= 0 {
		return candidates[firstVector], true
	}
	return best, haveBest
}

// computeChain reports the theme and everything it inherits, hicolor last.
func (r *Resolver) computeChain() []string {
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

// directorySize reads the leading pixel size of a theme directory such as
// "48x48" or "32". A scalable directory reports zero: its files carry no fixed
// size, and the resolver treats them as fitting every request.
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

func isVectorFile(path string) bool { return hasReadableExtension(path, vectorExtensions) }

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
