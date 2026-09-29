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
	pluginStoreMinCardWidth = 290
	pluginStoreCardPadding  = 10
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
	}
	children = append(children, pluginStoreChips(h, state, inner, metrics)...)
	allFailed, stale := pluginStoreSourceState(state.Sources)
	loading := strings.Contains(strings.ToLower(state.Busy), "refresh") && len(state.Listings) == 0
	switch {
	case loading:
		children = append(children, pluginStoreBanner(h, "Loading plugins…", "", false, inner, metrics))
	case allFailed:
		children = append(children, pluginStoreBanner(h, "All sources failed", firstSourceError(state.Sources), true, inner, metrics))
	}
	if len(stale) > 0 {
		children = append(children, pluginStoreBanner(h, "Stale sources", strings.Join(stale, ", "), false, inner, metrics))
	}

	if len(listings) == 0 {
		if !loading && !allFailed {
			children = append(children, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "No plugins match", TextRole: theme.RoleTitle},
				{Kind: ui.KindText, Text: "Try another search, source or category.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
				pluginStoreButton("store-clear", "Clear filters", metrics),
			}})
		}
		if r.pluginStore != nil {
			r.pluginStore.Want(nil)
		}
		return pluginStoreRoot(children)
	}

	// The cards leave the scroll bar its own lane at the right edge.
	cards := max(inner-theme.MarginM, 1)
	columns := max(1, (cards+pluginStoreGap)/(pluginStoreMinCardWidth+pluginStoreGap))
	cardWidth := (cards - pluginStoreGap*(columns-1)) / columns
	rowHeight := pluginStoreCardHeight(h, cardWidth) + pluginStoreGap
	rows := (len(listings) + columns - 1) / columns
	used := pluginStorePadding*2 + pluginStoreGap
	for _, c := range children {
		ch, _ := ui.ContentHeight(c, inner, h.measureText())
		used += ch + pluginStoreGap
	}
	gridHeight := max(h.place.Panel.H-used, rowHeight)
	h.pluginStoreColumns = columns
	h.pluginStoreRowHeight = rowHeight
	h.pluginStoreGridHeight = gridHeight
	h.pluginStoreScroll = min(max(h.pluginStoreScroll, 0), max(rows*rowHeight-gridHeight, 0))
	startRow := h.pluginStoreScroll / rowHeight
	visibleRows := max(1, (gridHeight+rowHeight-1)/rowHeight)
	media := pluginStoreVisibleMedia(listings, state.Media, columns, startRow, visibleRows)
	if r.pluginStore != nil {
		r.pluginStore.Want(media)
	}
	grid := &ui.Node{
		Kind: ui.KindVirtualList, Key: "plugin-store-grid", Width: inner,
		Height: gridHeight, ItemCount: rows, ItemHeight: rowHeight,
		ScrollOffset: h.pluginStoreScroll,
		Item: func(row int) *ui.Node {
			return pluginStoreGridRow(r, h, listings, row, columns, cardWidth, rowHeight)
		},
	}
	children = append(children, grid)
	return pluginStoreRoot(children)
}

func pluginStoreRoot(children []*ui.Node) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Padding: pluginStorePadding, Gap: pluginStoreGap, Children: children}
}

func pluginStoreHeader(h *PanelHost, count int, busy string) *ui.Node {
	metrics := h.theme.Metrics
	countText := fmt.Sprintf("%d plugins", count)
	if count == 1 {
		countText = "1 plugin"
	}
	if strings.Contains(strings.ToLower(busy), "refresh") {
		countText = "Checking sources…"
	}
	left := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "extension", IconSize: metrics.IconNormal},
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Plugin store", Name: "Plugin store", Role: "heading", TextRole: theme.RoleTitle},
			{Kind: ui.KindText, Text: "Install plugins from your enabled sources · " + countText, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		}},
	}}
	right := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
		pluginStoreIconButton("store-refresh", "Refresh", "refresh", metrics),
		pluginStoreIconButton("store-close", "Close", "close", metrics),
	}}
	return &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{left, right}}
}

func pluginStoreSearch(h *PanelHost, width int, metrics theme.Metrics) *ui.Node {
	field := h.search.Node("Search")
	field.Key = "plugin-store-search"
	field.Placeholder = "Search plugins…"
	field.Width = width
	field.Height = metrics.StandardControl
	field.Padding = metrics.ButtonPadding
	return field
}

// pluginStoreChips is the filter row, and under it the category row while
// Categories is open (Noctalia's expander: every choice stays a chip, so the
// row keeps one height and the keyboard reaches each option in turn).
func pluginStoreChips(h *PanelHost, state store.State, width int, metrics theme.Metrics) []*ui.Node {
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Width: width, Height: metrics.CompactControl}
	var sources []string
	for _, source := range state.Sources {
		if source.Name != "" && !slices.Contains(sources, source.Name) {
			sources = append(sources, source.Name)
		}
	}
	row.Children = append(row.Children, pluginStoreChip("store-source:", "All sources", "", h.pluginStoreQuery.Source == "", metrics))
	for _, source := range sources {
		row.Children = append(row.Children, pluginStoreChip("store-source:"+source, pluginSourceLabel(source), "", h.pluginStoreQuery.Source == source, metrics))
	}
	categoryLabelText := "Categories"
	if h.pluginStoreQuery.Category != "" {
		categoryLabelText = categoryLabel(h.pluginStoreQuery.Category)
	}
	chevron := "chevron_right"
	if h.pluginStoreCategoriesOpen {
		chevron = "expand_more"
	}
	row.Children = append(row.Children,
		pluginStoreChip("store-categories", categoryLabelText, chevron, h.pluginStoreQuery.Category != "", metrics),
		pluginStoreChip("store-hide-installed", "Hide installed", "", h.pluginStoreQuery.HideInstalled, metrics),
		pluginStoreChip("store-sort", "Sort: "+pluginStoreSortLabel(h.pluginStoreQuery.Sort), "swap_vert", false, metrics),
	)
	out := []*ui.Node{row}
	if h.pluginStoreCategoriesOpen {
		cats := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Width: width, Height: metrics.CompactControl}
		cats.Children = append(cats.Children, pluginStoreChip("store-category:", "All categories", "", h.pluginStoreQuery.Category == "", metrics))
		for _, category := range pluginStoreCategories(h, state) {
			cats.Children = append(cats.Children, pluginStoreChip("store-category:"+category, categoryLabel(category), "", h.pluginStoreQuery.Category == category, metrics))
		}
		out = append(out, cats)
	}
	return out
}

// pluginStoreCategories is every category the chosen source offers.
func pluginStoreCategories(h *PanelHost, state store.State) []string {
	var categories []string
	for _, listing := range state.Listings {
		if h.pluginStoreQuery.Source != "" && listing.Source != h.pluginStoreQuery.Source {
			continue
		}
		if listing.Entry.Category != "" && !slices.Contains(categories, listing.Entry.Category) {
			categories = append(categories, listing.Entry.Category)
		}
	}
	slices.Sort(categories)
	return categories
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

func pluginStoreChip(action, label, icon string, selected bool, metrics theme.Metrics) *ui.Node {
	content := []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel}}
	if selected {
		content = append([]*ui.Node{{Kind: ui.KindIcon, Icon: "check", IconSize: metrics.IconSmall}}, content...)
	}
	if icon != "" {
		content = append(content, &ui.Node{Kind: ui.KindIcon, Icon: icon, IconSize: metrics.IconSmall})
	}
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: label, Role: "button",
		Focusable: true, Height: metrics.CompactControl, Padding: metrics.ButtonPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginXS, Children: content}},
	}
	if selected {
		n.Fill = ui.FillSoft
		n.State |= ui.StateSelected
	}
	return n
}

func pluginStoreButton(action, label string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Text: label, Name: label,
		Role: "button", Focusable: true, Height: metrics.CompactControl,
		Padding: metrics.ButtonPadding, Fill: ui.FillOutline, Shape: ui.ShapeMedium,
	}
}

// pluginStoreFilledButton is the one prominent action on a surface.
func pluginStoreFilledButton(action, label, icon string, fill ui.Fill, metrics theme.Metrics) *ui.Node {
	content := []*ui.Node{{Kind: ui.KindText, Text: label}}
	if icon != "" {
		content = append([]*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: metrics.IconSmall}}, content...)
	}
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: label, Role: "button", Focusable: true,
		Height: metrics.CompactControl, Padding: metrics.ButtonPadding, Fill: fill, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginXS, Children: content}},
	}
}

func pluginStoreIconButton(action, name, icon string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: name, Role: "button",
		Focusable: true, Width: metrics.IconButton, Height: metrics.IconButton,
		Shape: ui.ShapeCircle, Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: metrics.IconNormal}},
	}
}

func pluginStoreBanner(h *PanelHost, title, detail string, retry bool, width int, metrics theme.Metrics) *ui.Node {
	fill, icon := ui.FillContainerHigh, "refresh"
	if retry {
		fill, icon = ui.FillErrorContainer, "cancel"
	}
	text := []*ui.Node{{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel}}
	actionsW := 0
	var trailing *ui.Node
	if retry {
		trailing = pluginStoreButton("store-refresh", "Retry", metrics)
		actionsW = settingsResetWidth(h) + theme.MarginM
	}
	textW := max(width-2*metrics.CardPadding-metrics.IconNormal-theme.MarginM-actionsW, 1)
	if detail != "" {
		text = append(text, h.wrappedText(detail, theme.RoleCaption, ui.ToneNormal, textW, 2))
	}
	lead := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: icon, IconSize: metrics.IconNormal},
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: text},
	}}
	body := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: width - 2*metrics.CardPadding, Children: []*ui.Node{lead}}
	if trailing != nil {
		body.Children = append(body.Children, trailing)
	}
	return &ui.Node{Kind: ui.KindCapsule, Width: width, Padding: metrics.CardPadding, Fill: fill, Shape: ui.ShapeMedium, Children: []*ui.Node{body}}
}

// wrappedText breaks text into measured lines at width, at most maxLines of
// them (0 for no limit), the last one ending in an ellipsis when cut. A text
// node's Multiline only honours the breaks already in its string.
func (h *PanelHost) wrappedText(text string, role theme.TextRole, tone ui.Tone, width, maxLines int) *ui.Node {
	measure := h.measureText()
	attrs := ui.TextAttrsOf(&ui.Node{Kind: ui.KindText, TextRole: role})
	lines := wrapLines(text, max(width, 1), func(s string) int { w, _ := measure(s, attrs); return w }, maxLines)
	col := &ui.Node{Kind: ui.KindColumn, Width: width}
	for _, line := range lines {
		col.Children = append(col.Children, &ui.Node{Kind: ui.KindText, Text: line, TextRole: role, Tone: tone, MaxWidth: width})
	}
	return col
}

// pluginStoreLineHeight is one line of text in a role, as the painter sets it.
func (h *PanelHost) pluginStoreLineHeight(role theme.TextRole) int {
	_, lh := h.measureText()("Ag", ui.TextAttrsOf(&ui.Node{Kind: ui.KindText, TextRole: role}))
	return lh
}

func pluginStoreImageHeight(cardWidth int) int {
	return max(cardWidth-2*pluginStoreCardPadding, 1) * 2 / 5
}

// pluginStoreCardHeight is a card sized to what it holds: the preview, the
// title row, the byline and two lines of description. Deriving it keeps the
// card tight at every density and scale instead of a fixed height that left
// dead space under the text.
func pluginStoreCardHeight(h *PanelHost, cardWidth int) int {
	m := h.theme.Metrics
	caption := h.pluginStoreLineHeight(theme.RoleCaption)
	title := max(h.pluginStoreLineHeight(theme.RoleTitle), m.CompactControl)
	return 2*pluginStoreCardPadding + pluginStoreImageHeight(cardWidth) + theme.MarginS +
		title + theme.MarginXXS + caption + theme.MarginXS + 2*caption
}

func pluginStoreGridRow(r *Registry, h *PanelHost, listings []store.Listing, row, columns, cardWidth, rowHeight int) *ui.Node {
	start := row * columns
	count := min(columns, len(listings)-start)
	out := &ui.Node{Kind: ui.KindRow, Gap: pluginStoreGap, Height: rowHeight}
	for i := start; i < start+max(count, 0); i++ {
		out.Children = append(out.Children, pluginStoreCardNode(r, h, listings[i], cardWidth))
	}
	return out
}

// pluginStoreCardNode is one plugin in the grid: the preview with its badges
// laid over the corner (DMS), then the title and status, byline, and two
// lines of description (Noctalia).
func pluginStoreCardNode(r *Registry, h *PanelHost, listing store.Listing, width int) *ui.Node {
	m := h.theme.Metrics
	card := cardFor(listing, r.pluginStoreSnapshot.Media)
	key := pluginStoreKey(listing)
	inner := max(width-2*pluginStoreCardPadding, 1)
	imageHeight := pluginStoreImageHeight(width)
	stack := &ui.Node{Kind: ui.KindStack, Width: inner, Height: imageHeight, Children: []*ui.Node{
		{Kind: ui.KindCapsule, Width: inner, Height: imageHeight, Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium},
	}}
	glyph := &ui.Node{Kind: ui.KindIcon, Icon: card.Glyph, IconSize: m.IconLarge, Tone: ui.ToneAccent}
	if card.ScreenshotPath != "" {
		image := &ui.Node{
			Kind: ui.KindImage, ImageW: inner, ImageH: imageHeight,
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
			stack.Children = append(stack.Children, glyph)
		}
	} else {
		stack.Children = append(stack.Children, glyph)
	}
	if len(card.Badges) > 0 {
		badges := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXXS}
		for _, badge := range card.Badges {
			badges.Children = append(badges.Children, &ui.Node{
				Kind: ui.KindCapsule, Fill: pluginStoreBadgeFill(badge.Tone), Shape: ui.ShapeSmall, Padding: theme.MarginXS,
				Children: []*ui.Node{{Kind: ui.KindText, Text: badge.Label, TextRole: theme.RoleCaption}},
			})
		}
		stack.Children = append(stack.Children, &ui.Node{Kind: ui.KindColumn, Padding: theme.MarginXS, Children: []*ui.Node{badges}})
	}
	description := card.Description
	if description == "" {
		description = "No description provided"
	}
	status := pluginStoreCardStatus(card, m)
	statusLabel := status.Children[0].Children[1]
	labelW, _ := h.measureText()(statusLabel.Text, ui.TextAttrsOf(statusLabel))
	statusW := 2*theme.MarginXS + m.IconSmall + theme.MarginXXS + labelW
	titleRow := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Height: max(h.pluginStoreLineHeight(theme.RoleTitle), m.CompactControl), Children: []*ui.Node{
		{Kind: ui.KindText, Text: card.Title, TextRole: theme.RoleTitle, MaxWidth: max(inner-statusW-theme.MarginS, 1)},
		status,
	}}
	content := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
		stack,
		{Kind: ui.KindColumn, Height: theme.MarginS},
		titleRow,
		{Kind: ui.KindColumn, Height: theme.MarginXXS},
		{Kind: ui.KindText, Text: card.Byline, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: inner},
		{Kind: ui.KindColumn, Height: theme.MarginXS},
		h.wrappedText(description, theme.RoleCaption, ui.ToneNormal, inner, 2),
	}}
	n := &ui.Node{
		Kind: ui.KindButton, Key: "store-card:" + key, Action: "store-open:" + key,
		Name: card.Title, Role: "button", Focusable: true, Width: width, Height: pluginStoreCardHeight(h, width),
		Padding: pluginStoreCardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium,
		Tooltip: card.Reason, Children: []*ui.Node{content},
	}
	if h.pluginStoreSelected == key {
		// The selection is a lift, not a flood: the focus ring already marks
		// the card, and an accent fill drowned its text in the references'
		// place.
		n.Fill = ui.FillContainerHighest
	}
	return n
}

func pluginStoreBadgeFill(tone BadgeTone) ui.Fill {
	switch tone {
	case BadgeOfficial:
		return ui.FillAccent
	case BadgeCommunity, BadgeWarning:
		return ui.FillSoft
	case BadgeLocal:
		return ui.FillContainerHigh
	default:
		return ui.FillContainer
	}
}

// pluginStoreCardStatus is the card's state as a small chip beside the title,
// where DMS keeps its install button.
func pluginStoreCardStatus(card Card, m theme.Metrics) *ui.Node {
	icon, label, fill := "download", "Install", ui.FillOutline
	switch card.Action {
	case ActionInstalled:
		icon, label, fill = "check", "Installed", ui.FillNone
	case ActionUpdate:
		icon, label, fill = "restart_alt", "Update", ui.FillSoft
	case ActionInstall:
	default:
		// The reason is the useful half: "Local copy" or "Needs protocol 1.9"
		// says what to do about it, "Unavailable" does not.
		icon, label, fill = "disabled_by_default", "Unavailable", ui.FillNone
		if card.Reason == "local copy" {
			icon = "folder_open"
		}
		if card.Reason != "" {
			label = strings.ToUpper(card.Reason[:1]) + card.Reason[1:]
		}
	}
	return &ui.Node{Kind: ui.KindCapsule, Fill: fill, Shape: ui.ShapeSmall, Padding: theme.MarginXS, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconSmall},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption},
		}},
	}}
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
	rowHeight := max(h.pluginStoreRowHeight, 1)
	// Only whole rows count as visible: a selection on a clipped row scrolls.
	visibleRows := max(1, h.pluginStoreGridHeight/rowHeight)
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
	// A detail opens on its action, so Enter then Enter reaches the consent
	// step and never installs by itself; without one, on Back.
	keys := []string{"store-card:" + h.pluginStoreSelected}
	if h.pluginStoreDetail != "" {
		keys = []string{"store-primary", "store-back"}
	}
	for _, key := range keys {
		for i, node := range h.focus {
			if node != nil && node.StableKey() == key {
				h.roving.Set(i)
				return
			}
		}
	}
	h.focusByName("Search")
}

func (h *PanelHost) activatePluginStore(r *Registry, n *ui.Node) bool {
	if n == nil {
		return false
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
	case n.Action == "store-categories":
		h.pluginStoreCategoriesOpen = !h.pluginStoreCategoriesOpen
		r.rebuildPanel(h)
	case strings.HasPrefix(n.Action, "store-category:"):
		h.pluginStoreQuery.Category = strings.TrimPrefix(n.Action, "store-category:")
		h.pluginStoreCategoriesOpen = false
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
