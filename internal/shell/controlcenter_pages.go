package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	ccDash            = "—"
	ccAvatarSize      = 56
	ccAccountsIconDir = "/var/lib/AccountsService/icons"

	// The control centre's measured composition. These are one grid rather
	// than a ladder: the page is a fixed 480 tall, and the two columns 355 and
	// 228 wide. The tile and forecast widths are
	// not listed because they are derived from these and the gap between
	// them -- the two were sized to fit the old gap exactly, so a literal
	// would silently overflow the moment the ladder moved.
	ccPageH = 480
	ccCardH = 88
	// The left column is what the body has left once the right column and the
	// gap between them are taken: ccBodyWidth resolves to 596 at the standard
	// 700px panel, and 356 + MarginL + 228 came to 597 -- one pixel wider than
	// the row it sits in, which is how a composition sized to fill its
	// container exactly starts refusing a child.
	ccLeftColumnW  = 355
	ccRightColumnW = 228
	// ccTileW is one quick tile: the right column holds two of them with a
	// gap between, and it was written as 110 for a gap of 8. Derived once
	// here because two pages build tiles, and a repeated expression is the
	// same drift as a repeated number.
	ccTileW          = (ccRightColumnW - theme.MarginM) / 2
	ccGaugeSize      = 40
	ccSliderCapsuleH = 52
	ccSliderW        = 360
	ccValueW         = 44
	ccAvatarIconSize = 28
	ccTodayH         = 336
	ccForecastH      = 132
	ccSessionCardH   = 232
	ccCalendarRowMin = 32
	// ccCalendarWeekdayH is the weekday header band: one label line and the
	// small gap Noctalia leaves under it.
	ccCalendarWeekdayH = 24
	// Event markers follow Noctalia: up to three dots in a strip reserved
	// under every day number, so a day with events and one without keep the
	// same baseline.
	ccCalendarDot    = 5
	ccCalendarDotGap = 2
	ccCalendarDotMax = 3
	ccNotifyListH    = 416
)

type ccIdentity struct {
	Name, Account, Uptime, ImagePath string
}

func readCCIdentity() ccIdentity {
	facts := readMachineFacts()
	host, _ := os.Hostname()
	name, account, imagePath := "", "", ""
	if current, err := user.Current(); err == nil {
		name = strings.TrimSpace(current.Name)
		if name == "" {
			name = current.Username
		}
		account = current.Username
		imagePath = ccProfileImagePath(current.HomeDir, current.Username, ccAccountsIconDir)
	}
	if account != "" && host != "" {
		account += "@" + host
	}
	return ccIdentity{Name: name, Account: account, Uptime: facts.Uptime, ImagePath: imagePath}
}

func ccProfileImagePath(home, username, accountsDir string) string {
	for _, path := range []string{
		filepath.Join(home, ".face.icon"),
		filepath.Join(home, ".face"),
		filepath.Join(accountsDir, username),
	} {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func ccAvatarNode(image *ui.Image) *ui.Node {
	if image != nil {
		return &ui.Node{
			Kind: ui.KindImage, Width: ccAvatarSize, Height: ccAvatarSize,
			ImageSize: ccAvatarSize, Image: image, Shape: ui.ShapeCircle,
		}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Width: ccAvatarSize, Height: ccAvatarSize,
		Fill: ui.FillContainerHighest, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "person", IconSize: ccAvatarIconSize}},
	}
}

func ccAvatar(r *Registry, h *PanelHost, identity ccIdentity) *ui.Node {
	if r == nil || r.trayIcons == nil || identity.ImagePath == "" {
		return ccAvatarNode(nil)
	}
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	key := icons.Square(identity.ImagePath, scale.Physical(ccAvatarSize))
	if image, ok := r.trayIcons.Lookup(key); ok {
		return ccAvatarNode(image)
	}
	if _, failed := r.controlAvatarFailed[key]; !failed {
		_, _, _ = r.trayIcons.Request(key)
	}
	return ccAvatarNode(nil)
}

// ccT resolves a translator for a control-centre page from the panel host's
// draft config, falling back to the registry config, then English.
func ccT(h *PanelHost, r *Registry) translateFunc {
	if h != nil {
		return h.tr
	}
	if r != nil {
		return cfgT(r.cfg)
	}
	return nil
}

func ccText(s string) string {
	if strings.TrimSpace(s) == "" {
		return ccDash
	}
	return s
}

func ccClock(now time.Time) string {
	if now.IsZero() {
		return ccDash
	}
	return now.Format("15:04")
}

func ccDate(now time.Time) string {
	if now.IsZero() {
		return ccDash
	}
	return now.Format("Monday, 2 January")
}

func ccWeatherSummary(reading services.Reading, tr translateFunc) (string, ui.Tone) {
	if !reading.Observed {
		if !reading.FailedSince.IsZero() {
			return cardT(tr, "cc.weather_unavailable", "weather unavailable"), ui.ToneError
		}
		return ccDash, ui.ToneNormal
	}
	isDay := reading.IsDay == nil || *reading.IsDay
	text := fmt.Sprintf("%c %.0f%s", render.WeatherIcon(reading.Code, isDay), reading.Temperature, unitSuffix(reading.Unit))
	if reading.Stale() {
		text += " (" + humaniseAge(time.Since(reading.FetchedAt)) + ")"
	}
	return text, ui.ToneNormal
}

func ccPercent(value int, ok bool) string {
	if !ok {
		return ccDash
	}
	return fmt.Sprintf("%d%%", value)
}

func ccQuickAccessButton(width int, icon, label, action string, selected bool) *ui.Node {
	n := ccSegment(icon, label, action, selected)
	n.Width, n.Height, n.Shape = width, 48, ui.ShapeStadium
	if !selected {
		n.Fill = ui.FillContainerHigh
	}
	return n
}

func ccResourceGroup(snap services.Snapshot, id, label string, sel services.Selector, selected bool, slotWidth int, tr translateFunc) *ui.Node {
	value, valueText, ok := ccResourceValue(snap, sel, selected)
	name := ccResourceName(snap, id, selected, tr)
	detail := valueText
	if !ok {
		detail = cardT(tr, "cc.unavailable", "unavailable")
		if id == "gpu" && ccGPUAmbiguous(snap) {
			detail = cardT(tr, "cc.gpu_ambiguous_detail", "device identity ambiguous")
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Width: slotWidth, Gap: theme.MarginXXS, Name: name, Role: "group", Children: []*ui.Node{
		{Kind: ui.KindRadialGauge, Width: ccGaugeSize, Height: ccGaugeSize, Value: value, ValueText: valueText, Absent: !ok, Name: name, Role: "img", Tooltip: name + ": " + detail},
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption, CenterX: true},
	}}
}

func ccGPUAmbiguous(snap services.Snapshot) bool {
	return snap.GPU != nil && len(snap.GPU.GPUs) > 1
}

func ccResourceName(snap services.Snapshot, id string, selected bool, tr translateFunc) string {
	if id == "gpu" && !selected && ccGPUAmbiguous(snap) {
		return cardT(tr, "cc.usage.gpu_ambiguous", "GPU usage: device identity ambiguous")
	}
	switch id {
	case "cpu":
		return cardT(tr, "cc.usage.cpu", "CPU usage")
	case "memory":
		return cardT(tr, "cc.usage.memory", "Memory usage")
	case "temperature":
		return cardT(tr, "cc.usage.temp", "CPU temperature")
	case "gpu":
		return cardT(tr, "cc.usage.gpu", "GPU usage")
	default:
		return id
	}
}

func ccResourceValue(snap services.Snapshot, sel services.Selector, selected bool) (float64, string, bool) {
	if !selected {
		return 0, ccDash, false
	}
	value, ok := snap.Fraction(sel)
	if !ok {
		return 0, ccDash, false
	}
	if sel.Source == services.SourceCPU && sel.Subject == "temperature" {
		if snap.Thermal == nil || !snap.Thermal.Valid {
			return 0, ccDash, false
		}
		return value, fmt.Sprintf("%.0f°C", snap.Thermal.Celsius), true
	}
	return value, ccPercent(int(value*100+0.5), true), true
}

func ccOnOff(on bool, tr translateFunc) string {
	if on {
		return cardT(tr, "cc.on", "On")
	}
	return cardT(tr, "cc.off", "Off")
}

func ccSegment(icon, label, action string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label, Role: "button", Focusable: true,
		Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
		},
	}
	if selected {
		n.State |= ui.StateSelected
		n.Fill = ui.FillAccent
	}
	return n
}

// ccQuickTile is one filled card in the control centre. Its inset is the
// density row's card rung, not the panel rung: a tile is a card, and a card
// that pads like the panel it sits in reads as a panel. The metrics row is
// passed in rather than read from a package constant so the tile follows the
// ladder when the ladder moves.
func ccQuickTile(m theme.Metrics, width int, icon, label, value, action string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindCapsule, Width: width, Height: ccCardH, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Action: action, Name: label, Role: "button", Focusable: true,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: value, TextRole: theme.RoleCaption, Tabular: true},
		}}},
	}
	if selected {
		n.State |= ui.StateSelected
		n.Fill = ui.FillAccent
	}
	return n
}

func ccDisable(n *ui.Node) {
	if n == nil {
		return
	}
	n.Action = ""
	n.State |= ui.StateDisabled
}

func ccSlider(m theme.Metrics, icon, label, action string, value int, ok bool) *ui.Node {
	control := &ui.Node{
		Kind: ui.KindSlider, Action: action, Name: label, Role: "slider", Focusable: true,
		Value: float64(value), Min: 0, Max: 100, Step: 5, Width: ccSliderW, Absent: !ok,
	}
	if !ok {
		control.State |= ui.StateDisabled
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Height: ccSliderCapsuleH, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
			control,
			{Kind: ui.KindText, Text: ccPercent(value, ok), Width: ccValueW, MinWidthText: "100%", Tabular: true},
		}}},
	}
}

func hProfileNext(h *PanelHost) string {
	if h == nil || len(h.profiles) == 0 {
		return ""
	}
	for i, name := range h.profiles {
		if name == h.profileActive {
			return h.profiles[(i+1)%len(h.profiles)]
		}
	}
	return h.profiles[0]
}

func ccWeather(r *Registry, h *PanelHost) *ui.Node {
	tr := ccT(h, r)
	m := h.metrics()
	reading := services.Reading{}
	location := ccDash
	if r != nil {
		reading = r.reading
		location = weatherLocation(r.cfg.Weather, reading)
	}
	today := weatherHeroCard(reading, location, m,
		max(ccBodyWidth(h)-2*m.CardPadding, 0), ccTodayH, weatherTodayEffectKey, tr, r.weatherNow())

	// Four slots share the body width and the three gaps between them. The
	// width is derived rather than written down: it was 143 for a gap of 8,
	// and a literal would overflow the row the moment the ladder moved.
	const forecastSlots = 4
	slotW := max((ccBodyWidth(h)-(forecastSlots-1)*theme.MarginM)/forecastSlots, 0)
	forecast := &ui.Node{Kind: ui.KindRow, Height: ccForecastH, Gap: theme.MarginM}
	for i := 0; i < forecastSlots; i++ {
		var day *services.Day
		if i+1 < len(reading.Daily) {
			day = &reading.Daily[i+1]
		}
		forecast.Children = append(forecast.Children, ccForecastDay(m, slotW, day, reading.Unit))
	}
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Gap: theme.MarginL, Children: []*ui.Node{today, forecast}}
}

func ccForecastDay(m theme.Metrics, width int, day *services.Day, unit services.Unit) *ui.Node {
	return weatherForecastSlot(m, width, ccForecastH, day, unit)
}

func ccAudio(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	tr := ccT(h, r)
	state := services.AudioState{}
	ok := false
	if r != nil && r.audio != nil {
		state, ok = r.audio.CachedState()
	}
	mute := ccQuickTile(m, ccTileW, "volume_off", cardT(tr, "cc.mute", "Mute"), ccOnOff(state.Muted, tr), "cc:mute", state.Muted)
	if !ok {
		ccDisable(mute)
	}
	rows := []*ui.Node{
		monitorCardTitle(cardT(tr, "cc.output", "Output"), 0),
		ccSlider(m, "volume_up", cardT(tr, "cc.volume", "Volume"), "cc:volume", state.Level, ok),
		mute,
	}
	if h != nil && h.errLabel != "" {
		rows = append([]*ui.Node{{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError}}, rows...)
	}
	card := monitorCard(m, rows)
	card.Height = ccPageH
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Children: []*ui.Node{card}}
}

func ccPrivacy(r *Registry, h *PanelHost) *ui.Node {
	state := privacyState{}
	now := time.Now()
	if r != nil {
		state = r.privacy
		if !r.now.IsZero() {
			now = r.now
		}
	}
	rows := []*ui.Node{
		monitorCardTitle("Capture activity", 0),
		privacyStatusSection("Microphone", state.microphone, privacySignalDetail(state, privacyMicrophone)),
		privacyStatusSection("Camera", state.camera, privacySignalDetail(state, privacyCamera)),
		privacyStatusSection("Screen sharing", state.screenShare, privacySignalDetail(state, privacyScreenShare)),
	}
	m := h.metrics()
	errLabel := ""
	if h != nil {
		errLabel = h.errLabel
	}
	snapshot, snapshotErr := ccPrivacySnapshotFor(r)
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
		monitorCard(m, rows),
		ccPrivacyAuditCard(m, snapshot, now, errLabel, snapshotErr != nil),
	}}
}

const (
	ccPrivacyPluginID     = "org.sysc.privacy"
	ccPrivacyStateKey     = "control_center"
	ccPrivacySchema       = 1
	ccPrivacyStateLimit   = 128 << 10
	ccPrivacyLabelLimit   = 160
	ccPrivacySummaryLimit = 512
	ccPrivacyStaleAfter   = 15 * time.Minute
	ccPrivacyCategoryMax  = 16
	ccPrivacyAppMax       = 4
	ccPrivacyFindingMax   = 5
	ccPrivacyReducedMax   = 12
	ccPrivacyDefectMax    = 4
)

type ccPrivacySnapshot struct {
	Schema              int                 `json:"schema"`
	Available           bool                `json:"available"`
	Reason              string              `json:"reason"`
	Hint                string              `json:"hint"`
	GeneratedAt         time.Time           `json:"generated_at"`
	Overall             ccPrivacyOverall    `json:"overall"`
	Sandbox             *ccPrivacySandbox   `json:"sandbox"`
	Network             ccPrivacyNetwork    `json:"network"`
	Categories          []ccPrivacyCategory `json:"categories"`
	CategoriesShown     int                 `json:"categories_shown"`
	CategoriesTotal     int                 `json:"categories_total"`
	CategoriesTruncated bool                `json:"categories_truncated"`
	Findings            []ccPrivacyFinding  `json:"findings"`
	FindingsShown       int                 `json:"findings_shown"`
	FindingsTotal       int                 `json:"findings_total"`
	FindingsTruncated   bool                `json:"findings_truncated"`
	Reduced             []ccPrivacyReduced  `json:"reduced"`
	ReducedShown        int                 `json:"reduced_shown"`
	ReducedTotal        int                 `json:"reduced_total"`
	ReducedTruncated    bool                `json:"reduced_truncated"`
}

type ccPrivacyOverall struct {
	Grade    string `json:"grade"`
	Findings int    `json:"findings"`
	High     int    `json:"high"`
}

type ccPrivacySandbox struct {
	Applicable bool   `json:"applicable"`
	Grade      string `json:"grade"`
}

type ccPrivacyNetwork struct {
	VPN                         ccPrivacyVPN `json:"vpn"`
	TelemetryConnectionsUnknown bool         `json:"telemetry_connections_unknown"`
}

type ccPrivacyVPN struct {
	State                 string               `json:"state"`
	Client                string               `json:"client"`
	DefaultRouteViaTunnel *bool                `json:"default_route_via_tunnel"`
	DNSContained          *bool                `json:"dns_contained"`
	IPv6Contained         *bool                `json:"ipv6_contained"`
	KillSwitchNote        string               `json:"kill_switch_note"`
	Defects               []ccPrivacyVPNDefect `json:"defects"`
	DefectsShown          int                  `json:"defects_shown"`
	DefectsTotal          int                  `json:"defects_total"`
	DefectsTruncated      bool                 `json:"defects_truncated"`
}

type ccPrivacyVPNDefect struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type ccPrivacyCategory struct {
	Name               string         `json:"name"`
	Grade              string         `json:"grade"`
	Installed          []ccPrivacyApp `json:"installed"`
	InstalledShown     int            `json:"installed_shown"`
	InstalledTotal     int            `json:"installed_total"`
	InstalledTruncated bool           `json:"installed_truncated"`
	Worst              *ccPrivacyApp  `json:"worst"`
}

type ccPrivacyApp struct {
	Name  string `json:"name"`
	Grade string `json:"grade"`
}

type ccPrivacyFinding struct {
	Severity string `json:"severity"`
	Grade    string `json:"grade"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
}

type ccPrivacyReduced struct {
	Effect string `json:"effect"`
}

// ccPrivacySnapshotFor reads only the running privacy plugin's bounded schema-1 state.
func ccPrivacySnapshotFor(r *Registry) (*ccPrivacySnapshot, error) {
	if r == nil || r.plugins == nil {
		return nil, nil
	}
	raw, ok := r.plugins.stateValue(ccPrivacyPluginID, ccPrivacyStateKey)
	if !ok {
		return nil, nil
	}
	return parseCCPrivacySnapshot(raw)
}

func parseCCPrivacySnapshot(raw []byte) (*ccPrivacySnapshot, error) {
	if len(raw) == 0 || len(raw) > ccPrivacyStateLimit {
		return nil, fmt.Errorf("privacy snapshot is empty or exceeds %d bytes", ccPrivacyStateLimit)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode privacy snapshot: %w", err)
	}
	if _, ok := fields["available"]; !ok {
		return nil, fmt.Errorf("privacy snapshot has no availability state")
	}
	var snapshot ccPrivacySnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode privacy snapshot: %w", err)
	}
	if snapshot.Schema != ccPrivacySchema {
		return nil, fmt.Errorf("unsupported privacy snapshot schema %d", snapshot.Schema)
	}
	if snapshot.GeneratedAt.IsZero() {
		return nil, fmt.Errorf("privacy snapshot has no timestamp")
	}
	if !validCCPrivacyGrade(snapshot.Overall.Grade) {
		return nil, fmt.Errorf("invalid privacy overall grade %q", snapshot.Overall.Grade)
	}
	if sandbox := snapshot.Sandbox; sandbox != nil {
		if !validCCPrivacyGrade(sandbox.Grade) {
			return nil, fmt.Errorf("invalid Flatpak sandbox grade %q", sandbox.Grade)
		}
		if sandbox.Applicable && ccPrivacyGradeRank(snapshot.Overall.Grade) < ccPrivacyGradeRank(sandbox.Grade) {
			return nil, fmt.Errorf("privacy overall grade is safer than Flatpak sandbox grade")
		}
	}
	if snapshot.Overall.Findings < 0 || snapshot.Overall.High < 0 || snapshot.Overall.High > snapshot.Overall.Findings {
		return nil, fmt.Errorf("invalid privacy finding totals")
	}
	if snapshot.Network.VPN.State != "unknown" && snapshot.Network.VPN.State != "connected" {
		return nil, fmt.Errorf("invalid VPN state %q", snapshot.Network.VPN.State)
	}
	if !snapshot.Network.TelemetryConnectionsUnknown {
		return nil, fmt.Errorf("privacy snapshot must report telemetry attribution as unknown")
	}
	if !ccPrivacyTextFits(snapshot.Reason, ccPrivacySummaryLimit) || !ccPrivacyTextFits(snapshot.Hint, ccPrivacySummaryLimit) ||
		!ccPrivacyTextFits(snapshot.Network.VPN.Client, ccPrivacyLabelLimit) ||
		!ccPrivacyTextFits(snapshot.Network.VPN.KillSwitchNote, ccPrivacySummaryLimit) {
		return nil, fmt.Errorf("privacy snapshot contains oversized text")
	}
	if len(snapshot.Categories) > ccPrivacyCategoryMax ||
		!ccPrivacyCountsValid(snapshot.CategoriesShown, snapshot.CategoriesTotal, snapshot.CategoriesTruncated, len(snapshot.Categories)) {
		return nil, fmt.Errorf("invalid privacy category totals")
	}
	if len(snapshot.Findings) > ccPrivacyFindingMax ||
		!ccPrivacyCountsValid(snapshot.FindingsShown, snapshot.FindingsTotal, snapshot.FindingsTruncated, len(snapshot.Findings)) {
		return nil, fmt.Errorf("invalid privacy finding totals")
	}
	if snapshot.Overall.Findings != snapshot.FindingsTotal {
		return nil, fmt.Errorf("inconsistent privacy finding totals")
	}
	if len(snapshot.Reduced) > ccPrivacyReducedMax ||
		!ccPrivacyCountsValid(snapshot.ReducedShown, snapshot.ReducedTotal, snapshot.ReducedTruncated, len(snapshot.Reduced)) {
		return nil, fmt.Errorf("invalid privacy coverage totals")
	}
	if len(snapshot.Network.VPN.Defects) > ccPrivacyDefectMax ||
		!ccPrivacyCountsValid(snapshot.Network.VPN.DefectsShown, snapshot.Network.VPN.DefectsTotal,
			snapshot.Network.VPN.DefectsTruncated, len(snapshot.Network.VPN.Defects)) {
		return nil, fmt.Errorf("invalid VPN defect totals")
	}
	if !snapshot.Available {
		if snapshot.Overall.Grade != "unknown" || snapshot.Reason == "" ||
			snapshot.CategoriesTotal != 0 || snapshot.FindingsTotal != 0 || snapshot.ReducedTotal != 0 ||
			snapshot.Network.VPN.DefectsTotal != 0 {
			return nil, fmt.Errorf("unavailable privacy snapshot is incomplete")
		}
		return &snapshot, nil
	}
	if snapshot.CategoriesTotal == 0 || len(snapshot.Categories) == 0 {
		return nil, fmt.Errorf("available privacy snapshot has no category judgement")
	}
	for _, category := range snapshot.Categories {
		if category.Name == "" || !ccPrivacyTextFits(category.Name, ccPrivacyLabelLimit) ||
			!validCCPrivacyGrade(category.Grade) || len(category.Installed) > ccPrivacyAppMax ||
			!ccPrivacyCountsValid(category.InstalledShown, category.InstalledTotal,
				category.InstalledTruncated, len(category.Installed)) {
			return nil, fmt.Errorf("invalid privacy category")
		}
		if ccPrivacyGradeRank(snapshot.Overall.Grade) < ccPrivacyGradeRank(category.Grade) {
			return nil, fmt.Errorf("privacy overall grade is safer than category %q", category.Name)
		}
		for _, app := range category.Installed {
			if app.Name == "" || !ccPrivacyTextFits(app.Name, ccPrivacyLabelLimit) || !validCCPrivacyGrade(app.Grade) {
				return nil, fmt.Errorf("invalid installed privacy app")
			}
			if ccPrivacyGradeRank(category.Grade) < ccPrivacyGradeRank(app.Grade) {
				return nil, fmt.Errorf("privacy category grade is safer than installed app %q", app.Name)
			}
		}
		if category.Worst != nil && (category.Worst.Name == "" || !ccPrivacyTextFits(category.Worst.Name, ccPrivacyLabelLimit) ||
			!validCCPrivacyGrade(category.Worst.Grade)) {
			return nil, fmt.Errorf("invalid worst privacy app")
		}
		if category.Worst != nil && ccPrivacyGradeRank(category.Grade) < ccPrivacyGradeRank(category.Worst.Grade) {
			return nil, fmt.Errorf("privacy category grade is safer than its worst app")
		}
	}
	for _, finding := range snapshot.Findings {
		if finding.Title == "" || !ccPrivacyTextFits(finding.Title, ccPrivacyLabelLimit) ||
			!ccPrivacyTextFits(finding.Summary, ccPrivacySummaryLimit) || !validCCPrivacyGrade(finding.Grade) ||
			(finding.Severity != "low" && finding.Severity != "medium" && finding.Severity != "high") {
			return nil, fmt.Errorf("invalid privacy finding")
		}
	}
	for _, defect := range snapshot.Network.VPN.Defects {
		if defect.Title == "" || !ccPrivacyTextFits(defect.Title, ccPrivacyLabelLimit) ||
			!ccPrivacyTextFits(defect.Detail, ccPrivacySummaryLimit) {
			return nil, fmt.Errorf("invalid VPN defect")
		}
	}
	for _, item := range snapshot.Reduced {
		if item.Effect == "" || !ccPrivacyTextFits(item.Effect, ccPrivacySummaryLimit) {
			return nil, fmt.Errorf("invalid privacy coverage caveat")
		}
	}
	return &snapshot, nil
}

func validCCPrivacyGrade(grade string) bool {
	switch grade {
	case "green", "amber", "red", "unknown":
		return true
	default:
		return false
	}
}

func ccPrivacyGradeRank(grade string) int {
	switch grade {
	case "green":
		return 0
	case "amber":
		return 1
	case "red":
		return 2
	case "unknown":
		return 3
	default:
		return -1
	}
}

func ccPrivacyGradeChip(grade string) *ui.Node {
	word, icon, fill := "Unrated", "shield", ui.FillSoft
	switch grade {
	case "green":
		word, icon, fill = "Good", "verified_user", ui.FillAccent
	case "amber":
		word, icon, fill = "Review", "flag", ui.FillOutline
	case "red":
		word, icon, fill = "Risky", "gpp_bad", ui.FillErrorContainer
	}
	return &ui.Node{Kind: ui.KindCapsule, Key: "privacy-grade", Fill: fill, Shape: ui.ShapeStadium,
		Gap: theme.MarginXS, Padding: theme.MarginS, Children: []*ui.Node{{
			Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: icon},
				{Kind: ui.KindText, Text: word, TextRole: theme.RoleLabel, Name: word},
			},
		}}}
}

func ccPrivacyCountsValid(shown, total int, truncated bool, actual int) bool {
	return shown >= 0 && total >= shown && shown == actual && (total == shown || truncated)
}

func ccPrivacyTextFits(value string, limit int) bool {
	return len(value) <= limit
}

func ccPrivacyAuditCard(m theme.Metrics, snapshot *ccPrivacySnapshot, now time.Time, errLabel string, snapshotUnreadable bool) *ui.Node {
	rows := []*ui.Node{monitorCardTitle("Privacy audit", 0)}
	grade := "unknown"
	detail := "No audit result is available. Check that the Privacybit plugin is enabled."
	if snapshotUnreadable {
		detail = "Audit status could not be read. Open the full audit or check the Privacybit plugin status."
	} else if snapshot != nil {
		switch {
		case !snapshot.Available:
			detail = strings.TrimSpace(snapshot.Reason + " " + snapshot.Hint)
		case now.Before(snapshot.GeneratedAt):
			detail = "Last scan timestamp is in the future. Check the system clock and scan again."
		case now.Sub(snapshot.GeneratedAt) >= ccPrivacyStaleAfter:
			detail = "Last scan is stale · " + ccPrivacyAgeText(now.Sub(snapshot.GeneratedAt)) + ". Scan again to restore a current grade."
		default:
			grade = snapshot.Overall.Grade
			detail = "Updated " + ccPrivacyAgeText(now.Sub(snapshot.GeneratedAt))
			if ccPrivacyHasCoverageLimits(snapshot) {
				detail += " · Coverage limits are listed in the full audit."
			}
		}
	}
	rows = append(rows,
		&ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Overall", Name: "Overall"},
			ccPrivacyGradeChip(grade),
		}},
		&ui.Node{Kind: ui.KindText, Text: detail, Name: detail, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	)
	if errLabel != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: errLabel, Name: errLabel, TextRole: theme.RoleCaption, Tone: ui.ToneError})
	}
	open := ccSegment("shield", "Open full audit", ccPrivacyOpenAction, false)
	open.Height, open.Fill = m.StandardControl, ui.FillAccent
	rows = append(rows, open)
	return monitorCard(m, rows)
}

func ccPrivacyHasCoverageLimits(snapshot *ccPrivacySnapshot) bool {
	if snapshot.Network.TelemetryConnectionsUnknown || snapshot.ReducedTotal > 0 || snapshot.ReducedTruncated ||
		snapshot.CategoriesTruncated || snapshot.FindingsTruncated || snapshot.Network.VPN.DefectsTruncated {
		return true
	}
	for _, category := range snapshot.Categories {
		if category.InstalledTruncated {
			return true
		}
	}
	return false
}

func ccPrivacyAgeText(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%d min ago", int(age.Minutes()))
	default:
		return fmt.Sprintf("%d h ago", int(age.Hours()))
	}
}

func privacyStatusSection(label string, activity privacyActivity, detail string) *ui.Node {
	children := []*ui.Node{privacyStatusRow(label, activity)}
	if activity == privacyActive && detail != "" {
		children = append(children, &ui.Node{
			Kind: ui.KindText, Text: detail, Name: label + " details", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: children}
}

func privacyStatusRow(label string, activity privacyActivity) *ui.Node {
	status := &ui.Node{Kind: ui.KindText, Text: privacyStatus(activity), Name: privacyStatus(activity)}
	if activity == privacyUnknown {
		status.Tone = ui.ToneError
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label, Name: label},
		status,
	}}
}

func ccPower(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	tr := ccT(h, r)
	var snap services.Snapshot
	locker := ""
	if r != nil {
		snap = r.sample
		locker = r.cfg.Session.Locker
	}
	errLabel := ""
	if h != nil {
		errLabel = h.errLabel
	}
	battery := ccPowerBattery(m, snap, errLabel, tr)
	profiles := ccPowerProfiles(m, h)
	actions := ccSessionActions(m, locker, r.lockActionLabel(), tr)
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Gap: theme.MarginM,
		Children: []*ui.Node{battery, profiles, actions}}
}

func ccPowerBattery(m theme.Metrics, snap services.Snapshot, errLabel string, tr translateFunc) *ui.Node {
	rows := []*ui.Node{monitorCardTitle(cardT(tr, "session.battery", "Battery"), 0)}
	if errLabel != "" {
		rows = append([]*ui.Node{{Kind: ui.KindText, Text: errLabel, Tone: ui.ToneError}}, rows...)
	}
	b := snap.Battery
	if b == nil || !b.Present || !b.ChargeValid {
		rows = append(rows, monitorKeyValue(cardT(tr, "cc.status", "Status"), ccDash))
	} else {
		rows = append(rows,
			monitorKeyValue(cardT(tr, "cc.charge", "Charge"), fmt.Sprintf("%.0f%%", b.Charge*100)),
			monitorKeyValue(cardT(tr, "cc.state", "State"), cardT(tr, "session.state."+batteryStateLabel(b.State), batteryStateLabel(b.State))))
		if b.TimeValid {
			rows = append(rows, monitorKeyValue(cardT(tr, "cc.remaining", "Remaining"), batteryDuration(b.TimeRemaining)))
		}
	}
	card := monitorCard(m, rows)
	card.Height = 120
	return card
}

func ccPowerProfiles(m theme.Metrics, h *PanelHost) *ui.Node {
	tr := ccT(h, nil)
	rows := []*ui.Node{monitorCardTitle(cardT(tr, "session.power_profile", "Power profile"), 0)}
	if h == nil || !h.profilesOK || len(h.profiles) == 0 {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: cardT(tr, "cc.profiles_unavailable", "powerprofilesctl unavailable"), TextRole: theme.RoleCaption})
	} else {
		segments := &ui.Node{Kind: ui.KindSegmented, Key: "cc-power-profiles", Gap: sessionSegmentGap}
		for _, name := range h.profiles {
			selected := name == h.profileActive
			icon := sessionProfileIcon(name)
			if selected {
				icon = "check"
			}
			children := []*ui.Node{}
			if icon != "" {
				children = append(children, &ui.Node{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconProfile})
			}
			label := cardT(tr, "session.profile."+name, powerProfileLabel(name))
			children = append(children, &ui.Node{Kind: ui.KindText, Text: label})
			segment := &ui.Node{
				Kind: ui.KindButton, Action: "cc:profile:" + name,
				Name: label, Role: "tab", Focusable: true, Height: m.StandardControl,
				Gap: sessionSegmentContentGap, Padding: sessionSegmentPadding, Children: children,
			}
			if selected {
				segment.State |= ui.StateSelected
			}
			segments.Children = append(segments.Children, segment)
		}
		rows = append(rows, segments)
	}
	card := monitorCard(m, rows)
	card.Height = 112
	return card
}

func ccSessionActions(m theme.Metrics, locker, lockLabel string, tr translateFunc) *ui.Node {
	actions := []struct{ name, action, icon string }{
		{lockLabel, "session-lock", "lock"},
		{"Log out", "session-logout", "logout"},
		{"Suspend", "session-suspend", "bedtime"},
		{"Screen off", "session-display-off", "visibility_off"},
		{"Reboot", "session-reboot", "restart_alt"},
		{"Power off", "session-poweroff", "power_settings_new"},
	}
	if locker == "" {
		actions = actions[1:]
	}
	rows := []*ui.Node{monitorCardTitle(cardT(tr, "section.Session", "Session"), 0)}
	for _, action := range actions {
		name := cardT(tr, "session.action."+action.name, action.name)
		n := &ui.Node{
			Kind: ui.KindButton, Action: action.action, Name: name, Role: "button", Focusable: true,
			Height: m.CompactControl, Gap: theme.MarginM, Padding: m.CardPadding,
			Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: action.icon, IconSize: m.IconNormal},
				{Kind: ui.KindText, Text: name},
			},
		}
		if action.action == "session-reboot" || action.action == "session-poweroff" {
			n.Fill, n.Tone = ui.FillOutline, ui.ToneError
		}
		rows = append(rows, n)
	}
	card := monitorCard(m, rows)
	card.Height = ccSessionCardH
	return card
}

// The calendar plugin publishes a compact snapshot into its own persistent
// state; this page reads it. Key and plugin ID are the whole contract.
const (
	ccCalendarPluginID = "org.sysc.calendar"
	ccCalendarStateKey = "control_center"
)

type ccCalOccurrence struct {
	Start    string `json:"start"`
	Summary  string `json:"summary"`
	Calendar string `json:"calendar"`
	AllDay   bool   `json:"all_day"`
}

type ccCalendarSnapshot struct {
	Generated string            `json:"generated"`
	Sources   int               `json:"sources"`
	Days      map[string]int    `json:"days"`
	Upcoming  []ccCalOccurrence `json:"upcoming"`
}

// ccCalendarSnapshotFor reads the calendar plugin's published snapshot from
// the running plugin's in-memory store. No plugin, no key or malformed state
// simply means "no events"; it never breaks the page.
func ccCalendarSnapshotFor(r *Registry) *ccCalendarSnapshot {
	if r == nil {
		return nil
	}
	raw, ok := r.plugins.stateValue(ccCalendarPluginID, ccCalendarStateKey)
	if !ok {
		return nil
	}
	var snapshot ccCalendarSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil
	}
	return &snapshot
}

// ccCalendarDots is the marker strip under a day: one accent dot per event up
// to ccCalendarDotMax, or an empty strip of the same height.
func ccCalendarDots(count int) *ui.Node {
	if count <= 0 {
		return &ui.Node{Kind: ui.KindColumn, Height: ccCalendarDot}
	}
	strip := &ui.Node{Kind: ui.KindRow, Gap: ccCalendarDotGap, CenterX: true}
	for range min(count, ccCalendarDotMax) {
		strip.Children = append(strip.Children, &ui.Node{Kind: ui.KindCapsule, Width: ccCalendarDot, Fill: ui.FillAccent})
	}
	return strip
}

func ccCalendar(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	tr := ccT(h, r)
	now := time.Time{}
	if r != nil {
		now = r.now
	}
	if now.IsZero() {
		now = time.Now()
	}
	view := calendarMonth(now, h.monthDelta)
	grid := calendarGrid(view, now)
	snapshot := ccCalendarSnapshotFor(r)
	var dates []time.Time
	if snapshot != nil {
		first := time.Date(view.Year(), view.Month(), 1, 0, 0, 0, 0, view.Location())
		gridStart := first.AddDate(0, 0, -int(first.Weekday()))
		dates = make([]time.Time, len(grid.Weeks)*7)
		for i := range dates {
			dates[i] = gridStart.AddDate(0, 0, i)
		}
	}
	// Seven columns and six gaps span the card's content width.
	cellW := max((ccBodyWidth(h)-2*m.CardPadding-6*theme.MarginXS)/7, 0)
	rows := []*ui.Node{{
		Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			calendarArrow("chevron_left", "cal-prev", cardT(tr, "cc.prev_month", "Previous month"), h.theme),
			{Kind: ui.KindText, Text: view.Format("January 2006"), TextRole: theme.RoleTitle},
			calendarArrow("chevron_right", "cal-next", cardT(tr, "cc.next_month", "Next month"), h.theme),
		},
	}}
	weekdays := &ui.Node{Kind: ui.KindRow, Height: ccCalendarWeekdayH, Gap: theme.MarginXS}
	for _, day := range []string{"S", "M", "T", "W", "T", "F", "S"} {
		weekdays.Children = append(weekdays.Children, calendarCell(cellW,
			&ui.Node{Kind: ui.KindText, Text: day, TextRole: theme.RoleLabel, Tone: ui.ToneSubtle, CenterX: true}))
	}
	rows = append(rows, weekdays)
	// The footer holds up to three upcoming events, or the empty line, and the
	// Open calendar row; each is one compact control and the gap above it.
	footerH := 0
	if snapshot != nil {
		footerH = (max(min(len(snapshot.Upcoming), 3), 1) + 1) * (m.CompactControl + theme.MarginXS)
	}
	// D4: the grid is the page's principal block and grows to fill the card.
	// The card's column puts one gap under the nav row and one above each week.
	fixedH := 2*m.CardPadding + m.CompactControl + theme.MarginXS + ccCalendarWeekdayH + footerH
	weeks := max(len(grid.Weeks), 1)
	rowHeight := max((ccPageH-fixedH)/weeks-theme.MarginXS, ccCalendarRowMin)
	for wi, week := range grid.Weeks {
		row := &ui.Node{Kind: ui.KindRow, Height: rowHeight, Gap: theme.MarginXS}
		for ci, cell := range week {
			day := &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%d", cell.Day), CenterX: true, Tabular: true}
			switch {
			case cell.Today:
				day.Tone = ui.ToneAccent
			case !cell.InMonth:
				day.Tone = ui.ToneSubtle
			}
			content := []*ui.Node{day}
			if snapshot != nil {
				content = append(content, ccCalendarDots(snapshot.Days[dates[wi*7+ci].Format("2006-01-02")]))
			}
			row.Children = append(row.Children, calendarCell(cellW, content...))
		}
		rows = append(rows, row)
	}
	if snapshot != nil {
		if len(snapshot.Upcoming) == 0 {
			rows = append(rows, &ui.Node{Kind: ui.KindText, Text: cardT(tr, "cc.calendar_empty", "No upcoming events"), TextRole: theme.RoleLabel, Tone: ui.ToneSubtle, CenterX: true})
		}
		for _, occ := range snapshot.Upcoming[:min(len(snapshot.Upcoming), 3)] {
			label := ccCalendarOccurrenceLabel(occ, now)
			rows = append(rows, &ui.Node{
				Kind: ui.KindButton, Action: "plugin-calendar:open", Name: label, Role: "button", Focusable: true,
				Height: m.CompactControl, Gap: theme.MarginM, Padding: m.CardPadding,
				Children: []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel}},
			})
		}
		rows = append(rows, &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{{
			Kind: ui.KindButton, Action: "plugin-calendar:open", Name: cardT(tr, "cc.open_calendar", "Open calendar"), Role: "button", Focusable: true,
			Height: m.CompactControl, Gap: theme.MarginM, Padding: m.CardPadding,
			Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: "calendar_month", IconSize: m.IconNormal},
				{Kind: ui.KindText, Text: cardT(tr, "cc.open_calendar", "Open calendar")},
			},
		}}})
	}
	card := monitorCard(m, rows)
	card.Height = ccPageH
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Children: []*ui.Node{card}}
}

func ccCalendarOccurrenceLabel(occ ccCalOccurrence, now time.Time) string {
	when := ""
	if occ.AllDay {
		if day, err := time.Parse("2006-01-02", occ.Start); err == nil {
			when = day.Format("Mon 2 Jan")
		}
	} else if at, err := time.Parse(time.RFC3339, occ.Start); err == nil {
		if at.Format("2006-01-02") == now.Format("2006-01-02") {
			when = at.Format("15:04")
		} else {
			when = at.Format("Mon 15:04")
		}
	}
	label := when + "  " + occ.Summary
	if occ.Calendar != "" {
		label += " · " + occ.Calendar
	}
	return label
}

func ccNotifications(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	tr := ccT(h, r)
	now, dnd := time.Now(), false
	var active []protocol.Notification
	var history []protocol.HistoryEntry
	if r != nil {
		now = r.clockNow()
		_, dnd = r.dndStateAt(now)
		if r.notify != nil {
			r.notify.mu.Lock()
			for _, notification := range r.notify.active {
				active = append(active, notification)
			}
			history = append(history, r.notify.history...)
			r.notify.mu.Unlock()
		}
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Timestamp.After(history[j].Timestamp) })
	controls := &ui.Node{
		Kind: ui.KindCapsule, Height: ccSliderCapsuleH, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			ccSegment("do_not_disturb_on", cardT(tr, "notify.dnd", "Do not disturb"), "cc:dnd", dnd),
			ccSegment("delete", cardT(tr, "notify.clear_all", "Clear all"), "notify:center:clear", false),
		}}},
	}
	var cards []*ui.Node
	for _, group := range activeGroups(active) {
		latest := group.members[0]
		raster := r.notificationRasterLocked(latest.Image)
		cards = append(cards, activeGroupCardWithMetrics(group, now, false, raster, false, tr, m))
	}
	for _, entry := range history {
		raster := r.notificationRasterLocked(entry.Image)
		cards = append(cards, historyCardWithMetrics(entry, now, raster, false, tr, m))
	}
	if len(cards) == 0 {
		empty := monitorCard(m, []*ui.Node{
			{Kind: ui.KindIcon, Icon: "notifications", IconSize: m.IconLarge, Tone: ui.ToneSubtle},
			{Kind: ui.KindText, Text: cardT(tr, "notify.empty", "No notifications"), TextRole: theme.RoleLabel, Tone: ui.ToneSubtle, CenterX: true},
		})
		empty.Height = 416
		cards = append(cards, empty)
	}
	list := &ui.Node{Kind: ui.KindScroll, Height: ccNotifyListH, Gap: theme.MarginM, Children: cards}
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Gap: theme.MarginL, Children: []*ui.Node{controls, list}}
}

// ccSettings is the control centre's shortcut into settings, not a second
// settings surface. The standalone panel stays the complete one: the centre's
// body is roughly 480 logical pixels tall once the rail takes its 56, and
// rendering one settings tree into both would size-constrain every future
// section for no gain.
//
// So this page is a way in, one link per section, carrying the same glyph the
// settings rail draws so the two read as one place.
func ccSettings(h *PanelHost) *ui.Node {
	tr := ccT(h, nil)
	rows := []*ui.Node{{
		Kind: ui.KindText, Text: cardT(tr, "cc.settings_hint", "Open settings at a section."),
		TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
	}}
	for _, name := range settingsSections {
		rows = append(rows, &ui.Node{
			Kind: ui.KindRow, Gap: theme.MarginM, Height: h.metrics().StandardControl,
			Action: "settings-section:" + name, Name: cardT(tr, "section."+name, name), Role: "button", Focusable: true,
			Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: settingsSectionIcons[name]},
				{Kind: ui.KindText, Text: cardT(tr, "section."+name, name)},
			},
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: rows}
}
