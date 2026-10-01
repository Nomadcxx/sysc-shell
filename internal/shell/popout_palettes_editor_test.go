package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRoleGroupsPartitionEveryRoleExactlyOnce(t *testing.T) {
	seen := map[string]int{}
	for _, g := range paletteRoleGroups {
		for _, role := range g.Roles {
			seen[role]++
		}
	}
	for _, role := range theme.RoleNames() {
		if seen[role] != 1 {
			t.Errorf("role %s appears %d times in the editor groups, want 1", role, seen[role])
		}
		delete(seen, role)
	}
	for role := range seen {
		t.Errorf("editor group names %q, which is not a role", role)
	}
	if paletteRoleGroups[0].Title != "Primary" {
		t.Errorf("first group = %q, want Primary (R12)", paletteRoleGroups[0].Title)
	}
}

// savedPalette saves Nord under a name and opens the editor on it.
func savedPalette(t *testing.T) (*Registry, *PanelHost, string) {
	t.Helper()
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	press(t, reg, h, "palette-edit:nord-copy")
	return reg, h, "nord-copy"
}

func editRole(t *testing.T, reg *Registry, h *PanelHost, role, text string) {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	// fieldChanged runs after the field already holds the typed text.
	key := "palette-role:" + role
	if h.fields[key] == nil {
		h.fields[key] = ui.NewField(text)
	}
	h.fields[key].Text = text
	reg.paletteRoleEdited(h, role, text)
}

func modeValue(h *PanelHost, f theme.PaletteFile, role string) string {
	if h.palettes.mode == "light" {
		return f.Light[role]
	}
	return f.Dark[role]
}

func TestEditorShowsEveryRoleAsAField(t *testing.T) {
	reg, h, _ := savedPalette(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	fields := 0
	for _, n := range walk(h.root) {
		if strings.HasPrefix(n.Action, "palette-role:") {
			fields++
			if n.Name == "" || !n.Focusable {
				t.Errorf("role field %s lacks a name or focus", n.Action)
			}
		}
	}
	if fields != len(theme.RoleNames()) {
		t.Fatalf("%d role fields, want %d", fields, len(theme.RoleNames()))
	}
}

// R9: Save, Revert, Use and the name come before the first role, in reading
// and focus order, so nobody scrolls past 49 rows to save.
func TestEditorActionsComeBeforeTheRoles(t *testing.T) {
	reg, h, _ := savedPalette(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	pos := map[string]int{}
	firstRole := -1
	for i, n := range walk(h.root) {
		if strings.HasPrefix(n.Action, "palette-role:") && firstRole < 0 {
			firstRole = i
		}
		if _, seen := pos[n.Action]; !seen && n.Action != "" {
			pos[n.Action] = i
		}
	}
	for _, a := range []string{"palette-edit-name", "palette-save", "palette-revert", "palette-apply", "palette-export", "palette-copy", "palette-back"} {
		i, ok := pos[a]
		if !ok || i > firstRole {
			t.Errorf("%s at %d (present %v), first role at %d", a, i, ok, firstRole)
		}
	}
	if !hasText(h, "nord-copy") {
		t.Error("the editor does not show the ID scripts use")
	}
	if !hasText(h, "whole shell") {
		t.Error("the preview's reach is not stated (R14)")
	}
}

func TestValidEditUpdatesTheDraftAndPreviewsIt(t *testing.T) {
	reg, h, _ := savedPalette(t)
	editRole(t, reg, h, "primary", "#FF0000")
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if got := modeValue(h, h.palettes.draft, "primary"); got != "#ff0000" {
		t.Fatalf("draft primary = %q, want #ff0000 (stored lower-case)", got)
	}
	if !h.palettes.dirty || !h.palettes.preview || !reg.previewing {
		t.Fatalf("dirty=%v preview=%v previewing=%v after a valid edit", h.palettes.dirty, h.palettes.preview, reg.previewing)
	}
	if findAction(h.root, "palette-save-use") == nil {
		t.Error("a dirty draft must offer Save and use, not Use (R11)")
	}
}

func TestInvalidEditNeverReachesTheDraftAndExplainsTheFormat(t *testing.T) {
	reg, h, _ := savedPalette(t)
	reg.mu.Lock()
	before := h.palettes.draft.Dark["primary"]
	reg.mu.Unlock()
	for _, bad := range []string{"", "red", "#12", "#1234567", "#12345g", "#11223344"} {
		editRole(t, reg, h, "primary", bad)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.palettes.draft.Dark["primary"] != before || h.palettes.dirty {
		t.Fatal("an invalid hex changed the draft")
	}
	if !hasText(h, "Use #RRGGBB") {
		t.Error("an invalid field shows a tone but no explanation (R10)")
	}
}

// R3, R7, R10: the failing row carries the error in role labels, the summary
// counts it, and Fix contrast clears it.
func TestContrastErrorsSitOnTheRowAndFixClearsThem(t *testing.T) {
	reg, h, _ := savedPalette(t)
	reg.mu.Lock()
	surface := modeValue(h, h.palettes.draft, "surface")
	mode := h.palettes.mode
	reg.mu.Unlock()
	editRole(t, reg, h, "on_surface", surface)

	reg.mu.Lock()
	failures := paletteFailures(h.palettes.draft, mode)
	onRow := hasText(h, "Hard to read on Surface")
	wire := hasText(h, "on_surface on surface")
	summary := hasText(h, "hard to read in "+mode)
	singular := hasText(h, "1 colour is hard to read") && !hasText(h, "1 colours")
	reg.mu.Unlock()
	if !singular {
		t.Error("one failing colour is counted as \"1 colours are\"")
	}
	if len(failures) == 0 || failures[0].Fg != "on_surface" {
		t.Fatalf("failures = %+v, want one for on_surface", failures)
	}
	if !onRow || wire || !summary {
		t.Fatalf("row error %v, wire names shown %v, summary %v", onRow, wire, summary)
	}

	press(t, reg, h, "palette-fix")
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if left := paletteFailures(h.palettes.draft, mode); len(left) != 0 {
		t.Fatalf("failures remain after Fix contrast: %+v", left)
	}
	if modeValue(h, h.palettes.draft, "surface") != surface {
		t.Fatal("Fix contrast changed a surface; it may move only foregrounds")
	}
}

func TestSaveIsRefusedWhileTheDraftFailsValidation(t *testing.T) {
	reg, h, slug := savedPalette(t)
	reg.mu.Lock()
	surface := h.palettes.draft.Dark["surface"]
	h.palettes.mode = "dark"
	reg.mu.Unlock()
	editRole(t, reg, h, "on_surface", surface)
	press(t, reg, h, "palette-save")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && p.err != "" })
	reg.mu.Lock()
	refusal := h.palettes.err
	reg.mu.Unlock()
	if strings.Contains(refusal, "on_surface") {
		t.Errorf("refusal %q uses wire role names (R7)", refusal)
	}

	f, err := reg.paletteStore.Load(slug)
	if err != nil || f.Dark["on_surface"] == surface {
		t.Fatal("a failing draft was written to disk")
	}
}

func TestSaveWritesAndClearsDirtyAndEndsThePreview(t *testing.T) {
	reg, h, slug := savedPalette(t)
	editRole(t, reg, h, "primary", "#81a1c1")
	press(t, reg, h, "palette-save")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && !p.dirty && p.err == "" && p.notice != "" })

	reg.mu.Lock()
	defer reg.mu.Unlock()
	f, _ := reg.paletteStore.Load(slug)
	if modeValue(h, f, "primary") != "#81a1c1" {
		t.Fatalf("stored primary = %q", modeValue(h, f, "primary"))
	}
	if h.palettes.preview {
		t.Fatal("the preview is still marked as painted after Save")
	}
}

// R11: with a dirty draft, applying must write first. Applying the old file
// while the screen showed the draft made the shell visibly revert.
func TestSaveAndUseWritesBeforeApplying(t *testing.T) {
	reg, h, slug := savedPalette(t)
	editRole(t, reg, h, "primary", "#81a1c1")
	press(t, reg, h, "palette-save-use")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && !p.dirty })
	reg.mu.Lock()
	defer reg.mu.Unlock()
	f, _ := reg.paletteStore.Load(slug)
	if modeValue(h, f, "primary") != "#81a1c1" {
		t.Fatal("Save and use applied without writing the draft")
	}
	if h.draft.ThemeGen.Source != "custom" || h.draft.ThemeGen.Seed != slug {
		t.Fatalf("source = %q/%q after Save and use", h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed)
	}
}

// R8: renaming is the name field plus Save; the slug never changes.
func TestNameFieldRenamesOnSave(t *testing.T) {
	reg, h, slug := savedPalette(t)
	reg.mu.Lock()
	reg.paletteNameEdited(h, "Better name")
	reg.mu.Unlock()
	press(t, reg, h, "palette-save")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && !p.dirty })
	list, _ := reg.PalettesList()
	if len(list) != 1 || list[0].Name != "Better name" || list[0].Slug != slug {
		t.Fatalf("list = %+v, want the name changed and the slug kept", list)
	}
}

func TestRevertDiscardsTheDraftAndTheFieldText(t *testing.T) {
	reg, h, _ := savedPalette(t)
	reg.mu.Lock()
	original := h.palettes.draft.Dark["primary"]
	reg.mu.Unlock()
	editRole(t, reg, h, "primary", "#0a0b0c")
	press(t, reg, h, "palette-revert")
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.palettes.draft.Dark["primary"] != original || h.palettes.dirty {
		t.Fatal("Revert left the edit in place")
	}
	if f := h.fields["palette-role:primary"]; f != nil && strings.EqualFold(f.Text, "#0a0b0c") {
		t.Fatal("the field still shows the reverted text")
	}
}

func TestBackDiscardsUnsavedEditsAndHidesThePreview(t *testing.T) {
	reg, h, slug := savedPalette(t)
	editRole(t, reg, h, "primary", "#0a0b0c")
	press(t, reg, h, "palette-back")
	reg.mu.Lock()
	if h.palettes.editing != "" || h.palettes.dirty || h.palettes.preview {
		reg.mu.Unlock()
		t.Fatal("Back did not leave the editor and its preview")
	}
	reg.mu.Unlock()
	f, _ := reg.paletteStore.Load(slug)
	if strings.EqualFold(f.Dark["primary"], "#0a0b0c") || strings.EqualFold(f.Light["primary"], "#0a0b0c") {
		t.Fatal("an unsaved edit reached the disk")
	}
}

func TestModeToggleSwitchesWhichMapIsEdited(t *testing.T) {
	reg, h, _ := savedPalette(t)
	press(t, reg, h, "palette-mode:light")
	editRole(t, reg, h, "primary", "#0f0f0f")
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.palettes.mode != "light" || h.palettes.draft.Light["primary"] != "#0f0f0f" {
		t.Fatalf("mode %q light primary %q", h.palettes.mode, h.palettes.draft.Light["primary"])
	}
}

func TestRapidEditsLeaveTheLastValidOneInTheDraft(t *testing.T) {
	reg, h, _ := savedPalette(t)
	for _, v := range []string{"#111111", "#22222", "#222222", "#33", "#333333"} {
		editRole(t, reg, h, "secondary", v)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if got := modeValue(h, h.palettes.draft, "secondary"); got != "#333333" {
		t.Fatalf("draft secondary = %q, want the last valid edit #333333", got)
	}
}

func TestEditorRefusesAPaletteNotInTheSnapshot(t *testing.T) {
	reg, h := openPalettesPage(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.paletteOpenEditor(h, "missing")
	if h.palettes.editing != "" {
		t.Fatal("the editor opened on a palette that does not exist")
	}
	if h.palettes.err == "" {
		t.Fatal("no error shown for a palette that cannot be opened")
	}
}
