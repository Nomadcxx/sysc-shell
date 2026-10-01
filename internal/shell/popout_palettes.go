package shell

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/screenshot"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// paletteUI is the Palettes page's state. It lives on the settings host, is
// never persisted, and is discarded with the panel (spec P12, P16).
type paletteUI struct {
	editing  string            // slug open in the editor; "" shows the list
	mode     string            // "dark" or "light": the mode the editor shows
	draft    theme.PaletteFile // working copy of the palette being edited
	dirty    bool
	preview  bool   // a draft preview is painted and must be hidden
	deleting string // slug awaiting its second click
	busy     string // action of the operation in flight; "" when idle (R5)
	err      string // last failed operation
	notice   string // last successful operation
}

// finish records an outcome. Messages are for people: the package prefix the
// theme errors carry is dropped (R13).
func (p *paletteUI) finish(notice string, err error) {
	p.err, p.notice = "", ""
	if err != nil {
		p.err = strings.ReplaceAll(err.Error(), "theme: ", "")
		return
	}
	p.notice = notice
}

// clearStatus drops the last outcome when the user moves on (R13).
func (p *paletteUI) clearStatus() { p.err, p.notice = "", "" }

// leaveEditor drops the draft. The caller hides a painted preview.
func (p *paletteUI) leaveEditor() {
	p.editing, p.dirty, p.draft = "", false, theme.PaletteFile{}
}

// paletteName is a saved palette's display name from the snapshot, or its
// slug when the file is gone or unreadable.
func paletteName(list []theme.PaletteInfo, slug string) string {
	for _, p := range list {
		if p.Slug == slug {
			return p.Name
		}
	}
	return slug
}

// paletteSwatch is a rounded square of one colour. Nodes have no arbitrary RGB
// fill, so it is a small solid raster in an image node (spec P13).
func paletteSwatch(hex string, size int, name string) *ui.Node {
	c, err := theme.ParseColor(hex)
	if err != nil {
		c = theme.Color{}
	}
	w := max(size, 1)
	pix := make([]byte, w*w*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = c.B, c.G, c.R, 0xff
	}
	return &ui.Node{
		Kind: ui.KindImage, Image: &ui.Image{Width: w, Height: w, Stride: w * 4, Pix: pix},
		ImageSize: size, Shape: ui.ShapeMedium, Role: "img", Name: name,
	}
}

// paletteButton is an operation button. While any operation runs it is
// disabled, and the one that started it says what it is doing (R5).
func paletteButton(h *PanelHost, action, label, busyLabel string, m theme.Metrics) *ui.Node {
	b := pluginManagerButton(action, label, m)
	if busy := h.palettes.busy; busy != "" {
		b.State |= ui.StateDisabled
		b.Focusable = false
		if busy == action && busyLabel != "" {
			b.Text, b.Name = busyLabel, busyLabel
		}
	}
	return b
}

func paletteDisable(h *PanelHost, b *ui.Node) *ui.Node {
	if h.palettes.busy != "" {
		b.State |= ui.StateDisabled
		b.Focusable = false
	}
	return b
}

// runPaletteOp runs one palette operation off the owner and off Registry.mu,
// then records its outcome and rebuilds the page. Only one runs at a time per
// page: a second request while one is in flight is refused (R5). The
// Registry.Palette* method the op calls refreshes the snapshot itself, so this
// only rebuilds. If the panel closed meanwhile the result is dropped. onOK, if
// set, runs under the lock on success. Registry.mu is held by the caller.
func (r *Registry) runPaletteOp(h *PanelHost, action string, op func() (string, error), onOK func()) {
	if h.palettes.busy != "" {
		return
	}
	h.palettes.busy = action
	h.palettes.clearStatus()
	r.rebuildPanel(h)
	go func() {
		notice, err := op()
		r.mu.Lock()
		if r.panelHosts[PanelSettings] != h {
			r.mu.Unlock()
			return
		}
		h.palettes.busy = ""
		h.palettes.finish(notice, err)
		if err == nil && onOK != nil {
			onOK()
		}
		h.set = r.settingsForLocked(h.draft)
		r.rebuildPanel(h)
		output := h.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelSettings))
	}()
}

// refreshPalettesAsync re-reads the palettes directory off the lock and, if
// Settings is still open and the listing changed, rebuilds it from the new
// snapshot. Called with Registry.mu held when Settings opens and when the
// Palettes section is entered, so a hand-edited file appears without
// reopening the panel. An unchanged listing touches nothing.
func (r *Registry) refreshPalettesAsync(h *PanelHost) {
	go func() {
		r.paletteRefreshMu.Lock()
		defer r.paletteRefreshMu.Unlock()
		list := r.paletteLister(r.paletteStore)
		r.mu.Lock()
		if samePalettes(r.palettes, list) {
			r.mu.Unlock()
			return
		}
		r.palettes = list
		if r.panelHosts[PanelSettings] != h {
			r.mu.Unlock()
			return
		}
		h.set = r.settingsForLocked(h.draft)
		r.rebuildPanel(h)
		output := h.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelSettings))
	}()
}

// samePalettes reports whether two listings would draw the same page.
func samePalettes(a, b []theme.PaletteInfo) bool {
	if len(a) != len(b) {
		return false
	}
	errText := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Slug != y.Slug || x.Name != y.Name || errText(x.Err) != errText(y.Err) ||
			!maps.Equal(x.File.Dark, y.File.Dark) || !maps.Equal(x.File.Light, y.File.Light) {
			return false
		}
	}
	return true
}

func paletteField(h *PanelHost, action, label, seed string, width int) *ui.Node {
	if h.fields == nil {
		h.fields = map[string]*ui.Field{}
	}
	f := h.fields[action]
	if f == nil {
		f = ui.NewField(seed)
		h.fields[action] = f
	}
	n := f.Node(label)
	n.Action = action
	n.Width = width
	settingsFieldInset(h, n)
	return n
}

func paletteFieldText(h *PanelHost, action string) string {
	if f := h.fields[action]; f != nil {
		return strings.TrimSpace(f.Text)
	}
	return ""
}

// paletteImportSeed is where the import field starts: the folder Export
// writes to, shown with ~ when it is under home (R6).
func paletteImportSeed() string {
	dir := screenshot.UserDownloadsDir()
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			dir = filepath.Join("~", rel)
		}
	}
	return dir + "/"
}

// paletteStatusLine is the one place an outcome is shown: under the page
// title on the list, under the header in the editor (R9, R13).
func paletteStatusLine(r *Registry, h *PanelHost, inner int) []*ui.Node {
	var out []*ui.Node
	line := func(text string, tone ui.Tone) {
		out = append(out, h.wrappedText(text, theme.RoleBody, tone, inner, 0))
	}
	if h.palettes.err != "" {
		line(h.palettes.err, ui.ToneError)
	}
	if h.palettes.notice != "" {
		line(h.palettes.notice, ui.ToneSubtle)
	}
	if h.draft.ThemeGen.Source == "custom" && r.themeErr != "" {
		line("The palette in use could not be applied, so the shell keeps its last colours: "+
			strings.ReplaceAll(r.themeErr, "theme: ", ""), ui.ToneError)
	}
	return out
}

func palettesTree(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	if r == nil || r.paletteStore == nil {
		return &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Palettes are unavailable: the shell has no config directory.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		}}
	}
	if h.palettes.editing != "" {
		return paletteEditorTree(r, h, m)
	}
	inner := settingsCardInner(h)
	fieldW := max((inner-settingsControlWidth(h)/2)*3/5, 1)

	save := []*ui.Node{
		h.wrappedText("Keeps the colours in effect now, dark and light, under a name you choose.", theme.RoleCaption, ui.ToneSubtle, inner, 0),
		{Kind: ui.KindRow, PinEnd: true, Width: inner, Children: []*ui.Node{
			paletteField(h, "palette-name", "Palette name", r.paletteSuggestedNameLocked(h.draft), fieldW),
			paletteButton(h, "palette-save-current", "Save", "Saving…", m),
		}},
	}
	imp := []*ui.Node{
		h.wrappedText("A palette file, or any JSON with dark and light colour maps, such as matugen's colors.json. Up to 64 KiB.", theme.RoleCaption, ui.ToneSubtle, inner, 0),
		{Kind: ui.KindRow, PinEnd: true, Width: inner, Children: []*ui.Node{
			paletteField(h, "palette-import-path", "Path to a palette file", paletteImportSeed(), fieldW),
			paletteButton(h, "palette-import-file", "Import", "Importing…", m),
		}},
		{Kind: ui.KindRow, PinEnd: true, Width: inner, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Or paste a palette copied as JSON.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
			paletteButton(h, "palette-import-paste", "Paste from clipboard", "Importing…", m),
		}},
	}
	// The outcome of the last action sits first, so it is in view whichever
	// card the action was in (R9, R13).
	children := paletteStatusLine(r, h, inner)
	children = append(children,
		settingsGroupCard(h, "Saved", paletteRows(r, h, m, inner)),
		settingsGroupCard(h, "Save current", save),
		settingsGroupCard(h, "Import", imp),
	)
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
}

func paletteRows(r *Registry, h *PanelHost, m theme.Metrics, inner int) []*ui.Node {
	if len(r.palettes) == 0 {
		return []*ui.Node{h.wrappedText("No saved palettes yet. Save the current colours or import a file below.", theme.RoleCaption, ui.ToneSubtle, inner, 0)}
	}
	var rows []*ui.Node
	for _, p := range r.palettes {
		rows = append(rows, paletteRow(r, h, m, inner, p))
	}
	return rows
}

// paletteRow is one saved palette: its colours, its name, one status line and
// at most three controls (R8). Rename, export and copy live in the editor.
func paletteRow(r *Registry, h *PanelHost, m theme.Metrics, inner int, p theme.PaletteInfo) *ui.Node {
	active := h.draft.ThemeGen.Source == "custom" && h.draft.ThemeGen.Seed == p.Slug
	del := paletteDisable(h, pluginManagerIconButton("palette-delete:"+p.Slug, "Delete "+p.Name, "delete", m))

	caption, tone := "Dark and light", ui.ToneSubtle
	var actions []*ui.Node
	switch {
	case p.Err != nil:
		// The file is named so it can be repaired by hand.
		caption, tone = fmt.Sprintf("Cannot be used (%s.json): %s", p.Slug, paletteFileReason(p)), ui.ToneError
		actions = []*ui.Node{del}
	case h.palettes.deleting == p.Slug:
		caption, tone = "Delete this palette? This cannot be undone.", ui.ToneError
		if active {
			caption = "Delete this palette? It is in use; the shell keeps its current colours until you choose another."
		}
		actions = []*ui.Node{
			paletteButton(h, "palette-delete-cancel", "Cancel", "", m),
			paletteDisable(h, pluginManagerDestructive("palette-delete-confirm:"+p.Slug, "Delete", m)),
		}
		if h.palettes.busy == "palette-delete-confirm:"+p.Slug {
			actions[1].Text, actions[1].Name = "Deleting…", "Deleting…"
		}
	default:
		var use *ui.Node
		if active {
			// A status, not a control: pressing it again would do nothing (R8).
			use = &ui.Node{Kind: ui.KindText, Text: "In use", TextRole: theme.RoleLabel, Tone: ui.ToneAccent}
		} else {
			use = paletteDisable(h, pluginManagerButton("palette-use:"+p.Slug, "Use", m))
			use.Name = "Use " + p.Name
		}
		actions = []*ui.Node{
			use,
			paletteDisable(h, pluginManagerIconButton("palette-edit:"+p.Slug, "Edit colours of "+p.Name, "tune", m)),
			del,
		}
	}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Height: m.CompactControl, Children: actions}
	trailingW, _, err := ui.Measure(trailing, h.measureText())
	if err != nil {
		trailingW = inner / 2
	}
	// Every row reserves the strip's width, so a corrupt file's name lines
	// up with the names of the palettes beside it.
	strip := paletteSwatchStrip(p.File, h.draft.ThemeGen.Mode, m)
	stripW := 0
	if w, _, err := ui.Measure(strip, h.measureText()); err == nil {
		stripW = w + theme.MarginM
	}
	if p.Err != nil {
		strip = paletteBlankStrip(m)
	}
	labelW := max(inner-trailingW-stripW-theme.MarginL, 1)
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelW, Children: []*ui.Node{
		{Kind: ui.KindText, Text: p.Name, TextRole: theme.RoleLabel, MaxWidth: labelW},
		h.wrappedText(caption, theme.RoleCaption, tone, labelW, 0),
	}}
	lead := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{strip, label}}
	return &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Gap: theme.MarginM, Children: []*ui.Node{lead, trailing}}
}

// paletteBlankStrip holds a swatch strip's place in a row whose file cannot be
// read: five transparent, decorative squares laid out exactly as the strip is.
func paletteBlankStrip(m theme.Metrics) *ui.Node {
	size := max(m.CompactControl/2, 1)
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXXS}
	for range 5 {
		row.Children = append(row.Children, &ui.Node{
			Kind: ui.KindImage, ImageSize: size,
			Image: &ui.Image{Width: size, Height: size, Stride: size * 4, Pix: make([]byte, size*size*4)},
		})
	}
	return row
}

// paletteSwatchStrip is the five-colour preview of one mode of a palette.
func paletteSwatchStrip(f theme.PaletteFile, mode string, m theme.Metrics) *ui.Node {
	src := f.Dark
	if mode == "light" {
		src = f.Light
	}
	size := m.CompactControl / 2
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXXS}
	for _, role := range []string{"surface", "primary", "secondary", "tertiary", "error"} {
		row.Children = append(row.Children, paletteSwatch(src[role], size, settingsOptionLabel(role)+" "+src[role]))
	}
	return row
}

// handlePalettes routes the palette- actions. Registry.mu is held. It returns
// false for an action that is not its own, so activate falls through.
func (r *Registry) handlePalettes(h *PanelHost, n *ui.Node) bool {
	if n == nil || h.id != PanelSettings || !strings.HasPrefix(n.Action, "palette-") {
		return false
	}
	p := &h.palettes
	action := n.Action
	rebuild := func() { p.clearStatus(); r.rebuildPanel(h) }

	switch action {
	case "palette-save-current":
		name, cfg := paletteFieldText(h, "palette-name"), h.draft
		r.runPaletteOp(h, action, func() (string, error) {
			if _, err := r.PaletteSaveCurrent(cfg, name); err != nil {
				return "", err
			}
			return fmt.Sprintf("Saved “%s”", name), nil
		}, func() { delete(h.fields, "palette-name") })
		return true
	case "palette-import-file":
		path := paletteFieldText(h, "palette-import-path")
		r.runPaletteOp(h, action, func() (string, error) {
			slug, adjusted, err := r.PaletteImportFile(path)
			return r.importNotice(slug, adjusted), err
		}, func() { delete(h.fields, "palette-import-path") })
		return true
	case "palette-import-paste":
		r.runPaletteOp(h, action, func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			clip, err := readSystemClipboard(ctx)
			if err != nil {
				return "", err
			}
			slug, adjusted, err := r.PaletteImport([]byte(clip.Text), "")
			return r.importNotice(slug, adjusted), err
		}, nil)
		return true
	case "palette-delete-cancel":
		p.deleting = ""
		rebuild()
		return true
	case "palette-back":
		return r.paletteLeaveEditor(h)
	}

	verb, slug, ok := strings.Cut(strings.TrimPrefix(action, "palette-"), ":")
	if !ok {
		return r.handlePaletteEditor(h, n)
	}
	switch verb {
	case "use":
		e := h.set.ByPath("appearance.custom")
		if e == nil {
			p.finish("", fmt.Errorf("“%s” is not available", paletteName(r.palettes, slug)))
			r.rebuildPanel(h)
			return true
		}
		p.clearStatus()
		h.commitSetting(r, e, slug)
		r.rebuildPanel(h)
		return true
	case "edit":
		return r.paletteOpenEditor(h, slug)
	case "delete":
		if p.busy != "" {
			return true
		}
		p.deleting = slug
		rebuild()
		return true
	case "delete-confirm":
		name := paletteName(r.palettes, slug)
		r.runPaletteOp(h, action, func() (string, error) {
			return fmt.Sprintf("Deleted “%s”", name), r.PaletteDelete(slug)
		}, func() { p.deleting = "" })
		return true
	}
	return r.handlePaletteEditor(h, n)
}

// importNotice names the imported palette and says when colours were moved
// to meet the contrast floor. It runs off the lock, after the Palette* call
// refreshed the snapshot.
func (r *Registry) importNotice(slug string, adjusted int) string {
	if slug == "" {
		return ""
	}
	r.mu.Lock()
	name := paletteName(r.palettes, slug)
	r.mu.Unlock()
	if adjusted > 0 {
		return fmt.Sprintf("Imported “%s”; %d text colours were adjusted so they stay readable", name, adjusted)
	}
	return fmt.Sprintf("Imported “%s”", name)
}

// paletteFileReason is why a stored file cannot be used, without the store's
// prefixes that repeat what the row already says.
func paletteFileReason(p theme.PaletteInfo) string {
	reason := strings.ReplaceAll(p.Err.Error(), "theme: ", "")
	for _, prefix := range []string{fmt.Sprintf("palette %q: ", p.Slug), "palette file: "} {
		reason = strings.TrimPrefix(reason, prefix)
	}
	return reason
}
