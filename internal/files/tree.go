package files

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	// Monitor sibling: a file list with name, size, date and a preview pane.
	PanelWidth  = 800
	PanelHeight = 650

	ModeOpen     = "open"
	ModePickFile = "pick-file"
	ModePickDir  = "pick-directory"

	ActionUp       = "files-up"
	ActionChoose   = "files-choose"
	ActionRetry    = "files-retry"
	ActionHidden   = "files-hidden"
	ActionMkdir    = "files-mkdir"
	ActionCopy     = "files-copy"
	ActionCut      = "files-cut"
	ActionPaste    = "files-paste"
	ActionRename   = "files-rename"
	ActionDelete   = "files-delete"
	ActionDeleteOK = "files-delete-ok"
	ActionDeleteNo = "files-delete-no"
	ActionOpen     = "files-open"
	ActionEntry    = "files-entry-"
	ActionCrumb    = "files-crumb-"

	// Phone Connect actionButton / pill metrics.
	treePad    = 12
	treeGap    = 8
	titleH     = 32
	captionH   = 20
	toolbarH   = 40
	crumbH     = 28
	chooseH    = 40
	btnPad     = 10
	rowPad     = 8
	cardPad    = 12
	previewW   = 280
	previewH   = 180
	minListW   = 160
	minListH   = 80
	nameRunes  = 48
	cardRunes  = 28
	thumbBytes = 512 << 10
	maxThumbs  = 12
)

// Model is one browser frame the host converts and paints.
type Model struct {
	Root, Cwd, Title string
	Mode             string
	Width, Height    int
	Entries          []Entry
	Truncated        bool
	Loading          bool
	Error            string
	Selected         int
	SelectedFile     bool
	Hidden           bool
	CanPaste         bool
	Rename           string
	Deleting         int
	DeletingNames    []string
	PreviewPath      string
	Preview          Preview
}

// Tree is a panel view for one Model. Width/Height size the list viewport.
func Tree(m Model) *v1.Node {
	if m.Width <= 0 {
		m.Width = PanelWidth
	}
	if m.Height <= 0 {
		m.Height = PanelHeight
	}
	title := m.Title
	if title == "" {
		title = filepath.Base(m.Cwd)
	}
	rel := "/"
	if r, err := Rel(m.Root, m.Cwd); err == nil {
		rel = r
	}
	atRoot := rel == "/"
	hiddenIcon, hiddenName := "visibility_off", "Show hidden files"
	if m.Hidden {
		hiddenIcon, hiddenName = "visibility", "Hide hidden files"
	}
	hidden := toolButton(ActionHidden, hiddenIcon, "Hidden", hiddenName, false)
	if m.Hidden {
		hidden.Fill = "chip"
	}
	kids := []*v1.Node{
		{Kind: v1.KindText, Text: title, Size: "title", Bold: true},
		{Kind: v1.KindRow, Gap: treeGap, Children: []*v1.Node{
			toolButton(ActionUp, "chevron_left", "Up", "Up", atRoot),
			toolButton(ActionMkdir, "add", "New folder", "New folder", false),
			hidden,
			toolButton(ActionPaste, "content_paste", "Paste", "Paste", !m.CanPaste),
		}},
		crumbRow(rel),
	}
	// pad×2 + title + toolbar + crumbs, with a gap after each chrome row
	// including the gap before the list.
	chrome := treePad*2 + titleH + toolbarH + crumbH + 3*treeGap
	if m.Mode == ModePickDir {
		choose := toolButton(ActionChoose, "check", "Choose this folder", "Choose this folder", false)
		choose.Fill = "accent"
		kids = append(kids, choose)
		chrome += treeGap + chooseH
	}
	if m.Error != "" {
		kids = append(kids, &v1.Node{
			Kind: v1.KindColumn, Fill: "error-container", Radius: 12, Padding: cardPad, Gap: treeGap,
			Children: []*v1.Node{
				{Kind: v1.KindText, Text: m.Error, Tone: v1.ToneError},
				toolButton(ActionRetry, "restart_alt", "Retry", "Retry", false),
			},
		})
		chrome += treeGap + cardPad*2 + captionH + treeGap + chooseH
	}
	if m.Selected > 0 {
		var sel []*v1.Node
		sel = append(sel, subtle(fmt.Sprintf("%d selected", m.Selected)))
		sel = append(sel, toolButton(ActionCopy, "content_copy", "Copy", "Copy", false))
		sel = append(sel, toolButton(ActionCut, "swap_vert", "Cut", "Cut", false))
		if m.SelectedFile && m.Rename == "" && m.Deleting == 0 {
			open := toolButton(ActionOpen, "play_arrow", "Open", "Open", false)
			if m.Mode == ModePickFile {
				open.Text, open.Name = "Choose this file", "Choose this file"
			}
			open.Fill = "accent"
			sel = append(sel, open)
		}
		if m.Selected == 1 && m.Rename == "" && m.Deleting == 0 {
			sel = append(sel, toolButton(ActionRename, "edit", "Rename", "Rename", false))
		}
		if m.Deleting == 0 {
			del := toolButton(ActionDelete, "delete", "Delete", "Delete", false)
			del.Tone = v1.ToneError
			sel = append(sel, del)
		}
		kids = append(kids, &v1.Node{Kind: v1.KindRow, Gap: treeGap, Children: sel})
		chrome += treeGap + chooseH
	}
	if m.Deleting > 0 {
		ok := toolButton(ActionDeleteOK, "delete", "Delete", "Confirm delete", false)
		ok.Fill = "error-container"
		ok.Tone = v1.ToneError
		kids = append(kids, &v1.Node{
			Kind: v1.KindColumn, Fill: "error-container", Radius: 12, Padding: cardPad, Gap: treeGap,
			Children: []*v1.Node{
				{Kind: v1.KindText, Text: deleteCopy(m), Tone: v1.ToneError},
				{Kind: v1.KindRow, Gap: treeGap, Children: []*v1.Node{
					ok,
					toolButton(ActionDeleteNo, "close", "Cancel", "Cancel delete", false),
				}},
			},
		})
		chrome += treeGap + cardPad*2 + captionH + treeGap + chooseH
	}
	if m.Rename != "" && m.Deleting == 0 {
		kids = append(kids,
			&v1.Node{Kind: v1.KindText, Text: "Rename", Size: "caption", Tone: v1.ToneSubtle, Bold: true},
			&v1.Node{
				Kind: v1.KindTextInput, ID: ActionRename, Key: "files-rename",
				Text: m.Rename, Name: "New name", Role: "textbox",
				Height: toolbarH, SubmitOnEnter: true,
				Events: []v1.EventKind{v1.EventChange, v1.EventSubmit},
			})
		chrome += treeGap + captionH + treeGap + toolbarH
	}
	listH := m.Height - chrome - treeGap
	if listH < minListH {
		listH = minListH
	}
	rows := make([]*v1.Node, 0, len(m.Entries)+1)
	if m.Loading {
		rows = append(rows, subtle("Loading…"))
	}
	// Entries stay painted while a reload is in flight: a slow directory must
	// not blank the listing the user is reading.
	thumbs := 0
	for i, e := range m.Entries {
		thumb := !e.Dir && thumbs < maxThumbs && e.Size > 0 && e.Size <= thumbBytes && imageExt(e.Name)
		if thumb {
			thumbs++
		}
		rows = append(rows, entryRow(i, e, thumb))
	}
	if m.Truncated && !m.Loading {
		rows = append(rows, subtle(fmt.Sprintf("Showing first %d entries", MaxEntries)))
	}
	if len(rows) == 0 {
		rows = append(rows, subtle("Empty folder"))
	}
	// The preview column is always painted, so selecting a file does not
	// resize the list and move every row under the pointer.
	listW := m.Width - 2*treePad - treeGap - previewW
	if listW < minListW {
		listW = minListW
	}
	list := &v1.Node{Kind: v1.KindList, Height: listH, Width: listW, Children: rows}
	kids = append(kids, &v1.Node{Kind: v1.KindRow, Gap: treeGap, Height: listH, PinEnd: true, Children: []*v1.Node{
		list,
		previewPane(m, listH),
	}})
	return &v1.Node{Kind: v1.KindColumn, Padding: treePad, Gap: treeGap, Children: kids}
}

func subtle(text string) *v1.Node {
	return &v1.Node{Kind: v1.KindText, Text: text, Size: "caption", Tone: v1.ToneSubtle}
}

// deleteCopy names what dies. Remove is recursive and there is no undo, so one
// file is named outright and several are counted with their first names.
func deleteCopy(m Model) string {
	switch {
	case len(m.DeletingNames) == 1:
		return fmt.Sprintf("Delete %q?", m.DeletingNames[0])
	case len(m.DeletingNames) == 0:
		return fmt.Sprintf("Delete %d?", m.Deleting)
	default:
		const shown = 3
		names, tail := m.DeletingNames, ""
		if len(names) > shown {
			names, tail = names[:shown], ", …"
		}
		return fmt.Sprintf("Delete %d items? %s%s", m.Deleting, strings.Join(names, ", "), tail)
	}
}

func toolButton(id, icon, text, name string, disabled bool) *v1.Node {
	n := &v1.Node{
		Kind: v1.KindButton, ID: id, Icon: icon, Text: text, Name: name, Role: "button",
		Height: toolbarH, Padding: btnPad,
		Events: []v1.EventKind{v1.EventActivate},
	}
	if disabled {
		n.Disabled = true
	}
	return n
}

// previewPane is always painted, so selecting a file never resizes the list.
// With nothing selected it carries the prompt instead of vanishing; a directory
// or a file with no previewable body gets a card rather than an empty column.
func previewPane(m Model, h int) *v1.Node {
	kids := []*v1.Node{{Kind: v1.KindText, Text: "Preview", Size: "caption", Tone: v1.ToneSubtle, Bold: true}}
	inner := previewW - 2*cardPad
	if inner < 64 {
		inner = 64
	}
	if m.PreviewPath == "" {
		return previewCard(append(kids, subtle("Select a file to preview it")), h)
	}
	kids = append(kids, &v1.Node{Kind: v1.KindText, Text: ellipsis(filepath.Base(m.PreviewPath), cardRunes), Bold: true})
	switch {
	case m.Preview.Image != "":
		kids = append(kids, &v1.Node{Kind: v1.KindImage, Path: m.Preview.Image, ImageW: inner, ImageH: previewImageH(h)})
	case m.Preview.Text != "":
		kids = append(kids, &v1.Node{Kind: v1.KindText, Text: m.Preview.Text, Size: "mono", Tone: v1.ToneSubtle})
	case m.Preview.Dir:
		kids = append(kids, subtle(dirSummary(m.Preview)))
		for _, line := range m.Preview.Lines {
			kids = append(kids, subtle(line))
		}
		if m.Preview.More {
			kids = append(kids, subtle("…"))
		}
	case m.Preview.Size == 0 && m.Preview.ModTime.IsZero():
		kids = append(kids, subtle("No preview"))
	default:
		kids = append(kids, subtle(sizeDate(m.Preview.Size, m.Preview.ModTime)))
	}
	return previewCard(kids, h)
}

func previewCard(kids []*v1.Node, h int) *v1.Node {
	return &v1.Node{
		Kind: v1.KindColumn, Fill: "card", Radius: 12, Padding: cardPad, Gap: treeGap,
		Width: previewW, Height: h, Children: kids,
	}
}

func previewImageH(h int) int {
	imgH := previewH
	if imgH > h-40 {
		imgH = h - 40
	}
	if imgH < 64 {
		imgH = 64
	}
	return imgH
}

// dirSummary counts only what the bounded read saw, so it never claims a total
// it did not read.
func dirSummary(p Preview) string {
	switch {
	case p.More:
		return "First items"
	case len(p.Lines) == 0:
		return "Empty folder"
	default:
		return fmt.Sprintf("%d items", len(p.Lines))
	}
}

func crumbRow(rel string) *v1.Node {
	parts := relParts(rel)
	kids := []*v1.Node{crumbButton(0, "/", len(parts) == 0)}
	show := parts
	if len(parts) > 4 {
		kids = append(kids, crumbButton(-1, "…", true))
		kids[len(kids)-1].ID = ActionCrumb + "ellipsis"
		kids[len(kids)-1].Name = "More"
		show = parts[len(parts)-2:]
		for i, p := range show {
			idx := len(parts) - 2 + i
			kids = append(kids, crumbButton(idx+1, p, i == len(show)-1))
		}
		return &v1.Node{Kind: v1.KindRow, Gap: 4, Children: kids}
	}
	for i, p := range show {
		kids = append(kids, crumbButton(i+1, p, i == len(show)-1))
	}
	return &v1.Node{Kind: v1.KindRow, Gap: 4, Children: kids}
}

func crumbButton(index int, text string, current bool) *v1.Node {
	return &v1.Node{
		Kind: v1.KindButton, ID: fmt.Sprintf("%s%d", ActionCrumb, index),
		Text: text, Name: text, Role: "button", Disabled: current,
		Shape: "small", Height: crumbH, Padding: 6,
		Events: []v1.EventKind{v1.EventActivate},
	}
}

func entryRow(i int, e Entry, thumb bool) *v1.Node {
	label := ellipsis(e.Name, nameRunes)
	leading := &v1.Node{Kind: v1.KindIcon, Icon: "description"}
	if e.Dir {
		leading.Icon = "folder_open"
	}
	if thumb {
		leading = &v1.Node{Kind: v1.KindImage, Path: e.Path, ImageSize: 24}
	}
	inner := []*v1.Node{
		leading,
		{Kind: v1.KindText, Text: label},
	}
	body := &v1.Node{Kind: v1.KindRow, Gap: treeGap, Children: inner}
	if !e.Dir {
		body = &v1.Node{Kind: v1.KindRow, Gap: treeGap, PinEnd: true, Children: []*v1.Node{
			{Kind: v1.KindRow, Gap: treeGap, Children: inner},
			{Kind: v1.KindText, Text: sizeDate(e.Size, e.ModTime), Size: "caption", Tone: v1.ToneSubtle, Tabular: true},
		}}
	}
	row := &v1.Node{
		Kind: v1.KindButton, ID: EntryAction(i),
		Name: e.Name, Role: "button", Shape: "small", Height: toolbarH,
		Events: []v1.EventKind{v1.EventActivate}, Padding: rowPad,
		Children: []*v1.Node{body},
	}
	if label != e.Name {
		row.Tooltip = e.Name
	}
	return row
}

func ellipsis(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func sizeDate(size int64, mod time.Time) string {
	s := humanSize(size)
	if mod.IsZero() {
		return s
	}
	return s + " · " + mod.Format("Jan 2")
}

func humanSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%d KB", n/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(k*k))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(k*k*k))
	}
}

func imageExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

// EntryAction is the action id for files-entry-N.
func EntryAction(i int) string {
	return fmt.Sprintf("%s%d", ActionEntry, i)
}

// EntryIndex parses a files-entry-N action. -1 means not an entry.
func EntryIndex(id string) int {
	var i int
	if _, err := fmt.Sscanf(id, ActionEntry+"%d", &i); err != nil {
		return -1
	}
	if i < 0 {
		return -1
	}
	return i
}

// CrumbIndex parses a files-crumb-N action. -1 means not a crumb.
func CrumbIndex(id string) int {
	var i int
	if _, err := fmt.Sscanf(id, ActionCrumb+"%d", &i); err != nil {
		return -1
	}
	if i < 0 {
		return -1
	}
	return i
}
