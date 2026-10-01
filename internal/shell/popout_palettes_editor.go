package shell

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// paletteRoleGroups partitions the 49 roles into the editor's cards, accents
// first, as the Material theme builders order them (R12). A test proves the
// union equals theme.RoleNames() exactly, so a role added to the theme cannot
// be silently uneditable.
var paletteRoleGroups = []struct {
	Title string
	Roles []string
}{
	{"Primary", []string{
		"primary", "on_primary", "primary_container", "on_primary_container", "inverse_primary",
		"primary_fixed", "primary_fixed_dim", "on_primary_fixed", "on_primary_fixed_variant",
	}},
	{"Secondary", []string{
		"secondary", "on_secondary", "secondary_container", "on_secondary_container",
		"secondary_fixed", "secondary_fixed_dim", "on_secondary_fixed", "on_secondary_fixed_variant",
	}},
	{"Tertiary", []string{
		"tertiary", "on_tertiary", "tertiary_container", "on_tertiary_container",
		"tertiary_fixed", "tertiary_fixed_dim", "on_tertiary_fixed", "on_tertiary_fixed_variant",
	}},
	{"Error", []string{"error", "on_error", "error_container", "on_error_container"}},
	{"Surfaces", []string{
		"surface", "surface_dim", "surface_bright", "surface_container_lowest", "surface_container_low",
		"surface_container", "surface_container_high", "surface_container_highest", "surface_variant",
		"background", "inverse_surface", "surface_tint",
	}},
	{"Text and outline", []string{
		"on_surface", "on_surface_variant", "on_background", "inverse_on_surface",
		"outline", "outline_variant", "shadow", "scrim",
	}},
}

// paletteRoleLabel is the one user-facing name of a role (R7). The wire name
// stays in the field's accessible name for people who script against it.
func paletteRoleLabel(role string) string { return settingsOptionLabel(role) }

func otherMode(mode string) string {
	if mode == "light" {
		return "dark"
	}
	return "light"
}

// modeMap is the colour map of the mode being edited.
func (p *paletteUI) modeMap() map[string]string {
	if p.mode == "light" {
		return p.draft.Light
	}
	return p.draft.Dark
}

// paletteFailures is one mode's contrast failures as written, structured
// (R3). A map that does not parse reports none; the hex fields say why.
func paletteFailures(f theme.PaletteFile, mode string) []theme.ContrastFailure {
	src := f.Dark
	if mode == "light" {
		src = f.Light
	}
	tok, err := theme.ParseRoles(src)
	if err != nil {
		return nil
	}
	return tok.ContrastFailures(false)
}

// failingForegrounds counts roles, not pairs: on_surface failing on four
// surfaces is one colour to change.
func failingForegrounds(fs []theme.ContrastFailure) int {
	seen := map[string]bool{}
	for _, f := range fs {
		seen[f.Fg] = true
	}
	return len(seen)
}

func cloneRoleMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// resetPaletteFields drops the retained editor fields so the next build
// re-seeds them from the draft (after open, mode switch, revert and fix).
func resetPaletteFields(h *PanelHost) {
	for k := range h.fields {
		if strings.HasPrefix(k, "palette-role:") || k == "palette-edit-name" {
			delete(h.fields, k)
		}
	}
}

// paletteOpenEditor loads a palette from the snapshot into the draft. No file
// is read: Registry.mu is held (R2).
func (r *Registry) paletteOpenEditor(h *PanelHost, slug string) bool {
	p := &h.palettes
	var found *theme.PaletteInfo
	for i := range r.palettes {
		if r.palettes[i].Slug == slug {
			found = &r.palettes[i]
		}
	}
	switch {
	case found == nil:
		p.finish("", fmt.Errorf("that palette is no longer saved"))
		r.rebuildPanel(h)
		return true
	case found.Err != nil:
		p.finish("", fmt.Errorf("“%s” cannot be opened: %w", found.Name, found.Err))
		r.rebuildPanel(h)
		return true
	}
	f := found.File
	f.Dark, f.Light = cloneRoleMap(f.Dark), cloneRoleMap(f.Light)
	mode := h.draft.ThemeGen.Mode
	if mode != "light" {
		mode = "dark"
	}
	p.editing, p.mode, p.draft, p.dirty = slug, mode, f, false
	p.deleting = ""
	p.clearStatus()
	resetPaletteFields(h)
	h.settingsScroll, h.settingsScrollTop = 0, true // open at the top
	r.rebuildPanel(h)
	return true
}

// paletteLeaveEditor discards the draft and hides a painted preview.
func (r *Registry) paletteLeaveEditor(h *PanelHost) bool {
	p := &h.palettes
	if p.editing == "" && !p.preview {
		return false
	}
	p.leaveEditor()
	p.clearStatus()
	resetPaletteFields(h)
	r.paletteHidePreview(h)
	r.rebuildPanel(h)
	return true
}

// paletteRoleEdited records one role field's text. A valid colour updates the
// draft and previews it; anything else leaves the draft alone and the row
// explains the format. Registry.mu is held.
func (r *Registry) paletteRoleEdited(h *PanelHost, role, text string) {
	p := &h.palettes
	text = strings.TrimSpace(text)
	idx := h.roving.Index()
	if p.editing != "" && theme.ValidRoleColor(text) {
		if m := p.modeMap(); m != nil {
			if _, ok := m[role]; ok && m[role] != strings.ToLower(text) {
				m[role] = strings.ToLower(text)
				p.dirty = true
				r.paletteUpdatePreview(h)
			}
		}
	}
	r.rebuildPanel(h)
	h.roving.Set(idx)
}

// paletteNameEdited records the name field. The name is saved with the
// colours; an unusable name is refused by Save with the store's reason (R8).
func (r *Registry) paletteNameEdited(h *PanelHost, text string) {
	p := &h.palettes
	idx := h.roving.Index()
	if p.editing != "" && strings.TrimSpace(text) != p.draft.Name {
		p.draft.Name = strings.TrimSpace(text)
		p.dirty = true
	}
	r.rebuildPanel(h)
	h.roving.Set(idx)
}

// paletteUpdatePreview paints the repaired draft. Repair keeps the editor
// itself readable while its colours are being changed.
func (r *Registry) paletteUpdatePreview(h *PanelHost) {
	p := &h.palettes
	tok, err := theme.ParseRoles(p.modeMap())
	if err != nil {
		return
	}
	r.themePreviewTokensLocked(tok.Repair(false))
	p.preview = true
}

func (r *Registry) paletteHidePreview(h *PanelHost) {
	if h.palettes.preview {
		h.palettes.preview = false
		go r.themePreviewHide()
	}
}

// paletteFixDraft repairs both modes. Repair moves foregrounds only, so the
// surfaces and accents the user chose stay as they are.
func paletteFixDraft(p *paletteUI) {
	for _, mode := range []string{"dark", "light"} {
		src := p.draft.Dark
		if mode == "light" {
			src = p.draft.Light
		}
		tok, err := theme.ParseRoles(src)
		if err != nil {
			continue
		}
		fixed := tok.Repair(false).Roles()
		if mode == "light" {
			p.draft.Light = fixed
		} else {
			p.draft.Dark = fixed
		}
	}
	p.dirty = true
}

// paletteSaveDraft writes the draft off the lock. With use set, it then makes
// the palette the theme source: written first, applied second, so the shell
// never applies the old file while the screen shows the draft (R11).
func (r *Registry) paletteSaveDraft(h *PanelHost, action string, use bool) bool {
	p := &h.palettes
	if err := p.draft.Validate(); err != nil {
		// Contrast failures are already on their rows in role labels (R7);
		// anything else, such as an unusable name, is said as it is.
		switch {
		case len(paletteFailures(p.draft, p.mode)) > 0:
			err = errors.New("Fix the colours marked below before saving.")
		case len(paletteFailures(p.draft, otherMode(p.mode))) > 0:
			err = fmt.Errorf("Fix the colours marked in %s before saving.", otherMode(p.mode))
		default:
			err = firstLine(err)
		}
		p.finish("", err)
		r.rebuildPanel(h)
		return true
	}
	slug, draft := p.editing, p.draft
	draft.Dark, draft.Light = cloneRoleMap(draft.Dark), cloneRoleMap(draft.Light)
	r.runPaletteOp(h, action, func() (string, error) {
		if err := r.PaletteUpdate(slug, draft); err != nil {
			return "", err
		}
		if use {
			return fmt.Sprintf("Saved and using “%s”", draft.Name), nil
		}
		return fmt.Sprintf("Saved “%s”", draft.Name), nil
	}, func() {
		if use {
			if e := r.settingsForLocked(h.draft).ByPath("appearance.custom"); e != nil {
				h.commitSetting(r, e, slug)
			}
		}
		// An edit made while the save ran is not in the file: it stays
		// unsaved, with its fields and its preview, rather than being
		// marked saved and dropped on the way out.
		if p.editing != slug || !samePaletteFile(p.draft, draft) {
			return
		}
		p.dirty = false
		resetPaletteFields(h)
		r.paletteHidePreview(h)
	})
	return true
}

// samePaletteFile reports whether two drafts hold the same name and colours.
func samePaletteFile(a, b theme.PaletteFile) bool {
	return a.Name == b.Name && maps.Equal(a.Dark, b.Dark) && maps.Equal(a.Light, b.Light)
}

func (r *Registry) handlePaletteEditor(h *PanelHost, n *ui.Node) bool {
	p := &h.palettes
	if p.editing == "" {
		return false
	}
	if mode, ok := strings.CutPrefix(n.Action, "palette-mode:"); ok {
		if mode == "dark" || mode == "light" {
			p.mode = mode
			resetPaletteFields(h)
			if p.dirty || p.preview {
				r.paletteUpdatePreview(h)
			}
		}
		r.rebuildPanel(h)
		return true
	}
	slug := p.editing
	switch n.Action {
	case "palette-fix":
		paletteFixDraft(p)
		resetPaletteFields(h)
		r.paletteUpdatePreview(h)
		r.rebuildPanel(h)
		return true
	case "palette-revert":
		r.paletteHidePreview(h)
		return r.paletteOpenEditor(h, slug)
	case "palette-save":
		return r.paletteSaveDraft(h, n.Action, false)
	case "palette-save-use":
		return r.paletteSaveDraft(h, n.Action, true)
	case "palette-apply":
		e := h.set.ByPath("appearance.custom")
		if e == nil {
			p.finish("", errors.New("this palette is not available as a source"))
		} else {
			p.clearStatus()
			h.commitSetting(r, e, slug)
		}
		r.rebuildPanel(h)
		return true
	case "palette-export":
		r.runPaletteOp(h, n.Action, func() (string, error) {
			path, err := r.PaletteExport(slug)
			return "Exported to " + path, err
		}, nil)
		return true
	case "palette-copy":
		r.runPaletteOp(h, n.Action, func() (string, error) {
			return "Copied as JSON; paste it into Import on another machine", r.PaletteCopyJSON(slug)
		}, nil)
		return true
	}
	return false
}

func firstLine(err error) error {
	line, _, _ := strings.Cut(strings.ReplaceAll(err.Error(), "theme: ", ""), "\n")
	return errors.New(line)
}

func paletteEditorTree(r *Registry, h *PanelHost, m theme.Metrics) *ui.Node {
	p := &h.palettes
	inner := settingsCardInner(h)
	active := h.draft.ThemeGen.Source == "custom" && h.draft.ThemeGen.Seed == p.editing

	back := "Back"
	if p.dirty {
		back = "Discard and go back"
	}
	nameW := max(inner/2, 1)
	name := paletteField(h, "palette-edit-name", "Palette name", p.draft.Name, nameW)
	name.Focusable, name.Name = true, "Palette name"
	state := "No changes"
	if p.dirty {
		state = "Unsaved changes"
	}

	// Apply: Save and use while dirty (R11), Use when clean, a label when active.
	var apply *ui.Node
	switch {
	case p.dirty:
		apply = paletteButton(h, "palette-save-use", "Save and use", "Saving…", m)
	case active:
		apply = &ui.Node{Kind: ui.KindText, Text: "In use", TextRole: theme.RoleLabel, Tone: ui.ToneAccent}
	default:
		apply = paletteDisable(h, pluginManagerButton("palette-apply", "Use this palette", m))
	}
	save := paletteButton(h, "palette-save", "Save", "Saving…", m)
	revert := paletteButton(h, "palette-revert", "Revert", "", m)
	if !p.dirty {
		for _, b := range []*ui.Node{save, revert} {
			b.State |= ui.StateDisabled
			b.Focusable = false
		}
	}
	// Export and copy write the saved file, so they wait for a save.
	export := paletteDisable(h, pluginManagerIconButton("palette-export", "Export to Downloads", "download", m))
	copyJSON := paletteDisable(h, pluginManagerIconButton("palette-copy", "Copy as JSON", "content_copy", m))
	if p.dirty {
		for _, b := range []*ui.Node{export, copyJSON} {
			b.State |= ui.StateDisabled
			b.Focusable = false
		}
		state += ". Save to export or copy"
	}

	head := []*ui.Node{
		{Kind: ui.KindRow, PinEnd: true, Width: inner, Children: []*ui.Node{name, paletteButton(h, "palette-back", back, "", m)}},
		h.wrappedText("ID for scripts and IPC: "+p.editing+" · "+state, theme.RoleCaption, ui.ToneSubtle, inner, 0),
		{Kind: ui.KindRow, PinEnd: true, Width: inner, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{revert, save, apply}},
			{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{export, copyJSON}},
		}},
		{
			Kind: ui.KindSegmented, Key: "palette-mode", Gap: theme.MarginXXS, Width: settingsBodyWidth(h),
			Height: m.CompactControl, Name: "Palette mode", Role: "radiogroup",
			Children: []*ui.Node{paletteModeSegment(p, "dark", "Dark", m), paletteModeSegment(p, "light", "Light", m)},
		},
		h.wrappedText("Edits preview on the whole shell until you save or leave.", theme.RoleCaption, ui.ToneSubtle, inner, 0),
	}
	children := []*ui.Node{settingsGroupCard(h, "Editor", head)}
	children = append(children, paletteStatusLine(r, h, inner)...)

	failures := paletteFailures(p.draft, p.mode)
	otherN := failingForegrounds(paletteFailures(p.draft, otherMode(p.mode)))
	if len(failures) > 0 || otherN > 0 {
		children = append(children, paletteContrastSummary(h, p, failingForegrounds(failures), otherN, inner, m))
	}
	byRole := map[string][]theme.ContrastFailure{}
	for _, f := range failures {
		byRole[f.Fg] = append(byRole[f.Fg], f)
	}
	for _, g := range paletteRoleGroups {
		var rows []*ui.Node
		for _, role := range g.Roles {
			rows = append(rows, paletteRoleRow(h, role, p.modeMap()[role], byRole[role], inner))
		}
		children = append(children, settingsGroupCard(h, g.Title, rows))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
}

func paletteModeSegment(p *paletteUI, mode, label string, m theme.Metrics) *ui.Node {
	b := &ui.Node{
		Kind: ui.KindButton, Action: "palette-mode:" + mode, Name: label, Role: "radio",
		Focusable: true, Height: m.CompactControl, Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if p.mode == mode {
		b.State |= ui.StateSelected
	}
	return b
}

// paletteContrastSummary counts colours, not pairs, offers the other mode
// when it fails too, and says what Fix contrast will touch (R10).
func paletteContrastSummary(h *PanelHost, p *paletteUI, here, other, inner int, m theme.Metrics) *ui.Node {
	var col []*ui.Node
	if here > 0 {
		col = append(col, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%s hard to read in %s", colourCount(here), p.mode), TextRole: theme.RoleLabel, Tone: ui.ToneError})
	}
	if other > 0 {
		o := otherMode(p.mode)
		col = append(col, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: fmt.Sprintf("%s also has %d.", settingsOptionLabel(o), other), Tone: ui.ToneError},
			pluginManagerButton("palette-mode:"+o, "Show "+o, m),
		}})
	}
	col = append(col, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		pluginManagerButton("palette-fix", "Fix contrast", m),
		h.wrappedText("Changes only text and outline colours, in dark and light.", theme.RoleCaption, ui.ToneSubtle, max(inner/2, 1), 0),
	}})
	return &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium, Padding: m.CardPadding, Width: inner,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: col}}}
}

// paletteRoleRow is one role: a swatch, its label and a hex field, with the
// row's own error underneath (R10): the format when the field does not parse,
// the contrast when the colour does but fails.
func paletteRoleRow(h *PanelHost, role, value string, fails []theme.ContrastFailure, inner int) *ui.Node {
	m := h.metrics()
	label := paletteRoleLabel(role)
	fieldW := max(settingsControlWidth(h)/2, 1)
	action := "palette-role:" + role
	field := paletteField(h, action, label, value, fieldW)
	field.Focusable, field.Name = true, label+" ("+role+")"
	text := strings.TrimSpace(field.Text)

	var note string
	switch {
	case !theme.ValidRoleColor(text):
		field.Tone = ui.ToneError
		note = "Use #RRGGBB, for example #88c0d0"
	case len(fails) > 0:
		f := fails[0]
		note = fmt.Sprintf("Hard to read on %s: %.1f:1, needs %.1f:1", paletteRoleLabel(f.Bg), f.Ratio, f.Floor)
		if len(fails) > 1 {
			note += fmt.Sprintf(", and on %d other surfaces", len(fails)-1)
		}
	}
	row := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			paletteSwatch(value, m.CompactControl, label+" "+value),
			{Kind: ui.KindText, Text: label},
		}},
		field,
	}}
	if note == "" {
		return row
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		row,
		h.wrappedText(note, theme.RoleCaption, ui.ToneError, inner, 0),
	}}
}

// colourCount is "1 colour is" or "3 colours are".
func colourCount(n int) string {
	if n == 1 {
		return "1 colour is"
	}
	return fmt.Sprintf("%d colours are", n)
}
