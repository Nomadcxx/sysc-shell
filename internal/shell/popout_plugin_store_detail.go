package shell

import (
	"fmt"
	"net/url"
	"os/exec"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

type pluginStorePinnedConsent struct {
	key     string
	listing store.Listing
	ref     store.ReleaseRef
	action  StoreAction
	enabled bool
}

// pluginStorePendingOp is an install, update or removal the detail queued.
// The worker reports its outcome through the snapshot, so the line under the
// summary is read from the listing: in progress until the listing shows the
// result, and nothing once a new error appears, which the detail shows.
type pluginStorePendingOp struct {
	key, name, version string
	update, remove     bool
	enabled            bool
	prevErr            error
}

func (op *pluginStorePendingOp) status(l store.Listing) string {
	switch {
	case l.Err != nil && l.Err != op.prevErr:
		return ""
	case op.remove && l.Installed == nil:
		return "Removed " + op.name + ". Its settings are kept."
	case op.remove:
		return "Removing " + op.name + "…"
	case l.Installed != nil && l.Installed.Version == op.version:
		done := "Installed "
		if op.update {
			done = "Updated "
		}
		if op.enabled {
			return done + op.name + " " + op.version + ". It is enabled and starts now."
		}
		return done + op.name + " " + op.version + ". Turn it on in Settings → Plugins."
	case op.update:
		return "Updating " + op.name + " to " + op.version + "…"
	default:
		return "Installing " + op.name + " " + op.version + "…"
	}
}

var openURL = openURLDefault

func openURLDefault(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("only https URLs can be opened")
	}
	cmd := exec.Command("xdg-open", parsed.String())
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func pluginStoreDetail(r *Registry, h *PanelHost, key string, width int, metrics theme.Metrics) *ui.Node {
	listing, ok := pluginStoreFind(r.pluginStoreSnapshot.Listings, key)
	if !ok {
		return &ui.Node{Kind: ui.KindText, Text: "Plugin no longer available", TextRole: theme.RoleBody}
	}
	enabled := slices.Contains(r.cfg.Plugins.Enabled, listing.Entry.ID)
	pinned := h.pluginStoreConsent
	consentOpen := pinned != nil && pinned.key == key
	if consentOpen {
		listing = pinned.listing
		enabled = pinned.enabled
	}
	readmeKey := ""
	var keys []store.MediaKey
	detail := detailFor(listing, enabled, r.pluginStoreSnapshot.Media, "")
	if detail.Screenshot.SHA256 != "" {
		keys = append(keys, detail.Screenshot)
	}
	if listing.Entry.Readme != nil {
		readmeKey = listing.Entry.Readme.SHA256
		keys = append(keys, store.MediaKey{
			URL: listing.Entry.Readme.URL, SHA256: readmeKey, Max: catalog.MaxReadmeBytes,
		})
	}
	if r.pluginStore != nil {
		r.pluginStore.Want(keys)
	}
	readme := r.pluginStoreReadmes[readmeKey]
	detail = detailFor(listing, enabled, r.pluginStoreSnapshot.Media, readme)

	body := max(width-theme.MarginM, 1) // room for the scroll bar
	children := []*ui.Node{pluginStoreDetailSummary(r, h, detail, body, metrics)}
	if op := h.pluginStorePending; op != nil && op.key == key {
		if text := op.status(listing); text != "" {
			note := h.wrappedText(text, theme.RoleBody, ui.ToneAccent, body, 0)
			note.Key = "store-detail-note"
			children = append(children, note)
		}
	}
	if consentOpen {
		children = append(children, pluginStoreConsentBlock(h, detail, body, metrics))
	} else if h.pluginStoreRemoveConfirm {
		children = append(children, pluginStoreRemoveBlock(h, detail, body, metrics))
	}
	if errText := pluginStoreDetailError(h, listing); errText != "" {
		errNode := h.wrappedText(errText, theme.RoleBody, ui.ToneError, body, 0)
		errNode.Key = "store-detail-error"
		children = append(children, errNode)
	}
	if detail.Readme != "" {
		children = append(children, &ui.Node{Kind: ui.KindSeparator, Width: body})
		children = append(children, markdownNodes(detail.Readme, body, metrics)...)
	}
	header := pluginStoreDetailHeader(detail, width, metrics)
	headerH, _ := ui.ContentHeight(header, width, h.measureText())
	bodyHeight := max(h.place.Panel.H-2*pluginStorePadding-headerH-pluginStoreGap, metrics.StandardControl)
	return &ui.Node{Kind: ui.KindColumn, Gap: pluginStoreGap, Children: []*ui.Node{
		header,
		{
			Kind: ui.KindScroll, Width: width, Height: bodyHeight,
			ScrollOffset: h.pluginStoreDetailScroll, Children: []*ui.Node{{
				Kind: ui.KindColumn, Gap: pluginStoreGap, Children: children,
			}},
		},
	}}
}

// pluginStoreDetailHeader is back and the plugin's name at the start, and its
// links, primary action and close pinned to the end (DMS's detail bar).
func pluginStoreDetailHeader(detail Detail, width int, metrics theme.Metrics) *ui.Node {
	var actions []*ui.Node
	if detail.Homepage != "" {
		actions = append(actions, pluginStoreIconButton("store-open-homepage", "Open homepage", "link", metrics))
	}
	if detail.ReleaseNotes != "" {
		actions = append(actions, pluginStoreIconButton("store-open-release-notes", "Open release notes", "description", metrics))
	}
	// With no action to offer, the reason sits where the action would.
	actions = append(actions, pluginStoreDetailPrimary(detail, metrics))
	actions = append(actions, pluginStoreIconButton("store-close", "Close", "close", metrics))
	lead := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		pluginStoreIconButton("store-back", "Back to plugins", "chevron_left", metrics),
		{Kind: ui.KindIcon, Icon: detail.Glyph, IconSize: metrics.IconNormal},
		{Kind: ui.KindText, Text: detail.Title, Role: "heading", TextRole: theme.RoleTitle, MaxWidth: max(width/2, 1)}, // the actions take the other half
	}}
	return &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: width, Children: []*ui.Node{
		lead,
		{Kind: ui.KindRow, Gap: theme.MarginXS, Children: actions},
	}}
}

// pluginStoreDetailSummary is the preview beside what the plugin is: badges,
// byline, description, and its capabilities and requirements as chips.
func pluginStoreDetailSummary(r *Registry, h *PanelHost, detail Detail, width int, metrics theme.Metrics) *ui.Node {
	imageWidth := min(width*2/5, max((width-pluginStoreGap)/2, 1))
	imageHeight := imageWidth * 9 / 16
	glyph := &ui.Node{Kind: ui.KindIcon, Icon: detail.Glyph, IconSize: metrics.IconLarge, Tone: ui.ToneAccent}
	image := &ui.Node{
		Kind: ui.KindStack, Width: imageWidth, Height: imageHeight,
		Children: []*ui.Node{{Kind: ui.KindCapsule, Width: imageWidth, Height: imageHeight, Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium}},
	}
	if detail.ScreenshotPath != "" {
		img := &ui.Node{
			Kind: ui.KindImage, ImageW: imageWidth, ImageH: imageHeight,
			ImagePath: detail.ScreenshotPath, Background: true,
		}
		if r.plugins != nil && r.plugins.images != nil {
			if workerKey, ok := pluginImageKey(img); ok {
				if decoded, hit, err := r.plugins.images.Request(workerKey); err == nil && hit {
					img.Image = decoded
				}
			}
		}
		image.Children = append(image.Children, img)
		if img.Image == nil {
			image.Children = append(image.Children, glyph)
		}
	} else {
		image.Children = append(image.Children, glyph)
	}
	mediaLabel := ""
	if detail.Screenshot.SHA256 != "" {
		mediaLabel = "Preview loading…"
		if media, ok := r.pluginStoreSnapshot.Media[detail.Screenshot.SHA256]; ok {
			if media.Err != nil {
				mediaLabel = "Preview unavailable"
			} else if media.Path != "" {
				mediaLabel = ""
			}
		}
	}

	infoW := max(width-imageWidth-pluginStoreGap, 1)
	var tags []*ui.Node
	for _, badge := range detail.Badges {
		tags = append(tags, pluginStoreTag(badge.Label, pluginStoreBadgeFill(badge.Tone), infoW))
	}
	if detail.Category != "" {
		tags = append(tags, pluginStoreTag(categoryLabel(detail.Category), ui.FillContainerHighest, infoW))
	}
	if detail.InstalledVersion != "" {
		tags = append(tags, pluginStoreTag("Installed v"+detail.InstalledVersion, ui.FillSoft, infoW))
	}
	var meta []string
	if detail.Author != "" {
		meta = append(meta, "by "+detail.Author)
	}
	if detail.Version != "" {
		meta = append(meta, "v"+detail.Version)
	}
	if detail.License != "" {
		meta = append(meta, detail.License)
	}
	info := []*ui.Node{}
	if len(tags) > 0 {
		info = append(info, h.flowRows(tags, infoW, theme.MarginXS))
	}
	info = append(info, &ui.Node{Kind: ui.KindText, Text: strings.Join(meta, " · "), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: infoW})
	if detail.Description != "" {
		info = append(info, h.wrappedText(detail.Description, theme.RoleBody, ui.ToneNormal, infoW, 0))
	}
	info = append(info,
		pluginStoreChipSection(h, "Capabilities", detail.Capabilities, infoW),
		pluginStoreChipSection(h, "Requires", detail.RequiredCommands, infoW),
	)
	if commit := detail.CatalogCommit; commit != "" {
		info = append(info, &ui.Node{Kind: ui.KindText, Text: "Catalog commit " + commit[:min(len(commit), 12)], TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, Tabular: true})
	}
	if mediaLabel != "" {
		info = append(info, &ui.Node{Kind: ui.KindText, Text: mediaLabel, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	return &ui.Node{Kind: ui.KindRow, Gap: pluginStoreGap, Children: []*ui.Node{
		image,
		{Kind: ui.KindColumn, Gap: theme.MarginS, Width: infoW, Children: info},
	}}
}

// pluginStoreTag is a small capsule label. maxWidth bounds it: tags carry
// catalog data, and one long requirement must not fail the row.
func pluginStoreTag(label string, fill ui.Fill, maxWidth int) *ui.Node {
	return &ui.Node{Kind: ui.KindCapsule, Fill: fill, Shape: ui.ShapeSmall, Padding: theme.MarginXS,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption, MaxWidth: max(maxWidth-2*theme.MarginXS, 1)}}}
}

// pluginStoreChipSection is a label over its values as chips, the way DMS
// lists capabilities and dependencies, flowing onto more lines as needed.
func pluginStoreChipSection(h *PanelHost, title string, values []string, width int) *ui.Node {
	var chips []*ui.Node
	for _, v := range values {
		chips = append(chips, pluginStoreTag(v, ui.FillContainerHighest, width))
	}
	body := h.flowRows(chips, width, theme.MarginXS)
	if len(values) == 0 {
		body = &ui.Node{Kind: ui.KindText, Text: "None", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel},
		body,
	}}
}

func pluginStoreDetailPrimary(detail Detail, metrics theme.Metrics) *ui.Node {
	switch detail.Action {
	case ActionInstall:
		return pluginStoreFilledButton("store-primary", "Install", "download", ui.FillAccent, metrics)
	case ActionUpdate:
		return pluginStoreFilledButton("store-primary", "Update", "restart_alt", ui.FillAccent, metrics)
	case ActionRemove:
		return pluginStoreFilledButton("store-primary", "Remove", "delete", ui.FillErrorContainer, metrics)
	}
	text := detail.Reason
	if text == "" {
		text = "Unavailable"
	}
	return pluginStoreTag(strings.ToUpper(text[:1])+text[1:], ui.FillContainerHighest, pluginStoreMinCardWidth)
}

// pluginStoreConfirmRow is Cancel then the confirming action, at the end of
// the block the way every other confirmation in the shell reads.
func pluginStoreConfirmRow(width int, confirm *ui.Node, metrics theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: width, Height: metrics.CompactControl, Children: []*ui.Node{
		{Kind: ui.KindText, Text: ""},
		{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
			pluginStoreButton("store-cancel-confirmation", "Cancel", metrics),
			confirm,
		}},
	}}
}

func pluginStoreConsentBlock(h *PanelHost, detail Detail, width int, metrics theme.Metrics) *ui.Node {
	inner := max(width-2*metrics.CardPadding, 1)
	verb, icon := "Install", "download"
	if detail.Action == ActionUpdate {
		verb, icon = "Update", "restart_alt"
	}
	lines := []*ui.Node{{Kind: ui.KindText, Text: verb + " " + detail.Title + "?", TextRole: theme.RoleTitle}}
	for _, line := range detail.ConsentLines {
		lines = append(lines, h.wrappedText(line, theme.RoleCaption, ui.ToneNormal, inner, 0))
	}
	lines = append(lines, pluginStoreConfirmRow(inner, pluginStoreFilledButton("store-confirm", "Confirm "+verb, icon, ui.FillAccent, metrics), metrics))
	return &ui.Node{Kind: ui.KindCapsule, Width: width, Padding: metrics.CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium, Children: []*ui.Node{
		{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: lines},
	}}
}

func pluginStoreRemoveBlock(h *PanelHost, detail Detail, width int, metrics theme.Metrics) *ui.Node {
	inner := max(width-2*metrics.CardPadding, 1)
	return &ui.Node{Kind: ui.KindCapsule, Width: width, Padding: metrics.CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium, Children: []*ui.Node{
		{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Remove " + detail.Title + "?", TextRole: theme.RoleTitle},
			{Kind: ui.KindText, Text: "Its settings and enabled state are kept.", TextRole: theme.RoleCaption},
			pluginStoreConfirmRow(inner, pluginStoreFilledButton("store-confirm", "Confirm Remove", "delete", ui.FillErrorContainer, metrics), metrics),
		}},
	}}
}

func pluginStoreDetailError(h *PanelHost, listing store.Listing) string {
	if h.pluginStoreDetailErr != "" {
		return h.pluginStoreDetailErr
	}
	if listing.Err != nil {
		return listing.Err.Error()
	}
	return ""
}

func (h *PanelHost) pluginStoreBeginPrimary(r *Registry) {
	listing, ok := pluginStoreFind(r.pluginStoreSnapshot.Listings, h.pluginStoreDetail)
	if !ok {
		return
	}
	if h.pluginStoreConsent != nil && h.pluginStoreConsent.key == h.pluginStoreDetail {
		h.pluginStoreConsent = nil
	}
	h.pluginStoreDetailErr = ""
	h.pluginStorePending = nil
	enabled := slices.Contains(r.cfg.Plugins.Enabled, listing.Entry.ID)
	detail := detailFor(listing, enabled, r.pluginStoreSnapshot.Media, "")
	switch detail.Action {
	case ActionInstall, ActionUpdate:
		h.pluginStoreConsent = &pluginStorePinnedConsent{
			key: h.pluginStoreDetail, listing: listing, ref: detail.ReleaseRef,
			action: detail.Action, enabled: enabled,
		}
		h.pluginStoreRemoveConfirm = false
	case ActionRemove:
		h.pluginStoreConsent = nil
		h.pluginStoreRemoveConfirm = true
	default:
		return
	}
	r.rebuildPanel(h)
}

func (h *PanelHost) pluginStoreConfirm(r *Registry) {
	s := r.pluginStore
	if s == nil {
		h.pluginStoreDetailErr = "plugin store unavailable"
		r.rebuildPanel(h)
		return
	}
	if h.pluginStoreRemoveConfirm {
		listing, ok := pluginStoreFind(r.pluginStoreSnapshot.Listings, h.pluginStoreDetail)
		if !ok {
			return
		}
		_, err := s.Remove(listing.Entry.ID)
		if err != nil {
			h.pluginStoreDetailErr = err.Error()
		} else {
			h.pluginStoreRemoveConfirm = false
			h.pluginStoreDetailErr = ""
			h.pluginStorePending = &pluginStorePendingOp{
				key: h.pluginStoreDetail, name: listing.Entry.Name, remove: true, prevErr: listing.Err,
			}
		}
		r.rebuildPanel(h)
		return
	}
	pinned := h.pluginStoreConsent
	if pinned == nil || pinned.key != h.pluginStoreDetail {
		return
	}
	var err error
	if pinned.action == ActionInstall {
		_, err = s.Install(pinned.listing.Source, pinned.listing.Entry.ID, pinned.ref)
	} else if pinned.action == ActionUpdate {
		_, err = s.Update(pinned.listing.Entry.ID, pinned.ref, true)
	}
	switch {
	case store.KindOf(err) == store.KindConsent:
		// The catalog moved under the sheet: drop the pin so the detail
		// shows the release that is there now, and consent is asked for it.
		h.pluginStoreConsent = nil
		h.pluginStoreDetailErr = err.Error()
	case err != nil:
		h.pluginStoreDetailErr = err.Error()
	default:
		current, _ := pluginStoreFind(r.pluginStoreSnapshot.Listings, h.pluginStoreDetail)
		h.pluginStorePending = &pluginStorePendingOp{
			key: h.pluginStoreDetail, name: pinned.listing.Entry.Name, version: pinned.ref.Version,
			update: pinned.action == ActionUpdate, enabled: pinned.enabled, prevErr: current.Err,
		}
		h.pluginStoreConsent = nil
		h.pluginStoreDetailErr = ""
	}
	r.rebuildPanel(h)
}

func (h *PanelHost) pluginStoreLeaveDetail(r *Registry) {
	h.pluginStorePending = nil
	h.pluginStoreDetail = ""
	h.pluginStoreConsent = nil
	h.pluginStoreRemoveConfirm = false
	h.pluginStoreDetailErr = ""
	h.pluginStoreDetailScroll = 0
	r.rebuildPanel(h)
}

func (h *PanelHost) pluginStorePageDetail(r *Registry, up bool) {
	delta := max(h.place.Panel.H-2*pluginStorePadding, 1)
	if up {
		h.pluginStoreDetailScroll = max(h.pluginStoreDetailScroll-delta, 0)
	} else {
		h.pluginStoreDetailScroll += delta
	}
	r.rebuildPanel(h)
}

func (h *PanelHost) pluginStoreOpenLink(r *Registry, action string) {
	listing, ok := pluginStoreFind(r.pluginStoreSnapshot.Listings, h.pluginStoreDetail)
	if !ok {
		return
	}
	if pinned := h.pluginStoreConsent; pinned != nil && pinned.key == h.pluginStoreDetail {
		listing = pinned.listing
	}
	link := listing.Entry.Homepage
	if action == "store-open-release-notes" {
		release := listing.Entry.Release
		if listing.Resolution.Release != nil {
			release = *listing.Resolution.Release
		}
		link = release.ReleaseNotes
	}
	key := h.pluginStoreDetail
	go func() {
		err := openURL(link)
		if err == nil {
			return
		}
		r.mu.Lock()
		current := r.panelHosts[PanelPluginStore]
		var output uint32
		if current == h && current.pluginStoreDetail == key {
			current.pluginStoreDetailErr = err.Error()
			r.rebuildPanel(current)
			output = current.output
		}
		r.mu.Unlock()
		if output != 0 {
			r.publishSurface(output, panelSurfaceID(PanelPluginStore))
		}
	}()
}
