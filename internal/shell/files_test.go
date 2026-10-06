package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/files"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestFilesPanelFitsAtTargetAndLaptopSize(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := files.List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = &filesSession{root: root, cwd: root, title: "Files", mode: files.ModeOpen, entries: ents}
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelFiles]
	if h == nil {
		t.Fatal("missing files host")
	}
	size := panelTargetSize(PanelFiles)
	if size != (ui.Rect{W: files.PanelWidth, H: files.PanelHeight}) {
		t.Fatalf("files panel size = %+v, want %dx%d", size, files.PanelWidth, files.PanelHeight)
	}
	if size.W < 800 || size.H < 650 {
		t.Fatalf("files panel too small: %+v (monitor is 800x650)", size)
	}
	reg.mu.Lock()
	if err := h.configure(size.W, size.H, int(ui.ScaleUnit)); err != nil {
		reg.mu.Unlock()
		t.Fatalf("layout %dx%d: %v", size.W, size.H, err)
	}
	assertLaidOut(t, PanelFiles, h.root)
	place := Placement{Output: ui.Rect{W: 1366, H: 768}, BarZone: 40, Padding: 12, Panel: size}
	w, ht := place.FittedSize()
	h.place.Panel.W, h.place.Panel.H = w, ht
	reg.rebuildPanel(h)
	if err := h.configure(w, ht, int(ui.ScaleUnit)); err != nil {
		reg.mu.Unlock()
		t.Fatalf("laptop layout %dx%d: %v", w, ht, err)
	}
	assertLaidOut(t, PanelFiles, h.root)
	// The widest chrome is one selected file with a preview: the selection row
	// then carries a caption, Copy, Cut, Open, Rename and Delete beside a
	// permanent preview column. The confirm card and the rename field each add
	// a row of their own, and every state must fit at both sizes.
	selectFirstFile := func(s *filesSession) {
		for i, e := range s.entries {
			if e.Dir {
				continue
			}
			s.pointerOnEntry(i, 0, 1)
			return
		}
	}
	states := []func(*filesSession){
		selectFirstFile,
		func(s *filesSession) {
			selectFirstFile(s)
			s.pendingDelete = s.selectedPaths()
		},
		func(s *filesSession) {
			selectFirstFile(s)
			s.renameFrom = s.selectedPaths()[0]
			s.renameDraft = filepath.Base(s.renameFrom)
		},
	}
	for _, apply := range states {
		reg.files.clearSelected()
		reg.files.renameFrom, reg.files.renameDraft = "", ""
		reg.files.pendingDelete = nil
		apply(reg.files)
		for _, box := range [][2]int{{size.W, size.H}, {w, ht}} {
			h.place.Panel.W, h.place.Panel.H = box[0], box[1]
			reg.rebuildPanel(h)
			if err := h.configure(box[0], box[1], int(ui.ScaleUnit)); err != nil {
				reg.mu.Unlock()
				t.Fatalf("selected layout %dx%d: %v", box[0], box[1], err)
			}
			assertLaidOut(t, PanelFiles, h.root)
		}
	}
	reg.mu.Unlock()
}

func TestFilesActivateEntersOpensAndPicks(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened := make(chan string, 1)
	orig := filesOpenPath
	filesOpenPath = func(path string, _ func(error)) {
		opened <- path
	}
	t.Cleanup(func() { filesOpenPath = orig })

	cfg := config.Default()
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	ents, err := files.List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.files = &filesSession{root: root, cwd: root, title: "Files", mode: files.ModeOpen, entries: ents}
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelFiles]
	reg.mu.Lock()
	// Enter docs: find the entry index.
	var dirIdx int
	for i, e := range reg.files.entries {
		if e.Dir {
			dirIdx = i
			break
		}
	}
	h.activateFiles(reg, &ui.Node{Action: files.ActionEntry + strconv.Itoa(dirIdx)})
	reg.mu.Unlock()
	waitFilesLoaded(t, reg, root, "/docs", 0)
	reg.mu.Lock()
	h.activateFiles(reg, &ui.Node{Action: files.ActionUp})
	reg.mu.Unlock()
	waitFilesLoaded(t, reg, root, "/", 2)
	reg.mu.Lock()
	var fileIdx int
	for i, e := range reg.files.entries {
		if !e.Dir {
			fileIdx = i
			break
		}
	}
	h.activateFiles(reg, &ui.Node{Action: files.ActionEntry + strconv.Itoa(fileIdx)})
	reg.mu.Unlock()
	want, err := files.Contain(root, file)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-opened:
		if got != want {
			t.Fatalf("opened %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("xdg-open was not called")
	}

	pick := make(chan error, 1)
	reg.mu.Lock()
	ents, err = files.List(root, root)
	if err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	}
	sess := &filesSession{root: root, cwd: root, title: "Pick", mode: files.ModePickFile, entries: ents, pick: pick}
	reg.files = sess
	h.activateFiles(reg, &ui.Node{Action: files.ActionEntry + strconv.Itoa(fileIdx)})
	reg.mu.Unlock()
	if err := <-pick; err != nil {
		t.Fatal(err)
	}
	if sess.picked != want {
		t.Fatalf("picked %q, want %q", sess.picked, want)
	}
}

func TestFilesBrowseCallOpensPanel(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	_, err := reg.openFilesBrowser(t.Context(), v1.FilesBrowseParams{Root: root, Mode: files.ModeOpen, Title: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if reg.panelHosts[PanelFiles] == nil {
		t.Fatal("files panel did not open")
	}
	if reg.files == nil || reg.files.title != "Phone" {
		t.Fatalf("session = %+v", reg.files)
	}
}

func waitFilesPick(t *testing.T, reg *Registry) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		ready := reg.files != nil && reg.files.pick != nil
		reg.mu.Unlock()
		if ready {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("files pick never became pending")
}

// waitFilesLoaded polls until the session's pending listing for rel has
// applied; listings run off the registry lock.
func waitFilesLoaded(t *testing.T, reg *Registry, root, rel string, wantEntries int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		ready := reg.files != nil && !reg.files.loading && len(reg.files.entries) == wantEntries
		if ready {
			got, err := files.Rel(root, reg.files.cwd)
			ready = err == nil && got == rel
		}
		reg.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listing for %q never loaded", rel)
}

// A second browse must cancel a pending pick even when it lands between the
// first call publishing its session and entering its select.
func TestFilesSecondBrowseCancelsPendingPick(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	done := make(chan error, 1)
	reg.mu.Lock()
	go func() {
		_, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	go func() {
		_, _ = reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
	}()
	time.Sleep(20 * time.Millisecond)
	reg.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, errFilesCancelled) {
			t.Fatalf("first pick err = %v, want errFilesCancelled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first pick hung after a second browse cancelled it")
	}
}

// Reopening the files panel on another output tears the old panel down while
// r.files already names the new session; that teardown must not cancel or
// drop the new session.
func TestFilesCrossOutputReopenKeepsNewSession(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	reg.mu.Lock()
	reg.bars = map[uint32]*Bar{7: &Bar{conn: "DP-1"}, 8: &Bar{conn: "DP-2"}}
	reg.focused = "DP-1"
	reg.mu.Unlock()

	done1 := make(chan error, 1)
	go func() {
		_, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		done1 <- err
	}()
	// The first panel must be fully open on the focused output before focus
	// moves, or the second browse would reuse it instead of reopening.
	deadline := time.Now().Add(2 * time.Second)
	for {
		reg.mu.Lock()
		h1 := reg.panelHosts[PanelFiles]
		open := h1 != nil && h1.output == 7
		reg.mu.Unlock()
		if open {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first files panel never opened on output 7")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitFilesLoaded(t, reg, root, "/", 1)

	reg.mu.Lock()
	reg.focused = "DP-2"
	reg.mu.Unlock()
	type browse struct {
		path string
		err  error
	}
	done2 := make(chan browse, 1)
	go func() {
		res, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		done2 <- browse{path: res.Path, err: err}
	}()
	select {
	case got := <-done2:
		t.Fatalf("second browse finished early: err=%v (old panel teardown cancelled the new session)", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	waitFilesLoaded(t, reg, root, "/", 1)
	reg.mu.Lock()
	sess := reg.files
	h2 := reg.panelHosts[PanelFiles]
	out2 := uint32(0)
	if h2 != nil {
		out2 = h2.output
	}
	fileIdx := -1
	if sess != nil {
		for i, e := range sess.entries {
			if !e.Dir {
				fileIdx = i
				break
			}
		}
	}
	reg.mu.Unlock()
	if sess == nil || sess.pick == nil || fileIdx < 0 {
		t.Fatalf("new session not live and pickable: sess=%v pickPending=%v fileIdx=%d", sess != nil, sess != nil && sess.pick != nil, fileIdx)
	}
	if h2 == nil || out2 != 8 {
		t.Fatalf("second panel on output %d, want 8", out2)
	}
	if err := <-done1; !errors.Is(err, errFilesCancelled) {
		t.Fatalf("first pick err = %v, want errFilesCancelled", err)
	}
	reg.mu.Lock()
	h2.activateFiles(reg, &ui.Node{Action: files.ActionEntry + strconv.Itoa(fileIdx)})
	reg.mu.Unlock()
	got := <-done2
	if got.err != nil {
		t.Fatalf("second pick err = %v, want nil", got.err)
	}
	want, err := files.Contain(root, filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got.path != want {
		t.Fatalf("picked %q, want %q", got.path, want)
	}
}

func TestFilesStaleContextDoesNotCloseNewerPanel(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	reg.mu.Lock()
	reg.files = &filesSession{root: root, cwd: root, mode: files.ModePickFile, pick: make(chan error, 1)}
	stale := reg.files
	reg.files = &filesSession{root: root, cwd: root, mode: files.ModePickFile, pick: make(chan error, 1)}
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reg.closeFilesPanelIfCurrent(stale)
	reg.mu.Lock()
	open := reg.panelHosts[PanelFiles] != nil
	reg.mu.Unlock()
	if !open {
		t.Fatal("stale session closed the newer panel")
	}
	reg.ClosePanel(PanelFiles)
}

// A path listed earlier can be swapped for a symlink that escapes the jail
// before the user activates it; activate must re-check containment.
func TestFilesActivateRejectsSwappedSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	errs := make(chan error, 1)
	go func() {
		_, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		errs <- err
	}()
	waitFilesPick(t, reg)
	waitFilesLoaded(t, reg, root, "/", 1)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", target); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelFiles]
	reg.mu.Unlock()
	if h == nil {
		t.Fatal("no files panel")
	}
	reg.mu.Lock()
	h.activateFiles(reg, &ui.Node{Action: files.ActionEntry + "0"})
	sessErr := reg.files.err
	reg.mu.Unlock()
	if sessErr == "" {
		t.Fatal("swapped symlink was not rejected")
	}
	reg.ClosePanel(PanelFiles)
	<-errs
}

func TestFilesOpenPathSurvivesHandlerStart(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	script := filepath.Join(dir, "xdg-open")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 0.3\necho \"$1\" > "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	reported := make(chan error, 1)
	filesOpenPath("/tmp/example", func(err error) { reported <- err })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("xdg-open child was killed before it could run")
	}
	select {
	case err := <-reported:
		t.Fatalf("clean exit reported as a failure: %v", err)
	case <-time.After(time.Second):
	}
}

func TestFilesOpenModeLoadsOffLock(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	if _, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModeOpen}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		loaded := reg.files != nil && !reg.files.loading && len(reg.files.entries) == 1
		reg.mu.Unlock()
		if loaded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("listing never loaded")
}

// Choosing while a directory load is in flight must finish with the pending
// target, not the stale cwd from before the navigation started.
func TestFilesChooseDuringLoadPicksPendingDir(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := files.Contain(root, docs)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	ents, err := files.List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	pick := make(chan error, 1)
	reg.mu.Lock()
	sess := &filesSession{root: root, cwd: root, title: "Pick", mode: files.ModePickDir, entries: ents, pick: pick}
	reg.files = sess
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelFiles]
	if h == nil {
		t.Fatal("no files panel")
	}
	var dirIdx int
	for i, e := range ents {
		if e.Dir {
			dirIdx = i
			break
		}
	}
	reg.mu.Lock()
	// Enter docs (load starts) and choose in one critical section, so the
	// listing cannot apply in between: choose sees the in-flight load.
	h.activateFiles(reg, &ui.Node{Action: files.ActionEntry + strconv.Itoa(dirIdx)})
	if !sess.loading || sess.pendingCwd != want {
		pending := sess.pendingCwd
		reg.mu.Unlock()
		t.Fatalf("load not pending: loading=%v pendingCwd=%q", sess.loading, pending)
	}
	h.activateFiles(reg, &ui.Node{Action: files.ActionChoose})
	picked := sess.picked
	reg.mu.Unlock()
	if err := <-pick; err != nil {
		t.Fatal(err)
	}
	if picked != want {
		t.Fatalf("picked %q during load, want pending %q", picked, want)
	}
}

// Up while a navigation load is in flight must walk from the directory being
// loaded, not the stale cwd still held from the previous listing.
func TestFilesUpDuringLoadTargetsPendingParent(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	sub := filepath.Join(docs, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	reg.mu.Lock()
	reg.files = &filesSession{root: root, cwd: docs, pendingCwd: sub, loading: true, mode: files.ModeOpen}
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelFiles]
	if h == nil {
		t.Fatal("no files panel")
	}
	reg.mu.Lock()
	h.activateFiles(reg, &ui.Node{Action: files.ActionUp})
	reg.mu.Unlock()
	// The follow-up load must target docs (parent of the pending cwd), not
	// root (parent of the stale cwd).
	waitFilesLoaded(t, reg, root, "/docs", 1)
}

func TestFilesRetryReloads(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	if _, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModeOpen}); err != nil {
		t.Fatal(err)
	}
	waitFilesLoaded(t, reg, root, "/", 0)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelFiles]
	reg.mu.Unlock()
	if h == nil {
		t.Fatal("no files panel")
	}
	reg.mu.Lock()
	h.activateFiles(reg, &ui.Node{Action: files.ActionRetry})
	reg.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		loaded := reg.files != nil && !reg.files.loading && len(reg.files.entries) == 1
		reg.mu.Unlock()
		if loaded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("retry did not reload the folder")
}

func TestFilesFocusScrollsIntoView(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		if err := os.WriteFile(filepath.Join(root, "f"+strconv.Itoa(i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	if _, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModeOpen}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		loaded := reg.files != nil && !reg.files.loading && len(reg.files.entries) == 60
		reg.mu.Unlock()
		if loaded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelFiles]
	if h == nil || h.roving.Count == 0 {
		reg.mu.Unlock()
		t.Fatal("no focusable rows")
	}
	// ponytail: OpenPanel never lays out (only the compositor's Configure
	// callback does), so lay out here like the sibling tests do.
	size := panelTargetSize(PanelFiles)
	if err := h.configure(size.W, size.H, int(ui.ScaleUnit)); err != nil {
		reg.mu.Unlock()
		t.Fatalf("layout: %v", err)
	}
	h.roving.Set(h.roving.Count - 1)
	h.afterFocusChange(reg)
	s := findScroll(h.root)
	off := 0
	if s != nil {
		off = s.ScrollOffset
	}
	reg.mu.Unlock()
	if off <= 0 {
		t.Fatal("focus did not scroll the list")
	}
}

// A second browse on the SAME output reuses the open panel host (OpenPanel
// early-returns) and publishes r.files=S2 before the host's filesOwner is
// refreshed by S2's async load. A dismissal inside that window must still
// cancel S2's pending pick, not skip it because the stale owner no longer
// matches r.files.
func TestFilesSameOutputDismissCancelsPendingPick(t *testing.T) {
	root := t.TempDir()
	// Ballast widens the listing window: the second session's load goroutine
	// must read every name before it can re-acquire r.mu and refresh the
	// host's filesOwner, giving the dismissal a wide, reliable target.
	for i := 0; i < 2000; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("ballast-%04d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()

	done1 := make(chan error, 1)
	go func() {
		_, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		done1 <- err
	}()
	// First browse fully open on the default (focused) output, so the second
	// browse takes OpenPanel's same-output early-return and reuses the host.
	deadline := time.Now().Add(2 * time.Second)
	for {
		reg.mu.Lock()
		open := reg.panelHosts[PanelFiles] != nil
		reg.mu.Unlock()
		if open {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first files panel never opened")
		}
		time.Sleep(5 * time.Millisecond)
	}
	reg.mu.Lock()
	sess1 := reg.files
	reg.mu.Unlock()
	if sess1 == nil {
		t.Fatal("first session missing")
	}

	done2 := make(chan error, 1)
	go func() {
		_, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		done2 <- err
	}()
	// Freeze the reuse window: spin on r.mu until the second session is
	// published AND its load has been spawned (gen bumped) but not yet
	// applied, then dismiss in the same lock hold so the load goroutine
	// cannot refresh filesOwner in between.
	deadline = time.Now().Add(2 * time.Second)
	dismissed := false
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		if reg.files != nil && reg.files != sess1 && reg.files.gen >= 1 {
			reg.closePanelLocked(PanelFiles)
			dismissed = true
			reg.mu.Unlock()
			break
		}
		reg.mu.Unlock()
		runtime.Gosched()
	}
	if !dismissed {
		t.Fatal("second session never reached the reuse window; could not freeze it")
	}

	select {
	case err := <-done2:
		if !errors.Is(err, errFilesCancelled) {
			t.Fatalf("second pick err = %v, want errFilesCancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second pick was not cancelled by the same-output dismissal")
	}
	reg.mu.Lock()
	current := reg.files
	reg.mu.Unlock()
	if current != nil {
		t.Fatalf("reg.files = %+v, want nil after dismissal", current)
	}
	if err := <-done1; !errors.Is(err, errFilesCancelled) {
		t.Fatalf("first pick err = %v, want errFilesCancelled", err)
	}
}

func TestFilesSupersededStampDoesNotClobberOwner(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	go func() {
		for range reg.AuxRequests() {
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := reg.openFilesBrowser(context.Background(), v1.FilesBrowseParams{Root: root, Mode: files.ModePickFile})
		done <- err
	}()
	waitFilesPick(t, reg)
	reg.mu.Lock()
	s2 := reg.files
	h := reg.panelHosts[PanelFiles]
	if s2 == nil || h == nil || h.filesOwner != s2 {
		reg.mu.Unlock()
		t.Fatal("current session did not own the files panel")
	}
	stale := &filesSession{root: root, cwd: root, mode: files.ModePickFile, pick: make(chan error, 1)}
	reg.stampFilesOwnerLocked(stale)
	if h.filesOwner != s2 {
		reg.mu.Unlock()
		t.Fatal("stale session stamped filesOwner")
	}
	reg.mu.Unlock()
	reg.ClosePanel(PanelFiles)
	if err := <-done; !errors.Is(err, errFilesCancelled) {
		t.Fatalf("pick err = %v, want errFilesCancelled", err)
	}
}

func filesSessionWithTwo(t *testing.T) *filesSession {
	t.Helper()
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := files.List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	return &filesSession{root: root, cwd: root, mode: files.ModeOpen, entries: ents}
}

func TestFilesPointerSelectsWithoutOpening(t *testing.T) {
	opened := make(chan string, 1)
	orig := filesOpenPath
	filesOpenPath = func(path string, _ func(error)) {
		opened <- path
	}
	t.Cleanup(func() { filesOpenPath = orig })

	sess := filesSessionWithTwo(t)
	if sess.pointerOnEntry(0, 0, 1) {
		t.Fatal("single click activated")
	}
	if !sess.hasSelected(sess.entries[0].Path) || sess.hasSelected(sess.entries[1].Path) {
		t.Fatalf("selected = %v", sess.selected)
	}
	select {
	case p := <-opened:
		t.Fatalf("xdg-open on click: %s", p)
	default:
	}
	if !sess.pointerOnEntry(0, 0, 2) {
		t.Fatal("double click did not activate")
	}
}

func TestFilesShiftRangeAndCtrlToggle(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	sess.pointerOnEntry(1, ui.ModShift, 1)
	if !sess.hasSelected(sess.entries[0].Path) || !sess.hasSelected(sess.entries[1].Path) {
		t.Fatalf("shift range = %v", sess.selected)
	}
	sess.pointerOnEntry(1, ui.ModCtrl, 1)
	if sess.hasSelected(sess.entries[1].Path) {
		t.Fatal("ctrl click did not toggle off")
	}
}

func TestFilesReloadClearsSelection(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	reg.reloadFilesLocked(sess, sess.cwd)
	reg.mu.Unlock()
	if len(sess.selected) != 0 {
		t.Fatalf("reload left selection %v", sess.selected)
	}
}

func TestFilesEscapeClearsSelectionFirst(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelFiles]
	reg.mu.Lock()
	if !h.keyPress(reg, keyEsc) {
		reg.mu.Unlock()
		t.Fatal("esc not handled")
	}
	if len(sess.selected) != 0 {
		reg.mu.Unlock()
		t.Fatalf("esc left selection %v", sess.selected)
	}
	if reg.panelHosts[PanelFiles] == nil {
		reg.mu.Unlock()
		t.Fatal("esc dismissed the panel while clearing selection")
	}
	reg.mu.Unlock()
}

func TestFilesHiddenToggleReloads(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := files.List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	sess := &filesSession{root: root, cwd: root, mode: files.ModeOpen, entries: ents}
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionHidden}) {
		reg.mu.Unlock()
		t.Fatal("hidden toggle not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !sess.showHidden {
		t.Fatal("toggle did not set showHidden")
	}
	found := false
	for _, e := range sess.entries {
		if e.Name == ".secret" {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries after toggle = %v", namesOf(sess.entries))
	}
}

func TestFilesCopyPasteRoundTrip(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.filesKeyPress(reg, 0, ui.KeyInput{Sym: 'c', Mods: ui.ModCtrl}) {
		reg.mu.Unlock()
		t.Fatal("ctrl+c not handled")
	}
	if len(sess.clip) != 1 {
		reg.mu.Unlock()
		t.Fatalf("clip = %v", sess.clip)
	}
	if !h.filesKeyPress(reg, 0, ui.KeyInput{Sym: 'v', Mods: ui.ModCtrl}) {
		reg.mu.Unlock()
		t.Fatal("ctrl+v not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	ents, err := files.List(sess.root, sess.cwd)
	if err != nil {
		t.Fatal(err)
	}
	foundCopy := false
	for _, e := range ents {
		if strings.Contains(e.Name, "(copy)") {
			foundCopy = true
		}
	}
	if !foundCopy {
		t.Fatalf("after paste names = %v", namesOf(ents))
	}
}

func TestFilesRenameCommit(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionRename}) {
		reg.mu.Unlock()
		t.Fatal("rename start not handled")
	}
	if sess.renameFrom == "" || sess.renameDraft == "" {
		reg.mu.Unlock()
		t.Fatalf("rename state = %q %q", sess.renameFrom, sess.renameDraft)
	}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionRename, Text: "renamed.txt"}) {
		reg.mu.Unlock()
		t.Fatal("rename commit not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	if _, err := os.Stat(filepath.Join(sess.root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestFilesDeleteConfirm(t *testing.T) {
	sess := filesSessionWithTwo(t)
	p := sess.entries[0].Path
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionDelete}) {
		reg.mu.Unlock()
		t.Fatal("delete start not handled")
	}
	if len(sess.pendingDelete) != 1 {
		reg.mu.Unlock()
		t.Fatalf("pending = %v", sess.pendingDelete)
	}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionDeleteOK}) {
		reg.mu.Unlock()
		t.Fatal("delete confirm not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file survived confirmed delete")
	}
}

func TestFilesEscapeCancelsRenameFirst(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelFiles]
	reg.mu.Lock()
	reg.beginFilesRenameLocked(sess)
	if !h.keyPress(reg, keyEsc) {
		reg.mu.Unlock()
		t.Fatal("esc not handled")
	}
	if sess.renameFrom != "" {
		reg.mu.Unlock()
		t.Fatal("esc did not cancel rename")
	}
	if len(sess.selected) == 0 {
		reg.mu.Unlock()
		t.Fatal("esc cancelled selection while cancelling rename")
	}
	if reg.panelHosts[PanelFiles] == nil {
		reg.mu.Unlock()
		t.Fatal("esc dismissed the panel")
	}
	reg.mu.Unlock()
}

func TestFilesCrumbActivatesPrefix(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "docs", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	sess := &filesSession{root: root, cwd: sub, mode: files.ModeOpen}
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionCrumb + "0"}) {
		reg.mu.Unlock()
		t.Fatal("crumb not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	rootRes, err := files.Contain(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if sess.cwd != rootRes {
		t.Fatalf("cwd = %q, want %q", sess.cwd, rootRes)
	}
}

func TestFilesMkdirCreatesUntitled(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	sess := &filesSession{root: root, cwd: root, mode: files.ModeOpen}
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionMkdir}) {
		reg.mu.Unlock()
		t.Fatal("mkdir not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	created, err := files.Contain(root, filepath.Join(root, "Untitled Folder"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(created); err != nil {
		t.Fatal(err)
	}
	// The new folder is selected, so F2 renames it instead of making the user
	// find it in the list.
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(sess.selected) != 1 || !sess.hasSelected(created) {
		t.Fatalf("after mkdir selection = %v, want only the new folder", sess.selectedPaths())
	}
	if sess.selectedFileEntry().Path != "" {
		t.Fatal("the new folder was treated as a file")
	}
}

// The render path must not touch the jail: Phone Connect roots the browser on
// an sshfs mount, and a preview read under Registry.mu stalls every panel.
func TestFilesPreviewReadsOffLock(t *testing.T) {
	sess := filesSessionWithTwo(t)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles, place: Placement{Panel: ui.Rect{W: files.PanelWidth, H: files.PanelHeight}}}
	sess.pointerOnEntry(0, 0, 1)
	// The click's rebuild is the render path. It must leave the cache empty and
	// the read in flight, not resolve the preview itself.
	reg.filesTree(h)
	if sess.preview.Text != "" || sess.preview.Image != "" {
		reg.mu.Unlock()
		t.Fatalf("render read the jail under the lock: %+v", sess.preview)
	}
	if sess.previewPath == "" {
		reg.mu.Unlock()
		t.Fatal("no preview read was started for the selection")
	}
	reg.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		done := sess.preview.Text == "x"
		reg.mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("preview never arrived off the lock")
}

// Cut is a visible control, not a shortcut: the button must load the clip as a
// cut so a paste moves rather than duplicates.
func TestFilesCutButtonCutsTheClip(t *testing.T) {
	sess := filesSessionWithTwo(t)
	sess.pointerOnEntry(0, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionCut}) {
		t.Fatal("cut not handled")
	}
	if len(sess.clip) != 1 || !sess.clipCut {
		t.Fatalf("clip = %v cut = %v, want one cut path", sess.clip, sess.clipCut)
	}
}

// A single click on a file must reach xdg-open and a pick through the visible
// control, not only through a double click.
func TestFilesOpenControlActsOnTheSelectedFile(t *testing.T) {
	opened := make(chan string, 1)
	orig := filesOpenPath
	filesOpenPath = func(path string, _ func(error)) {
		opened <- path
	}
	t.Cleanup(func() { filesOpenPath = orig })

	sess := filesSessionWithTwo(t)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	sess.pointerOnEntry(0, 0, 1)
	if e := sess.selectedFileEntry(); e.Path == "" {
		reg.mu.Unlock()
		t.Fatal("a clicked file did not become the selected file")
	}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionOpen}) {
		reg.mu.Unlock()
		t.Fatal("open control not handled")
	}
	reg.mu.Unlock()
	select {
	case got := <-opened:
		want, err := files.Contain(sess.root, filepath.Join(sess.root, "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("opened %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the open control did not launch the handler")
	}
}

// The desktop handler refusing a file (xdg-open exits non-zero) must surface
// on the panel instead of opening nothing in silence.
func TestFilesOpenFailureReachesThePanel(t *testing.T) {
	reports := make(chan func(error), 1)
	orig := filesOpenPath
	filesOpenPath = func(path string, report func(error)) { reports <- report }
	t.Cleanup(func() { filesOpenPath = orig })

	sess := filesSessionWithTwo(t)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	sess.pointerOnEntry(0, 0, 1)
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionOpen}) {
		reg.mu.Unlock()
		t.Fatal("open control not handled")
	}
	reg.mu.Unlock()

	report := <-reports
	report(errors.New("no application knows .txt"))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		msg := sess.err
		reg.mu.Unlock()
		if strings.Contains(msg, "Open failed") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("handler failure never reached the panel (err = %q)", sess.err)
}

func waitFilesIdle(t *testing.T, r *Registry, sess *filesSession) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		idle := r.files == sess && !sess.loading
		r.mu.Unlock()
		if idle {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("files load did not finish")
}

func namesOf(ents []files.Entry) []string {
	out := make([]string, len(ents))
	for i, e := range ents {
		out[i] = e.Name
	}
	return out
}
