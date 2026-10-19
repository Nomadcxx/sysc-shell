package shell

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const keyboardLayoutListKey = "keyboard-layouts"

// Niri does not emit a switched event when an accepted request is already
// active, so an authoritative query bounds that no-op case.
// ponytail: query once after the event-stream window, without polling; a
// request-correlated Niri confirmation would remove this fallback.
const keyboardLayoutConfirmationDelay = 250 * time.Millisecond

func buildKeyboardLayoutWidget() textWidget {
	node := &ui.Node{
		Kind: ui.KindText, Action: panelKeyboardLayoutAction,
		Role: "button", Focusable: true, Absent: true,
	}
	w := textWidget{node: node, hideWhenAbsent: true}
	w.refresh = func(view barView) bool {
		before := *node
		node.Absent = len(view.Layouts.Names) < 2
		node.Tooltip = i18n.T(view.Locale, "keyboard-layout.name", "Keyboard layout")
		node.Text = ""
		node.Name = node.Tooltip
		if view.Layouts.Current >= 0 && view.Layouts.Current < len(view.Layouts.Names) {
			name := keyboardLayoutDisplayName(view.Layouts.Names[view.Layouts.Current], view.Layouts.Current)
			node.Text = name
			node.Name += ": " + name
		}
		return nodeVisualStateChanged(before, *node) || before.Name != node.Name || before.Tooltip != node.Tooltip
	}
	return w
}

func keyboardLayoutDisplayName(name string, index int) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "Layout " + strconv.Itoa(index+1)
}

func keyboardLayoutOptionName(locale, name string, current bool) string {
	if current {
		return i18n.T(locale, "keyboard-layout.current", "Current layout") + ": " + name
	}
	return i18n.T(locale, "keyboard-layout.select", "Select layout") + ": " + name
}

func keyboardLayoutTree(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	layouts := r.niriSnapshot.Layouts
	locale := i18n.Resolve(r.cfg.Session.Language)
	title := i18n.T(locale, "keyboard-layout.name", "Keyboard layout")
	closeName := i18n.T(locale, "keyboard-layout.close", "Close")
	close := &ui.Node{
		Kind: ui.KindButton, Text: "×", Action: "keyboard-layout-close",
		Name: closeName, Role: "button", Focusable: true,
		Width: m.CompactControl, Height: m.CompactControl, Shape: ui.ShapeCircle,
	}
	header := &ui.Node{Kind: ui.KindRow, Height: m.StandardControl, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: title, Name: title, Role: "heading", TextRole: theme.RoleTitle},
		close,
	}}
	children := []*ui.Node{header}
	status := ""
	switch {
	case len(layouts.Names) < 2:
		status = i18n.T(locale, "keyboard-layout.need-multiple", "At least two layouts are needed to switch.")
	case h.layoutUnavailable:
		status = i18n.T(locale, "keyboard-layout.unavailable", "Layout switching is unavailable")
	case h.layoutRequested >= 0 && h.layoutRequested < len(layouts.Names):
		name := keyboardLayoutDisplayName(layouts.Names[h.layoutRequested], h.layoutRequested)
		status = fmt.Sprintf(i18n.T(locale, "keyboard-layout.switching", "Switching to %s…"), name)
	}
	if status != "" {
		children = append(children, &ui.Node{
			Kind: ui.KindText, Text: status, Name: status, Role: "status",
			TextRole: theme.RoleCaption,
			Tone: func() ui.Tone {
				if h.layoutUnavailable {
					return ui.ToneError
				}
				return ui.ToneNormal
			}(),
		})
	}
	if len(layouts.Names) > 1 {
		panelH := h.place.Panel.H
		if panelH <= 0 {
			panelH = panelTargetSize(PanelKeyboardLayout).H
		}
		statusHeight := 0
		if status != "" {
			statusHeight = m.StandardControl / 2
		}
		viewportH := max(panelH-2*m.PanelPadding-m.StandardControl-statusHeight-theme.MarginM*2, 64)
		list := &ui.Node{Kind: ui.KindScroll, Key: keyboardLayoutListKey, Height: viewportH}
		for i, raw := range layouts.Names {
			name := keyboardLayoutDisplayName(raw, i)
			current := i == layouts.Current
			text := name
			if current {
				text = "✓ " + name
			}
			button := &ui.Node{
				Kind: ui.KindButton, Text: text,
				Name: keyboardLayoutOptionName(locale, name, current),
				Role: "menuitem", Focusable: true, Height: m.StandardControl,
				Action: "keyboard-layout-select:" + strconv.Itoa(i),
			}
			if current {
				button.Fill = ui.FillSoft
			}
			if h.layoutUnavailable || i > 255 {
				button.State |= ui.StateDisabled
			}
			list.Children = append(list.Children, button)
		}
		children = append(children, list)
	}
	return &ui.Node{
		Kind: ui.KindColumn, Height: h.place.Panel.H, Padding: m.PanelPadding,
		Gap: theme.MarginM, Children: children,
	}
}

func (h *PanelHost) focusKeyboardLayout(layouts niri.KeyboardLayouts) {
	index := h.layoutRequested
	if index < 0 || index >= len(layouts.Names) {
		index = layouts.Current
	}
	if index < 0 || index >= len(layouts.Names) {
		return
	}
	action := "keyboard-layout-select:" + strconv.Itoa(index)
	for i, node := range h.focus {
		if node != nil && node.Action == action {
			h.roving.Set(i)
			return
		}
	}
}

func (h *PanelHost) activateKeyboardLayout(r *Registry, n *ui.Node) bool {
	if n.Action == "keyboard-layout-close" {
		r.closePanelLocked(h.id)
		return true
	}
	indexText, ok := strings.CutPrefix(n.Action, "keyboard-layout-select:")
	if !ok {
		return false
	}
	index, err := strconv.Atoi(indexText)
	layouts := r.niriSnapshot.Layouts
	if err != nil || index < 0 || index > 255 || index >= len(layouts.Names) || len(layouts.Names) < 2 {
		return true
	}
	if h.layoutUnavailable || index == layouts.Current {
		return true
	}
	h.layoutRequestSeq++
	sequence := h.layoutRequestSeq
	h.layoutRequested = index
	r.rebuildPanel(h)
	r.sendNiriResultLocked(niri.SwitchLayout{Index: uint8(index)}, func(err error) {
		if err != nil {
			h.keyboardLayoutActionFailed(r, index, sequence)
			return
		}
		time.AfterFunc(keyboardLayoutConfirmationDelay, func() {
			r.mu.Lock()
			if r.panelHosts[PanelKeyboardLayout] != h || h.layoutRequestSeq != sequence || h.layoutRequested != index {
				r.mu.Unlock()
				return
			}
			before := r.niriSnapshot.Layouts
			before.Names = slices.Clone(before.Names)
			r.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			layouts, err := niri.GetKeyboardLayouts(ctx, os.Getenv("NIRI_SOCKET"))
			h.keyboardLayoutActionQueried(r, index, sequence, before, layouts, err)
		})
	})
	return true
}

func (h *PanelHost) keyboardLayoutActionQueried(r *Registry, index int, sequence uint64, before, layouts niri.KeyboardLayouts, queryErr error) {
	r.mu.Lock()
	if r.panelHosts[PanelKeyboardLayout] != h || h.layoutRequestSeq != sequence || h.layoutRequested != index {
		r.mu.Unlock()
		return
	}
	if queryErr != nil {
		r.mu.Unlock()
		h.keyboardLayoutActionFailed(r, index, sequence)
		return
	}

	latest := r.niriSnapshot.Layouts
	eventAdvanced := latest.Current != before.Current || !slices.Equal(latest.Names, before.Names)
	requestLayoutsCurrent := slices.Equal(before.Names, layouts.Names)
	queryConfirmed := requestLayoutsCurrent && layouts.Current == index
	latestConfirmed := slices.Equal(latest.Names, before.Names) && latest.Current == index
	previousLayouts := r.layouts
	previousLayoutsSeen := r.layoutsSeen
	if !eventAdvanced {
		layouts.Names = slices.Clone(layouts.Names)
		r.niriSnapshot.Layouts = layouts
		r.layouts = layouts
		if len(layouts.Names) > 0 {
			r.layoutsSeen = true
		}
	}
	h.layoutRequestSeq++
	h.layoutRequested = -1
	h.layoutUnavailable = !latestConfirmed && !queryConfirmed
	changed := []uint32(nil)
	var layoutView OSDView
	showLayout := false
	if !eventAdvanced {
		changed = r.applyRunningIconsLocked()
		layoutView, showLayout = layoutOSD(previousLayouts, layouts, previousLayoutsSeen)
	}
	r.rebuildPanel(h)
	output := h.output
	r.mu.Unlock()
	r.publish(changed)
	r.publishSurface(output, panelSurfaceID(PanelKeyboardLayout))
	if showLayout {
		r.OSD().Show(layoutView)
	}
}

func (h *PanelHost) keyboardLayoutActionFailed(r *Registry, index int, sequence uint64) {
	r.mu.Lock()
	if r.panelHosts[PanelKeyboardLayout] != h || h.layoutRequestSeq != sequence || h.layoutRequested != index {
		r.mu.Unlock()
		return
	}
	if r.niriSnapshot.Layouts.Current == index {
		h.layoutRequested = -1
		h.layoutUnavailable = false
	} else {
		h.layoutRequested = -1
		h.layoutUnavailable = true
	}
	r.rebuildPanel(h)
	output := h.output
	r.mu.Unlock()
	r.publishSurface(output, panelSurfaceID(PanelKeyboardLayout))
}
