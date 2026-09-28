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

	children := []*ui.Node{pluginStoreDetailHeader(detail, width, metrics)}
	children = append(children, pluginStoreDetailSummary(r, detail, width, metrics)...)
	if consentOpen {
		children = append(children, pluginStoreConsentBlock(detail, width, metrics))
	} else if h.pluginStoreRemoveConfirm {
		children = append(children, pluginStoreRemoveBlock(metrics))
	}
	if errText := pluginStoreDetailError(h, listing); errText != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Key: "store-detail-error", Text: errText, TextRole: theme.RoleBody, Tone: ui.ToneError, Multiline: true})
	}
	if detail.Readme != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "README", TextRole: theme.RoleTitle})
		children = append(children, markdownNodes(detail.Readme, max(width-2*pluginStoreCardPadding, 1), metrics)...)
	} else {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: detail.Description, TextRole: theme.RoleBody, MaxWidth: width, Multiline: true})
	}
	bodyHeight := max(h.place.Panel.H-2*pluginStorePadding, metrics.StandardControl)
	return &ui.Node{
		Kind: ui.KindScroll, Width: width, Height: bodyHeight,
		ScrollOffset: h.pluginStoreDetailScroll, Children: []*ui.Node{{
			Kind: ui.KindColumn, Gap: pluginStoreGap, Children: children,
		}},
	}
}

func pluginStoreDetailHeader(detail Detail, width int, metrics theme.Metrics) *ui.Node {
	children := []*ui.Node{
		pluginStoreIconButton("store-back", "Back to plugins", "chevron_left", metrics),
		{Kind: ui.KindText, Text: detail.Title, Role: "heading", TextRole: theme.RoleTitle, MaxWidth: max(width/2, 1)}, // the actions take the other half
	}
	if detail.Homepage != "" {
		children = append(children, pluginStoreIconButton("store-open-homepage", "Open homepage", "link", metrics))
	}
	if detail.ReleaseNotes != "" {
		children = append(children, pluginStoreIconButton("store-open-release-notes", "Open release notes", "description", metrics))
	}
	if detail.Action != ActionDisabled {
		children = append(children, pluginStoreDetailPrimary(detail, metrics))
	}
	children = append(children, pluginStoreIconButton("store-close", "Close", "close", metrics))
	return &ui.Node{Kind: ui.KindRow, Height: metrics.StandardControl, Gap: theme.MarginXS, PinEnd: true, Children: children}
}

func pluginStoreDetailSummary(r *Registry, detail Detail, width int, metrics theme.Metrics) []*ui.Node {
	imageWidth := min(420, max((width-pluginStoreGap)/2, 1))
	imageHeight := imageWidth * 9 / 16
	image := &ui.Node{
		Kind: ui.KindStack, Width: imageWidth, Height: imageHeight,
		Children: []*ui.Node{{Kind: ui.KindCapsule, Width: imageWidth, Height: imageHeight, Fill: ui.FillContainer, Shape: ui.ShapeMedium}},
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
			image.Children = append(image.Children, &ui.Node{Kind: ui.KindIcon, Icon: detail.Glyph, IconSize: metrics.IconLarge})
		}
	} else {
		image.Children = append(image.Children, &ui.Node{Kind: ui.KindIcon, Icon: detail.Glyph, IconSize: metrics.IconLarge})
	}
	mediaLabel := "No screenshot provided"
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

	meta := []string{}
	if detail.Author != "" {
		meta = append(meta, detail.Author)
	}
	if detail.Version != "" {
		meta = append(meta, "v"+detail.Version)
	}
	if detail.License != "" {
		meta = append(meta, detail.License)
	}
	meta = append(meta, detail.SourceBadge)
	info := []*ui.Node{{Kind: ui.KindText, Text: strings.Join(meta, " · "), TextRole: theme.RoleCaption}}
	if detail.Category != "" {
		info = append(info, &ui.Node{Kind: ui.KindText, Text: "Category: " + categoryLabel(detail.Category), TextRole: theme.RoleCaption})
	}
	if detail.CatalogCommit != "" {
		info = append(info, &ui.Node{Kind: ui.KindText, Text: "Catalog commit: " + detail.CatalogCommit, TextRole: theme.RoleCaption})
	}
	if detail.Description != "" && detail.Readme != "" {
		info = append(info, &ui.Node{Kind: ui.KindText, Text: detail.Description, TextRole: theme.RoleBody, MaxWidth: width - imageWidth - pluginStoreGap, Multiline: true})
	}
	info = append(info,
		pluginStoreDetailSection("Capabilities", detail.Capabilities),
		pluginStoreDetailSection("Required commands", detail.RequiredCommands),
	)
	if mediaLabel != "" {
		info = append(info, &ui.Node{Kind: ui.KindText, Text: mediaLabel, TextRole: theme.RoleCaption})
	}
	return []*ui.Node{
		{Kind: ui.KindRow, Gap: pluginStoreGap, Children: []*ui.Node{
			image,
			{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: info},
		}},
	}
}

func pluginStoreDetailPrimary(detail Detail, metrics theme.Metrics) *ui.Node {
	label := ""
	switch detail.Action {
	case ActionInstall:
		label = "Install"
	case ActionUpdate:
		label = "Update"
	case ActionRemove:
		label = "Remove"
	default:
		if detail.Reason != "" {
			return &ui.Node{Kind: ui.KindText, Text: detail.Reason, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
		}
		return &ui.Node{Kind: ui.KindText, Text: "Unavailable", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
	}
	return pluginStoreButton("store-primary", label, metrics)
}

func pluginStoreConsentBlock(detail Detail, width int, metrics theme.Metrics) *ui.Node {
	lines := make([]*ui.Node, 0, len(detail.ConsentLines)+2)
	for _, line := range detail.ConsentLines {
		lines = append(lines, &ui.Node{Kind: ui.KindText, Text: line, TextRole: theme.RoleCaption, MaxWidth: max(width-2*pluginStoreCardPadding, 1), Multiline: true})
	}
	label := "Confirm Install"
	if detail.Action == ActionUpdate {
		label = "Confirm Update"
	}
	lines = append(lines,
		pluginStoreButton("store-confirm", label, metrics),
		pluginStoreButton("store-cancel-confirmation", "Cancel", metrics),
	)
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Padding: pluginStoreCardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium, Children: lines}
}

func pluginStoreRemoveBlock(metrics theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Padding: pluginStoreCardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeMedium, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Remove this plugin?", TextRole: theme.RoleBody},
		{Kind: ui.KindText, Text: "Settings are kept.", TextRole: theme.RoleCaption},
		&ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
			pluginStoreButton("store-confirm", "Confirm Remove", metrics),
			pluginStoreButton("store-cancel-confirmation", "Cancel", metrics),
		}},
	}}
}

func pluginStoreDetailSection(title string, values []string) *ui.Node {
	content := "None"
	if len(values) > 0 {
		content = strings.Join(values, ", ")
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: title, TextRole: theme.RoleTitle},
		{Kind: ui.KindText, Text: content, TextRole: theme.RoleBody, Multiline: true},
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
	if err != nil {
		h.pluginStoreDetailErr = err.Error()
	} else {
		h.pluginStoreConsent = nil
		h.pluginStoreDetailErr = ""
	}
	r.rebuildPanel(h)
}

func (h *PanelHost) pluginStoreLeaveDetail(r *Registry) {
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
