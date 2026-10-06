package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestTreeFitsTheFilesPanel(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{PanelWidth, PanelHeight}, {400, 500}, {360, 400}} {
		tree := Tree(Model{
			Root: root, Cwd: root, Title: "Files", Mode: ModeOpen,
			Width: size[0], Height: size[1], Entries: entries,
		})
		if err := v1.Validate(tree, v1.ViewPanel); err != nil {
			t.Errorf("%dx%d: %v", size[0], size[1], err)
		}
	}
}

func TestTreeHasUpEntriesAndChooseByMode(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	open := Tree(Model{Root: root, Cwd: root, Title: "Phone", Mode: ModeOpen, Width: PanelWidth, Height: PanelHeight, Entries: entries})
	if findID(open, ActionUp) == nil {
		t.Fatal("missing Up")
	}
	if findID(open, ActionChoose) != nil {
		t.Fatal("open mode showed Choose")
	}
	if findID(open, ActionUp).Disabled != true {
		t.Fatal("Up should be disabled at the root")
	}
	docs := findNamed(open, "docs")
	if docs == nil || leadingIcon(docs) != "folder_open" {
		t.Fatalf("dir row = %+v", docs)
	}
	file := findNamed(open, "a.txt")
	if file == nil {
		t.Fatal("missing a.txt row")
	}
	if !strings.Contains(rowLabel(file), "a.txt") {
		t.Fatalf("file label = %q", rowLabel(file))
	}
	if !strings.Contains(rowLabel(file), "B") {
		t.Fatalf("file row missing size: %q", rowLabel(file))
	}
	pick := Tree(Model{Root: root, Cwd: root, Title: "Phone", Mode: ModePickDir, Width: PanelWidth, Height: PanelHeight, Entries: entries})
	if findID(pick, ActionChoose) == nil {
		t.Fatal("pick-directory missing Choose")
	}
}

func findID(n *v1.Node, id string) *v1.Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if got := findID(c, id); got != nil {
			return got
		}
	}
	return nil
}

func findText(n *v1.Node, text string) *v1.Node {
	if n == nil {
		return nil
	}
	if n.Text == text {
		return n
	}
	for _, c := range n.Children {
		if got := findText(c, text); got != nil {
			return got
		}
	}
	return nil
}

func TestTreeTruncatedNameCarriesTooltip(t *testing.T) {
	long := strings.Repeat("a", 60) + ".txt"
	m := Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Entries: []Entry{{Name: long, Path: "/r/" + long}}}
	tree := Tree(m)
	row := findButton(tree, ActionEntry+"0")
	if row == nil {
		t.Fatal("no entry row")
	}
	if row.Tooltip != long {
		t.Fatalf("tooltip = %q, want full name", row.Tooltip)
	}
	if row.Padding != 8 {
		t.Fatalf("padding = %d, want 8", row.Padding)
	}
}

func TestTreePanelMatchesMonitorSize(t *testing.T) {
	if PanelWidth != 800 || PanelHeight != 650 {
		t.Fatalf("files panel %dx%d, want 800x650 (monitor sibling)", PanelWidth, PanelHeight)
	}
}

func TestTreeToolbarButtonsAreLabeled(t *testing.T) {
	tree := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Selected: 2, SelectedFile: true, CanPaste: true})
	want := []struct{ id, text string }{
		{ActionUp, "Up"},
		{ActionMkdir, "New folder"},
		{ActionHidden, "Hidden"},
		{ActionPaste, "Paste"},
		{ActionCopy, "Copy"},
		{ActionCut, "Cut"},
		{ActionOpen, "Open"},
		{ActionDelete, "Delete"},
	}
	for _, w := range want {
		b := findID(tree, w.id)
		if b == nil {
			t.Errorf("missing %s", w.id)
			continue
		}
		if b.Text != w.text {
			t.Errorf("%s text = %q, want %q", w.id, b.Text, w.text)
		}
	}
}

// A single click on a file must leave a labelled way to act: Open in open
// mode, Choose this file in pick-file mode, and nothing for a folder.
func TestTreeSelectedFileOffersPrimaryAction(t *testing.T) {
	base := Model{Root: "/r", Cwd: "/r", Selected: 1, SelectedFile: true}
	open := Tree(withMode(base, ModeOpen))
	if b := findID(open, ActionOpen); b == nil || b.Text != "Open" {
		t.Fatalf("open mode action = %+v, want a labelled Open", b)
	}
	pick := Tree(withMode(base, ModePickFile))
	if b := findID(pick, ActionOpen); b == nil || b.Text != "Choose this file" {
		t.Fatalf("pick-file action = %+v, want Choose this file", b)
	}
	folder := base
	folder.SelectedFile = false
	if b := findID(Tree(withMode(folder, ModeOpen)), ActionOpen); b != nil {
		t.Fatalf("a folder selection offered %q", b.Text)
	}
	if b := findID(Tree(withMode(base, ModeOpen)), ActionCut); b == nil {
		t.Fatal("Cut is keyboard-only: no control beside Copy")
	}
	empty := Tree(withMode(Model{Root: "/r", Cwd: "/r"}, ModeOpen))
	if b := findID(empty, ActionCut); b != nil {
		t.Fatal("Cut showed with no selection")
	}
}

func withMode(m Model, mode string) Model {
	m.Mode = mode
	return m
}

// The preview column is always painted: selecting a file must not resize the
// list, and an empty selection must say what to do rather than leave a gap.
func TestTreePreviewSitsBesideList(t *testing.T) {
	plain := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Width: PanelWidth, Height: PanelHeight})
	list := findKind(plain, v1.KindList)
	if list == nil {
		t.Fatal("missing list")
	}
	if list.Width <= 0 {
		t.Fatal("list needs an explicit width so preview can sit beside it")
	}
	if findText(plain, "Select a file to preview it") == nil {
		t.Fatal("no prompt in the empty preview column")
	}
	shown := Tree(Model{
		Root: "/r", Cwd: "/r", Mode: ModeOpen, Width: PanelWidth, Height: PanelHeight,
		PreviewPath: "/r/a.txt", Preview: Preview{Text: "hello"},
	})
	withPreview := findKind(shown, v1.KindList)
	if withPreview.Width != list.Width {
		t.Fatalf("list width %d with a preview, %d without: selection reflows the list", withPreview.Width, list.Width)
	}
	if findText(shown, "hello") == nil {
		t.Fatal("missing preview text")
	}
	if findText(plain, "Select a file to preview it") == nil {
		t.Fatal("preview column vanished with no selection")
	}
}

// A folder or a file with no previewable body must still fill the column, or
// selecting one removes 280px of useful width and says nothing.
func TestTreePreviewFallsBackForDirsAndPlainFiles(t *testing.T) {
	when := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	dir := Tree(Model{
		Root: "/r", Cwd: "/r", Mode: ModeOpen, PreviewPath: "/r/docs",
		Preview: Preview{Dir: true, Lines: []string{"a.txt", "b.txt"}},
	})
	if findText(dir, "2 items") == nil {
		t.Fatal("directory preview did not count its children")
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if findText(dir, name) == nil {
			t.Fatalf("directory preview dropped child %q", name)
		}
	}
	more := Tree(Model{
		Root: "/r", Cwd: "/r", Mode: ModeOpen, PreviewPath: "/r/docs",
		Preview: Preview{Dir: true, Lines: []string{"a.txt"}, More: true},
	})
	if findText(more, "First items") == nil {
		t.Fatal("a truncated directory read claimed a total")
	}
	binary := Tree(Model{
		Root: "/r", Cwd: "/r", Mode: ModeOpen, PreviewPath: "/r/app.bin",
		Preview: Preview{Size: 2048, ModTime: when},
	})
	if findText(binary, "2 KB · Oct 4") == nil {
		t.Fatal("no size and date for a file with no previewable body")
	}
	if findText(Tree(Model{Root: "/r", Cwd: "/r", PreviewPath: "/r/gone"}), "No preview") == nil {
		t.Fatal("an unreadable file claimed a size instead of saying so")
	}
}

// Remove is recursive and there is no undo, so the confirm copy names what dies.
func TestTreeDeleteCopyNamesFiles(t *testing.T) {
	one := Tree(Model{Root: "/r", Cwd: "/r", Deleting: 1, DeletingNames: []string{"readme.md"}})
	if findText(one, `Delete "readme.md"?`) == nil {
		t.Fatalf("single-file confirm does not name it: %q", deleteCopy(Model{Deleting: 1, DeletingNames: []string{"readme.md"}}))
	}
	many := Tree(Model{Root: "/r", Cwd: "/r", Deleting: 4, DeletingNames: []string{"a", "b", "c", "d"}})
	if findText(many, "Delete 4 items? a, b, c, …") == nil {
		t.Fatalf("multi-file confirm = %q", deleteCopy(Model{Deleting: 4, DeletingNames: []string{"a", "b", "c", "d"}}))
	}
	if findText(many, "Delete 4?") != nil {
		t.Fatal("bare count still shown")
	}
	if findText(Tree(Model{Root: "/r", Cwd: "/r", Deleting: 2}), "Delete 2?") == nil {
		t.Fatal("no names available: the count must still confirm")
	}
}

func TestTreeRenameHasVisibleLabel(t *testing.T) {
	tree := Tree(Model{Root: "/r", Cwd: "/r", Rename: "readme.md"})
	if findText(tree, "Rename") == nil {
		t.Fatal("the rename field has no visible label")
	}
	if findID(tree, ActionRename) == nil {
		t.Fatal("missing rename field")
	}
}

// A slow directory must not blank the listing the user is reading.
func TestTreeLoadingKeepsEntries(t *testing.T) {
	entries := []Entry{{Name: "a.txt", Path: "/r/a.txt", Size: 1}, {Name: "docs", Path: "/r/docs", Dir: true}}
	tree := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Loading: true, Entries: entries})
	if findText(tree, "Loading…") == nil {
		t.Fatal("missing loading caption")
	}
	if findNamed(tree, "a.txt") == nil || findNamed(tree, "docs") == nil {
		t.Fatal("loading blanked the previous listing")
	}
}

func findKind(n *v1.Node, kind v1.NodeKind) *v1.Node {
	if n == nil {
		return nil
	}
	if n.Kind == kind {
		return n
	}
	for _, c := range n.Children {
		if got := findKind(c, kind); got != nil {
			return got
		}
	}
	return nil
}

func TestTreeTruncationCopy(t *testing.T) {
	m := Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Truncated: true}
	tree := Tree(m)
	if findText(tree, "Showing first 200 entries") == nil {
		t.Fatal("missing truncation copy")
	}
}

func findButton(n *v1.Node, id string) *v1.Node {
	if n.Kind == v1.KindButton && n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if got := findButton(c, id); got != nil {
			return got
		}
	}
	return nil
}

func TestTreeBreadcrumbsReplaceCaption(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	sub := filepath.Join(docs, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	tree := Tree(Model{Root: root, Cwd: sub, Title: "Files", Mode: ModeOpen, Width: PanelWidth, Height: PanelHeight})
	if findID(tree, ActionCrumb+"0") == nil {
		t.Fatal("missing root crumb")
	}
	if findID(tree, ActionCrumb+"2") == nil {
		t.Fatal("missing last crumb")
	}
	if findText(tree, "/docs/sub") != nil {
		t.Fatal("raw relative path caption still present")
	}
}

func TestTreeThumbnailRowKeepsName(t *testing.T) {
	root := t.TempDir()
	name := "pic.png"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := Tree(Model{
		Root: root, Cwd: root, Mode: ModeOpen, Width: PanelWidth, Height: PanelHeight,
		Entries: []Entry{{Name: name, Path: path, Size: 1}},
	})
	row := findNamed(tree, name)
	if row == nil {
		t.Fatal("missing image row")
	}
	if !strings.Contains(rowLabel(row), name) {
		t.Fatalf("thumbnail row dropped the name: %q", rowLabel(row))
	}
}

func findNamed(n *v1.Node, name string) *v1.Node {
	if n == nil {
		return nil
	}
	if n.Name == name && n.Kind == v1.KindButton {
		return n
	}
	for _, c := range n.Children {
		if got := findNamed(c, name); got != nil {
			return got
		}
	}
	return nil
}

func leadingIcon(n *v1.Node) string {
	if n.Icon != "" {
		return n.Icon
	}
	var walk func(*v1.Node) string
	walk = func(n *v1.Node) string {
		if n.Kind == v1.KindIcon {
			return n.Icon
		}
		for _, c := range n.Children {
			if got := walk(c); got != "" {
				return got
			}
		}
		return ""
	}
	return walk(n)
}

func rowLabel(n *v1.Node) string {
	if n == nil {
		return ""
	}
	parts := []string{n.Text}
	var walk func(*v1.Node)
	walk = func(n *v1.Node) {
		if n.Kind == v1.KindText && n.Text != "" {
			parts = append(parts, n.Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}

func TestTreeSelectedCaption(t *testing.T) {
	tree := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Selected: 2})
	if findText(tree, "2 selected") == nil {
		t.Fatal("missing selection caption")
	}
	if findText(Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen}), "0 selected") != nil {
		t.Fatal("empty selection still showed a count")
	}
}

func TestTreeLoadingCaption(t *testing.T) {
	tree := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Loading: true})
	if findText(tree, "Loading…") == nil {
		t.Fatal("missing loading caption")
	}
}

func TestTreeLoadingHidesTruncationCopy(t *testing.T) {
	tree := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Loading: true, Truncated: true})
	if findText(tree, "Loading…") == nil {
		t.Fatal("missing loading caption")
	}
	if findText(tree, "Showing first 200 entries") != nil {
		t.Fatal("truncation copy shown while loading")
	}
}

func TestTreeErrorHasRetry(t *testing.T) {
	tree := Tree(Model{Root: "/r", Cwd: "/r", Mode: ModeOpen, Error: "boom"})
	if findButton(tree, ActionRetry) == nil {
		t.Fatal("missing retry button")
	}
}

func TestTreeShowsTextPreview(t *testing.T) {
	tree := Tree(Model{
		Root: "/r", Cwd: "/r", Mode: ModeOpen,
		PreviewPath: "/r/a.txt", Preview: Preview{Text: "hello"},
	})
	if findText(tree, "hello") == nil {
		t.Fatal("missing preview text")
	}
}

// The widest chrome is a single selected file with a preview: it carries the
// selection row, the rename label and both columns. It must still validate at
// the target box and on the short output the panel is clamped to.
func TestTreeFitsTheFilesPanelWhenSelected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{PanelWidth, PanelHeight}, {400, 500}, {360, 400}} {
		m := Model{
			Root: root, Cwd: root, Title: "Files", Mode: ModeOpen,
			Width: size[0], Height: size[1], Entries: entries,
			Selected: 1, SelectedFile: true, CanPaste: true,
			PreviewPath: entries[0].Path, Preview: Preview{Text: "hello"},
		}
		if err := v1.Validate(Tree(m), v1.ViewPanel); err != nil {
			t.Errorf("%dx%d selected: %v", size[0], size[1], err)
		}
		deleting := m
		deleting.Rename, deleting.Deleting = "", 1
		deleting.DeletingNames = []string{entries[0].Name}
		if err := v1.Validate(Tree(deleting), v1.ViewPanel); err != nil {
			t.Errorf("%dx%d deleting: %v", size[0], size[1], err)
		}
	}
}

// Every glyph the panel paints must exist in one of the shell's two icon
// catalogues: iconNode fails the whole view build on a name in neither,
// which is how selecting a folder crashed the panel with "no icon named
// content_cut".
func TestTreeIconsExistInShellInventory(t *testing.T) {
	models := []Model{
		{Root: "/r", Cwd: "/r", Mode: ModeOpen, Selected: 1, SelectedFile: true, CanPaste: true, Error: "boom"},
		{Root: "/r", Cwd: "/r", Mode: ModePickDir, Selected: 2, Deleting: 1},
		{Root: "/r", Cwd: "/r", Mode: ModePickFile, Selected: 1, SelectedFile: true, Rename: "new.txt", Entries: []Entry{{Name: "a.txt"}, {Name: "lib", Dir: true}}},
	}
	for _, m := range models {
		var walk func(*v1.Node)
		walk = func(n *v1.Node) {
			if n.Icon != "" && !render.ValidMaterialIcon(n.Icon) {
				if _, ok := render.IconByName(n.Icon); !ok {
					t.Errorf("icon %q is in no shell inventory", n.Icon)
				}
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(Tree(m))
	}
}
