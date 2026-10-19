package shell

import (
	"fmt"
	"slices"
	"time"

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

// clipboardBarSummary omits entry metadata because the bar only displays a
// count and persistence state. The clipboard panel reads the full projection.
type clipboardBarSummary struct {
	Connected   bool
	Count       int
	Persistence clipboardprotocol.PersistenceState
	Wayland     clipboardprotocol.WaylandState
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

type clipboardCommandCompletion struct {
	requestID string
	done      chan error
}

const clipboardCommandTimeout = 6 * time.Second

// ponytail: cap pending restore confirmations at 16; activation reports an
// error on overflow instead of letting a stalled client grow this map.
const clipboardCompletionLimit = 16

// enqueueClipboardRestore tracks only the response for this restore. The client
// can drop updates when its bounded queue fills, so FIFO position cannot
// identify a response after a dropped update.
func (r *Registry) enqueueClipboardRestore(sender clipboardCommandSender, id string) (*clipboardCommandCompletion, error) {
	if sender == nil {
		return nil, fmt.Errorf("clipboard daemon unavailable")
	}
	restoreSender, ok := sender.(interface {
		RestoreWithRequestID(string) (string, error)
	})
	if !ok {
		return nil, fmt.Errorf("clipboard restore confirmations are unavailable")
	}
	completion := &clipboardCommandCompletion{done: make(chan error, 1)}
	r.clipboardCommandMu.Lock()
	defer r.clipboardCommandMu.Unlock()
	if len(r.clipboardCompletions) >= clipboardCompletionLimit {
		return nil, fmt.Errorf("clipboard command confirmations are pending")
	}
	requestID, err := restoreSender.RestoreWithRequestID(id)
	if err != nil {
		return nil, err
	}
	if requestID == "" {
		return nil, fmt.Errorf("clipboard restore returned no request ID")
	}
	if r.clipboardCompletions == nil {
		r.clipboardCompletions = make(map[string]*clipboardCommandCompletion)
	}
	if _, exists := r.clipboardCompletions[requestID]; exists {
		return nil, fmt.Errorf("clipboard restore returned a duplicate request ID")
	}
	completion.requestID = requestID
	r.clipboardCompletions[requestID] = completion
	return completion, nil
}

func (r *Registry) waitClipboardCommand(completion *clipboardCommandCompletion) error {
	if completion == nil {
		return fmt.Errorf("clipboard restore was not queued")
	}
	timer := time.NewTimer(clipboardCommandTimeout)
	defer timer.Stop()
	select {
	case err := <-completion.done:
		return err
	case <-timer.C:
		r.forgetClipboardCommand(completion)
		return fmt.Errorf("clipboard restore timed out")
	case <-r.closed:
		r.forgetClipboardCommand(completion)
		return fmt.Errorf("clipboard restore interrupted")
	}
}

func (r *Registry) forgetClipboardCommand(completion *clipboardCommandCompletion) {
	r.clipboardCommandMu.Lock()
	if r.clipboardCompletions[completion.requestID] == completion {
		delete(r.clipboardCompletions, completion.requestID)
	}
	r.clipboardCommandMu.Unlock()
}

func (r *Registry) completeClipboardCommand(update clipboardUpdate) {
	var completed []*clipboardCommandCompletion
	var result error
	r.clipboardCommandMu.Lock()
	switch update.Message.Type {
	case clipboardprotocol.TypeAck, clipboardprotocol.TypeThumbnail:
		requestID := update.Message.RequestID
		if update.Message.Ack != nil {
			requestID = update.Message.Ack.RequestID
		}
		if completion := r.clipboardCompletions[requestID]; completion != nil {
			completed = append(completed, completion)
			delete(r.clipboardCompletions, requestID)
		}
	case clipboardprotocol.TypeError:
		requestID := update.Message.RequestID
		if update.Message.Error != nil {
			requestID = update.Message.Error.RequestID
		}
		if completion := r.clipboardCompletions[requestID]; completion != nil {
			completed = append(completed, completion)
			delete(r.clipboardCompletions, requestID)
			if update.Message.Error != nil && update.Message.Error.Message != "" {
				result = fmt.Errorf("%s", update.Message.Error.Message)
			} else {
				result = fmt.Errorf("clipboard command failed")
			}
		}
	case clipboardprotocol.TypeSnapshot:
		if !update.Connected {
			for requestID, completion := range r.clipboardCompletions {
				completed = append(completed, completion)
				delete(r.clipboardCompletions, requestID)
			}
			result = fmt.Errorf("clipboard daemon disconnected before the command completed")
		}
	}
	r.clipboardCommandMu.Unlock()
	for _, completion := range completed {
		completion.done <- result
	}
}

func newClipboardProjection() clipboardProjection {
	return clipboardProjection{Snapshot: clipboardprotocol.Snapshot{
		Persistence: clipboardprotocol.PersistenceUnavailable,
		Wayland:     clipboardprotocol.WaylandUnavailable,
	}}
}

func (p clipboardProjection) barSummary() clipboardBarSummary {
	return clipboardBarSummary{
		Connected: p.Connected, Count: len(p.Snapshot.Entries),
		Persistence: p.Snapshot.Persistence, Wayland: p.Snapshot.Wayland,
	}
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

func clipboardTooltip(view clipboardBarSummary) string {
	if !view.Connected || view.Wayland != clipboardprotocol.WaylandReady {
		return "Clipboard unavailable"
	}
	count := view.Count
	itemWord := "items"
	if count == 1 {
		itemWord = "item"
	}
	persistence := string(view.Persistence)
	if persistence == "" {
		persistence = "unknown"
	}
	if view.Persistence == clipboardprotocol.PersistenceUnavailable {
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
	r.completeClipboardCommand(update)
	next := clipboardProjection{Snapshot: cloneClipboardSnapshot(update.Snapshot), Connected: update.Connected}
	var thumbnail *ui.Image
	if update.Message.Type == clipboardprotocol.TypeThumbnail && update.Message.Thumbnail != nil {
		if err := clipboardprotocol.ValidateThumbnail(*update.Message.Thumbnail); err == nil {
			thumbnail = decodeClipboardThumbnail(update.Message.Thumbnail)
		}
	}
	r.mu.Lock()
	projectionChanged := !sameClipboardProjection(r.clipboard, next)
	if projectionChanged {
		r.clipboard = next
	}
	changed := make([]uint32, 0, len(r.bars))
	if projectionChanged {
		for global, bar := range r.bars {
			if bar.apply(r.viewLocked(bar.connector())) {
				changed = append(changed, global)
			}
		}
	}
	panelChanged := false
	panelOutput := uint32(0)
	if h := r.panelHosts[PanelClipboard]; h != nil {
		if projectionChanged {
			h.errLabel = ""
			pruneClipboardImages(h, r.clipboard.Snapshot.Entries)
			panelChanged = true
		}
		if update.Message.Type == clipboardprotocol.TypeError && update.Message.Error != nil && h.errLabel != update.Message.Error.Message {
			h.errLabel = update.Message.Error.Message
			panelChanged = true
		}
		if thumbnail != nil && update.Message.Thumbnail != nil && clipboardImageEntry(r.clipboard.Snapshot.Entries, update.Message.Thumbnail.ID) {
			if h.clipboardThumbnails == nil {
				h.clipboardThumbnails = make(map[string]*ui.Image)
			}
			h.clipboardThumbnails[update.Message.Thumbnail.ID] = thumbnail
			delete(h.clipboardThumbnailRequest, update.Message.Thumbnail.ID)
			panelChanged = true
		}
		if panelChanged {
			r.rebuildPanel(h)
			panelOutput = h.output
		}
	}
	r.mu.Unlock()
	r.publish(changed)
	if panelChanged {
		r.publishSurface(panelOutput, panelSurfaceID(PanelClipboard))
	}
	return changed
}

func (r *Registry) applyClipboard(update clipboardUpdate) []uint32 {
	return r.ApplyClipboard(update)
}
