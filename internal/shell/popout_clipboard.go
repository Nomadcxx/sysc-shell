package shell

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"

	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	clipboardRowHeight     = 64
	clipboardThumbnailSize = 48
	clipboardDetailHeight  = 124
	// Keep the text/long-text split aligned with DMS's 200-byte threshold.
	clipboardLongTextThreshold = 200
	// Two text lines plus the card inset and their inter-line gap. Keeping the
	// full stack inside the capsule prevents the detail line from being clipped
	// by a fixed-height notice.
	clipboardStatusHeight   = 56
	clipboardConfirmHeight  = 96
	clipboardThumbnailMaxPX = 96
)

const (
	clipboardFilterAll      = "all"
	clipboardFilterText     = "text"
	clipboardFilterLongText = "long_text"
	clipboardFilterImage    = "image"
)

// clipboardTree is the one presenter for the daemon's metadata snapshot. The
// daemon remains the only owner of payload bytes; this tree contains previews
// and decoded, bounded thumbnails only.
func clipboardTree(r *Registry, h *PanelHost) *ui.Node {
	m := DefaultTheme().Metrics
	if h != nil {
		m = h.metrics()
	}
	tr := hostT(h)
	if h.search == nil {
		h.search = ui.NewField(h.query)
	}
	panelW := panelTargetSize(PanelClipboard).W
	panelH := panelTargetSize(PanelClipboard).H
	if h.place.Panel.W > 0 {
		panelW = h.place.Panel.W
	}
	if h.place.Panel.H > 0 {
		panelH = h.place.Panel.H
	}

	view := newClipboardProjection()
	if r != nil {
		view = r.clipboard
	}
	entries := view.Snapshot.Entries
	filter := h.clipboardFilter
	if filter == "" {
		filter = clipboardFilterAll
	}
	filtered := filterClipboardEntries(entries, h.query, filter)
	selected, selectedOK := clipboardSelection(h, filtered)

	field := h.search.Node(cardT(tr, "clip.search", "Search"))
	field.Width = max(panelW-2*m.PanelPadding, 1)
	field.Height = m.StandardControl
	field.Padding = m.ButtonPadding

	children := []*ui.Node{
		clipboardHeader(m, tr),
		field,
		clipboardFilterRow(filter, max(panelW-2*m.PanelPadding, 1), m, tr),
		clipboardToolbar(h, m, tr),
	}
	if state := clipboardStatus(view, m, tr); state != nil {
		children = append(children, state)
	}
	if h.errLabel != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError, TextRole: theme.RoleCaption})
	}

	if len(filtered) == 0 {
		message := cardT(tr, "clip.empty", "No clipboard history")
		if len(entries) > 0 {
			message = cardT(tr, "clip.no_matches", "No clipboard matches")
		}
		children = append(children, clipboardNotice(message, cardT(tr, "clip.hint.search", "Try a different search or filter"), m))
	} else {
		rows := make([]*ui.Node, len(filtered))
		for i, entry := range filtered {
			rows[i] = clipboardRow(r, h, entry, entry.ID == selected.ID && selectedOK, m, panelW, tr)
		}
		list := &ui.Node{
			Kind: ui.KindVirtualList, ItemCount: len(rows), ItemHeight: clipboardRowHeight,
			HideScrollbar: true, Item: func(i int) *ui.Node {
				if i < 0 || i >= len(rows) {
					return nil
				}
				return rows[i]
			},
		}
		used := 2*m.PanelPadding + clipboardHeaderHeight(m) + m.CardGap + m.StandardControl +
			m.CardGap + clipboardFilterHeight(m) + m.CardGap + clipboardToolbarHeight(m) + m.CardGap
		if state := clipboardStatus(view, m, tr); state != nil {
			used += clipboardStatusHeight + m.CardGap
		}
		if h.errLabel != "" {
			used += 16 + m.CardGap
		}
		if h.clipboardConfirmScope != "" || h.clipboardDeleteConfirmID != "" || h.clipboardDeleteManyConfirm {
			used += clipboardConfirmHeight
		} else if selectedOK && !h.clipboardMultiSelect {
			used += clipboardDetailHeight
		}
		used += m.CardGap
		list.Height = max(panelH-used, clipboardRowHeight)
		children = append(children, list)
	}

	if h.clipboardConfirmScope != "" {
		children = append(children, clipboardConfirmCard("clear", h.clipboardConfirmScope, m, tr))
	} else if h.clipboardDeleteConfirmID != "" {
		children = append(children, clipboardConfirmCard("delete", h.clipboardDeleteConfirmID, m, tr))
	} else if h.clipboardDeleteManyConfirm {
		children = append(children, clipboardConfirmCard("delete-many", fmt.Sprint(len(h.clipboardSelectedIDs)), m, tr))
	} else if selectedOK && !h.clipboardMultiSelect {
		children = append(children, clipboardDetail(r, h, selected, m, tr))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: m.CardGap, Padding: m.PanelPadding, Children: children}
}

func clipboardHeader(m theme.Metrics, tr translateFunc) *ui.Node {
	title := &ui.Node{Kind: ui.KindRow, Gap: m.CardGap, Height: m.StandardControl, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "content_paste", IconSize: m.IconNormal, Tone: ui.ToneAccent},
		{Kind: ui.KindText, Text: cardT(tr, "clip.title", "Clipboard History"), TextRole: theme.RoleHeadline, Name: cardT(tr, "clip.title", "Clipboard History"), Role: "heading"},
	}}
	close := &ui.Node{
		Kind: ui.KindButton, Key: "clipboard-close", Action: "clipboard-close", Name: cardT(tr, "notify.close", "Close"), Role: "button", Focusable: true,
		Width: m.CompactControl, Height: m.CompactControl, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: m.IconNormal}},
	}
	return &ui.Node{Kind: ui.KindRow, Height: m.StandardControl, Gap: m.CardGap, PinEnd: true, Children: []*ui.Node{title, close}}
}

func clipboardHeaderHeight(m theme.Metrics) int { return m.StandardControl }

func clipboardToolbar(h *PanelHost, m theme.Metrics, tr translateFunc) *ui.Node {
	if h != nil && h.clipboardMultiSelect {
		count := len(h.clipboardSelectedIDs)
		deleteLabel := fmt.Sprintf(cardT(tr, "clip.delete_selected", "Delete selected (%d)"), count)
		delete := clipboardButton("clipboard:delete-selected", deleteLabel, cardT(tr, "clip.delete_selected_hint", "Delete selected clipboard items"), m)
		if count == 0 {
			delete.State |= ui.StateDisabled
		}
		return &ui.Node{Kind: ui.KindRow, Height: m.CompactControl, Gap: m.CardGap, Children: []*ui.Node{
			delete,
			clipboardButton("clipboard:selection:toggle", cardT(tr, "clip.cancel_selection", "Cancel selection"), cardT(tr, "clip.cancel_selection_hint", "Stop selecting clipboard items"), m),
		}}
	}
	return &ui.Node{Kind: ui.KindRow, Height: m.CompactControl, Gap: m.CardGap, Children: []*ui.Node{
		clipboardButton("clipboard:selection:toggle", cardT(tr, "clip.select_items", "Select items"), cardT(tr, "clip.select_items_hint", "Select multiple clipboard items"), m),
		clipboardButton("clipboard:clear:unpinned", cardT(tr, "clip.clear_unpinned", "Clear unpinned"), cardT(tr, "clip.clear_unpinned_hint", "Clear unpinned clipboard entries"), m),
		clipboardButton("clipboard:clear:all", cardT(tr, "clip.clear_all", "Clear all"), cardT(tr, "clip.clear_all_hint", "Clear all clipboard history"), m),
	}}
}

func clipboardToolbarHeight(m theme.Metrics) int { return m.CompactControl }

func clipboardFilterRow(active string, width int, m theme.Metrics, tr translateFunc) *ui.Node {
	filters := []struct {
		id  string
		key string
		eng string
	}{
		{clipboardFilterAll, "clip.filter_all", "All"},
		{clipboardFilterText, "clip.filter_text", "Text"},
		{clipboardFilterLongText, "clip.filter_long_text", "Long text"},
		{clipboardFilterImage, "clip.filter_image", "Image"},
	}
	segments := make([]*ui.Node, 0, len(filters))
	for _, filter := range filters {
		label := cardT(tr, filter.key, filter.eng)
		segment := &ui.Node{
			Kind: ui.KindButton, Action: "clipboard:filter:" + filter.id,
			Name: label, Role: "tab", Focusable: true,
			Height: m.CompactControl, Padding: theme.MarginXXS,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption}},
		}
		if active == filter.id {
			segment.State |= ui.StateSelected
		}
		segments = append(segments, segment)
	}
	return &ui.Node{
		Kind: ui.KindSegmented, Key: "clipboard-filter", Gap: theme.MarginXXS,
		Width: width, Height: m.CompactControl, Children: segments,
	}
}

func clipboardFilterHeight(m theme.Metrics) int { return m.CompactControl }

func clipboardStatus(view clipboardProjection, m theme.Metrics, tr translateFunc) *ui.Node {
	if !view.Connected || view.Snapshot.Wayland != clipboardprotocol.WaylandReady {
		return clipboardNotice(cardT(tr, "clip.unavailable", "Clipboard unavailable"), cardT(tr, "clip.hint.unavailable", "Start sysc-clipboard to capture and restore history"), m)
	}
	switch view.Snapshot.Persistence {
	case clipboardprotocol.PersistenceVolatile:
		return clipboardNotice(cardT(tr, "clip.volatile", "Clipboard persistence is volatile"), cardT(tr, "clip.hint.volatile", "History will not survive a daemon restart"), m)
	case clipboardprotocol.PersistenceUnavailable:
		return clipboardNotice(cardT(tr, "clip.persistence_unavailable", "Clipboard persistence unavailable"), cardT(tr, "clip.hint.persistence", "History remains available in memory"), m)
	default:
		return nil
	}
}

func clipboardNotice(title, detail string, m theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindCapsule, Height: clipboardStatusHeight, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: detail, TextRole: theme.RoleCaption},
		}}}}
}

func clipboardSelection(h *PanelHost, entries []clipboardprotocol.Entry) (clipboardprotocol.Entry, bool) {
	if h == nil {
		return clipboardprotocol.Entry{}, false
	}
	if h.clipboardSelectedID != "" {
		if entry, ok := clipboardEntryByID(entries, h.clipboardSelectedID); ok {
			return entry, true
		}
	}
	if len(entries) == 0 {
		h.clipboardSelectedID = ""
		return clipboardprotocol.Entry{}, false
	}
	h.clipboardSelectedID = entries[0].ID
	return entries[0], true
}

func clipboardEntryByID(entries []clipboardprotocol.Entry, id string) (clipboardprotocol.Entry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return clipboardprotocol.Entry{}, false
}

func selectedClipboardIDsInOrder(entries []clipboardprotocol.Entry, selected map[string]struct{}) []string {
	ids := make([]string, 0, len(selected))
	for _, entry := range entries {
		if _, ok := selected[entry.ID]; ok {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

func pruneClipboardSelections(h *PanelHost, entries []clipboardprotocol.Entry) {
	if h == nil || len(h.clipboardSelectedIDs) == 0 {
		return
	}
	present := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		present[entry.ID] = struct{}{}
	}
	for id := range h.clipboardSelectedIDs {
		if _, ok := present[id]; !ok {
			delete(h.clipboardSelectedIDs, id)
		}
	}
	if len(h.clipboardSelectedIDs) == 0 {
		h.clipboardSelectedIDs = nil
		h.clipboardDeleteManyConfirm = false
	}
}

func filterClipboardEntries(entries []clipboardprotocol.Entry, query, filter string) []clipboardprotocol.Entry {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]clipboardprotocol.Entry, 0, len(entries))
	for _, entry := range entries {
		switch filter {
		case "", clipboardFilterAll:
		case clipboardFilterText:
			if entry.Kind != clipboardprotocol.KindText || entry.Size > clipboardLongTextThreshold {
				continue
			}
		case clipboardFilterLongText:
			if entry.Kind != clipboardprotocol.KindText || entry.Size <= clipboardLongTextThreshold {
				continue
			}
		case clipboardFilterImage:
			if entry.Kind != clipboardprotocol.KindImage {
				continue
			}
		default:
			continue
		}
		metadata := strings.ToLower(entry.Preview + " " + entry.MIME)
		if query != "" && !strings.Contains(metadata, query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func clipboardRow(r *Registry, h *PanelHost, entry clipboardprotocol.Entry, selected bool, m theme.Metrics, panelW int, tr translateFunc) *ui.Node {
	preview := clipboardPreview(entry)
	name := cardT(tr, "clip.restore_item", "Restore clipboard item")
	if preview != "" {
		name += ": " + preview
	} else if entry.Kind == clipboardprotocol.KindImage {
		name += ": image"
	}

	var previewNode *ui.Node
	if entry.Kind == clipboardprotocol.KindImage {
		var image *ui.Image
		if h != nil && h.clipboardThumbnails != nil {
			image = h.clipboardThumbnails[entry.ID]
		}
		if image == nil {
			requestClipboardThumbnail(r, h, entry.ID)
		}
		previewNode = &ui.Node{Kind: ui.KindImage, Image: image, ImageSize: clipboardThumbnailSize}
	} else {
		previewNode = &ui.Node{Kind: ui.KindIcon, Icon: "content_copy", IconSize: m.IconNormal}
	}

	labels := []*ui.Node{}
	if preview != "" {
		labels = append(labels, &ui.Node{Kind: ui.KindText, Text: preview, TextRole: theme.RoleLabel, MaxWidth: max(panelW/2, 120)})
	} else if entry.Kind == clipboardprotocol.KindImage {
		if h == nil || h.clipboardThumbnails == nil || h.clipboardThumbnails[entry.ID] == nil {
			labels = append(labels, &ui.Node{Kind: ui.KindText, Text: cardT(tr, "clip.image_unavailable", "Image preview unavailable"), TextRole: theme.RoleLabel})
		} else {
			labels = append(labels, &ui.Node{Kind: ui.KindText, Text: cardT(tr, "clip.image_preview", "Image preview"), TextRole: theme.RoleLabel})
		}
	}
	labels = append(labels,
		&ui.Node{Kind: ui.KindText, Text: entry.MIME + " · " + clipboardSize(entry.Size), TextRole: theme.RoleCaption},
		&ui.Node{Kind: ui.KindText, Text: clipboardTime(entry, tr), TextRole: theme.RoleCaption},
	)
	if entry.Pinned {
		labels = append(labels, &ui.Node{Kind: ui.KindText, Text: cardT(tr, "clip.pinned", "Pinned"), TextRole: theme.RoleCaption, Tone: ui.ToneAccent})
	}
	action := "clipboard:restore:" + entry.ID
	role := "button"
	if h != nil && h.clipboardMultiSelect {
		_, selected = h.clipboardSelectedIDs[entry.ID]
		action = "clipboard:select:" + entry.ID
		role = "checkbox"
		name = cardT(tr, "clip.select_item", "Select clipboard item")
		if preview != "" {
			name += ": " + preview
		} else if entry.Kind == clipboardprotocol.KindImage {
			name += ": image"
		}
	}
	labelW := max(panelW-2*m.PanelPadding-2*m.CardPadding-clipboardThumbnailSize-m.CardGap, 120)
	body := &ui.Node{Kind: ui.KindRow, Gap: m.CardGap, Children: []*ui.Node{
		previewNode,
		{Kind: ui.KindColumn, Width: labelW, Gap: theme.MarginXXS, Children: labels},
	}}
	fill := ui.FillNone
	if selected {
		fill = ui.FillSoft
	}
	state := ui.Interaction(0)
	if selected {
		state |= ui.StateSelected
	}
	return &ui.Node{
		Kind: ui.KindButton, Key: "clipboard-row:" + entry.ID, Action: action,
		Name: name, Role: role, Focusable: true, Height: clipboardRowHeight,
		Padding: m.CardPadding, Fill: fill, Shape: ui.ShapeMedium, State: state, Children: []*ui.Node{body},
	}
}

func clipboardDetail(r *Registry, h *PanelHost, entry clipboardprotocol.Entry, m theme.Metrics, tr translateFunc) *ui.Node {
	var image *ui.Image
	if h != nil && h.clipboardThumbnails != nil {
		image = h.clipboardThumbnails[entry.ID]
	}
	children := []*ui.Node{{Kind: ui.KindText, Text: cardT(tr, "clip.selected", "Selected item"), TextRole: theme.RoleTitle}}
	if entry.Kind == clipboardprotocol.KindImage {
		if image == nil {
			requestClipboardThumbnail(r, h, entry.ID)
		}
		children = append(children, &ui.Node{Kind: ui.KindRow, Height: clipboardThumbnailSize, Gap: m.CardGap, Children: []*ui.Node{
			{Kind: ui.KindImage, Image: image, ImageSize: clipboardThumbnailSize},
			{Kind: ui.KindText, Text: cardT(tr, "clip.image", "Image") + " · " + entry.MIME + " · " + clipboardSize(entry.Size), TextRole: theme.RoleCaption},
		}})
	} else {
		preview := clipboardPreview(entry)
		if preview == "" {
			preview = cardT(tr, "clip.no_preview", "No text preview")
		}
		children = append(children, &ui.Node{Kind: ui.KindText, Text: preview, TextRole: theme.RoleBody})
	}
	children = append(children, &ui.Node{Kind: ui.KindRow, Height: m.CompactControl, Gap: m.CardGap, Children: []*ui.Node{
		clipboardButtonWithKey("clipboard-detail-restore:"+entry.ID, "clipboard:restore:"+entry.ID, cardT(tr, "clip.restore", "Restore"), cardT(tr, "clip.restore_hint", "Restore selected clipboard item"), m),
		clipboardButtonWithKey("clipboard-detail-pin:"+entry.ID, "clipboard:pin:"+entry.ID, pinLabel(entry, tr), pinName(entry, tr), m),
		clipboardButtonWithKey("clipboard-detail-delete:"+entry.ID, "clipboard:delete:"+entry.ID, cardT(tr, "clip.delete", "Delete"), cardT(tr, "clip.delete_hint", "Delete selected clipboard item"), m),
	}})
	return &ui.Node{Kind: ui.KindCapsule, Height: clipboardDetailHeight, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Children: []*ui.Node{{Kind: ui.KindColumn, Gap: m.CardGap, Children: children}}}
}

func clipboardConfirmCard(kind, value string, m theme.Metrics, tr translateFunc) *ui.Node {
	title := cardT(tr, "clip.confirm_clear", "Clear clipboard history?")
	confirmAction := "clipboard:confirm-clear:" + value
	confirmName := cardT(tr, "clip.confirm_hint", "Confirm destructive clipboard action")
	if value == string(clipboardprotocol.ClearAll) {
		title = cardT(tr, "clip.confirm_clear_all", "Clear all clipboard history?")
	} else if value == string(clipboardprotocol.ClearUnpinned) {
		title = cardT(tr, "clip.confirm_clear_unpinned", "Clear unpinned clipboard history?")
	}
	if kind == "delete" {
		title = cardT(tr, "clip.confirm_delete", "Delete clipboard item?")
		confirmAction = "clipboard:confirm-delete:" + value
	} else if kind == "delete-many" {
		if value == "1" {
			title = cardT(tr, "clip.confirm_delete_one", "Delete 1 clipboard item?")
		} else {
			title = fmt.Sprintf(cardT(tr, "clip.confirm_delete_many", "Delete %s clipboard items?"), value)
		}
		confirmAction = "clipboard:confirm-delete-many"
		confirmName = cardT(tr, "clip.confirm_delete_many_hint", "Confirm deletion of selected clipboard items")
	}
	return &ui.Node{Kind: ui.KindCapsule, Height: clipboardConfirmHeight, Padding: m.CardPadding,
		Fill: ui.FillErrorContainer, Shape: ui.ShapeCard, Children: []*ui.Node{{Kind: ui.KindColumn, Gap: m.CardGap, Children: []*ui.Node{
			{Kind: ui.KindText, Text: title, TextRole: theme.RoleTitle},
			{Kind: ui.KindRow, Height: m.CompactControl, Gap: m.CardGap, Children: []*ui.Node{
				clipboardButton(confirmAction, cardT(tr, "clip.confirm", "Confirm"), confirmName, m),
				clipboardButton("clipboard:cancel-"+kind, cardT(tr, "common.cancel", "Cancel"), cardT(tr, "clip.cancel_hint", "Cancel destructive clipboard action"), m),
			}},
		}}}}
}

func clipboardButton(action, label, name string, m theme.Metrics) *ui.Node {
	return clipboardButtonWithKey(action, action, label, name, m)
}

func clipboardButtonWithKey(key, action, label, name string, m theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindButton, Key: key, Action: action, Text: label, Name: name, Role: "button", Focusable: true,
		Height: m.CompactControl, Padding: m.ButtonPadding, Fill: ui.FillOutline, Shape: ui.ShapeMedium}
}

func requestClipboardThumbnail(r *Registry, h *PanelHost, id string) {
	if r == nil || h == nil || r.clipboardSender == nil || id == "" {
		return
	}
	if h.clipboardThumbnailRequest == nil {
		h.clipboardThumbnailRequest = make(map[string]struct{})
	}
	if _, ok := h.clipboardThumbnailRequest[id]; ok {
		return
	}
	if err := r.clipboardSender.Thumbnail(id, clipboardThumbnailMaxPX); err != nil {
		h.errLabel = err.Error()
		return
	}
	h.clipboardThumbnailRequest[id] = struct{}{}
}

func clipboardPreview(entry clipboardprotocol.Entry) string {
	return strings.Join(strings.Fields(entry.Preview), " ")
}

func clipboardTime(entry clipboardprotocol.Entry, tr translateFunc) string {
	if entry.CapturedAt.IsZero() {
		return cardT(tr, "clip.time_unknown", "Unknown time")
	}
	return entry.CapturedAt.Local().Format("15:04")
}

func clipboardSize(size uint64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func pinLabel(entry clipboardprotocol.Entry, tr translateFunc) string {
	if entry.Pinned {
		return cardT(tr, "clip.unpin", "Unpin")
	}
	return cardT(tr, "clip.pin", "Pin")
}

func pinName(entry clipboardprotocol.Entry, tr translateFunc) string {
	if entry.Pinned {
		return cardT(tr, "clip.unpin_hint", "Unpin selected clipboard item")
	}
	return cardT(tr, "clip.pin_hint", "Pin selected clipboard item")
}

func decodeClipboardThumbnail(thumbnail *clipboardprotocol.Thumbnail) *ui.Image {
	if thumbnail == nil || len(thumbnail.Data) == 0 || len(thumbnail.Data) > clipboardprotocol.MaxThumbnailBytes ||
		thumbnail.Width == 0 || thumbnail.Height == 0 || int(thumbnail.Width) > clipboardprotocol.MaxThumbnailPixels ||
		int(thumbnail.Height) > clipboardprotocol.MaxThumbnailPixels {
		return nil
	}
	config, err := png.DecodeConfig(bytes.NewReader(thumbnail.Data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > clipboardprotocol.MaxThumbnailPixels || config.Height > clipboardprotocol.MaxThumbnailPixels {
		return nil
	}
	source, err := png.Decode(bytes.NewReader(thumbnail.Data))
	if err != nil {
		return nil
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || width > clipboardprotocol.MaxThumbnailPixels || height > clipboardprotocol.MaxThumbnailPixels {
		return nil
	}
	out := &ui.Image{Width: width, Height: height, Stride: width * 4, Pix: make([]byte, width*height*4)}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			offset := y*out.Stride + x*4
			out.Pix[offset+0] = uint8(b >> 8)
			out.Pix[offset+1] = uint8(g >> 8)
			out.Pix[offset+2] = uint8(r >> 8)
			out.Pix[offset+3] = uint8(a >> 8)
		}
	}
	return out
}

func (h *PanelHost) clipboardSelectionFromFocus() {
	if h == nil {
		return
	}
	n := h.focused()
	if n == nil {
		return
	}
	if id, ok := strings.CutPrefix(n.Action, "clipboard:restore:"); ok && id != "" {
		h.clipboardSelectedID = id
	}
}

func (h *PanelHost) clipboardKeyPress(r *Registry, key uint32) bool {
	switch key {
	case keyDelete:
		if h.clipboardMultiSelect {
			if len(h.clipboardSelectedIDs) > 0 {
				h.clipboardDeleteManyConfirm = true
				h.clipboardDeleteConfirmID = ""
				h.clipboardConfirmScope = ""
				r.rebuildPanel(h)
			}
			return true
		}
		h.clipboardSelectionFromFocus()
		id := h.clipboardSelectedID
		if id == "" {
			if entries := r.clipboard.Snapshot.Entries; len(entries) > 0 {
				id = entries[0].ID
			}
		}
		if id == "" {
			return true
		}
		h.clipboardDeleteConfirmID = id
		h.clipboardConfirmScope = ""
		r.rebuildPanel(h)
		return true
	case keyHome, keyEnd:
		list := findClipboardList(h.root)
		if list == nil || list.ItemCount == 0 {
			return false
		}
		index := 0
		if key == keyEnd {
			index = list.ItemCount - 1
		}
		row := list.Item(index)
		if row == nil {
			return false
		}
		for i, focus := range h.focus {
			if focus != nil && focus.StableKey() == row.StableKey() {
				h.roving.Set(i)
				if id, ok := strings.CutPrefix(row.Action, "clipboard:restore:"); ok {
					h.clipboardSelectedID = id
				}
				r.rebuildPanel(h)
				if key == keyHome {
					h.scrollTo(0)
				} else {
					h.scrollTo(1 << 30)
				}
				return true
			}
		}
		return false
	case keyPageUp, keyPageDown:
		list := findClipboardList(h.root)
		if list == nil {
			return false
		}
		delta := max(h.logicalH, clipboardRowHeight)
		if key == keyPageUp {
			delta = -delta
		}
		ui.ScrollBy(list, delta)
		if h.logicalW > 0 {
			_ = h.configure(h.logicalW, h.logicalH, h.scale120)
		}
		return true
	}
	return false
}

func findClipboardList(root *ui.Node) *ui.Node {
	if root == nil {
		return nil
	}
	if root.Kind == ui.KindVirtualList {
		return root
	}
	for _, child := range root.Children {
		if list := findClipboardList(child); list != nil {
			return list
		}
	}
	return nil
}

func (h *PanelHost) activateClipboard(r *Registry, n *ui.Node) bool {
	action := n.Action
	if action == "" {
		if h.clipboardSelectedID == "" {
			return false
		}
		return h.sendClipboard(r, h.clipboardSelectedID, func(sender clipboardCommandSender, id string) error { return sender.Restore(id) })
	}
	if action == "clipboard-close" {
		r.closePanelLocked(h.id)
		return true
	}
	if action == "clipboard:selection:toggle" {
		h.clipboardMultiSelect = !h.clipboardMultiSelect
		h.clipboardSelectedIDs = nil
		h.clipboardDeleteManyConfirm = false
		h.clipboardDeleteConfirmID = ""
		h.clipboardConfirmScope = ""
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "clipboard:select:"); ok {
		if !h.clipboardMultiSelect {
			return true
		}
		if _, exists := clipboardEntryByID(r.clipboard.Snapshot.Entries, id); !exists {
			return true
		}
		if h.clipboardSelectedIDs == nil {
			h.clipboardSelectedIDs = make(map[string]struct{})
		}
		if _, exists := h.clipboardSelectedIDs[id]; exists {
			delete(h.clipboardSelectedIDs, id)
		} else {
			h.clipboardSelectedIDs[id] = struct{}{}
		}
		h.clipboardDeleteManyConfirm = false
		h.clipboardConfirmScope = ""
		h.clipboardDeleteConfirmID = ""
		r.rebuildPanel(h)
		return true
	}
	if action == "clipboard:delete-selected" {
		if h.clipboardMultiSelect && len(h.clipboardSelectedIDs) > 0 {
			h.clipboardDeleteManyConfirm = true
			h.clipboardConfirmScope = ""
			h.clipboardDeleteConfirmID = ""
			r.rebuildPanel(h)
		}
		return true
	}
	if action == "clipboard:cancel-delete-many" {
		h.clipboardDeleteManyConfirm = false
		r.rebuildPanel(h)
		return true
	}
	if action == "clipboard:confirm-delete-many" {
		ids := selectedClipboardIDsInOrder(r.clipboard.Snapshot.Entries, h.clipboardSelectedIDs)
		if len(ids) == 0 {
			h.clipboardSelectedIDs = nil
			h.clipboardDeleteManyConfirm = false
			r.rebuildPanel(h)
			return true
		}
		if err := h.clipboardCommand(r, "", func(sender clipboardCommandSender, _ string) error { return sender.DeleteMany(ids) }); err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return true
		}
		h.clipboardSelectedIDs = nil
		h.clipboardDeleteManyConfirm = false
		h.errLabel = ""
		r.rebuildPanel(h)
		return true
	}
	if filter, ok := strings.CutPrefix(action, "clipboard:filter:"); ok {
		switch filter {
		case clipboardFilterAll, clipboardFilterText, clipboardFilterLongText, clipboardFilterImage:
			h.clipboardFilter = filter
			r.rebuildPanel(h)
			return true
		default:
			return false
		}
	}
	if id, ok := strings.CutPrefix(action, "clipboard:restore:"); ok {
		h.clipboardSelectedID = id
		ok := h.sendClipboard(r, id, func(sender clipboardCommandSender, id string) error { return sender.Restore(id) })
		r.rebuildPanel(h)
		return ok
	}
	if id, ok := strings.CutPrefix(action, "clipboard:pin:"); ok {
		entry, exists := clipboardEntryByID(r.clipboard.Snapshot.Entries, id)
		if !exists {
			return true
		}
		h.clipboardSelectedID = id
		return h.sendClipboard(r, id, func(sender clipboardCommandSender, id string) error { return sender.Pin(id, !entry.Pinned) })
	}
	if id, ok := strings.CutPrefix(action, "clipboard:delete:"); ok {
		h.clipboardSelectedID = id
		h.clipboardDeleteConfirmID = id
		h.clipboardConfirmScope = ""
		r.rebuildPanel(h)
		return true
	}
	if scope, ok := strings.CutPrefix(action, "clipboard:clear:"); ok && (scope == string(clipboardprotocol.ClearAll) || scope == string(clipboardprotocol.ClearUnpinned)) {
		h.clipboardConfirmScope = scope
		h.clipboardDeleteConfirmID = ""
		r.rebuildPanel(h)
		return true
	}
	if scope, ok := strings.CutPrefix(action, "clipboard:confirm-clear:"); ok {
		return sendClipboardScope(r, h, clipboardprotocol.ClearScope(scope))
	}
	if action == "clipboard:cancel-clear" {
		h.clipboardConfirmScope = ""
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "clipboard:confirm-delete:"); ok {
		if err := h.clipboardCommand(r, id, func(sender clipboardCommandSender, id string) error { return sender.Delete(id) }); err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return true
		}
		h.clipboardDeleteConfirmID = ""
		r.rebuildPanel(h)
		return true
	}
	if action == "clipboard:cancel-delete" {
		h.clipboardDeleteConfirmID = ""
		r.rebuildPanel(h)
		return true
	}
	return false
}

func (h *PanelHost) sendClipboard(r *Registry, id string, send func(clipboardCommandSender, string) error) bool {
	if err := h.clipboardCommand(r, id, send); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
	}
	return true
}

func (h *PanelHost) clipboardCommand(r *Registry, id string, send func(clipboardCommandSender, string) error) error {
	if r.clipboardSender == nil {
		return fmt.Errorf("clipboard daemon unavailable")
	}
	return send(r.clipboardSender, id)
}

func clipboardImageEntry(entries []clipboardprotocol.Entry, id string) bool {
	entry, ok := clipboardEntryByID(entries, id)
	return ok && entry.Kind == clipboardprotocol.KindImage
}

func pruneClipboardImages(h *PanelHost, entries []clipboardprotocol.Entry) {
	if h == nil {
		return
	}
	for id := range h.clipboardThumbnails {
		if !clipboardImageEntry(entries, id) {
			delete(h.clipboardThumbnails, id)
		}
	}
	for id := range h.clipboardThumbnailRequest {
		if !clipboardImageEntry(entries, id) {
			delete(h.clipboardThumbnailRequest, id)
		}
	}
}

func sendClipboardScope(r *Registry, h *PanelHost, scope clipboardprotocol.ClearScope) bool {
	if err := h.clipboardCommand(r, "", func(sender clipboardCommandSender, _ string) error { return sender.Clear(scope) }); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return true
	}
	h.clipboardConfirmScope = ""
	r.rebuildPanel(h)
	return true
}
