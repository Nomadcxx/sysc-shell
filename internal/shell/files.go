package shell

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/files"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// filesSession is the one process-wide file browser. A second files.browse
// replaces it and cancels a pending pick.
type filesSession struct {
	root, cwd, title, mode string
	entries                []files.Entry
	err                    string
	truncated              bool
	loading                bool
	gen                    int // bumped per load; a result whose gen no longer matches is stale and dropped
	pendingCwd             string
	pick                   chan error
	picked                 string
	selected               map[string]struct{}
	anchor                 int
	showHidden             bool
	clip                   []string
	clipCut                bool
	renameFrom             string
	renameDraft            string
	pendingDelete          []string
	previewPath            string
	preview                files.Preview
	selectAfter            string
}

var errFilesCancelled = errors.New("cancelled")

// filesOpenPath launches the desktop handler. Tests replace it. Start, not
// Run: callers launch it off the Registry lock. report receives every
// failure — including the handler's exit code, non-zero when no application
// claims the file — and nothing on success.
var filesOpenPath = func(path string, report func(error)) {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		report(err)
		return
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			report(err)
		}
	}()
}

func (r *Registry) openFilesBrowser(ctx context.Context, p v1.FilesBrowseParams) (v1.FilesBrowseResult, error) {
	if err := ctx.Err(); err != nil {
		return v1.FilesBrowseResult{}, err
	}
	start := p.Path
	if start == "" {
		start = p.Root
	}
	root, err := files.Contain(p.Root, p.Root)
	if err != nil {
		return v1.FilesBrowseResult{}, err
	}
	cwd, err := files.Contain(root, start)
	if err != nil {
		return v1.FilesBrowseResult{}, err
	}
	sess := &filesSession{
		root: root, cwd: cwd, title: p.Title, mode: p.Mode, loading: true,
	}
	if p.Mode != files.ModeOpen {
		sess.pick = make(chan error, 1)
	}
	pick := sess.pick

	r.mu.Lock()
	r.cancelFilesPickLocked(errFilesCancelled)
	r.files = sess
	out, trig := r.focusedTriggerLocked()
	if err := r.openPanelLocked(PanelFiles, out, trig); err != nil {
		if r.files == sess {
			r.files = nil
		}
		r.mu.Unlock()
		return v1.FilesBrowseResult{}, err
	}
	// Stamp under the same lock as publish so a superseded call cannot
	// OpenPanel or overwrite filesOwner after a newer session is live.
	r.stampFilesOwnerLocked(sess)
	if r.files == sess {
		r.startFilesLoadLocked(sess, cwd)
	}
	r.mu.Unlock()
	if p.Mode == files.ModeOpen {
		return v1.FilesBrowseResult{}, nil
	}
	select {
	case err := <-pick:
		if err != nil {
			return v1.FilesBrowseResult{}, err
		}
		return v1.FilesBrowseResult{Path: sess.picked}, nil
	case <-ctx.Done():
		r.closeFilesPanelIfCurrent(sess)
		return v1.FilesBrowseResult{}, ctx.Err()
	}
}

// stampFilesOwnerLocked records that this host renders sess. No-op when sess
// is no longer current, so a stale caller cannot orphan a newer pick.
func (r *Registry) stampFilesOwnerLocked(sess *filesSession) {
	if r.files != sess {
		return
	}
	if h := r.panelHosts[PanelFiles]; h != nil {
		h.filesOwner = sess
	}
}

func (r *Registry) cancelFilesPickLocked(err error) {
	sess := r.files
	if sess == nil || sess.pick == nil {
		return
	}
	select {
	case sess.pick <- err:
	default:
	}
	sess.pick = nil
}

// closeFilesPanelIfCurrent closes the panel only while sess is still the
// current browser, so a stale context cannot close a newer session's panel.
func (r *Registry) closeFilesPanelIfCurrent(sess *filesSession) {
	r.mu.Lock()
	if r.files == sess {
		r.closePanelLocked(PanelFiles)
	}
	r.mu.Unlock()
}

func (r *Registry) finishFilesPickLocked(sess *filesSession, path string, err error) {
	if sess == nil {
		return
	}
	sess.picked = path
	if sess.pick != nil {
		select {
		case sess.pick <- err:
		default:
		}
		sess.pick = nil
	}
	r.closePanelLocked(PanelFiles)
}

func (r *Registry) reloadFilesLocked(sess *filesSession, cwd string) {
	sess.loading = true
	sess.err = ""
	sess.pendingCwd = cwd
	sess.selected = nil
	sess.anchor = 0
	sess.renameFrom, sess.renameDraft = "", ""
	sess.pendingDelete = nil
	if h := r.panelHosts[PanelFiles]; h != nil {
		r.rebuildPanel(h)
	}
	r.startFilesLoadLocked(sess, cwd)
}

// startFilesLoadLocked lists cwd off the registry lock and applies the result
// only while sess is current and no newer load superseded it. Caller holds r.mu.
func (r *Registry) startFilesLoadLocked(sess *filesSession, cwd string) {
	sess.gen++
	gen := sess.gen
	root := sess.root
	hidden := sess.showHidden
	go func() {
		ents, truncated, err := files.ListLimited(root, cwd, files.MaxEntries, hidden)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.files != sess || sess.gen != gen {
			return
		}
		sess.loading = false
		if err != nil {
			sess.err = err.Error()
		} else {
			sess.cwd, sess.entries, sess.truncated, sess.err = cwd, ents, truncated, ""
		}
		sess.pendingCwd = ""
		// A created folder is selected and revealed, so F2 renames it without
		// hunting for it in the list.
		focusEntry := -1
		if want := sess.selectAfter; want != "" {
			sess.selectAfter = ""
			for i, e := range sess.entries {
				if e.Path == want {
					sess.selectOnly(i)
					focusEntry = i
					break
				}
			}
		}
		if h := r.panelHosts[PanelFiles]; h != nil {
			r.rebuildPanel(h)
			if focusEntry >= 0 {
				h.focusFilesEntry(focusEntry)
			}
		}
	}()
}

// syncFilesPreviewLocked keeps one cached preview per selected path and reads
// it off the lock, the same shape as startFilesLoadLocked. The render path must
// not touch the jail: the live consumer is Phone Connect, whose root is an
// sshfs mount, so a synchronous read under Registry.mu stalls every panel.
func (r *Registry) syncFilesPreviewLocked(sess *filesSession, path string) {
	if sess == nil || sess.previewPath == path {
		return
	}
	sess.previewPath = path
	sess.preview = files.Preview{}
	if path == "" {
		return
	}
	root := sess.root
	go func() {
		p, _ := files.PeekPreview(root, path)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.files != sess || sess.previewPath != path {
			return
		}
		sess.preview = p
		if h := r.panelHosts[PanelFiles]; h != nil {
			r.rebuildPanel(h)
		}
	}()
}

func (r *Registry) filesTree(h *PanelHost) *ui.Node {
	sess := r.files
	if sess == nil {
		return pluginPanelError("no folder", false)
	}
	w, ht := h.place.Panel.W, h.place.Panel.H
	if w <= 0 {
		w = files.PanelWidth
	}
	if ht <= 0 {
		ht = files.PanelHeight
	}
	title := sess.title
	if title == "" {
		title = "Files"
	}
	// The preview is read off the lock; the tree only ever sees the cache.
	previewPath := ""
	if len(sess.selected) == 1 && sess.renameFrom == "" && len(sess.pendingDelete) == 0 {
		for p := range sess.selected {
			previewPath = p
			break
		}
	}
	r.syncFilesPreviewLocked(sess, previewPath)
	var preview files.Preview
	if previewPath != "" && sess.previewPath == previewPath {
		preview = sess.preview
	}
	selectedFile := sess.selectedFileEntry().Path != ""
	deletingNames := make([]string, 0, len(sess.pendingDelete))
	for _, p := range sess.pendingDelete {
		deletingNames = append(deletingNames, filepath.Base(p))
	}
	wire := files.Tree(files.Model{
		Root: sess.root, Cwd: sess.cwd, Title: title, Mode: sess.mode,
		Width: w, Height: ht, Entries: sess.entries, Error: sess.err,
		Truncated: sess.truncated, Loading: sess.loading, Selected: len(sess.selected),
		SelectedFile: selectedFile, Hidden: sess.showHidden, CanPaste: len(sess.clip) > 0,
		Rename: sess.renameDraft, Deleting: len(sess.pendingDelete), DeletingNames: deletingNames,
		PreviewPath: previewPath, Preview: preview,
	})
	converted, err := plugin.Convert(wire, v1.ViewPanel)
	if err != nil {
		return pluginPanelError(err.Error(), false)
	}
	stampFilesSelected(converted, sess)
	return converted
}

func stampFilesSelected(n *ui.Node, sess *filesSession) {
	if n == nil || sess == nil {
		return
	}
	if i := files.EntryIndex(n.Action); i >= 0 && i < len(sess.entries) && sess.hasSelected(sess.entries[i].Path) {
		n.State |= ui.StateSelected
	}
	for _, c := range n.Children {
		stampFilesSelected(c, sess)
	}
}

func (s *filesSession) hasSelected(path string) bool {
	_, ok := s.selected[path]
	return ok
}

// selectedFileEntry is the single selected row when it is a file, and the zero
// Entry otherwise. It drives the Open / Choose this file control, so a correct
// Explorer single click has a labelled way to act.
func (s *filesSession) selectedFileEntry() files.Entry {
	if s == nil || len(s.selected) != 1 {
		return files.Entry{}
	}
	for _, e := range s.entries {
		if !e.Dir && s.hasSelected(e.Path) {
			return e
		}
	}
	return files.Entry{}
}

func (s *filesSession) ensureSelected() {
	if s.selected == nil {
		s.selected = make(map[string]struct{})
	}
}

func (s *filesSession) selectOnly(i int) {
	s.selected = map[string]struct{}{s.entries[i].Path: {}}
	s.anchor = i
}

func (s *filesSession) toggle(i int) {
	s.ensureSelected()
	p := s.entries[i].Path
	if _, ok := s.selected[p]; ok {
		delete(s.selected, p)
	} else {
		s.selected[p] = struct{}{}
	}
	s.anchor = i
}

func (s *filesSession) selectRange(i int) {
	s.ensureSelected()
	lo, hi := s.anchor, i
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 0 {
		lo = 0
	}
	if hi >= len(s.entries) {
		hi = len(s.entries) - 1
	}
	for j := lo; j <= hi; j++ {
		s.selected[s.entries[j].Path] = struct{}{}
	}
}

func (s *filesSession) selectAll() {
	s.ensureSelected()
	for _, e := range s.entries {
		s.selected[e.Path] = struct{}{}
	}
}

func (s *filesSession) clearSelected() {
	s.selected = nil
	s.anchor = 0
}

// pointerOnEntry applies Explorer click rules. true means the caller should
// activate (double-click). Clicks is 1 for a first press, 2+ for a multi-click.
func (s *filesSession) pointerOnEntry(i int, mods ui.Mods, clicks int) bool {
	if s == nil || i < 0 || i >= len(s.entries) {
		return false
	}
	switch {
	case mods.Has(ui.ModCtrl):
		s.toggle(i)
		return false
	case mods.Has(ui.ModShift):
		s.selectRange(i)
		return false
	case clicks >= 2:
		s.selectOnly(i)
		return true
	default:
		s.selectOnly(i)
		return false
	}
}

func (h *PanelHost) activateFiles(r *Registry, n *ui.Node) bool {
	if h == nil || r == nil || n == nil || h.id != PanelFiles {
		return false
	}
	sess := r.files
	if sess == nil {
		return true
	}
	switch n.Action {
	case files.ActionUp:
		from := sess.cwd
		if sess.pendingCwd != "" {
			from = sess.pendingCwd
		}
		next, err := files.Walk(sess.root, from, "..")
		if err != nil {
			sess.err = err.Error()
			r.rebuildPanel(h)
			return true
		}
		r.reloadFilesLocked(sess, next)
		return true
	case files.ActionChoose:
		target := sess.cwd
		if sess.pendingCwd != "" {
			target = sess.pendingCwd
		}
		r.finishFilesPickLocked(sess, target, nil)
		return true
	case files.ActionRetry:
		r.reloadFilesLocked(sess, sess.cwd)
		return true
	case files.ActionHidden:
		sess.showHidden = !sess.showHidden
		r.reloadFilesLocked(sess, filesCwd(sess))
		return true
	case files.ActionMkdir:
		created, err := files.MkdirUntitled(sess.root, filesCwd(sess))
		if err != nil {
			sess.err = err.Error()
			r.rebuildPanel(h)
			return true
		}
		sess.selectAfter = created
		r.reloadFilesLocked(sess, filesCwd(sess))
		return true
	case files.ActionCopy:
		sess.clipSelected(false)
		r.rebuildPanel(h)
		return true
	case files.ActionCut:
		sess.clipSelected(true)
		r.rebuildPanel(h)
		return true
	case files.ActionPaste:
		r.pasteFilesLocked(sess)
		return true
	case files.ActionOpen:
		if e := sess.selectedFileEntry(); e.Path != "" {
			r.openFilesTargetLocked(sess, e)
		}
		return true
	case files.ActionRename:
		if sess.renameFrom != "" {
			r.commitFilesRenameLocked(sess, n.Text)
			return true
		}
		r.beginFilesRenameLocked(sess)
		return true
	case files.ActionDelete:
		r.beginFilesDeleteLocked(sess)
		return true
	case files.ActionDeleteOK:
		r.confirmFilesDeleteLocked(sess)
		return true
	case files.ActionDeleteNo:
		sess.pendingDelete = nil
		r.rebuildPanel(h)
		return true
	}
	if i := files.CrumbIndex(n.Action); i >= 0 {
		next, err := files.CrumbTarget(sess.root, filesCwd(sess), i)
		if err != nil {
			sess.err = err.Error()
			r.rebuildPanel(h)
			return true
		}
		r.reloadFilesLocked(sess, next)
		return true
	}
	i := files.EntryIndex(n.Action)
	if i < 0 || i >= len(sess.entries) {
		return true
	}
	e := sess.entries[i]
	if _, err := files.Contain(sess.root, e.Path); err != nil {
		sess.err = err.Error()
		r.rebuildPanel(h)
		return true
	}
	if e.Dir {
		r.reloadFilesLocked(sess, e.Path)
		return true
	}
	r.openFilesTargetLocked(sess, e)
	return true
}

// openFilesTargetLocked acts on one selected file the way activating its row
// does. pick-file returns the path and closes the panel; open hands it to the
// desktop handler. Any other mode keeps the file merely selected.
func (r *Registry) openFilesTargetLocked(sess *filesSession, e files.Entry) {
	switch sess.mode {
	case files.ModePickFile:
		r.finishFilesPickLocked(sess, e.Path, nil)
	case files.ModeOpen:
		go filesOpenPath(e.Path, func(err error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if err == nil || r.files != sess {
				return
			}
			sess.err = "Open failed: " + err.Error()
			if h := r.panelHosts[PanelFiles]; h != nil {
				r.rebuildPanel(h)
			}
		})
	}
}

func (h *PanelHost) filesPointerRelease(r *Registry, n *ui.Node) bool {
	sess := r.files
	if sess == nil || n == nil {
		return false
	}
	i := files.EntryIndex(n.Action)
	if i < 0 {
		h.filesClicks = 0
		return false
	}
	now := time.Now()
	clicks := 1
	if n.StableKey() == h.filesClickKey && now.Sub(h.filesClickAt) <= multiClickInterval {
		clicks = h.filesClicks + 1
	}
	h.filesClickKey, h.filesClickAt, h.filesClicks = n.StableKey(), now, clicks
	if sess.pointerOnEntry(i, h.mods, clicks) {
		return false
	}
	r.rebuildPanel(h)
	return true
}

func (h *PanelHost) filesKeyPress(r *Registry, key uint32, k ui.KeyInput) bool {
	sess := r.files
	if sess == nil {
		return false
	}
	if n := h.focused(); n != nil && n.Kind == ui.KindTextField {
		return false
	}
	if key == keySpace {
		n := h.focused()
		if n == nil {
			return false
		}
		i := files.EntryIndex(n.Action)
		if i < 0 || i >= len(sess.entries) {
			return false
		}
		sess.toggle(i)
		r.rebuildPanel(h)
		return true
	}
	if k.Mods.Has(ui.ModCtrl) {
		switch k.Sym {
		case 'a', 'A':
			sess.selectAll()
			r.rebuildPanel(h)
			return true
		case 'c', 'C':
			if !sess.clipSelected(false) {
				return false
			}
			r.rebuildPanel(h)
			return true
		case 'x', 'X':
			if !sess.clipSelected(true) {
				return false
			}
			r.rebuildPanel(h)
			return true
		case 'v', 'V':
			if len(sess.clip) == 0 {
				return false
			}
			r.pasteFilesLocked(sess)
			return true
		}
	}
	if key == keyDelete {
		r.beginFilesDeleteLocked(sess)
		return true
	}
	if key == keyF2 {
		r.beginFilesRenameLocked(sess)
		return true
	}
	return false
}

func filesCwd(sess *filesSession) string {
	if sess.pendingCwd != "" {
		return sess.pendingCwd
	}
	return sess.cwd
}

func (s *filesSession) clipSelected(cut bool) bool {
	if len(s.selected) == 0 {
		return false
	}
	s.clip = s.clip[:0]
	for p := range s.selected {
		s.clip = append(s.clip, p)
	}
	sort.Strings(s.clip)
	s.clipCut = cut
	return true
}

func (r *Registry) pasteFilesLocked(sess *filesSession) {
	if sess == nil || len(sess.clip) == 0 {
		return
	}
	var err error
	if sess.clipCut {
		_, err = files.MoveInto(sess.root, filesCwd(sess), sess.clip)
	} else {
		_, err = files.CopyInto(sess.root, filesCwd(sess), sess.clip)
	}
	if err != nil {
		sess.err = err.Error()
		if h := r.panelHosts[PanelFiles]; h != nil {
			r.rebuildPanel(h)
		}
		return
	}
	if sess.clipCut {
		sess.clip, sess.clipCut = nil, false
	}
	r.reloadFilesLocked(sess, filesCwd(sess))
}

func (s *filesSession) selectedPaths() []string {
	out := make([]string, 0, len(s.selected))
	for p := range s.selected {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) beginFilesRenameLocked(sess *filesSession) {
	if sess == nil || len(sess.selected) != 1 {
		return
	}
	p := sess.selectedPaths()[0]
	sess.renameFrom = p
	sess.renameDraft = filepath.Base(p)
	if h := r.panelHosts[PanelFiles]; h != nil {
		r.rebuildPanel(h)
	}
}

func (r *Registry) commitFilesRenameLocked(sess *filesSession, name string) {
	if sess == nil || sess.renameFrom == "" {
		return
	}
	if name == "" {
		name = sess.renameDraft
	}
	if _, err := files.Rename(sess.root, sess.renameFrom, name); err != nil {
		sess.err = err.Error()
		if h := r.panelHosts[PanelFiles]; h != nil {
			r.rebuildPanel(h)
		}
		return
	}
	r.reloadFilesLocked(sess, filesCwd(sess))
}

func (r *Registry) beginFilesDeleteLocked(sess *filesSession) {
	if sess == nil || len(sess.selected) == 0 {
		return
	}
	sess.pendingDelete = sess.selectedPaths()
	sess.renameFrom, sess.renameDraft = "", ""
	if h := r.panelHosts[PanelFiles]; h != nil {
		r.rebuildPanel(h)
	}
}

func (r *Registry) confirmFilesDeleteLocked(sess *filesSession) {
	if sess == nil || len(sess.pendingDelete) == 0 {
		return
	}
	if err := files.Remove(sess.root, sess.pendingDelete); err != nil {
		sess.err = err.Error()
		sess.pendingDelete = nil
		if h := r.panelHosts[PanelFiles]; h != nil {
			r.rebuildPanel(h)
		}
		return
	}
	r.reloadFilesLocked(sess, filesCwd(sess))
}
