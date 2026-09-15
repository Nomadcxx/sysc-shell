package shell

import (
	"fmt"
	"slices"

	clipboardclient "github.com/Nomadcxx/sysc-clipboard/client"
	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelClipboardAction = "panel:clipboard"

// clipboardProjection is the metadata-only state the shell may retain. The
// daemon remains the owner of payload bytes and persistence.
type clipboardProjection struct {
	Snapshot  clipboardprotocol.Snapshot
	Connected bool
}

type clipboardUpdate = clipboardclient.Update

type clipboardCommandSender interface {
	Restore(string) error
	Pin(string, bool) error
	Delete(string) error
	Clear(clipboardprotocol.ClearScope) error
	Thumbnail(string, uint16) error
	Resync() error
}

func newClipboardProjection() clipboardProjection {
	return clipboardProjection{Snapshot: clipboardprotocol.Snapshot{
		Persistence: clipboardprotocol.PersistenceUnavailable,
		Wayland:     clipboardprotocol.WaylandUnavailable,
	}}
}

func (p clipboardProjection) clone() clipboardProjection {
	p.Snapshot = cloneClipboardSnapshot(p.Snapshot)
	return p
}

func cloneClipboardSnapshot(snapshot clipboardprotocol.Snapshot) clipboardprotocol.Snapshot {
	cloned := snapshot
	cloned.Entries = make([]clipboardprotocol.Entry, len(snapshot.Entries))
	for i, entry := range snapshot.Entries {
		entry.OfferedMIME = append([]string(nil), entry.OfferedMIME...)
		cloned.Entries[i] = entry
	}
	return cloned
}

func sameClipboardProjection(a, b clipboardProjection) bool {
	if a.Connected != b.Connected || a.Snapshot.Revision != b.Snapshot.Revision ||
		a.Snapshot.Persistence != b.Snapshot.Persistence || a.Snapshot.Wayland != b.Snapshot.Wayland ||
		len(a.Snapshot.Entries) != len(b.Snapshot.Entries) {
		return false
	}
	for i, left := range a.Snapshot.Entries {
		right := b.Snapshot.Entries[i]
		if left.ID != right.ID || left.Kind != right.Kind || left.MIME != right.MIME ||
			left.Size != right.Size || left.SHA256 != right.SHA256 || left.Pinned != right.Pinned ||
			!left.CapturedAt.Equal(right.CapturedAt) || left.Preview != right.Preview ||
			!slices.Equal(left.OfferedMIME, right.OfferedMIME) {
			return false
		}
	}
	return true
}

func clipboardTooltip(view clipboardProjection) string {
	if !view.Connected || view.Snapshot.Wayland != clipboardprotocol.WaylandReady {
		return "Clipboard unavailable"
	}
	count := len(view.Snapshot.Entries)
	itemWord := "items"
	if count == 1 {
		itemWord = "item"
	}
	persistence := string(view.Snapshot.Persistence)
	if persistence == "" {
		persistence = "unknown"
	}
	if view.Snapshot.Persistence == clipboardprotocol.PersistenceUnavailable {
		persistence = "persistence unavailable"
	}
	return fmt.Sprintf("Clipboard: %d %s (%s)", count, itemWord, persistence)
}

func buildClipboardWidget(m theme.Metrics) textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "content_paste", IconSize: m.IconNormal,
		Action: panelClipboardAction, Name: "Clipboard", Role: "button",
	}
	return textWidget{
		node: icon,
		refresh: func(view barView) bool {
			tooltip := clipboardTooltip(view.Clipboard)
			if icon.Tooltip == tooltip {
				return false
			}
			icon.Tooltip = tooltip
			return true
		},
	}
}

func (r *Registry) BindClipboard(sender clipboardCommandSender) {
	r.mu.Lock()
	r.clipboardSender = sender
	r.mu.Unlock()
}

// ApplyClipboard applies one client update and invalidates only bars whose
// metadata projection changed.
func (r *Registry) ApplyClipboard(update clipboardUpdate) []uint32 {
	next := clipboardProjection{Snapshot: cloneClipboardSnapshot(update.Snapshot), Connected: update.Connected}
	r.mu.Lock()
	if sameClipboardProjection(r.clipboard, next) {
		r.mu.Unlock()
		return nil
	}
	r.clipboard = next
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()
	r.publish(changed)
	return changed
}

func (r *Registry) applyClipboard(update clipboardUpdate) []uint32 {
	return r.ApplyClipboard(update)
}
