package shell

import (
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/onboarding"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/render"
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
	onbIntroRegion = "Set the weather location and units, and choose the interface " +
		"language. Keyboard layouts stay with the OS."
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
	onbStartLabel      = "Get started"
	onbNextLabel       = "Continue"
	onbFinishLabel     = "Finish"
	onbSkipLabel       = "Not now"
	onbWallpaperButton = "Choose wallpaper"
)

func onbMainLabel(p onboardingPage) string {
	switch p {
	case onbWelcome:
		return onbStartLabel
	case onbReady:
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

// onbBodyKey names the wizard's one scroll so its offset can be restored
// across rebuilds, the way the Settings body does.
const onbBodyKey = "onboarding-body"

// onbStepRailMinWidth is the frame width below which the rail folds into a
// header row.
const onbStepRailMinWidth = 900

// onbRailWidth is the brand rail's logical width beside the step content.
const onbRailWidth = 280

func onbWide(h *PanelHost) bool { return onbBodyWidth(h) >= onbStepRailMinWidth }

// onboardingTree is the wizard frame: the brand rail beside the step content,
// or below onbStepRailMinWidth a header row above it. onbFitFrame sizes the
// stretchy tracks once the frame height is known.
func onboardingTree(r *Registry, h *PanelHost) *ui.Node {
	h.onboardingTreeScale = h.scale120
	m := h.theme.Metrics
	content := onbContent(r, h)
	if !onbWide(h) {
		children := []*ui.Node{onbHeader(h)}
		if h.onbMoveMode {
			children = append(children, onbMoveHint(h, onbBodyWidth(h)))
		}
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: m.PanelPadding, Children: append(children, content)}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: m.PanelPadding, Padding: m.PanelPadding, Children: []*ui.Node{
		onbRail(r, h), content,
	}}
}

func onbRail(r *Registry, h *PanelHost) *ui.Node {
	return &ui.Node{
		Kind: ui.KindCapsule, Key: "onb-rail", Width: onbRailWidth, Padding: theme.MarginXL,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Stroke: 1, StrokeFill: ui.FillOutlineVariant, // token-exempt: a hairline border, not a ladder value
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXL, Children: []*ui.Node{
			onboardingWordmark(h, onbWordmarkW, onbWordmarkH),
			onbStepper(r, h),
			onbRailFoot(h),
		}}},
	}
}

func onbRailWidthInner() int { return onbRailWidth - 2*theme.MarginXL }

// onbStepLabels name the steps in the rail; the step titles in the content
// column are longer. Three labels are the titles themselves.
var onbStepLabels = [onbPages]string{"Welcome", onbTitleAppearance, onbTitleRegion, onbTitleIdle, "Plugins", "Review"}

var onbStepLabelKeys = [onbPages]string{
	"onboarding.step.welcome", "onboarding.title.appearance", "onboarding.title.region",
	"onboarding.title.idle", "onboarding.step.plugins", "onboarding.step.review",
}

func onbStepLabel(h *PanelHost, page onboardingPage) string {
	return h.tr(onbStepLabelKeys[page], onbStepLabels[page])
}

const (
	onbStepMarker = 26 // token-exempt: the stepper disc, sized to the prototype
	onbStepLine   = 2  // token-exempt: the connector between discs
	onbStepRow    = 52 // token-exempt: a disc and its connector, as in the prototype
)

// onbStepper lists every step as a button: any step can be opened, since
// each is optional. A step behind the user shows what it chose.
func onbStepper(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	textWidth := max(onbRailWidthInner()-onbStepMarker-theme.MarginL-2*theme.MarginXS, 0)
	items := make([]*ui.Node, 0, onbPages)
	for page := onboardingPage(0); page < onbPages; page++ {
		label := onbStepLabel(h, page)
		marker := &ui.Node{
			Kind: ui.KindCapsule, Key: fmt.Sprintf("onb-step-marker:%d", page),
			Width: onbStepMarker, Height: onbStepMarker, Shape: ui.ShapeCircle, CenterX: true,
		}
		line := ui.FillOutline
		textTone := ui.ToneSubtle
		switch {
		case page < h.onbPage:
			marker.Fill = ui.FillAccent
			marker.Children = []*ui.Node{{Kind: ui.KindColumn, Children: []*ui.Node{{
				Kind: ui.KindIcon, Icon: "check", IconSize: m.IconSmall, CenterX: true, CenterY: true,
			}}}}
			line = ui.FillAccent
			textTone = ui.ToneNormal
		case page == h.onbPage:
			marker.Fill = ui.FillSoft
			marker.Stroke = 2 // token-exempt: the current step's ring
			marker.StrokeFill = ui.FillAccent
			textTone = ui.ToneNormal
		default:
			marker.Stroke = 1 // token-exempt: a hairline ring, not a ladder value
			marker.StrokeFill = ui.FillOutline
		}
		if marker.Children == nil {
			marker.Children = []*ui.Node{{Kind: ui.KindColumn, Children: []*ui.Node{{
				Kind: ui.KindText, Text: fmt.Sprintf("%d", int(page)+1), TextRole: theme.RoleMono,
				CenterX: true, CenterY: true,
			}}}}
		}
		track := []*ui.Node{marker}
		if page+1 < onbPages {
			track = append(track, &ui.Node{
				Kind: ui.KindCapsule, Width: onbStepLine, Height: onbStepRow - onbStepMarker - theme.MarginXS,
				Fill: line, Shape: ui.ShapeStadium, CenterX: true,
			})
		}
		title := &ui.Node{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel, Tone: textTone, Bold: page == h.onbPage}
		text := []*ui.Node{title}
		if page < h.onbPage {
			if summary := onbStepSummary(r, h, page); summary != "" {
				line := h.wrappedText(summary, theme.RoleCaption, ui.ToneSubtle, textWidth, 1)
				line.Key = fmt.Sprintf("onb-step-summary:%d", page)
				text = append(text, line)
			}
		}
		// A ghost row: the rail shows through, and only the current step
		// carries the accent wash.
		fill, tone := ui.FillNone, ui.ToneSubtle
		if page == h.onbPage {
			fill, tone = ui.FillSoft, ui.ToneNormal
		}
		items = append(items, &ui.Node{
			Kind: ui.KindButton, Action: fmt.Sprintf("onb-step:%d", page), Name: label, Role: "button",
			Focusable: true, Padding: theme.MarginXS, Shape: ui.ShapeMedium, Fill: fill, Tone: tone,
			Children: []*ui.Node{{Kind: ui.KindColumn, Children: []*ui.Node{{
				Kind: ui.KindRow, Height: onbStepRow, Gap: theme.MarginL, Children: []*ui.Node{
					{Kind: ui.KindColumn, Width: onbStepMarker, Gap: theme.MarginXS, Children: track},
					{Kind: ui.KindColumn, Width: textWidth, Gap: theme.MarginXXS, Children: text},
				},
			}}}},
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Key: "onb-stepper", Children: items}
}

// onbStepSummary is what a step chose, read from the draft: the rail's
// second line and the Review cards' value.
func onbStepSummary(r *Registry, h *PanelHost, page onboardingPage) string {
	switch page {
	case onbAppearance:
		summary := onbGet(h, "appearance.mode")
		if name := onbWallpaperName(r, h); name != "" {
			summary += " · " + name
		}
		return summary
	case onbRegion:
		city := strings.TrimSpace(h.draft.Weather.City)
		if city == "" {
			city = onbText(h, "summary.not_set", "not set")
		}
		unit := "°C"
		if e := h.set.ByPath("weather.unit"); e != nil && e.Get(h.draft) == "fahrenheit" {
			unit = "°F"
		}
		return city + " · " + unit
	case onbIdle:
		return onbIdleMode(h, settings.WhenIdleMode(h.draft.Idle.Lock, r.wallsSnapshot.EnabledAtLogin()))
	case onbPlugins:
		return fmt.Sprintf(onbText(h, "summary.plugins_selected", "%d selected"), len(onbSelectedPlugins(r, h)))
	}
	return ""
}

// onbWallpaperName names what the wizard's output shows, or "" when nothing
// is assigned there.
func onbWallpaperName(r *Registry, h *PanelHost) string {
	bar, svc := r.bars[h.output], r.wallpaperServiceLocked()
	if bar == nil || svc == nil {
		return ""
	}
	assignment, ok := svc.Snapshot().Assignments[bar.connector()]
	if !ok {
		return ""
	}
	return wallpaperAssignmentLabel(assignment)
}

// onbRailFoot holds Not now and the move control under the stepper.
func onbRailFoot(h *PanelHost) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Key: "onb-rail-foot", Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindRow, Key: "onb-move-controls", Gap: theme.MarginS, PinEnd: true, Children: []*ui.Node{
			onbSkipButton(h), onbMoveButton(h),
		}},
		onbMoveHint(h, onbRailWidthInner()),
	}}
}

// onbHeader is the narrow frame's rail: the half-size wordmark and the
// progress eyebrow, with the move control and Not now at the end.
func onbHeader(h *PanelHost) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Key: "onb-header", PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginL, Children: []*ui.Node{
			onboardingWordmark(h, (onbWordmarkW+1)/2, onbWordmarkH/2), onbEyebrow(h),
		}},
		{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{onbMoveButton(h), onbSkipButton(h)}},
	}}
}

func onbEyebrow(h *PanelHost) *ui.Node {
	text := fmt.Sprintf(onbText(h, "progress", "STEP %02d OF %02d"), int(h.onbPage)+1, int(onbPages))
	return &ui.Node{
		Kind: ui.KindText, Key: "onb-eyebrow", Text: text, Name: text, Role: "status",
		TextRole: theme.RoleMono, Tone: ui.ToneAccent,
	}
}

func onbSkipButton(h *PanelHost) *ui.Node {
	skip := h.button("onb-skip", onbText(h, "action.not_now", onbSkipLabel), "")
	skip.Fill = ui.FillOutline
	skip.Shape = ui.ShapeStadium
	return skip
}

// onbMoveButton is the keyboard route to move mode, an icon beside Super-drag.
func onbMoveButton(h *PanelHost) *ui.Node {
	button := h.button("onb-move", onbText(h, "action.move", "Move window"), "drag_indicator")
	button.Children = button.Children[:1]
	button.Fill = ui.FillOutline
	button.Shape = ui.ShapeStadium
	return button
}

func onbMoveHint(h *PanelHost, width int) *ui.Node {
	text := onbText(h, "move_hint", "SUPER + DRAG TO MOVE")
	if h.onbMoveMode {
		text = onbText(h, "move_mode_hint", "Use arrows or click a position. Enter or Escape exits move mode.")
	}
	hint := h.wrappedText(text, theme.RoleMono, ui.ToneSubtle, width, 0)
	hint.Key, hint.Role = "onb-move-hint", "status"
	return hint
}

// onbContent is the step column: a top-anchored head, the scrolling step
// body, and the footer pinned under it.
func onbContent(r *Registry, h *PanelHost) *ui.Node {
	var head []*ui.Node
	if onbWide(h) {
		head = append(head, onbEyebrow(h))
	}
	title := onbTitle(h, h.onbPage)
	intro := h.wrappedText(onbIntro(h, h.onbPage), theme.RoleBody, ui.ToneSubtle, onbContentWidth(h), 0)
	intro.Key = "onb-intro"
	head = append(head,
		&ui.Node{Kind: ui.KindText, Key: "onb-title", Text: title, Name: title, Role: "heading", TextRole: theme.RoleHeadline},
		intro,
	)
	children := []*ui.Node{
		{Kind: ui.KindColumn, Key: "onb-head", Gap: theme.MarginS, Children: head},
		{Kind: ui.KindScroll, Key: onbBodyKey, ScrollOffset: h.onbScroll, Children: []*ui.Node{{
			Kind: ui.KindColumn, Gap: theme.MarginM, Children: onboardingBody(r, h),
		}}},
	}
	if h.errLabel != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Key: "onb-error", Text: h.errLabel, Tone: ui.ToneError})
	}
	children = append(children, onbFooter(r, h))
	return &ui.Node{Kind: ui.KindColumn, Key: "onb-content", Padding: onbContentPadding(h), Gap: theme.MarginL, Children: children}
}

func onbContentPadding(h *PanelHost) int {
	if onbWide(h) {
		return theme.MarginXL
	}
	return h.metrics().CardPadding
}

// onbFitFrame gives the scroll body, and the rail's stepper, the height the
// frame has left, so the footer and the rail foot sit at the foot of the
// frame on every step. Layout has no flex track; this sizes the wizard's.
func onbFitFrame(h *PanelHost, box ui.Rect) error {
	root := h.root
	content := onbNodeByKey(root, "onb-content")
	if content == nil {
		return nil
	}
	measure := h.measureText()
	innerW, innerH := max(box.W-2*root.Padding, 0), max(box.H-2*root.Padding, 0)
	contentW := innerW
	if root.Kind == ui.KindRow {
		rail := root.Children[0]
		railInnerH := max(innerH-2*rail.Padding, 0)
		if err := onbFillColumn(rail.Children[0], "onb-stepper", rail.Width-2*ui.CapsulePadX(rail), railInnerH, measure); err != nil {
			return err
		}
		content.Height = innerH
		contentW = max(innerW-rail.Width-root.Gap, 0)
	} else if err := onbFillColumn(root, "onb-content", box.W, box.H, measure); err != nil {
		return err
	}
	return onbFillColumn(content, onbBodyKey, contentW, content.Height, measure)
}

// onbFillColumn sets the height of col's child keyed key to whatever the
// other children leave of height.
func onbFillColumn(col *ui.Node, key string, width, height int, measure ui.MeasureText) error {
	var flex *ui.Node
	used := 2*col.Padding + col.Gap*max(len(col.Children)-1, 0)
	for _, child := range col.Children {
		if child.Key == key {
			flex = child
			continue
		}
		childH, err := ui.ContentHeight(child, max(width-2*col.Padding, 0), measure)
		if err != nil {
			return err
		}
		used += childH
	}
	if flex != nil {
		flex.Height = max(height-used, 0)
	}
	return nil
}

func onbNodeByKey(n *ui.Node, key string) *ui.Node {
	if n == nil || n.Key == key {
		return n
	}
	for _, child := range n.Children {
		if found := onbNodeByKey(child, key); found != nil {
			return found
		}
	}
	return nil
}

// onboardingBody holds the page content. Every control is the Settings
// panel's own entry row, so the wizard writes through the same validated
// setters and never grows a second settings model.
func onboardingBody(r *Registry, h *PanelHost) []*ui.Node {
	var children []*ui.Node
	switch h.onbPage {
	case onbWelcome:
		children = append(children, onbWelcomeCards(h)...)
	case onbAppearance:
		children = append(children, onbModeCards(h), onbWallpaperRow(r, h))
	case onbRegion:
		children = append(children, onbRegionBody(r, h)...)
	case onbIdle:
		children = append(children, onbIdleCards(r, h))
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

// onbWelcomeCards say what the steps ahead cover, rather than reviewing
// settings before any choice was made.
func onbWelcomeCards(h *PanelHost) []*ui.Node {
	cards := []struct{ key, title, line string }{
		{"look", "Look", "Dark or light, and a wallpaper."},
		{"weather", "Weather", "Your city and units."},
		{"idle", "Idle", "Screensaver or lock."},
		{"plugins", "Plugins", "Bar extras from your installed plugins."},
	}
	out := make([]*ui.Node, 0, len(cards))
	for i, card := range cards {
		out = append(out, onbCard(h, "onb-welcome-"+card.key, fmt.Sprint(i+1),
			onbText(h, "welcome."+card.key, card.title), onbText(h, "welcome."+card.key+"_line", card.line)))
	}
	return out
}

const onbBadge = 28 // token-exempt: the overview's number badge, sized to the prototype

// onbCard is a content-width card: a mono badge, a title and one line.
func onbCard(h *PanelHost, key, badge, title, line string) *ui.Node {
	m := h.metrics()
	width := onbContentWidth(h)
	textWidth := max(width-2*m.CardPadding-onbBadge-theme.MarginL, 0)
	return &ui.Node{
		Kind: ui.KindCapsule, Key: key, Width: width, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginL, Children: []*ui.Node{
			{
				Kind: ui.KindCapsule, Width: onbBadge, Height: onbBadge, Fill: ui.FillSoft, Shape: ui.ShapeMedium,
				Children: []*ui.Node{{Kind: ui.KindColumn, Children: []*ui.Node{{
					Kind: ui.KindText, Text: badge, TextRole: theme.RoleMono, Tone: ui.ToneAccent, CenterX: true, CenterY: true,
				}}}},
			},
			{Kind: ui.KindColumn, Width: textWidth, Gap: theme.MarginXXS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel},
				h.wrappedText(line, theme.RoleCaption, ui.ToneSubtle, textWidth, 0),
			}},
		}}},
	}
}

// onbModeCards choose the colour mode through the appearance.mode entry,
// the way the Settings row does.
func onbModeCards(h *PanelHost) *ui.Node {
	current := ""
	if e := h.set.ByPath("appearance.mode"); e != nil {
		current = e.Get(h.draft)
	}
	width := (onbContentWidth(h) - theme.MarginL) / 2
	modes := []struct{ value, title, line string }{
		{"dark", "Dark", "Light text on dark surfaces."},
		{"light", "Light", "Dark text on light surfaces."},
	}
	cards := make([]*ui.Node, 0, len(modes))
	for _, mode := range modes {
		cards = append(cards, onbChoiceCard(h, "onb-mode:"+mode.value, "pick:appearance.mode="+mode.value,
			onbText(h, "mode."+mode.value, mode.title), onbText(h, "mode."+mode.value+"_line", mode.line),
			mode.value == current, width))
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Children: onbEvenCards(cards)}
}

// onbIdleCards choose the idle action through idle.after. An action this
// session cannot run stays visible, dimmed by settingsDimIdle, with a chip
// saying what it needs.
func onbIdleCards(r *Registry, h *PanelHost) *ui.Node {
	current := settings.WhenIdleMode(h.draft.Idle.Lock, r.wallsSnapshot.EnabledAtLogin())
	// Three across on the wide frame; stacked on the narrow one, where a third
	// of the column cannot hold a reason chip.
	width := (onbContentWidth(h) - 2*theme.MarginL) / 3
	if !onbWide(h) {
		width = onbContentWidth(h)
	}
	modes := []struct{ value, line, reason string }{
		{"nothing", "Leave the screen as it is.", ""},
		{"screensaver", "Run sysc-walls over the desktop.", "Needs sysc-walls"},
		{"lock", "Ask for your password to return.", "Needs a locker in Settings"},
	}
	available := map[string]bool{
		"nothing":     true,
		"screensaver": r.wallsSnapshot.ServiceAvailable,
		"lock":        strings.TrimSpace(h.draft.Session.Locker) != "",
	}
	cards := make([]*ui.Node, 0, len(modes))
	for _, mode := range modes {
		var chip *ui.Node
		if !available[mode.value] {
			chip = &ui.Node{
				Kind: ui.KindCapsule, Key: "onb-idle-reason:" + mode.value, Padding: theme.MarginXS,
				PaddingX: theme.MarginS, Fill: ui.FillContainerHighest, Shape: ui.ShapeStadium,
				Children: []*ui.Node{{Kind: ui.KindText, Text: onbText(h, "idle.reason_"+mode.value, mode.reason), TextRole: theme.RoleCaption}},
			}
		}
		cards = append(cards, onbChoiceCardWith(h, "onb-idle:"+mode.value, "pick:idle.after="+mode.value,
			onbIdleMode(h, mode.value), onbText(h, "idle."+mode.value+"_line", mode.line),
			mode.value == current, width, chip))
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Children: onbEvenCards(cards)}
	if !onbWide(h) {
		row = &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: cards}
	}
	settingsDimIdle(r, h, row)
	return row
}

// onbEvenCards gives cards sharing a row the tallest one's height.
func onbEvenCards(cards []*ui.Node) []*ui.Node {
	tallest := 0
	for _, card := range cards {
		tallest = max(tallest, card.Height)
	}
	for _, card := range cards {
		card.Height = tallest
	}
	return cards
}

// onbChoiceCard is one option of a choice: a title and a line on a card that
// is selected with an accent ring and wash.
func onbChoiceCard(h *PanelHost, key, action, title, line string, selected bool, width int) *ui.Node {
	return onbChoiceCardWith(h, key, action, title, line, selected, width, nil)
}

// onbChoiceCardWith is onbChoiceCard with a chip under its line.
func onbChoiceCardWith(h *PanelHost, key, action, title, line string, selected bool, width int, chip *ui.Node) *ui.Node {
	m := h.metrics()
	textWidth := max(width-2*m.CardPadding, 0)
	body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel},
		h.wrappedText(line, theme.RoleCaption, ui.ToneSubtle, textWidth, 0),
	}}
	if chip != nil {
		body.Gap = theme.MarginXS
		body.Children = append(body.Children, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{chip}})
	}
	height, err := ui.ContentHeight(body, textWidth, h.measureText())
	if err != nil {
		height = m.StandardControl
	}
	card := &ui.Node{
		Kind: ui.KindButton, Key: key, Action: action, Name: title, Role: "radio", Focusable: true,
		Shape: ui.ShapeCard, Fill: ui.FillContainerHigh, Padding: m.CardPadding,
		Width: width, Height: height + 2*m.CardPadding,
		Stroke: 1, StrokeFill: ui.FillOutlineVariant, // token-exempt: a hairline border, not a ladder value
		Children: []*ui.Node{body},
	}
	// A wash and a ring rather than StateSelected, which paints a solid
	// accent chip the caption cannot be read on.
	if selected {
		card.Fill = ui.FillSoft
		card.Stroke = 2 // token-exempt: the chosen option's ring
		card.StrokeFill = ui.FillAccent
	}
	return card
}

// onbWallpaperRow names what the output shows and opens the shared picker.
func onbWallpaperRow(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	name := onbWallpaperName(r, h)
	if name == "" {
		name = onbText(h, "wallpaper.none", "None assigned")
	}
	change := onbActionButton(h, "onb-wallpaper", "action.change", "Change", "wallpaper")
	return &ui.Node{
		Kind: ui.KindCapsule, Key: "onb-wallpaper-row", Width: onbContentWidth(h), Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: onbText(h, "wallpaper.label", "Wallpaper"), TextRole: theme.RoleLabel},
				{Kind: ui.KindText, Text: name, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
			}},
			change,
		}}},
	}
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

// onbRows renders the named settings entries as Settings renders them.
func onbRows(r *Registry, h *PanelHost, paths ...string) []*ui.Node {
	var out []*ui.Node
	for _, e := range onbEntries(r, h, paths...) {
		out = append(out, settingsEntryRow(h, e, onbContentWidth(h)))
	}
	return out
}

// onbEntries looks up the named settings entries with the wizard's own
// descriptions. An unknown path is a programmer error, not a user one, so it
// simply drops.
func onbEntries(r *Registry, h *PanelHost, paths ...string) []settings.Entry {
	var entries []settings.Entry
	for _, p := range paths {
		if e := h.set.ByPath(p); e != nil {
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
	return entries
}

// onbRegionBody stacks each label over its control: language and unit side
// by side, the city across the column with the place it resolves to.
func onbRegionBody(r *Registry, h *PanelHost) []*ui.Node {
	width := onbContentWidth(h)
	half := (width - theme.MarginL) / 2
	entries := onbEntries(r, h, "session.language", "weather.unit", "weather.city")
	if len(entries) != 3 {
		return nil
	}
	language, unit, city := entries[0], entries[1], entries[2]
	unitW := (half - theme.MarginS) / 2
	unitValue := unit.Get(h.draft)
	units := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		onbChoiceButton(h, "onb-unit:celsius", "pick:weather.unit=celsius", onbText(h, "unit.celsius", "°C Celsius"), unitValue == "celsius", unitW),
		onbChoiceButton(h, "onb-unit:fahrenheit", "pick:weather.unit=fahrenheit", onbText(h, "unit.fahrenheit", "°F Fahrenheit"), unitValue == "fahrenheit", unitW),
	}}
	out := []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginL, Children: []*ui.Node{
		onbStackedField(h, language, settingsControl(h, language, half), half),
		onbStackedField(h, unit, units, half),
	}}}
	out = append(out, onboardingFontOption(h)...)
	out = append(out, onbStackedField(h, city, settingsControl(h, city, width), width))
	if place := onbPlaceCard(r, h); place != nil {
		out = append(out, place)
	}
	return out
}

// onbStackedField is a settings entry as a form field: the label above the
// control and the description below it.
func onbStackedField(h *PanelHost, e settings.Entry, control *ui.Node, width int) *ui.Node {
	children := []*ui.Node{
		{Kind: ui.KindText, Key: "onb-label:" + e.Path, Text: e.Label, TextRole: theme.RoleLabel},
		{Kind: ui.KindColumn, Key: "onb-control:" + e.Path, Width: width, Children: []*ui.Node{control}},
	}
	if e.Describe != "" {
		children = append(children, h.wrappedText(e.Describe, theme.RoleCaption, ui.ToneSubtle, width, 0))
	}
	return &ui.Node{Kind: ui.KindColumn, Width: width, Gap: theme.MarginXS, Children: children}
}

// onbChoiceButton is a one-line option of a choice, marked like a choice card.
func onbChoiceButton(h *PanelHost, key, action, label string, selected bool, width int) *ui.Node {
	button := h.button(action, label, "")
	button.Key, button.Role, button.Width = key, "radio", width
	button.Shape = ui.ShapeMedium
	button.Fill = ui.FillContainerHigh
	button.Stroke, button.StrokeFill = 1, ui.FillOutlineVariant // token-exempt: a hairline border, not a ladder value
	button.Children[0].CenterX = true
	if selected {
		button.Fill = ui.FillSoft
		button.Stroke = 2 // token-exempt: the chosen option's ring
		button.StrokeFill = ui.FillAccent
	}
	return button
}

// onbPlaceCard shows what the typed city resolved to once the weather service
// has looked it up, and until then says when it will be.
func onbPlaceCard(r *Registry, h *PanelHost) *ui.Node {
	city := strings.TrimSpace(h.draft.Weather.City)
	if city == "" {
		return nil
	}
	m := h.metrics()
	width := onbContentWidth(h)
	lines := []*ui.Node{h.wrappedText(onbText(h, "region.place_pending", "The place is looked up when setup finishes."),
		theme.RoleCaption, ui.ToneSubtle, width-2*m.CardPadding, 0)}
	reading := r.reading
	resolved := strings.EqualFold(strings.TrimSpace(r.cfg.Weather.City), city) && reading.Location != "" &&
		(r.weather == nil || !r.weather.LocationPending())
	if resolved {
		condition := render.WeatherCondition(reading.Code)
		lines = []*ui.Node{
			{Kind: ui.KindText, Text: reading.Location, TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%s · %s", reading.Temperature, unitSuffix(reading.Unit),
				h.tr("wx.cond."+condKey(condition), condition)), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Key: "onb-place", Role: "status", Width: width, Padding: m.CardPadding,
		Fill: ui.FillSoft, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: lines}},
	}
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
	selected := onbText(h, "summary.plugins_none", "none")
	if names := onbSelectedPlugins(r, h); len(names) > 0 {
		selected = strings.Join(names, ", ")
	}
	return fmt.Sprintf(onbText(h, "summary.plugins", "Startup plugins: %s"), selected)
}

// onbSelectedPlugins names the candidates the staged selection enables and
// places on the bar.
func onbSelectedPlugins(r *Registry, h *PanelHost) []string {
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
	return names
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

// onbFooter keeps one primary action per step (Next/Finish, accent fill)
// beside an outlined Back, with the step's status line at the start.
func onbFooter(r *Registry, h *PanelHost) *ui.Node {
	back := h.button("onb-back", onbText(h, "action.back", onbBackLabel), "")
	back.Fill = ui.FillOutline
	back.Shape = ui.ShapeStadium
	if h.onbPage == onbWelcome {
		back.State |= ui.StateDisabled
		back.AriaDisabled = true
	}
	action := "onb-next"
	if h.onbPage == onbReady {
		action = "onb-finish"
	}
	main := h.button(action, onbMainLabelFor(h, h.onbPage), "")
	main.Shape = ui.ShapeStadium
	main.Fill = ui.FillAccent
	if h.onbPage == onbReady && r.idleApplying {
		main.State |= ui.StateDisabled
		main.AriaDisabled = true
	}
	status := &ui.Node{Kind: ui.KindText, Key: "onb-status", Role: "status", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
	if h.onbPage == onbPlugins {
		status.Text = onbStepSummary(r, h, onbPlugins)
	}
	return &ui.Node{Kind: ui.KindRow, Key: "onb-footer", PinEnd: true, Children: []*ui.Node{
		status,
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{back, main}},
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
	switch page {
	case onbWelcome:
		return onbText(h, "action.start", onbStartLabel)
	case onbReady:
		return onbText(h, "action.finish", onbFinishLabel)
	}
	return onbText(h, "action.continue", onbNextLabel)
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
	width := onbBodyWidth(h) - 2*onbContentPadding(h)
	if onbWide(h) {
		width -= onbRailWidth + h.metrics().PanelPadding
	}
	return max(width, 0)
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
	if step, ok := strings.CutPrefix(n.Action, "onb-step:"); ok {
		page, err := strconv.Atoi(step)
		if err != nil || page < 0 || page >= int(onbPages) {
			return false
		}
		h.onbPage = onboardingPage(page)
		h.onbScroll = 0
		if h.onbPage == onbRegion {
			r.checkOnboardingFont(h)
		}
		r.rebuildPanel(h)
		h.focusByName(onbStepLabel(h, h.onbPage))
		return true
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
