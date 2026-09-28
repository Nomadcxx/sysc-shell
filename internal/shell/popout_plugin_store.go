package shell

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	pluginStorePadding      = 20
	pluginStoreGap          = 12
	pluginStoreMinCardWidth = 300
	pluginStoreCardHeight   = 344
	pluginStoreCardPadding  = 10
	pluginStoreBadgeHeight  = 24
	// pluginStoreDescriptionHeight holds two caption lines, so every card
	// in a row keeps the same status line position.
	pluginStoreDescriptionHeight = 40
	pluginStoreControlHeight     = 40
)

// pluginStoreTree projects the immutable worker snapshot into the browse panel.
func pluginStoreTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	h.pluginStoreQuery.Text = h.search.Text
	state := r.pluginStoreSnapshot
	metrics := h.theme.Metrics
	inner := max(h.place.Panel.W-2*pluginStorePadding, 0)
	if h.pluginStoreDetail != "" {
		if pluginStoreHasListing(state.Listings, h.pluginStoreDetail) {
			return pluginStoreRoot([]*ui.Node{pluginStoreDetail(r, h, h.pluginStoreDetail, inner, metrics)})
		}
		h.pluginStoreDetail = ""
		h.pluginStoreConsent = nil
		h.pluginStoreRemoveConfirm = false
		h.pluginStoreDetailErr = ""
		h.pluginStoreDetailScroll = 0
	}
	listings := browseListings(state.Listings, h.pluginStoreQuery)
	if !pluginStoreHasListing(listings, h.pluginStoreSelected) {
		h.pluginStoreSelected = ""
		if len(listings) > 0 {
			h.pluginStoreSelected = pluginStoreKey(listings[0])
			h.pluginStoreScroll = 0
		}
	}

	children := []*ui.Node{
		pluginStoreHeader(h, len(listings), state.Busy),
		pluginStoreSearch(h, inner, metrics),
		pluginStoreChips(h, state, inner, metrics),
	}
	allFailed, stale := pluginStoreSourceState(state.Sources)
	loading := strings.Contains(strings.ToLower(state.Busy), "refresh") && len(state.Listings) == 0
	switch {
	case loading:
		children = append(children, pluginStoreBanner("Loading plugins…", "", false, inner, metrics))
	case allFailed:
		children = append(children, pluginStoreBanner("All sources failed", firstSourceError(state.Sources), true, inner, metrics))
	}
	if len(stale) > 0 {
		children = append(children, pluginStoreBanner("Stale sources", strings.Join(stale, ", "), false, inner, metrics))
	}

	if len(listings) == 0 {
		if !loading && !allFailed {
			clear := pluginStoreButton("store-clear", "Clear filters", metrics)
			children = append(children, &ui.Node{
				Kind: ui.KindRow, Gap: pluginStoreGap, Height: metrics.StandardControl,
				Children: []*ui.Node{
					{Kind: ui.KindText, Text: "No plugins match", TextRole: theme.RoleBody}, clear,
				},
			})
		}
		if r.pluginStore != nil {
			r.pluginStore.Want(nil)
		}
		return pluginStoreRoot(children)
	}

	columns := max(1, inner/pluginStoreMinCardWidth)
	rows := (len(listings) + columns - 1) / columns
	used := pluginStorePadding*2 + pluginStoreHeaderHeight(metrics) + pluginStoreGap +
		pluginStoreControlHeight + pluginStoreGap + pluginStoreControlHeight + pluginStoreGap
	if loading || allFailed {
		used += metrics.StandardControl + pluginStoreGap
	}
	if len(stale) > 0 {
		used += metrics.StandardControl + pluginStoreGap
	}
	gridHeight := max(h.place.Panel.H-used, pluginStoreCardHeight)
	h.pluginStoreColumns = columns
	h.pluginStoreRowHeight = pluginStoreCardHeight
	h.pluginStoreGridHeight = gridHeight
	h.pluginStoreScroll = min(max(h.pluginStoreScroll, 0), max(rows*pluginStoreCardHeight-gridHeight, 0))
	startRow := h.pluginStoreScroll / pluginStoreCardHeight
	visibleRows := max(1, (gridHeight+pluginStoreCardHeight-1)/pluginStoreCardHeight)
	media := pluginStoreVisibleMedia(listings, state.Media, columns, startRow, visibleRows)
	if r.pluginStore != nil {
		r.pluginStore.Want(media)
	}
	grid := &ui.Node{
		Kind: ui.KindVirtualList, Key: "plugin-store-grid", Width: inner,
		Height: gridHeight, ItemCount: rows, ItemHeight: pluginStoreCardHeight,
		ScrollOffset: h.pluginStoreScroll,
		Item: func(row int) *ui.Node {
			return pluginStoreGridRow(r, h, listings, row, columns, inner)
		},
	}
	children = append(children, grid)
	return pluginStoreRoot(children)
}

func pluginStoreRoot(children []*ui.Node) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Padding: pluginStorePadding, Gap: pluginStoreGap, Children: children}
}

func pluginStoreHeaderHeight(m theme.Metrics) int { return m.StandardControl }

func pluginStoreHeader(h *PanelHost, count int, busy string) *ui.Node {
	metrics := h.theme.Metrics
	countText := fmt.Sprintf("%d plugins", count)
	if strings.Contains(strings.ToLower(busy), "refresh") {
		countText = "Checking sources…"
	}
	left := &ui.Node{Kind: ui.KindRow, Gap: pluginStoreGap, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Plugins", Name: "Plugins", Role: "heading", TextRole: theme.RoleTitle},
		{Kind: ui.KindText, Text: countText, TextRole: theme.RoleCaption},
	}}
	right := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
		pluginStoreIconButton("store-refresh", "Refresh", "refresh", metrics),
		pluginStoreIconButton("store-close", "Close", "close", metrics),
	}}
	return &ui.Node{Kind: ui.KindRow, Height: metrics.StandardControl, PinEnd: true, Children: []*ui.Node{left, right}}
}

func pluginStoreSearch(h *PanelHost, width int, metrics theme.Metrics) *ui.Node {
	field := h.search.Node("Search")
	field.Key = "plugin-store-search"
	field.Placeholder = "Search plugins…"
	field.Width = width
	field.Height = metrics.StandardControl
	field.Padding = 8
	return field
}

func pluginStoreChips(h *PanelHost, state store.State, width int, metrics theme.Metrics) *ui.Node {
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Width: width, Height: metrics.StandardControl}
	sources := make([]string, 0, len(state.Sources))
	for _, source := range state.Sources {
		if source.Name != "" && !slices.Contains(sources, source.Name) {
			sources = append(sources, source.Name)
		}
	}
	row.Children = append(row.Children, pluginStoreChip("store-source:", "All", h.pluginStoreQuery.Source == "", metrics))
	for _, source := range sources {
		row.Children = append(row.Children, pluginStoreChip("store-source:"+source, source, h.pluginStoreQuery.Source == source, metrics))
	}
	row.Children = append(row.Children, pluginStoreCategoryMenu(h, state, metrics))
	row.Children = append(row.Children,
		pluginStoreChip("store-hide-installed", "Hide installed", h.pluginStoreQuery.HideInstalled, metrics),
		pluginStoreChip("store-sort", pluginStoreSortLabel(h.pluginStoreQuery.Sort), false, metrics),
	)
	return row
}

func pluginStoreCategoryMenu(h *PanelHost, state store.State, metrics theme.Metrics) *ui.Node {
	categories := make([]string, 0, len(state.Listings))
	for _, listing := range state.Listings {
		if h.pluginStoreQuery.Source != "" && listing.Source != h.pluginStoreQuery.Source {
			continue
		}
		if listing.Entry.Category != "" && !slices.Contains(categories, listing.Entry.Category) {
			categories = append(categories, listing.Entry.Category)
		}
	}
	slices.Sort(categories)
	options := []string{"All categories"}
	for _, category := range categories {
		options = append(options, categoryLabel(category))
	}
	selected := 0
	if h.pluginStoreQuery.Category != "" {
		for i, category := range categories {
			if category == h.pluginStoreQuery.Category {
				selected = i + 1
				break
			}
		}
	}
	if h.menus == nil {
		h.menus = map[string]*Menu{}
	}
	const path = "plugin-store-category"
	menu := h.menus[path]
	if menu == nil || !slices.Equal(menu.options, options) {
		wasOpen := menu != nil && h.menu == menu && menu.Opened()
		menu = NewMenu(options, selected)
		if wasOpen {
			menu.Open()
			h.menu = menu
		}
		h.menus[path] = menu
	} else {
		menu.index = selected
		if !menu.Opened() {
			menu.cursor = selected
		}
	}
	n := menu.Node()
	n.Action = path
	n.Key = path
	n.Width = 180
	n.Height = metrics.StandardControl
	n.Padding = metrics.ButtonPadding
	n.Name = "Category"
	n.Role = "combobox"
	return n
}

func categoryLabel(category string) string {
	if category == "" {
		return category
	}
	return strings.ToUpper(category[:1]) + category[1:]
}

func pluginStoreSortLabel(key SortKey) string {
	switch key {
	case SortUpdated:
		return "Updated"
	case SortAdded:
		return "Added"
	case SortCategory:
		return "Category"
	case SortInstalledFirst:
		return "Installed first"
	default:
		return "Name"
	}
}

func pluginStoreChip(action, label string, selected bool, metrics theme.Metrics) *ui.Node {
	fill := ui.FillContainerHigh
	if selected {
		fill = ui.FillAccent
	}
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: label, Role: "button",
		Focusable: true, Height: metrics.StandardControl, Padding: metrics.ButtonPadding,
		Fill: fill, Shape: ui.ShapeMedium, Children: []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

func pluginStoreButton(action, label string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Text: label, Name: label,
		Role: "button", Focusable: true, Height: metrics.StandardControl,
		Padding: metrics.ButtonPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium,
	}
}

func pluginStoreIconButton(action, name, icon string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: name, Role: "button",
		Focusable: true, Width: metrics.StandardControl, Height: metrics.StandardControl,
		Shape: ui.ShapeCircle, Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: metrics.IconNormal}},
	}
}

func pluginStoreBanner(title, detail string, retry bool, width int, metrics theme.Metrics) *ui.Node {
	children := []*ui.Node{{Kind: ui.KindText, Text: title, TextRole: theme.RoleBody}}
	if detail != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: detail, TextRole: theme.RoleCaption, MaxWidth: max(width-pluginStoreMinCardWidth, 1)}) // the title and Retry fit in a card width
	}
	if retry {
		children = append(children, pluginStoreButton("store-refresh", "Retry", metrics))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Height: metrics.StandardControl, Fill: ui.FillErrorContainer, Shape: ui.ShapeMedium, Children: children}
}

func pluginStoreGridRow(r *Registry, h *PanelHost, listings []store.Listing, row, columns, width int) *ui.Node {
	start := row * columns
	count := min(columns, len(listings)-start)
	if count <= 0 {
		return &ui.Node{Kind: ui.KindRow, Height: pluginStoreCardHeight}
	}
	gap := pluginStoreGap
	cardWidth := (width - gap*(columns-1)) / columns
	children := make([]*ui.Node, 0, count)
	for i := start; i < start+count; i++ {
		children = append(children, pluginStoreCardNode(r, h, listings[i], cardWidth))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: gap, Height: pluginStoreCardHeight, Children: children}
}

func pluginStoreCardNode(r *Registry, h *PanelHost, listing store.Listing, width int) *ui.Node {
	card := cardFor(listing, r.pluginStoreSnapshot.Media)
	key := pluginStoreKey(listing)
	imageWidth := max(width-2*pluginStoreCardPadding, 1)
	imageHeight := imageWidth * 9 / 16
	placeholderLabel := "No screenshot provided"
	if card.Screenshot.SHA256 != "" {
		placeholderLabel = "Preview loading…"
		if media, ok := r.pluginStoreSnapshot.Media[card.Screenshot.SHA256]; ok && media.Err != nil {
			placeholderLabel = "Preview unavailable"
		}
	}
	stack := &ui.Node{Kind: ui.KindStack, Width: imageWidth, Height: imageHeight, Children: []*ui.Node{
		{Kind: ui.KindCapsule, Width: imageWidth, Height: imageHeight, Fill: ui.FillContainer, Shape: ui.ShapeMedium},
	}}
	if card.ScreenshotPath != "" {
		image := &ui.Node{
			Kind: ui.KindImage, ImageW: imageWidth, ImageH: imageHeight,
			ImagePath: card.ScreenshotPath, Background: true,
		}
		if r.plugins != nil && r.plugins.images != nil {
			if workerKey, ok := pluginImageKey(image); ok {
				if decoded, hit, err := r.plugins.images.Request(workerKey); err == nil && hit {
					image.Image = decoded
				}
			}
		}
		stack.Children = append(stack.Children, image)
		if image.Image == nil {
			stack.Children = append(stack.Children, &ui.Node{Kind: ui.KindIcon, Icon: card.Glyph, IconSize: h.theme.Metrics.IconLarge})
		}
	} else {
		stack.Children = append(stack.Children, &ui.Node{Kind: ui.KindIcon, Icon: card.Glyph, IconSize: h.theme.Metrics.IconLarge})
	}
	if len(card.Badges) > 0 {
		badgeGap := theme.MarginXXS
		badgeWidth := max((imageWidth-badgeGap*(len(card.Badges)-1))/len(card.Badges), 1)
		badges := &ui.Node{Kind: ui.KindRow, Width: imageWidth, Height: pluginStoreBadgeHeight, Gap: badgeGap}
		for _, badge := range card.Badges {
			badges.Children = append(badges.Children, &ui.Node{
				Kind: ui.KindCapsule, Width: badgeWidth, Height: pluginStoreBadgeHeight - 2*theme.MarginXXS,
				Fill: pluginStoreBadgeFill(badge.Tone), Shape: ui.ShapeSmall, Padding: theme.MarginXXS,
				Children: []*ui.Node{{Kind: ui.KindText, Text: badge.Label, MaxWidth: badgeWidth - 2*theme.MarginXXS, TextRole: theme.RoleCaption}},
			})
		}
		stack.Children = append(stack.Children, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{badges}})
	}
	description := card.Description
	if description == "" {
		description = "No description provided"
	}
	statusIcon, statusLabel := pluginStoreCardStatus(card)
	contentChildren := []*ui.Node{stack}
	if placeholderLabel != "" {
		contentChildren = append(contentChildren, &ui.Node{Kind: ui.KindText, Text: placeholderLabel, TextRole: theme.RoleCaption, MaxWidth: imageWidth})
	}
	contentChildren = append(contentChildren,
		&ui.Node{Kind: ui.KindText, Text: card.Title, TextRole: theme.RoleBody, MaxWidth: imageWidth},
		&ui.Node{Kind: ui.KindText, Text: card.Byline, TextRole: theme.RoleCaption, MaxWidth: imageWidth},
		&ui.Node{Kind: ui.KindText, Text: description, TextRole: theme.RoleCaption, MaxWidth: imageWidth, Height: pluginStoreDescriptionHeight, Multiline: true},
		&ui.Node{Kind: ui.KindRow, PinEnd: true, Gap: theme.MarginXXS, Height: h.theme.Metrics.CompactControl, Tooltip: card.Reason, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: statusIcon, IconSize: h.theme.Metrics.IconSmall},
			{Kind: ui.KindText, Text: statusLabel, TextRole: theme.RoleCaption, MaxWidth: imageWidth - h.theme.Metrics.IconSmall - theme.MarginXXS},
		}},
	)
	content := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: contentChildren}
	state := ui.Interaction(0)
	if h.pluginStoreSelected == key {
		state |= ui.StateSelected
	}
	return &ui.Node{
		Kind: ui.KindButton, Key: "store-card:" + key, Action: "store-open:" + key,
		Name: card.Title, Role: "button", Focusable: true, Width: width, Height: pluginStoreCardHeight,
		Padding: pluginStoreCardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium,
		State: state, Children: []*ui.Node{content},
	}
}

func pluginStoreBadgeFill(tone BadgeTone) ui.Fill {
	switch tone {
	case BadgeOfficial:
		return ui.FillAccent
	case BadgeCommunity, BadgeWarning:
		return ui.FillSoft
	case BadgeLocal:
		return ui.FillContainerHighest
	default:
		return ui.FillContainer
	}
}

func pluginStoreCardStatus(card Card) (string, string) {
	switch card.Action {
	case ActionInstall:
		return "add", "Install"
	case ActionInstalled:
		return "check", "Installed"
	case ActionUpdate:
		return "restart_alt", "Update"
	default:
		if card.Reason == "" {
			return "disabled_by_default", "Unavailable"
		}
		return "disabled_by_default", card.Reason
	}
}

func pluginStoreVisibleMedia(listings []store.Listing, media map[string]store.MediaState, columns, startRow, visibleRows int) []store.MediaKey {
	if columns < 1 {
		columns = 1
	}
	rows := (len(listings) + columns - 1) / columns
	start := min(max(startRow, 0), rows)
	end := min(rows, start+max(visibleRows, 1)+1)
	var keys []store.MediaKey
	for row := start; row < end; row++ {
		for i := row * columns; i < min((row+1)*columns, len(listings)); i++ {
			card := cardFor(listings[i], media)
			if card.Screenshot.SHA256 != "" {
				keys = append(keys, card.Screenshot)
			}
		}
	}
	return keys
}

func pluginStoreSourceState(sources []store.SourceState) (bool, []string) {
	allFailed := len(sources) > 0
	var stale []string
	for _, source := range sources {
		if source.Err == nil {
			allFailed = false
			continue
		}
		if !source.FetchedAt.IsZero() {
			stale = append(stale, source.Name)
		}
	}
	return allFailed, stale
}

func firstSourceError(sources []store.SourceState) string {
	for _, source := range sources {
		if source.Err != nil {
			return source.Err.Error()
		}
	}
	return ""
}

func pluginStoreKey(listing store.Listing) string { return listing.Source + "/" + listing.Entry.ID }

func pluginStoreHasListing(listings []store.Listing, key string) bool {
	if key == "" {
		return false
	}
	for _, listing := range listings {
		if pluginStoreKey(listing) == key {
			return true
		}
	}
	return false
}

func pluginStoreFind(listings []store.Listing, key string) (store.Listing, bool) {
	for _, listing := range listings {
		if pluginStoreKey(listing) == key {
			return listing, true
		}
	}
	return store.Listing{}, false
}

func (h *PanelHost) pluginStoreKeyPress(r *Registry, key uint32) bool {
	focused := h.focused()
	searching := focused != nil && focused.Kind == ui.KindTextField
	if key == keyEsc {
		if h.pluginStoreDetail != "" {
			h.pluginStoreLeaveDetail(r)
		} else if h.search != nil && h.search.Text != "" {
			h.search.Clear()
			h.pluginStoreQuery.Text = ""
			h.pluginStoreScroll = 0
			r.rebuildPanel(h)
		} else {
			r.closePanelLocked(PanelPluginStore)
		}
		return true
	}
	if h.pluginStoreDetail != "" && (key == keyPageUp || key == keyPageDown) {
		h.pluginStorePageDetail(r, key == keyPageUp)
		return true
	}
	if !searching && !h.mods.Has(ui.ModCtrl) && !h.mods.Has(ui.ModAlt) {
		if text, ok := ui.EvdevText(key, h.mods.Has(ui.ModShift)); ok && text == "/" {
			h.focusByName("Search")
			r.rebuildPanel(h)
			return true
		}
	}
	if searching {
		// fieldKey has already taken the caret and editing keys; the grid
		// keys are inert while the query has focus.
		switch key {
		case keyUp, keyDown, keyPageUp, keyPageDown:
			return true
		case keyEnter:
			return true
		}
		return false
	}
	listings := browseListings(r.pluginStoreSnapshot.Listings, h.pluginStoreQuery)
	index := pluginStoreSelectionIndex(listings, h.pluginStoreSelected)
	switch key {
	case keyLeft:
		h.movePluginStoreSelection(r, index-1, listings)
	case keyRight:
		h.movePluginStoreSelection(r, index+1, listings)
	case keyUp:
		h.movePluginStoreSelection(r, index-h.pluginStoreColumns, listings)
	case keyDown:
		h.movePluginStoreSelection(r, index+h.pluginStoreColumns, listings)
	case keyPageUp, keyPageDown:
		pageRows := max(1, h.pluginStoreGridHeight/max(h.pluginStoreRowHeight, 1))
		delta := pageRows * h.pluginStoreColumns
		if key == keyPageUp {
			delta = -delta
		}
		h.movePluginStoreSelection(r, index+delta, listings)
	case keyEnter:
		if h.pluginStoreSelected != "" {
			h.pluginStoreDetail = h.pluginStoreSelected
			h.pluginStoreConsent = nil
			h.pluginStoreRemoveConfirm = false
			h.pluginStoreDetailErr = ""
			h.pluginStoreDetailScroll = 0
			r.rebuildPanel(h)
		}
	default:
		return false
	}
	return true
}

func pluginStoreSelectionIndex(listings []store.Listing, key string) int {
	for i, listing := range listings {
		if pluginStoreKey(listing) == key {
			return i
		}
	}
	return 0
}

func (h *PanelHost) movePluginStoreSelection(r *Registry, index int, listings []store.Listing) {
	if len(listings) == 0 {
		return
	}
	index = min(max(index, 0), len(listings)-1)
	h.pluginStoreSelected = pluginStoreKey(listings[index])
	columns := max(h.pluginStoreColumns, 1)
	row := index / columns
	rowHeight := max(h.pluginStoreRowHeight, pluginStoreCardHeight)
	visibleRows := max(1, (h.pluginStoreGridHeight+rowHeight-1)/rowHeight)
	start := h.pluginStoreScroll / rowHeight
	if row < start {
		h.pluginStoreScroll = row * rowHeight
	} else if row >= start+visibleRows {
		h.pluginStoreScroll = (row - visibleRows + 1) * rowHeight
	}
	maxScroll := max(((len(listings)+columns-1)/columns)*rowHeight-h.pluginStoreGridHeight, 0)
	h.pluginStoreScroll = min(max(h.pluginStoreScroll, 0), maxScroll)
	r.rebuildPanel(h)
}

func (h *PanelHost) focusPluginStoreSelection() {
	if h == nil {
		return
	}
	key := "store-card:" + h.pluginStoreSelected
	for i, node := range h.focus {
		if node != nil && node.StableKey() == key {
			h.roving.Set(i)
			return
		}
	}
	h.focusByName("Search")
}

func (h *PanelHost) activatePluginStore(r *Registry, n *ui.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == ui.KindMenu && n.Action == "plugin-store-category" {
		menu := h.menus[n.Action]
		if menu == nil {
			return true
		}
		h.menu = menu
		h.menuPath = n.Action
		if !menu.Opened() {
			menu.Open()
			r.rebuildPanel(h)
			return true
		}
		if !menu.PickAt(n, h.hoverX, h.hoverY) {
			return h.pressMenuFilter(r)
		}
		menu.Select()
		return h.applyPluginStoreCategory(r)
	}
	switch {
	case n.Action == "store-close":
		r.closePanelLocked(PanelPluginStore)
	case n.Action == "store-refresh":
		if r.pluginStore != nil {
			_, _ = r.pluginStore.Refresh()
		}
	case n.Action == "store-clear":
		h.pluginStoreQuery = BrowseQuery{Sort: SortName}
		if h.search != nil {
			h.search.Clear()
		}
		h.pluginStoreScroll = 0
		r.rebuildPanel(h)
	case n.Action == "store-hide-installed":
		h.pluginStoreQuery.HideInstalled = !h.pluginStoreQuery.HideInstalled
		h.pluginStoreScroll = 0
		r.rebuildPanel(h)
	case n.Action == "store-sort":
		h.pluginStoreQuery.Sort = h.pluginStoreQuery.Sort.Next()
		h.pluginStoreScroll = 0
		r.rebuildPanel(h)
	case strings.HasPrefix(n.Action, "store-source:"):
		h.pluginStoreQuery.Source = strings.TrimPrefix(n.Action, "store-source:")
		h.pluginStoreScroll = 0
		r.rebuildPanel(h)
	case strings.HasPrefix(n.Action, "store-open:"):
		h.pluginStoreSelected = strings.TrimPrefix(n.Action, "store-open:")
		h.pluginStoreDetail = h.pluginStoreSelected
		h.pluginStoreConsent = nil
		h.pluginStoreRemoveConfirm = false
		h.pluginStoreDetailErr = ""
		h.pluginStoreDetailScroll = 0
		r.rebuildPanel(h)
	case n.Action == "store-back":
		h.pluginStoreLeaveDetail(r)
	case n.Action == "store-primary":
		h.pluginStoreBeginPrimary(r)
	case n.Action == "store-confirm":
		h.pluginStoreConfirm(r)
	case n.Action == "store-cancel-confirmation":
		h.pluginStoreConsent = nil
		h.pluginStoreRemoveConfirm = false
		h.pluginStoreDetailErr = ""
		r.rebuildPanel(h)
	case n.Action == "store-open-homepage", n.Action == "store-open-release-notes":
		h.pluginStoreOpenLink(r, n.Action)
	default:
		return false
	}
	return true
}

func (h *PanelHost) applyPluginStoreCategory(r *Registry) bool {
	if h.menu != nil {
		value := h.menu.Value()
		if strings.EqualFold(value, "All categories") {
			h.pluginStoreQuery.Category = ""
		} else {
			h.pluginStoreQuery.Category = strings.ToLower(value)
		}
		h.menuPath = ""
	}
	h.pluginStoreScroll = 0
	r.rebuildPanel(h)
	return true
}
