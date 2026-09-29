package shell

import (
	"slices"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

type windowSwitcherModel struct {
	windows  []niri.Window
	selected int
}

func newWindowSwitcherModel(snapshot niri.Snapshot) windowSwitcherModel {
	var model windowSwitcherModel
	model.refresh(snapshot)
	return model
}

func (m *windowSwitcherModel) refresh(snapshot niri.Snapshot) {
	selectedID := uint64(0)
	if selected, ok := m.selectedWindow(); ok {
		selectedID = selected.ID
	}
	m.windows = focusedOutputWindows(snapshot)
	m.selected = 0
	for i, window := range m.windows {
		if window.ID == selectedID {
			m.selected = i
			break
		}
	}
}

func focusedOutputWindows(snapshot niri.Snapshot) []niri.Window {
	if snapshot.FocusedOutput == "" {
		return nil
	}
	workspaces := make(map[uint64]struct{})
	for _, workspace := range snapshot.Workspaces {
		if workspace.Output == snapshot.FocusedOutput {
			workspaces[workspace.ID] = struct{}{}
		}
	}
	windows := make([]niri.Window, 0, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		if window.ID == 0 || !window.HasWorkspace {
			continue
		}
		if _, ok := workspaces[window.WorkspaceID]; ok {
			windows = append(windows, window)
		}
	}
	slices.SortFunc(windows, compareWindowMRU)
	return windows
}

// compareWindowMRU orders the most recently focused window first, with null
// timestamps last and Niri IDs as the stable tie break.
func compareWindowMRU(a, b niri.Window) int {
	switch {
	case a.FocusTimestamp == 0 && b.FocusTimestamp != 0:
		return 1
	case a.FocusTimestamp != 0 && b.FocusTimestamp == 0:
		return -1
	case a.FocusTimestamp > b.FocusTimestamp:
		return -1
	case a.FocusTimestamp < b.FocusTimestamp:
		return 1
	case a.ID < b.ID:
		return -1
	case a.ID > b.ID:
		return 1
	default:
		return 0
	}
}

func cloneNiriSnapshot(snapshot niri.Snapshot) niri.Snapshot {
	snapshot.Workspaces = slices.Clone(snapshot.Workspaces)
	snapshot.Windows = slices.Clone(snapshot.Windows)
	snapshot.Layouts.Names = slices.Clone(snapshot.Layouts.Names)
	return snapshot
}

func (m *windowSwitcherModel) cycle(delta int) bool {
	n := len(m.windows)
	if n == 0 || delta == 0 {
		return false
	}
	m.selected = (m.selected + delta%n + n) % n
	return true
}

func (m *windowSwitcherModel) selectIndex(index int) bool {
	if index < 0 || index >= len(m.windows) || index == m.selected {
		return false
	}
	m.selected = index
	return true
}

func (m *windowSwitcherModel) selectedWindow() (niri.Window, bool) {
	if m.selected < 0 || m.selected >= len(m.windows) {
		return niri.Window{}, false
	}
	return m.windows[m.selected], true
}
