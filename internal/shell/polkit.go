package shell

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	polkitSurfaceID    = "polkit-auth"
	polkitNamespace    = "sysc-shell-polkit"
	polkitCardMaxWidth = 560
)

type polkitPromptPhase uint8

const (
	polkitChoosingIdentity polkitPromptPhase = iota
	polkitAwaitingPrompt
	polkitEnteringResponse
	polkitShowingInfo
)

func polkitFocusOrder(phase polkitPromptPhase, hasDetails, hasIdentityChooser bool) []string {
	var order []string
	switch phase {
	case polkitChoosingIdentity:
		if hasIdentityChooser {
			order = append(order, "identity")
		}
		order = append(order, "continue", "cancel")
	case polkitEnteringResponse:
		order = []string{"password", "submit", "cancel"}
	case polkitShowingInfo:
		order = []string{"continue", "cancel"}
	default:
		order = []string{"cancel"}
	}
	if hasDetails {
		order = append(order, "details")
	}
	return order
}

func polkitPromptMasked(prompt polkit.Prompt) bool { return prompt.Secret && !prompt.Echo }

func polkitSelectedIdentity(identities []polkit.Identity, currentUID string) int {
	for i, identity := range identities {
		if identity.Kind == "unix-user" && identity.Values["uid"] == currentUID {
			return i
		}
	}
	return 0
}

type polkitHost struct {
	r       *Registry
	request func(wayland.AuxRequest)

	open_     bool
	closed    bool
	output    uint32
	connector string
	rootGen   uint64
	request_  polkit.Request
	requester string

	phase         polkitPromptPhase
	identities    []polkit.Identity
	selected      int
	prompt        polkit.Prompt
	field         *ui.Field
	wrongPassword bool
	detailsOpen   bool
	waiting       int
	focus         string
	pressed       string

	logicalW, logicalH, scale120 int
	text                         *render.TextRenderer
	fontFamily                   string
	style                        render.Style
	metrics                      theme.Metrics
	root                         *ui.Node
	nodes                        map[string]*ui.Node
}

func newPolkitHost(r *Registry) *polkitHost {
	return &polkitHost{r: r, request: func(req wayland.AuxRequest) { r.sendAux(req) }}
}

func (h *polkitHost) openLocked(req polkit.Request, output uint32, connector string, waiting int) {
	if h.open_ {
		h.finishLocked(true)
	}
	h.identities = h.identities[:0]
	for _, identity := range req.Identities {
		if identity.Kind == "unix-user" && identity.Name != "" {
			h.identities = append(h.identities, identity)
		}
	}
	if len(h.identities) == 0 {
		req.Cancel()
		return
	}
	h.open_, h.closed = true, false
	h.output, h.connector = output, connector
	h.request_ = req
	h.requester = polkitCallerProgram(req.Details["polkit.caller-pid"])
	if h.requester == "unknown" {
		h.requester = "Unknown application"
	}
	h.phase = polkitChoosingIdentity
	h.selected, h.waiting = polkitSelectedIdentity(h.identities, strconv.Itoa(os.Getuid())), waiting
	h.prompt = polkit.Prompt{}
	h.field = nil
	h.detailsOpen = false
	h.focus, h.pressed = "continue", ""
	if len(h.identities) > 1 {
		h.focus = "identity"
	}
	currentTheme := h.r.panelThemeFor(output)
	h.style = currentTheme.OverlayStyle()
	h.metrics = currentTheme.Metrics
	h.style.NoGround = true
	h.rebuild()
	h.rootGen = h.r.roots.openRoot(polkitAuthRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.r.dwell.leave()
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
}

func (h *polkitHost) releaseForChainClose() {
	if !h.open_ {
		return
	}
	h.finishLocked(true)
}

func (h *polkitHost) spec() *wayland.AuxSpec {
	output, cookie := h.output, h.request_.Cookie
	// blur-exempt: this transparent authentication overlay keeps session context around its prompt card, like the window switcher.
	return &wayland.AuxSpec{
		ID: polkitSurfaceID, Namespace: polkitNamespace,
		Layer: layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorBottom |
			layershell.ZwlrLayerSurfaceV1AnchorLeft |
			layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone:    -1,
		Keyboard:         keyboardExclusive,
		InhibitShortcuts: true,
		Callbacks: wayland.HostCallbacks{
			Configure: h.configureLocking,
			Render:    h.renderLocking,
			Handle:    h.handleLocking,
			WantIME:   h.wantIMELocking,
			IBeamAt:   h.ibeamAtLocking,
		},
		OnDrop: func() { h.drop(output, cookie) },
	}
}

func (h *polkitHost) drop(output uint32, cookie string) {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	if h.open_ && h.output == output && h.request_.Cookie == cookie {
		h.finishLocked(true)
	}
}

func (h *polkitHost) configureLocking(width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	h.logicalW, h.logicalH, h.scale120 = width, height, scale120
	h.style.Scale120 = ui.Scale120(max(scale120, int(ui.ScaleUnit)))
	h.style.Body = ui.Rect{W: width, H: height}
	return h.relayout()
}

func (h *polkitHost) renderLocking(pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	fontFamily := h.r.cfg.ForConnector(h.connector).FontFamily
	if h.text == nil || h.fontFamily != fontFamily {
		fonts, err := render.NewSystemFontMap(fontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
		h.fontFamily = fontFamily
	}
	if err := h.relayout(); err != nil {
		return err
	}
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	return render.Paint(canvas, h.root, h.text, h.style)
}

func (h *polkitHost) handleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.handleLocked(event)
}

func (h *polkitHost) wantIMELocking() bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.open_ && h.field != nil && h.focus == "password"
}

func (h *polkitHost) ibeamAtLocking(x, y float64) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	n := h.nodes["password"]
	return h.open_ && n != nil && n.Bounds.Contains(int(math.Floor(x)), int(math.Floor(y)))
}

func (h *polkitHost) relayout() error {
	if h.logicalW <= 0 || h.logicalH <= 0 || h.root == nil {
		return nil
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: h.logicalW, H: h.logicalH}, h.measure())
}

func (h *polkitHost) measure() ui.MeasureText {
	return func(value string, attrs ui.TextAttrs) (int, int) {
		if h.text != nil {
			if w, height, err := h.text.Measure(value, render.SpecFor(h.style, attrs), attrs.Tabular); err == nil {
				return w, height
			}
		}
		return len([]rune(value)) * 8, 18
	}
}

func (h *polkitHost) rebuild() {
	cardWidth := polkitCardMaxWidth
	if h.logicalW > 0 {
		cardWidth = min(cardWidth, max(h.logicalW-48, 280))
	}
	innerWidth := max(cardWidth-2*theme.MarginL, 1)
	children := []*ui.Node{
		{Kind: ui.KindText, Text: "////// AUTH //////", TextRole: theme.RoleMono, Tone: ui.ToneAccent},
		{Kind: ui.KindText, Text: "Authentication required", TextRole: theme.RoleTitle, Name: "Authentication required", Role: "heading"},
	}
	if h.request_.Message != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.request_.Message, TextRole: theme.RoleBody, MaxWidth: innerWidth})
	}
	requester := h.requester
	if requester == "" {
		requester = "Unknown application"
	}
	children = append(children,
		&ui.Node{Kind: ui.KindText, Text: "REQUESTED BY", TextRole: theme.RoleMono, Tone: ui.ToneSubtle},
		&ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			h.appIcon(),
			{Kind: ui.KindText, Text: requester, TextRole: theme.RoleLabel, MaxWidth: max(innerWidth-h.metrics.IconLarge-theme.MarginS, 1)},
		}},
		&ui.Node{Kind: ui.KindSeparator, Width: innerWidth},
	)
	if h.phase != polkitChoosingIdentity && h.selected >= 0 && h.selected < len(h.identities) {
		children = append(children,
			&ui.Node{Kind: ui.KindText, Text: "ACCOUNT", TextRole: theme.RoleMono, Tone: ui.ToneSubtle},
			&ui.Node{Kind: ui.KindText, Text: h.identities[h.selected].Name, TextRole: theme.RoleLabel},
		)
	}

	h.nodes = make(map[string]*ui.Node)
	switch h.phase {
	case polkitChoosingIdentity:
		identity := h.identities[h.selected]
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "ACCOUNT", TextRole: theme.RoleMono, Tone: ui.ToneSubtle})
		if len(h.identities) > 1 {
			label := fmt.Sprintf("%s  ·  %d of %d", identity.Name, h.selected+1, len(h.identities))
			h.nodes["identity"] = h.buttonNode("identity", label, "Choose account", h.focus == "identity", innerWidth)
			children = append(children, h.nodes["identity"])
		} else {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: identity.Name, TextRole: theme.RoleLabel})
		}
	case polkitAwaitingPrompt:
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "Waiting for the authentication prompt…", TextRole: theme.RoleMono, Tone: ui.ToneSubtle})
	case polkitEnteringResponse:
		promptLabel := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(h.prompt.Text), ":"))
		if promptLabel == "" {
			promptLabel = "Authentication response"
		}
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.prompt.Text, TextRole: theme.RoleMono, Tone: ui.ToneSubtle, MaxWidth: innerWidth})
		node := h.field.Node(promptLabel)
		node.Width, node.Height = innerWidth, h.metrics.InputHeight
		node.Placeholder = "Enter your response"
		node.Editing = h.focus == "password"
		node.TextRole = theme.RoleBody
		if h.wrongPassword {
			node.Tone = ui.ToneError
		}
		h.nodes["password"] = node
		children = append(children, node)
	case polkitShowingInfo:
		if h.wrongPassword {
			children = append(children, &ui.Node{
				Kind: ui.KindCapsule, Width: innerWidth, Padding: theme.MarginS,
				Shape: ui.ShapeMedium, Fill: ui.FillErrorContainer,
				Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
					{Kind: ui.KindText, Text: "!", TextRole: theme.RoleTitle, Tone: ui.ToneError},
					{Kind: ui.KindText, Text: h.prompt.Text, TextRole: theme.RoleBody, MaxWidth: max(innerWidth-4*theme.MarginS-16, 1)},
				}}},
			})
		} else {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: h.prompt.Text, TextRole: theme.RoleBody, Tone: ui.ToneSubtle, MaxWidth: innerWidth})
		}
	}

	if h.waiting > 0 {
		text := fmt.Sprintf("%d more authentication request", h.waiting)
		if h.waiting != 1 {
			text += "s"
		}
		text += " waiting"
		children = append(children, &ui.Node{Kind: ui.KindText, Text: text, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if h.detailsOpen {
		for _, line := range h.detailLines() {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: line, TextRole: theme.RoleMono, Tone: ui.ToneSubtle, MaxWidth: innerWidth})
		}
	}

	buttons := []*ui.Node{}
	switch h.phase {
	case polkitChoosingIdentity:
		buttons = append(buttons, h.buttonNode("continue", "Authenticate", "Authenticate", h.focus == "continue", 0))
	case polkitEnteringResponse:
		buttons = append(buttons, h.buttonNode("submit", "Authenticate", "Authenticate", h.focus == "submit", 0))
	case polkitShowingInfo:
		buttons = append(buttons, h.buttonNode("continue", "Continue", "Continue", h.focus == "continue", 0))
	}
	buttons = append(buttons, h.buttonNode("cancel", "Cancel", "Cancel", h.focus == "cancel", 0))
	children = append(children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: buttons})
	if h.hasDetails() {
		label, name := "+ REQUEST DETAILS", "Show request details"
		if h.detailsOpen {
			label, name = "− REQUEST DETAILS", "Hide request details"
		}
		h.nodes["details"] = h.buttonNode("details", label, name, h.focus == "details", innerWidth)
		children = append(children, h.nodes["details"])
	}

	card := &ui.Node{
		Kind: ui.KindCapsule, Width: cardWidth, Padding: theme.MarginL,
		Shape: ui.ShapeCard, Fill: ui.FillContainerHigh, CenterX: true, CenterY: true,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: children}},
	}
	h.root = &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{card}}
	if h.logicalW > 0 && h.logicalH > 0 {
		_ = h.relayout()
	}
}

func (h *polkitHost) appIcon() *ui.Node {
	if h.r != nil {
		if image := h.r.lookupNotifyIcon(h.request_.IconName); image != nil {
			return &ui.Node{Kind: ui.KindImage, Image: image, ImageSize: h.metrics.IconLarge}
		}
	}
	return &ui.Node{Kind: ui.KindIcon, Icon: "shield", IconSize: h.metrics.IconLarge}
}

func (h *polkitHost) buttonNode(id, label, name string, focused bool, width int) *ui.Node {
	fill := ui.FillNone
	primary := id == "continue" || id == "submit"
	if primary {
		fill = ui.FillAccent
	} else if focused || id == "identity" {
		fill = ui.FillSoft
	}
	node := &ui.Node{
		Kind: ui.KindButton, Text: label, Action: "polkit:" + id,
		Name: name, Role: "button", Focusable: true,
		Width: width, Height: h.metrics.StandardControl, Padding: theme.MarginS,
		Shape: ui.ShapeMedium, Fill: fill,
	}
	if width <= 0 {
		if measured, _, err := ui.Measure(node, h.measure()); err == nil {
			width = measured
		}
		width = max(width, h.metrics.StandardControl*2)
		node.Width = width
	}
	if focused {
		node.Stroke, node.StrokeFill = 1, ui.FillOutline
	}
	if id == "details" {
		node.TextRole = theme.RoleMono
	}
	h.nodes[id] = node
	return node
}

func (h *polkitHost) detailLines() []string {
	keys := make([]string, 0, len(h.request_.Details))
	for key := range h.request_.Details {
		if key != "polkit.caller-pid" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	limit := min(len(keys), 8)
	lines := []string{
		"Action ID: " + h.request_.ActionID,
		"Program: " + polkitCallerProgram(h.request_.Details["polkit.caller-pid"]),
	}
	for _, key := range keys[:limit] {
		lines = append(lines, key+": "+strings.TrimSpace(h.request_.Details[key]))
	}
	if len(keys) > limit {
		lines = append(lines, fmt.Sprintf("and %d more details", len(keys)-limit))
	}
	return lines
}

func (h *polkitHost) hasDetails() bool {
	return h.request_.ActionID != "" || len(h.request_.Details) > 0
}

func polkitCallerProgram(pid string) string {
	parsed, err := strconv.ParseUint(pid, 10, 32)
	if err != nil || parsed == 0 {
		return "unknown"
	}
	executable, err := os.Readlink(filepath.Join("/proc", strconv.FormatUint(parsed, 10), "exe"))
	if err != nil || executable == "" {
		return "unknown"
	}
	return filepath.Base(executable)
}

func (h *polkitHost) handleLocked(event wayland.Event) bool {
	if !h.open_ {
		return false
	}
	switch event.Kind {
	case wayland.EventKeyPress:
		key := event.Key
		sym, text := event.Sym, event.Text
		if sym == 0 && key != 0 {
			fallback := ui.FallbackKey(key, event.Mods)
			sym, text = fallback.Sym, fallback.Text
		}
		if sym == ui.SymEscape || key == keyEsc {
			h.finishLocked(true)
			return true
		}
		if sym == ui.SymTab || key == keyTab {
			direction := 1
			if event.Mods.Has(ui.ModShift) {
				direction = -1
			}
			h.cycleFocus(direction)
			h.repaintLocked()
			return true
		}
		if h.focus == "identity" && len(h.identities) > 1 && (sym == ui.SymUp || sym == ui.SymDown) {
			direction := 1
			if sym == ui.SymUp {
				direction = -1
			}
			h.selected = (h.selected + direction + len(h.identities)) % len(h.identities)
			h.rebuild()
			h.repaintLocked()
			return true
		}
		if h.field != nil && h.focus == "password" {
			result := h.field.HandleKey(ui.KeyInput{Code: key, Sym: sym, Text: text, Mods: event.Mods, Serial: event.Serial})
			if h.r != nil {
				h.r.requestClipboard(result, event.Serial)
			}
			if result.Changed {
				h.wrongPassword = false
			}
			if result.Submit {
				h.submitLocked()
				return true
			}
			if result.Handled {
				h.rebuild()
				h.repaintLocked()
				return true
			}
		}
		if sym == ui.SymReturn || sym == ui.SymKPEnter || key == keyEnter {
			h.activateFocusLocked()
			return true
		}
		if sym == ' ' && (h.focus == "submit" || h.focus == "continue" || h.focus == "cancel" || h.focus == "details") {
			h.activateFocusLocked()
			return true
		}
	case wayland.EventIME:
		if h.field == nil || h.focus != "password" {
			return false
		}
		if event.IMECommit != "" {
			h.wrongPassword = false
		}
		if event.IMEPreedit != "" {
			h.field.Preedit(event.IMEPreedit)
		}
		if event.IMECommit != "" {
			h.field.Commit(event.IMECommit)
		}
		if event.IMEDeleteBefore > 0 || event.IMEDeleteAfter > 0 {
			h.field.DeleteSurrounding(int(event.IMEDeleteBefore), int(event.IMEDeleteAfter))
		}
		h.rebuild()
		h.repaintLocked()
		return true
	case wayland.EventPaste:
		if h.field == nil || h.focus != "password" {
			return false
		}
		h.wrongPassword = false
		h.field.Commit(strings.ReplaceAll(flattenPaste(event.Paste), "\x00", ""))
		h.rebuild()
		h.repaintLocked()
		return true
	case wayland.EventPointerPress:
		action := h.hitAction(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		if action == "password" && (event.Button == 0 || event.Button == buttonLeft) {
			h.focus = "password"
			h.field.MoveTextEdge(true, false)
			h.pressed = "password"
			h.rebuild()
			h.repaintLocked()
			return true
		}
		if action != "" && (event.Button == 0 || event.Button == buttonLeft) {
			h.focus, h.pressed = action, action
			h.rebuild()
			return true
		}
		h.pressed = ""
	case wayland.EventPointerRelease:
		action := h.hitAction(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		pressed := h.pressed
		h.pressed = ""
		if action != "" && action == pressed && (event.Button == 0 || event.Button == buttonLeft) {
			h.activateActionLocked(action)
			return true
		}
	case wayland.EventPointerLeave:
		h.pressed = ""
	}
	return false
}

func (h *polkitHost) hitAction(x, y int) string {
	for _, key := range []string{"identity", "password", "continue", "submit", "cancel", "details"} {
		if node := h.nodes[key]; node != nil && node.Bounds.Contains(x, y) {
			return key
		}
	}
	return ""
}

func (h *polkitHost) cycleFocus(direction int) {
	order := polkitFocusOrder(h.phase, h.hasDetails(), len(h.identities) > 1)
	index := 0
	for i, name := range order {
		if name == h.focus {
			index = i
			break
		}
	}
	index = (index + direction + len(order)) % len(order)
	h.focus = order[index]
}

func (h *polkitHost) activateFocusLocked() {
	h.activateActionLocked(h.focus)
}

func (h *polkitHost) activateActionLocked(action string) {
	switch action {
	case "identity":
		if len(h.identities) > 1 {
			h.selected = (h.selected + 1) % len(h.identities)
			h.rebuild()
			h.repaintLocked()
		}
	case "continue":
		if h.phase == polkitShowingInfo {
			h.acknowledgeInfoLocked()
		} else {
			h.beginAuthenticationLocked()
		}
	case "password", "submit":
		h.submitLocked()
	case "cancel":
		h.finishLocked(true)
	case "details":
		h.detailsOpen = !h.detailsOpen
		h.rebuild()
		h.repaintLocked()
	}
}

func (h *polkitHost) acknowledgeInfoLocked() {
	if !h.open_ || h.phase != polkitShowingInfo {
		return
	}
	if !h.request_.Submit("") {
		h.finishLocked(false)
		return
	}
	h.phase, h.focus = polkitAwaitingPrompt, "cancel"
	h.prompt = polkit.Prompt{}
	h.rebuild()
	h.repaintLocked()
}

func (h *polkitHost) beginAuthenticationLocked() {
	if h.phase != polkitChoosingIdentity || h.selected < 0 || h.selected >= len(h.identities) {
		return
	}
	h.phase, h.focus = polkitAwaitingPrompt, "cancel"
	h.field = nil
	h.rebuild()
	if !h.request_.Start(h.identities[h.selected]) {
		h.finishLocked(false)
		return
	}
	h.repaintLocked()
}

func (h *polkitHost) setPromptLocked(prompt polkit.Prompt) {
	h.prompt = prompt
	if prompt.Secret {
		h.phase = polkitEnteringResponse
		h.field = ui.NewField("")
		h.field.Masked = polkitPromptMasked(prompt)
		h.field.SubmitOnEnter = true
		h.focus = "password"
	} else {
		h.wrongPassword = prompt.Text == "Authentication failed. Try again."
		h.phase = polkitShowingInfo
		h.field = nil
		h.focus = "continue"
	}
	h.rebuild()
	h.repaintLocked()
}

func (h *polkitHost) submitLocked() {
	if !h.open_ || h.field == nil || h.phase != polkitEnteringResponse {
		return
	}
	answer := h.field.Text
	h.field = ui.NewField("")
	accepted := h.request_.Submit(answer)
	answer = ""
	if !accepted {
		h.finishLocked(false)
		return
	}
	h.phase, h.focus = polkitAwaitingPrompt, "cancel"
	h.wrongPassword = false
	h.prompt = polkit.Prompt{}
	h.rebuild()
	h.repaintLocked()
}

func (h *polkitHost) clearResponseLocked() {
	if h.field != nil {
		h.field = ui.NewField("")
	}
	h.wrongPassword = false
}

func (h *polkitHost) finishLocked(cancel bool) {
	if !h.open_ {
		return
	}
	req := h.request_
	gen := h.rootGen
	h.open_, h.rootGen = false, 0
	h.clearResponseLocked()
	h.request_ = polkit.Request{}
	h.requester = ""
	h.identities = nil
	h.prompt = polkit.Prompt{}
	h.root, h.nodes = &ui.Node{Kind: ui.KindColumn}, nil
	h.focus, h.pressed = "", ""
	h.closeSurface()
	if cancel {
		req.Cancel()
	}
	if gen != 0 {
		h.r.roots.closeRoot(gen)
	}
}

func (h *polkitHost) closeSurface() {
	if h.closed || h.output == 0 {
		return
	}
	h.closed = true
	h.request(wayland.AuxRequest{Output: h.output, ID: polkitSurfaceID})
}

func (h *polkitHost) suspendForLockerLocked() {
	if !h.open_ || h.closed {
		return
	}
	h.clearResponseLocked()
	h.pressed = ""
	h.rebuild()
	h.closeSurface()
}

func (h *polkitHost) resumeAfterLockerLocked() {
	if !h.open_ || !h.closed || h.output == 0 {
		return
	}
	h.closed = false
	h.request(wayland.AuxRequest{Output: h.output, Open: h.spec()})
}

func (h *polkitHost) refreshWaitingLocked(waiting int) {
	if !h.open_ || h.waiting == waiting {
		return
	}
	h.waiting = waiting
	h.rebuild()
	h.repaintLocked()
}

func (h *polkitHost) repaintLocked() {
	if h.open_ && h.output != 0 {
		h.r.publishSurface(h.output, polkitSurfaceID)
	}
}

func (h *polkitHost) retheme(next Theme) {
	scale, body := h.style.Scale120, h.style.Body
	h.style = next.OverlayStyle()
	h.metrics = next.Metrics
	h.style.NoGround = true
	h.style.Scale120, h.style.Body = scale, body
	h.rebuild()
}
