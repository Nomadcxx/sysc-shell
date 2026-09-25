package shell

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const notesPluginID = "org.sysc.notes"

func (h *pluginHost) launcherNotes(output string, generation uint32, body string) error {
	if len(body) > v1.MaxInputBytes || !utf8.ValidString(body) || strings.ContainsRune(body, '\x00') {
		return errors.New("Notes capture is invalid or exceeds the 1 MiB limit")
	}
	h.mu.Lock()
	slot := h.slots[notesPluginID]
	viewID := ""
	if h.panel != nil && h.panel.Plugin == notesPluginID && h.panel.Entry == "panel" && h.panel.Output == output {
		viewID = h.panel.ID
	}
	h.mu.Unlock()
	if slot == nil {
		return errors.New("Notes plugin is not running")
	}
	if viewID == "" {
		result, err := h.openPanel(notesPluginID, v1.PanelParams{
			Entry: "panel", Output: output, Generation: generation, Instance: "launcher",
		})
		if err != nil {
			return fmt.Errorf("open Notes: %w", err)
		}
		viewID = result.ViewID
		if viewID == "" {
			return errors.New("Notes panel closed before capture could be delivered")
		}
	}
	if body != "" && !h.deliver(pluginHit{ViewID: viewID, Node: "launcher-capture"}, v1.EventSubmit, "", body, 0) {
		return errors.New("Notes panel is no longer available")
	}
	return nil
}
