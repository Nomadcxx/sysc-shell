package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Selections is the shell's system-clipboard request stream. The platform
// owner drains it; a full buffer drops the request rather than block the
// registry lock.
func (r *Registry) Selections() <-chan wayland.SelectionRequest { return r.selections }

func (r *Registry) requestSelection(req wayland.SelectionRequest) {
	select {
	case r.selections <- req:
	default:
	}
}

// requestClipboard carries a field key's copy or paste to the platform, with
// the serial of the key that asked.
func (r *Registry) requestClipboard(res ui.FieldResult, serial uint32) {
	if res.Copy != "" {
		r.requestSelection(wayland.SelectionRequest{Copy: res.Copy, Serial: serial})
	}
	if res.Paste {
		r.requestSelection(wayland.SelectionRequest{Paste: true, Serial: serial})
	}
}

// flattenPaste makes clipboard text fit a single-line field: each line break
// becomes one space.
func flattenPaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", " ")
}

// applyPaste inserts clipboard text into the focused field as one undo step.
// NUL is dropped here too: the platform strips it, but this is the boundary
// the shell owns.
func (h *PanelHost) applyPaste(r *Registry, text string) bool {
	text = strings.ReplaceAll(text, "\x00", "")
	return h.editField(r, func(f *ui.Field) {
		if !f.Multiline || f.SubmitOnEnter {
			text = flattenPaste(text)
		}
		f.BreakUndo()
		f.Commit(text)
	})
}
