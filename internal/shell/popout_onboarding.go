package shell

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/onboarding"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// onboardingPage is the wizard's one linear flow: short steps, no
// branches. Page math is pure so a table test can pin the navigation.
type onboardingPage uint8

const (
	onbWelcome onboardingPage = iota
	onbAppearance
	onbRegion
	onbIdle
	onbPlugins
	onbReady
	onbPages
)

func (p onboardingPage) next() onboardingPage {
	if p+1 >= onbPages {
		return p
	}
	return p + 1
}

func (p onboardingPage) prev() onboardingPage {
	if p == 0 {
		return p
	}
	return p - 1
}

// onboardingCopy holds English source strings; translations live in the
// existing internal/i18n catalogs.
const (
	onbTitleWelcome    = "Welcome to sysc-shell"
	onbTitleAppearance = "Appearance"
	onbTitleRegion     = "Region and language"
	onbTitleIdle       = "When idle"
	onbTitlePlugins    = "Startup plugins"
	onbTitleReady      = "You are ready"

	onbIntroWelcome = "Set up the basics for a comfortable start. Changes apply as " +
		"you make them and stay available later in Settings."
	onbIntroAppearance = "Set the theme mode and wallpaper. Detailed palettes, " +
		"schemes and bar layout stay in Settings."
	onbIntroRegion = "Weather location and units. The interface language follows " +
		"your session locale; keyboard layouts stay with the OS."
	onbIntroIdle = "Choose the action for an idle session, or keep the current " +
		"behaviour. Timing and availability stay in Settings."
	onbIntroPlugins = "Choose installed bar plugins to enable and place on the bar. " +
		"Your choices save when you finish; install plugins later in Settings."
	onbIntroReady = "Review what applied. Settings holds every control this " +
		"wizard used, plus the rest of the shell."

	onbWallpaperHint = "Choose an image now; you can change it later in the Wallpaper panel."
	onbRegionHint    = "The interface language does not change your keyboard layout. " +
		"Set precise weather coordinates in Settings if needed."
	onbIdleHint = "Timing and availability are managed in Settings, under Session. " +
		"Unavailable actions stay disabled."
	onbIdleModeDescription = "Choose one action to run when the session is idle."
	onbCityDescription     = "Enter a city or town for local weather."
	onbLanguageDescription = "Choose the interface language; your keyboard layout stays with the OS."
	onbEntryHint           = "Settings holds every control used here, beside the rest of " +
		"the shell: launcher, notifications, clipboard and tray panels sit in " +
		"the bar."

	onbBackLabel       = "Back"
	onbNextLabel       = "Next"
	onbFinishLabel     = "Finish"
	onbSkipLabel       = "Not now"
	onbWallpaperButton = "Choose wallpaper"
)

func onbMainLabel(p onboardingPage) string {
	if p == onbReady {
		return onbFinishLabel
	}
	return onbNextLabel
}

var onbTitles = [onbPages]string{
	onbTitleWelcome, onbTitleAppearance, onbTitleRegion, onbTitleIdle, onbTitlePlugins, onbTitleReady,
}

var onbTitleKeys = [onbPages]string{
	"onboarding.title.welcome", "onboarding.title.appearance", "onboarding.title.region",
	"onboarding.title.idle", "onboarding.title.plugins", "onboarding.title.ready",
}

var onbIntros = [onbPages]string{
	onbIntroWelcome, onbIntroAppearance, onbIntroRegion, onbIntroIdle, onbIntroPlugins, onbIntroReady,
}

var onbIntroKeys = [onbPages]string{
	"onboarding.intro.welcome", "onboarding.intro.appearance", "onboarding.intro.region",
	"onboarding.intro.idle", "onboarding.intro.plugins", "onboarding.intro.ready",
}

// onboardingSyscWordmarkRows is the desktop's SYSC.txt art, rendered like
// the Moonbit plugin's mono wordmark. The source's leading tab is expanded to
// eight spaces so the tagline keeps its terminal alignment in shaped text.
var onboardingSyscWordmarkRows = padOnboardingRows(
	`  ██████████████░████░       ████░   ██████████████░  ██████████████░`,
	`████████████████░████░       ████░ ████████████████░████████████████░`,
	`████░            ██████░   ██████░ ████░            ████░`,
	`██████████████░    ████████████░   ██████████████░  ████░`,
	`  ██████████████░    ████████░       ██████████████░████░`,
	`            ████░      ████░                   ████░████░`,
	`████████████████░      ████░       ████████████████░████████████████░`,
	`██████████████░        ████░       ██████████████░    ██████████████░`,
	`        //////////////SEE YOU IN SPACE COWBOY//////////`,
)

func padOnboardingRows(rows ...string) []string {
	width := 0
	for _, row := range rows {
		width = max(width, utf8.RuneCountInString(row))
	}
	for i, row := range rows {
		rows[i] += strings.Repeat(" ", width-utf8.RuneCountInString(row))
	}
	return rows
}

func onboardingSyscWordmark(h *PanelHost) *ui.Node {
	rows := onboardingSyscWordmarkRows
	if onbBodyWidth(h) < onbStepRailMinWidth {
		rows = []string{"SYSC"}
	}
	children := make([]*ui.Node, 0, len(rows))
	for _, row := range rows {
		children = append(children, &ui.Node{
			Kind: ui.KindText, Text: row, TextRole: theme.RoleMono,
			Tone: ui.ToneAccent, CenterX: true,
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Key: "onb-ascii-wordmark", Name: "SYSC", Role: "img", Children: children}
}

// onbBodyKey names the wizard's one scroll so its offset can be restored
// across rebuilds, the way the Settings body does.
const onbBodyKey = "onboarding-body"

const onbStepRailMinWidth = 900
const onbStepRailWidth = 240
const onbCardViewportFillPercent = 75

var onbStepKeys = [onbPages]string{
	"onb-step-welcome", "onb-step-appearance", "onb-step-region", "onb-step-idle", "onb-step-plugins", "onb-step-ready",
}

func onboardingTree(r *Registry, h *PanelHost) *ui.Node {
	h.onboardingTreeScale = h.scale120
	m := h.theme.Metrics
	step := &ui.Node{
		Key:      "onb-progress-label",
		Kind:     ui.KindText,
		Text:     fmt.Sprintf("// %02d / %02d", int(h.onbPage)+1, int(onbPages)),
		Name:     onbText(h, "progress_name", "Setup progress"),
		Role:     "status",
		TextRole: theme.RoleMono,
		Tone:     ui.ToneAccent,
		CenterX:  true,
	}
	brand := onboardingSyscWordmark(h)
	brandPill := &ui.Node{
		Kind: ui.KindCapsule, Key: "onb-brand-pill", Padding: m.PanelPadding,
		PaddingX: m.PanelPadding + theme.MarginS, CenterX: true,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeStadium, Stroke: 1, // token-exempt: a hairline border, not a ladder value
		StrokeFill: ui.FillOutline,
		Children:   []*ui.Node{brand},
	}
	setupLabel := &ui.Node{
		Kind: ui.KindText, Key: "onb-setup-label", Text: onbText(h, "label", "SETUP"),
		Name: "Setup", Role: "heading", TextRole: theme.RoleTitle,
		Tone: ui.ToneAccent, CenterX: true,
	}
	head := &ui.Node{
		Kind: ui.KindColumn, Key: "onb-header", Gap: theme.MarginS, Padding: m.PanelPadding,
		Children: []*ui.Node{step, brandPill, setupLabel},
	}
	moveButton := h.button("onb-move", onbText(h, "action.move", "Move window"), "drag_indicator")
	moveButton.Fill = ui.FillOutline
	moveButton.Shape = ui.ShapeStadium
	moveHintText := onbText(h, "move_hint", "SUPER + LEFT-DRAG TO MOVE")
	if h.onbMoveMode {
		moveHintText = onbText(h, "move_mode_hint", "Use arrows or click a position. Enter or Escape exits move mode.")
	}
	moveHint := &ui.Node{
		Kind: ui.KindText, Key: "onb-move-hint", Text: moveHintText, Role: "status",
		TextRole: theme.RoleMono, Tone: ui.ToneSubtle,
	}
	moveControls := &ui.Node{Kind: ui.KindRow, Key: "onb-move-controls", Gap: theme.MarginS, Children: []*ui.Node{moveButton, moveHint}}
	navigation := onbActions(r, h)
	footer := &ui.Node{Kind: ui.KindRow, Key: "onb-footer", Gap: theme.MarginXL, PinEnd: true, Children: []*ui.Node{
		moveControls, navigation,
	}}
	if onbBodyWidth(h) < onbStepRailMinWidth {
		footer = &ui.Node{Kind: ui.KindColumn, Key: "onb-footer", Gap: theme.MarginXS, Children: []*ui.Node{
			moveControls, navigation,
		}}
	}
	stageChildren := append([]*ui.Node{{
		Kind: ui.KindText, Text: onbTitle(h, h.onbPage), Name: onbTitle(h, h.onbPage),
		Role: "heading", TextRole: theme.RoleTitle,
	}}, onboardingBody(r, h)...)
	stage := &ui.Node{
		Kind: ui.KindColumn, Key: "onb-stage", Width: onbContentWidth(h),
		Gap: theme.MarginM, CenterY: true, Children: stageChildren,
	}
	main := &ui.Node{
		Kind: ui.KindColumn, Key: "onb-content-main", Width: onbContentWidth(h),
		Children: []*ui.Node{stage},
	}
	rowChildren := []*ui.Node{main}
	withRail := onbBodyWidth(h) >= onbStepRailMinWidth
	if withRail {
		rowChildren = []*ui.Node{onbStepRail(h), main}
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, PinEnd: withRail, Children: rowChildren}
	card := &ui.Node{
		Kind: ui.KindCapsule, Key: "onb-content-card", Width: onbBodyWidth(h),
		Padding: m.CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Stroke: 1, StrokeFill: ui.FillOutlineVariant, // token-exempt: a hairline border, not a ladder value
		Children: []*ui.Node{row},
	}
	frame := &ui.Node{
		Kind: ui.KindColumn, Key: "onb-content-frame", Width: onbBodyWidth(h),
		Children: []*ui.Node{card},
	}
	body := &ui.Node{
		Kind:         ui.KindScroll,
		Key:          onbBodyKey,
		Width:        onbBodyWidth(h),
		ScrollOffset: h.onbScroll,
		Children:     []*ui.Node{frame},
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: m.PanelPadding, Children: []*ui.Node{
		head, body, footer,
	}}
}

func onbStepRail(h *PanelHost) *ui.Node {
	m := h.metrics()
	items := make([]*ui.Node, 0, onbPages)
	for page := onboardingPage(0); page < onbPages; page++ {
		number := &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%02d", int(page)+1), TextRole: theme.RoleMono}
		item := &ui.Node{
			Kind: ui.KindCapsule, Key: onbStepKeys[page], Width: onbStepRailWidth,
			Height: m.StandardControl, Padding: m.ButtonPadding,
			Fill: ui.FillOutline, Shape: ui.ShapeMedium,
			Stroke: 1, StrokeFill: ui.FillOutlineVariant, // token-exempt: a hairline border, not a ladder value
			Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
				number,
				{Kind: ui.KindText, Text: onbTitle(h, page), Name: onbTitle(h, page)},
			}}},
		}
		if page == h.onbPage {
			item.Fill = ui.FillAccent
		}
		items = append(items, item)
	}
	return &ui.Node{Kind: ui.KindColumn, Width: onbStepRailWidth, Children: []*ui.Node{{
		Kind: ui.KindColumn, CenterY: true, Gap: theme.MarginS, Children: items,
	}}}
}

// onboardingBody holds the page content. Every control is the Settings
// panel's own entry row, so the wizard writes through the same validated
// setters and never grows a second settings model.
func onboardingBody(r *Registry, h *PanelHost) []*ui.Node {
	intro := h.wrappedText(onbIntro(h, h.onbPage), theme.RoleBody, ui.ToneNormal, onbContentWidth(h), 0)
	intro.Key = "onb-intro"
	children := []*ui.Node{intro}
	switch h.onbPage {
	case onbWelcome:
		children = append(children, onbSummary(r, h)...)
		children = append(children, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{onbDocsButton(h)}})
	case onbAppearance:
		children = append(children, onbRows(r, h, "appearance.mode")...)
		children = append(children, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
			onbActionButton(h, "onb-wallpaper", "action.wallpaper", onbWallpaperButton, "wallpaper"),
		}})
		children = append(children, onbCaption(h, onbText(h, "hint.wallpaper", onbWallpaperHint)))
	case onbRegion:
		children = append(children, onbRows(r, h, "session.language")...)
		children = append(children, onboardingFontOption(h)...)
		children = append(children, onbRows(r, h, "weather.city", "weather.unit")...)
		children = append(children, onbCaption(h, onbText(h, "hint.region", onbRegionHint)))
	case onbIdle:
		rows := onbRows(r, h, "idle.after")
		body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: rows}
		settingsDimIdle(r, h, body)
		children = append(children, body)
		children = append(children, onbCaption(h, onbText(h, "hint.idle", onbIdleHint)))
	case onbPlugins:
		children = append(children, onbPluginRows(r, h)...)
	case onbReady:
		children = append(children, onbSummary(r, h)...)
		children = append(children, onbCaption(h, onbText(h, "hint.ready", onbEntryHint)))
		children = append(children,
			&ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
				onbActionButton(h, "onb-settings", "action.settings", "Open Settings", "settings"),
				onbActionButton(h, "onb-launcher", "action.launcher", "Open launcher", "apps"),
			}},
			&ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
				onbActionButton(h, "onb-wallpaper", "action.wallpaper", onbWallpaperButton, "wallpaper"),
				onbDocsButton(h),
			}},
		)
	}
	return children
}

func onbPluginRows(r *Registry, h *PanelHost) []*ui.Node {
	candidates := onbPluginCandidates(r)
	if len(candidates) == 0 {
		return []*ui.Node{onbCaption(h, onbText(h, "plugins.empty", "No installed bar plugins. Install plugins later in Settings."))}
	}
	view, err := onbPluginSelectionConfig(h.draft, candidates, h.onbPluginSelections)
	if err != nil {
		view = h.draft
	}
	m := h.metrics()
	innerWidth := max(onbContentWidth(h)-2*m.CardPadding, 0)
	var rows []*ui.Node
	for i, candidate := range candidates {
		if i > 0 {
			rows = append(rows, &ui.Node{Kind: ui.KindSeparator, Width: innerWidth})
		}
		manifest := candidate.Manifest
		name := manifest.Name
		if name == "" {
			name = manifest.ID
		}
		widget := manifest.Widgets[0]
		widgetLabel := widget.Label
		if widgetLabel == "" {
			widgetLabel = widget.ID
		}
		enabled := slices.Contains(view.Plugins.Enabled, manifest.ID)
		placed := onbConfigHasPluginPlacement(view, manifest.ID)
		value := 0.0
		if enabled && placed {
			value = 1
		}
		state := onbText(h, "plugins.disabled", "Disabled")
		if enabled {
			state = onbText(h, "plugins.enabled", "Enabled")
		}
		placement := onbText(h, "plugins.not_on_bar", "Not on bar")
		if placed {
			placement = onbText(h, "plugins.on_bar", "On bar")
		}
		toggle := &ui.Node{
			Kind: ui.KindToggle, Action: "onb-plugin:" + manifest.ID, Value: value,
			Name: fmt.Sprintf(onbText(h, "plugins.toggle", "Place %s on the bar"), name),
			Role: "switch", Focusable: true, Height: m.StandardControl,
		}
		toggleWidth, _, err := ui.Measure(toggle, h.measureText())
		if err != nil {
			toggleWidth = m.StandardControl
		}
		labelWidth := max(innerWidth-toggleWidth-theme.MarginL, 0)
		label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelWidth, Children: []*ui.Node{
			h.wrappedText(name, theme.RoleLabel, ui.ToneNormal, labelWidth, 0),
			h.wrappedText(fmt.Sprintf(onbText(h, "plugins.widget", "Bar widget: %s"), widgetLabel), theme.RoleCaption, ui.ToneSubtle, labelWidth, 0),
			h.wrappedText(state+" · "+placement, theme.RoleCaption, ui.ToneSubtle, labelWidth, 0),
		}}
		rows = append(rows, &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: innerWidth, Gap: theme.MarginL, Children: []*ui.Node{label, toggle}})
	}
	return []*ui.Node{{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
		Shape: ui.ShapeCard, Width: onbContentWidth(h), Children: []*ui.Node{{
			Kind: ui.KindColumn, Gap: theme.MarginM, Children: rows,
		}},
	}}
}

func onbPluginCandidates(r *Registry) []plugin.Candidate {
	if r == nil || r.plugins == nil {
		return nil
	}
	var out []plugin.Candidate
	for _, candidate := range r.plugins.discovered().Plugins {
		if candidate.Startable() && len(candidate.Manifest.Widgets) > 0 {
			out = append(out, candidate)
		}
	}
	return out
}

func onbPluginSelectionConfig(cfg config.Config, candidates []plugin.Candidate, selections map[string]bool) (config.Config, error) {
	out := cfg.Clone()
	for _, candidate := range candidates {
		selected, changed := selections[candidate.Manifest.ID]
		if !changed {
			continue
		}
		next, err := onbSetPluginSelected(out, candidate.Manifest, selected)
		if err != nil {
			return config.Config{}, err
		}
		out = next
	}
	return out, nil
}

func cloneOnboardingPluginSelections(selections map[string]bool) map[string]bool {
	if len(selections) == 0 {
		return nil
	}
	cloned := make(map[string]bool, len(selections))
	for id, selected := range selections {
		cloned[id] = selected
	}
	return cloned
}

func onbConfigHasPluginPlacement(cfg config.Config, pluginID string) bool {
	if onbBarHasPluginPlacement(cfg.Bar, pluginID) {
		return true
	}
	for _, output := range cfg.Outputs {
		if onbBarHasPluginPlacement(output.Bar, pluginID) {
			return true
		}
	}
	return false
}

func onbBarHasPluginPlacement(bar config.Bar, pluginID string) bool {
	return onbBarContains(bar, func(item config.Item) bool {
		return item.ID == "plugin" && item.Plugin == pluginID
	})
}

func onbBarHasWeatherPlacement(bar config.Bar) bool {
	return onbBarContains(bar, func(item config.Item) bool { return item.ID == "weather" })
}

func onbBarContains(bar config.Bar, match func(config.Item) bool) bool {
	var has func([]config.Item) bool
	has = func(items []config.Item) bool {
		for _, item := range items {
			if match(item) || has(item.Items) {
				return true
			}
		}
		return false
	}
	return has(bar.Left) || has(bar.Center) || has(bar.Right)
}

func onbEnsureWeatherPlacement(cfg *config.Config) error {
	ensure := func(bar *config.Bar) error {
		if onbBarHasWeatherPlacement(*bar) {
			return nil
		}
		items, err := config.InsertItem(bar.Right, len(bar.Right), config.Item{ID: "weather"})
		if err != nil {
			return err
		}
		bar.Right = items
		return nil
	}
	if err := ensure(&cfg.Bar); err != nil {
		return err
	}
	for i := range cfg.Outputs {
		if err := ensure(&cfg.Outputs[i].Bar); err != nil {
			return err
		}
	}
	return nil
}

func onbSetPluginSelected(cfg config.Config, manifest plugin.Manifest, selected bool) (config.Config, error) {
	if manifest.ID == "" || len(manifest.Widgets) == 0 || manifest.Widgets[0].ID == "" {
		return config.Config{}, fmt.Errorf("plugin has no usable bar widget")
	}
	out := cfg.Clone()
	out.Plugins = out.Plugins.Clone()
	if selected {
		if !slices.Contains(out.Plugins.Enabled, manifest.ID) {
			out.Plugins.Enabled = append(out.Plugins.Enabled, manifest.ID)
		}
		minter := config.NewMinter(out)
		ensurePlacement := func(bar *config.Bar) error {
			if onbBarHasPluginPlacement(*bar, manifest.ID) {
				return nil
			}
			item := config.Item{ID: "plugin", Plugin: manifest.ID, Entry: manifest.Widgets[0].ID}
			minter.Ensure(&item)
			items, err := config.InsertItem(bar.Right, len(bar.Right), item)
			if err != nil {
				return err
			}
			bar.Right = items
			return nil
		}
		if err := ensurePlacement(&out.Bar); err != nil {
			return config.Config{}, err
		}
		for i := range out.Outputs {
			if err := ensurePlacement(&out.Outputs[i].Bar); err != nil {
				return config.Config{}, err
			}
		}
		return out, nil
	}
	out.Plugins.Enabled = slices.DeleteFunc(out.Plugins.Enabled, func(id string) bool { return id == manifest.ID })
	remove := func(bar *config.Bar) {
		bar.Left = onbRemovePluginItems(bar.Left, manifest.ID)
		bar.Center = onbRemovePluginItems(bar.Center, manifest.ID)
		bar.Right = onbRemovePluginItems(bar.Right, manifest.ID)
	}
	remove(&out.Bar)
	for i := range out.Outputs {
		remove(&out.Outputs[i].Bar)
	}
	return out, nil
}

func onbRemovePluginItems(items []config.Item, pluginID string) []config.Item {
	out := make([]config.Item, 0, len(items))
	for _, item := range items {
		if item.ID == "plugin" && item.Plugin == pluginID {
			continue
		}
		if item.ID == "group" {
			item.Items = onbRemovePluginItems(item.Items, pluginID)
			if len(item.Items) == 0 {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func onboardingFontOption(h *PanelHost) []*ui.Node {
	copy := onboardingFontCopy(h, h.onbFontLocale, h.onbFontState)
	if copy == "" {
		return nil
	}
	out := []*ui.Node{onbCaption(h, copy)}
	if h.onbFontState == onboardingFontMissing || h.onbFontState == onboardingFontFailed {
		button := h.button("onb-font-install", onbText(h, "font.install", "Install Noto CJK"), "download")
		button.Shape = ui.ShapeStadium
		out = append(out, button)
	}
	return out
}

// onbRows renders the named settings entries as Settings renders them. An
// unknown path is a programmer error, not a user one, so it simply drops.
func onbRows(r *Registry, h *PanelHost, paths ...string) []*ui.Node {
	var out []*ui.Node
	width := onbContentWidth(h)
	var entries []settings.Entry
	for _, p := range paths {
		e := h.set.ByPath(p)
		if e != nil {
			entries = append(entries, *e)
		}
	}
	entries = overlayIdleGets(r, h.draft.Session.Locker, entries)
	if h.id == PanelOnboarding {
		for i := range entries {
			switch entries[i].Path {
			case "idle.after":
				entries[i].Describe = onbText(h, "idle.mode_description", onbIdleModeDescription)
			case "weather.city":
				entries[i].Describe = onbText(h, "region.city_description", onbCityDescription)
			case "session.language":
				entries[i].Describe = onbText(h, "region.language_description", onbLanguageDescription)
			}
		}
	}
	for _, e := range entries {
		out = append(out, settingsEntryRow(h, e, width))
	}
	return out
}

// onbSummary states the current choices as plain lines; visiting a page
// writes nothing, so the wizard can always open.
func onbSummary(r *Registry, h *PanelHost) []*ui.Node {
	mode := settings.WhenIdleMode(h.draft.Idle.Lock, r.wallsSnapshot.EnabledAtLogin())
	mode = onbIdleMode(h, mode)
	weather := strings.TrimSpace(h.draft.Weather.City)
	if weather == "" {
		if h.draft.Weather.Configured {
			weather = onbText(h, "summary.coordinates_set", "coordinates set")
		} else {
			weather = onbText(h, "summary.not_set", "not set")
		}
	}
	lines := []string{
		fmt.Sprintf(onbText(h, "interface_language", "Interface language: %s"), onboardingLocale(h)),
		fmt.Sprintf(onbText(h, "summary.theme", "Theme: %s, %s"), onbGet(h, "appearance.mode"), onbGet(h, "appearance.source")),
		fmt.Sprintf(onbText(h, "summary.weather", "Weather location: %s"), weather),
		fmt.Sprintf(onbText(h, "summary.idle", "Idle: %s"), mode),
		onbPluginSummary(r, h),
	}
	out := make([]*ui.Node, 0, len(lines))
	for _, l := range lines {
		out = append(out, h.wrappedText(l, theme.RoleCaption, ui.ToneNormal, onbContentWidth(h), 0))
	}
	return out
}

func onbPluginSummary(r *Registry, h *PanelHost) string {
	candidates := onbPluginCandidates(r)
	view, err := onbPluginSelectionConfig(h.draft, candidates, h.onbPluginSelections)
	if err != nil {
		view = h.draft
	}
	var names []string
	for _, candidate := range candidates {
		if !slices.Contains(view.Plugins.Enabled, candidate.Manifest.ID) || !onbConfigHasPluginPlacement(view, candidate.Manifest.ID) {
			continue
		}
		name := candidate.Manifest.Name
		if name == "" {
			name = candidate.Manifest.ID
		}
		names = append(names, name)
	}
	selected := onbText(h, "summary.plugins_none", "none")
	if len(names) > 0 {
		selected = strings.Join(names, ", ")
	}
	return fmt.Sprintf(onbText(h, "summary.plugins", "Startup plugins: %s"), selected)
}

func onbGet(h *PanelHost, path string) string {
	if e := h.set.ByPath(path); e != nil {
		if v := strings.TrimSpace(e.Get(h.draft)); v != "" {
			for i, option := range e.Options {
				if option == v {
					return settingsEntryOptionLabel(*e, i)
				}
			}
			return v
		}
	}
	return onbText(h, "summary.default", "default")
}

func onbCaption(h *PanelHost, text string) *ui.Node {
	return h.wrappedText(text, theme.RoleCaption, ui.ToneSubtle, onbContentWidth(h), 0)
}

// onboardingLocale shows the configured interface language, or the language
// selected by the shared system-locale resolver when the preference follows
// the session.
func onboardingLocale(h *PanelHost) string {
	code := i18n.Match(h.draft.Session.Language)
	if code == "" {
		code = i18n.Resolve("")
	}
	name := i18n.Native[code]
	if name == "" {
		name = code
	}
	if h.draft.Session.Language == "" {
		return fmt.Sprintf(onbText(h, "language.follow_system", "Follow system (%s)"), name)
	}
	return name
}

// onbActions keeps one primary action per step (Next/Finish, default fill);
// Back and Not now use the existing outlined button style.
func onbActions(r *Registry, h *PanelHost) *ui.Node {
	back := h.button("onb-back", onbText(h, "action.back", onbBackLabel), "")
	back.Fill = ui.FillOutline
	back.Shape = ui.ShapeStadium
	if h.onbPage == onbWelcome {
		back.State |= ui.StateDisabled
		back.AriaDisabled = true
	}
	main := h.button("onb-next", onbText(h, "action.next", onbNextLabel), "chevron_right")
	main.Shape = ui.ShapeStadium
	main.Fill = ui.FillAccent
	if h.onbPage == onbReady {
		main = h.button("onb-finish", onbText(h, "action.finish", onbFinishLabel), "check")
		main.Shape = ui.ShapeStadium
		main.Fill = ui.FillAccent
		if r.idleApplying {
			main.State |= ui.StateDisabled
			main.AriaDisabled = true
		}
	}
	skip := h.button("onb-skip", onbText(h, "action.not_now", onbSkipLabel), "")
	skip.Fill = ui.FillOutlineVariant
	skip.Shape = ui.ShapeStadium
	skip.Stroke = 1
	skip.StrokeFill = ui.FillAccent
	navigation := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		skip,
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{back, main}},
	}}
	if h.errLabel == "" {
		return navigation
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		{Kind: ui.KindText, Key: "onb-error", Text: h.errLabel, Tone: ui.ToneError},
		navigation,
	}}
}

func onbActionButton(h *PanelHost, action, key, fallback, icon string) *ui.Node {
	button := h.button(action, onbText(h, key, fallback), icon)
	button.Fill = ui.FillOutline
	button.Shape = ui.ShapeStadium
	return button
}

func onbDocsButton(h *PanelHost) *ui.Node {
	return onbActionButton(h, "onb-docs", "action.docs", "Read the documentation", "link")
}

func onbText(h *PanelHost, key, english string) string {
	return h.tr("onboarding."+key, english)
}

func onbTitle(h *PanelHost, page onboardingPage) string {
	return h.tr(onbTitleKeys[page], onbTitles[page])
}

func onbIntro(h *PanelHost, page onboardingPage) string {
	return h.tr(onbIntroKeys[page], onbIntros[page])
}

// moveOnboardingTo places the window with its full frame inside the output.
// Registry.mu is held by the event handler.
func (h *PanelHost) moveOnboardingTo(r *Registry, x, y int) {
	next := h.rect
	next.X = clampAxis(x, next.W, h.place.Output.W, h.place.Padding)
	next.Y = clampAxis(y, next.H, h.place.Output.H, h.place.Padding)
	r.updatePanelPlacementLocked(h, next)
}

func onbMainLabelFor(h *PanelHost, page onboardingPage) string {
	if page == onbReady {
		return onbText(h, "action.finish", onbFinishLabel)
	}
	return onbText(h, "action.next", onbNextLabel)
}

func onbIdleMode(h *PanelHost, mode string) string {
	key, english := "summary.idle_nothing", "Nothing"
	switch mode {
	case "screensaver":
		key, english = "summary.idle_screensaver", "Screensaver"
	case "lock":
		key, english = "summary.idle_lock", "Lock the session"
	}
	return onbText(h, key, english)
}

func (h *PanelHost) button(action, label, icon string) *ui.Node {
	m := h.metrics()
	node := &ui.Node{
		Kind: ui.KindButton, Action: action,
		Name: label, Role: "button", Focusable: true,
		Gap: m.ButtonPadding / 2, Padding: m.ButtonPadding,
		Height: m.StandardControl,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: label},
		},
	}
	if icon != "" {
		node.Children = append([]*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconNormal}}, node.Children...)
	}
	return node
}

func onbBodyWidth(h *PanelHost) int {
	w := panelTargetSize(PanelOnboarding).W
	if h.place.Panel.W > 0 {
		w = h.place.Panel.W
	}
	return max(w-2*h.metrics().PanelPadding, 0)
}

func onbContentWidth(h *PanelHost) int {
	width := max(onbBodyWidth(h)-2*h.metrics().CardPadding, 0)
	if onbBodyWidth(h) >= onbStepRailMinWidth {
		width = max(width-onbStepRailWidth-theme.MarginL, 0)
	}
	return width
}

// finishOnboardingLocked persists staged selections and records completion.
// Callers hold r.mu and keep the panel open when it returns an error.
func (r *Registry) finishOnboardingLocked(h *PanelHost) error {
	if r.idleApplying {
		return fmt.Errorf("%s", onbText(h, "error.idle_applying", "Wait for the idle setting to finish applying."))
	}
	if h.writeTimer != nil {
		h.writeTimer.Stop()
		h.writeTimer = nil
	}
	draft, err := onbPluginSelectionConfig(h.draft, onbPluginCandidates(r), h.onbPluginSelections)
	if err != nil {
		return err
	}
	if strings.TrimSpace(draft.Weather.City) != "" {
		if err := onbEnsureWeatherPlacement(&draft); err != nil {
			return err
		}
	}
	if err := r.writeConfig(draft); err != nil {
		return err
	}
	return r.markOnboardingOutcomeLocked(onboarding.OutcomeCompleted)
}

// onboardingScrollOffset reads the body scroll back out of the built tree so
// the next rebuild restores the position.
func onboardingScrollOffset(root *ui.Node) int {
	var out int
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || out != 0 {
			return
		}
		if n.Kind == ui.KindScroll && n.Key == onbBodyKey {
			out = n.ScrollOffset
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// activateOnboarding claims the wizard's own actions and returns false for
// everything else, so pick:, set: and browse: keep the shared handlers.
func (h *PanelHost) activateOnboarding(r *Registry, n *ui.Node) bool {
	if id, ok := strings.CutPrefix(n.Action, "onb-plugin:"); ok {
		candidates := onbPluginCandidates(r)
		for _, candidate := range candidates {
			if candidate.Manifest.ID != id {
				continue
			}
			selections := cloneOnboardingPluginSelections(h.onbPluginSelections)
			if selections == nil {
				selections = make(map[string]bool)
			}
			selections[id] = n.Value == 0
			_, err := onbPluginSelectionConfig(h.draft, candidates, selections)
			if err != nil {
				h.errLabel = err.Error()
			} else {
				h.onbPluginSelections = selections
				h.errLabel = ""
			}
			r.rebuildPanel(h)
			return true
		}
		return false
	}
	switch n.Action {
	case "onb-move":
		h.onbMoveMode = !h.onbMoveMode
		r.rebuildPanel(h)
		return true
	case "onb-back":
		h.onbPage = h.onbPage.prev()
		h.onbScroll = 0
		r.rebuildPanel(h)
		h.focusByName(onbMainLabelFor(h, h.onbPage))
		return true
	case "onb-next":
		h.onbPage = h.onbPage.next()
		h.onbScroll = 0
		if h.onbPage == onbRegion {
			r.checkOnboardingFont(h)
		}
		r.rebuildPanel(h)
		h.focusByName(onbMainLabelFor(h, h.onbPage))
		return true
	case "onb-font-install":
		r.installOnboardingFont(h)
		return true
	case "onb-wallpaper":
		// Reuse the picker rather than a second wallpaper UI: it opens on top
		// of the wizard, and the shield closes the picker first.
		if err := r.openPanelLocked(PanelWallpaper, h.output, Trigger{}); err != nil {
			log.Printf("shell: onboarding wallpaper picker: %v", err)
		}
		return true
	case "onb-skip":
		if strings.TrimSpace(h.draft.Weather.City) != "" {
			if err := onbEnsureWeatherPlacement(&h.draft); err != nil {
				h.errLabel = err.Error()
				r.rebuildPanel(h)
				return true
			}
		}
		if err := r.markOnboardingOutcomeLocked(onboarding.OutcomeDismissed); err != nil {
			log.Printf("shell: dismiss onboarding: %v", err)
		}
		r.closePanelLocked(h.id)
		return true
	case "onb-finish":
		if err := r.finishOnboardingLocked(h); err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return true
		}
		r.closePanelLocked(h.id)
		return true
	case "onb-settings", "onb-launcher":
		if err := r.finishOnboardingLocked(h); err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return true
		}
		id := PanelSettings
		if n.Action == "onb-launcher" {
			id = PanelLauncher
		}
		output := h.output
		r.closePanelLocked(h.id)
		if err := r.openPanelLocked(id, output, Trigger{}); err != nil {
			log.Printf("shell: open %s from onboarding: %v", id, err)
		}
		return true
	case "onb-docs":
		go func() {
			err := openURL.Get()("https://nomadcxx.github.io/sysc/docs/")
			if err == nil {
				return
			}
			r.mu.Lock()
			if r.panelHosts[PanelOnboarding] != h {
				r.mu.Unlock()
				return
			}
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			output := h.output
			r.mu.Unlock()
			r.publishSurface(output, panelSurfaceID(PanelOnboarding))
		}()
		return true
	}
	return false
}

// onboardingReopenCard is the Settings → Session action that shows the
// wizard again with current values. It never clears the lifecycle marker:
// reopening is not a reset.
func onboardingReopenCard(h *PanelHost) *ui.Node {
	m := h.metrics()
	label := onbText(h, "action.reopen", "Run setup again")
	return &ui.Node{
		Kind: ui.KindButton, Action: "onboarding:open",
		Name: label, Role: "button", Focusable: true,
		Width: settingsBodyWidth(h), Height: m.StandardControl,
		Gap: m.ButtonPadding / 2, Padding: m.ButtonPadding,
		Fill:     ui.FillOutline,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
}
