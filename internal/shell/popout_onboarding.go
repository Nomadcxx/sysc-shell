package shell

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/onboarding"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// onboardingPage is the wizard's one linear flow: five short steps, no
// branches. Page math is pure so a table test can pin the navigation.
type onboardingPage uint8

const (
	onbWelcome onboardingPage = iota
	onbAppearance
	onbRegion
	onbIdle
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

// onboardingCopy centralizes the wizard's English strings (decision D2).
// Product localization has not landed; when it does, this block is the one
// place to extract from.
const (
	onbTitleWelcome    = "Welcome to sysc-shell"
	onbTitleAppearance = "Appearance"
	onbTitleRegion     = "Region and language"
	onbTitleIdle       = "When idle"
	onbTitleReady      = "You are ready"

	onbIntroWelcome = "sysc-shell provides the bar, panels and launcher; " +
		"notifications, clipboard, tray and locking are companions beside it. " +
		"These steps set appearance and regional basics. Everything can change " +
		"later in Settings, and choices apply as you pick them."
	onbIntroAppearance = "Set the theme mode and wallpaper. Detailed palettes, " +
		"schemes and bar layout stay in Settings."
	onbIntroRegion = "Weather location and units. The interface language follows " +
		"your session locale; keyboard layouts stay with the OS."
	onbIntroIdle = "Choose what happens when the session is idle, or leave the " +
		"current behaviour alone."
	onbIntroReady = "Review what applied. Settings holds every control this " +
		"wizard used, plus the rest of the shell."

	onbDocsHint = "Docs: https://nomadcxx.github.io/sysc/docs/"

	onbWallpaperHint = "The picker browses images, sets per-output assignments " +
		"and updates its folder; presets and bar layout live in Settings."
	onbRegionHint = "Language, keyboard layout and timezone stay with your OS " +
		"session. Coordinates accept 0; blank a field to clear it."
	onbIdleHint = "A screensaver does not secure the session. The lock choice " +
		"needs a locker command set below."
	onbEntryHint = "Settings holds every control used here, beside the rest of " +
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
	onbTitleWelcome, onbTitleAppearance, onbTitleRegion, onbTitleIdle, onbTitleReady,
}

var onbIntros = [onbPages]string{
	onbIntroWelcome, onbIntroAppearance, onbIntroRegion, onbIntroIdle, onbIntroReady,
}

// onbBodyKey names the wizard's one scroll so its offset can be restored
// across rebuilds, the way the Settings body does.
const onbBodyKey = "onboarding-body"

func onboardingTree(r *Registry, h *PanelHost) *ui.Node {
	m := h.theme.Metrics
	step := &ui.Node{
		Kind:     ui.KindText,
		Text:     fmt.Sprintf("Step %d / %d", int(h.onbPage)+1, int(onbPages)),
		TextRole: theme.RoleCaption,
	}
	head := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindText, Text: onbTitles[h.onbPage], Name: onbTitles[h.onbPage], Role: "heading", TextRole: theme.RoleTitle},
		step,
	}}
	body := &ui.Node{
		Kind:         ui.KindScroll,
		Key:          onbBodyKey,
		Width:        onbBodyWidth(h),
		Gap:          theme.MarginM,
		ScrollOffset: h.onbScroll,
		Children:     onboardingBody(r, h),
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: m.PanelPadding, Children: []*ui.Node{
		head, body, onbActions(h),
	}}
}

// onboardingBody holds the page content. Every control is the Settings
// panel's own entry row, so the wizard writes through the same validated
// setters and never grows a second settings model.
func onboardingBody(r *Registry, h *PanelHost) []*ui.Node {
	children := []*ui.Node{
		{Kind: ui.KindText, Text: onbIntros[h.onbPage]},
	}
	switch h.onbPage {
	case onbWelcome:
		children = append(children, onbCaption("Session locale: "+onboardingLocale()))
		children = append(children, onbSummary(r, h)...)
		children = append(children, onbCaption(onbDocsHint))
	case onbAppearance:
		children = append(children, onbRows(h, "appearance.mode", "appearance.source",
			"appearance.palette", "appearance.scheme")...)
		pick := h.button("onb-wallpaper", onbWallpaperButton, "wallpaper")
		pick.Width = onbBodyWidth(h)
		children = append(children, pick)
		children = append(children, onbCaption(onbWallpaperHint))
	case onbRegion:
		children = append(children, onbRows(h, "weather.city", "weather.latitude",
			"weather.longitude", "weather.unit")...)
		children = append(children, onbCaption(onbRegionHint))
	case onbIdle:
		rows := onbRows(h, "idle.after", "idle.delay", "session.locker")
		body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: rows}
		settingsDimIdle(r, h, body)
		children = append(children, body)
		children = append(children, onbCaption(onbIdleHint))
	case onbReady:
		children = append(children, onbSummary(r, h)...)
		children = append(children, onbCaption(onbEntryHint))
		children = append(children, onbCaption(onbDocsHint))
	}
	return children
}

// onbRows renders the named settings entries as Settings renders them. An
// unknown path is a programmer error, not a user one, so it simply drops.
func onbRows(h *PanelHost, paths ...string) []*ui.Node {
	var out []*ui.Node
	width := onbBodyWidth(h)
	for _, p := range paths {
		e := h.set.ByPath(p)
		if e == nil {
			continue
		}
		out = append(out, settingsEntryRow(h, *e, width))
	}
	return out
}

// onbSummary states the current choices as plain lines; visiting a page
// writes nothing, so the wizard can always open.
func onbSummary(r *Registry, h *PanelHost) []*ui.Node {
	mode := settings.WhenIdleMode(h.draft.Idle.Lock, r.wallsSnapshot.EnabledAtLogin())
	weather := strings.TrimSpace(h.draft.Weather.City)
	if weather == "" {
		if h.draft.Weather.Configured {
			weather = "coordinates set"
		} else {
			weather = "not set"
		}
	}
	lines := []string{
		"Theme: " + onbGet(h, "appearance.mode") + ", " + onbGet(h, "appearance.source"),
		"Weather location: " + weather,
		"Idle: " + mode,
	}
	out := make([]*ui.Node, 0, len(lines))
	for _, l := range lines {
		out = append(out, onbCaption(l))
	}
	return out
}

func onbGet(h *PanelHost, path string) string {
	if e := h.set.ByPath(path); e != nil {
		if v := strings.TrimSpace(e.Get(h.draft)); v != "" {
			return v
		}
	}
	return "default"
}

func onbCaption(text string) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Text: text, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
}

// onboardingLocale reads the session's locale the way libc does: the first
// non-empty of LC_ALL, LC_MESSAGES, LANG. Product localization has not
// landed; this reports what the session says, nothing more.
func onboardingLocale() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return "not set"
}

// onbActions keeps one primary action per step (Next/Finish, default fill);
// Back and Not now wear the Settings panel's outline style so they read as
// subordinate, the way Noctalia's wizard puts one Get-started button forward.
func onbActions(h *PanelHost) *ui.Node {
	back := h.button("onb-back", onbBackLabel, "")
	back.Fill = ui.FillOutline
	back.Shape = ui.ShapeMedium
	if h.onbPage == onbWelcome {
		back.State |= ui.StateDisabled
		back.AriaDisabled = true
	}
	main := h.button("onb-next", onbNextLabel, "chevron_right")
	if h.onbPage == onbReady {
		main = h.button("onb-finish", onbFinishLabel, "check")
	}
	skip := h.button("onb-skip", onbSkipLabel, "")
	skip.Fill = ui.FillOutline
	skip.Shape = ui.ShapeMedium
	skip.Tone = ui.ToneSubtle
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		skip, back, main,
	}}
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
	switch n.Action {
	case "onb-back":
		h.onbPage = h.onbPage.prev()
		h.onbScroll = 0
		r.rebuildPanel(h)
		h.focusByName(onbMainLabel(h.onbPage))
		return true
	case "onb-next":
		h.onbPage = h.onbPage.next()
		h.onbScroll = 0
		r.rebuildPanel(h)
		h.focusByName(onbMainLabel(h.onbPage))
		return true
	case "onb-wallpaper":
		// Reuse the picker rather than a second wallpaper UI: it opens on top
		// of the wizard, and the shield closes the picker first.
		if err := r.openPanelLocked(PanelWallpaper, h.output, Trigger{}); err != nil {
			log.Printf("shell: onboarding wallpaper picker: %v", err)
		}
		return true
	case "onb-skip":
		markOnboardingOutcome(onboarding.OutcomeDismissed)
		r.closePanelLocked(h.id)
		return true
	case "onb-finish":
		markOnboardingOutcome(onboarding.OutcomeCompleted)
		r.closePanelLocked(h.id)
		return true
	}
	return false
}

// onboardingReopenCard is the Settings → Session action that shows the
// wizard again with current values. It never clears the lifecycle marker:
// reopening is not a reset.
func onboardingReopenCard(h *PanelHost) *ui.Node {
	m := h.metrics()
	label := "Reopen setup wizard"
	return &ui.Node{
		Kind: ui.KindButton, Action: "onboarding:open",
		Name: label, Role: "button", Focusable: true,
		Width: settingsBodyWidth(h), Height: m.StandardControl,
		Gap: m.ButtonPadding / 2, Padding: m.ButtonPadding,
		Fill:     ui.FillOutline,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
}

// markOnboardingOutcome records the lifecycle marker. Failure is logged, not
// surfaced: a crash or refused write leaves the marker absent, so the wizard
// simply offers itself again instead of lying about completion.
func markOnboardingOutcome(outcome onboarding.Outcome) {
	if err := onboarding.Mark(onboarding.StatePath(), outcome); err != nil {
		log.Printf("shell: onboarding %s marker: %v", outcome, err)
	}
}
