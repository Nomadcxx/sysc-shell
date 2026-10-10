package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	bluetoothPowerAction = "bluetooth-power"
	bluetoothScanAction  = "bluetooth-scan"
	bluetoothRetryAction = "bluetooth-retry"
)

// bluetoothBody is the one device-management tree used by both Bluetooth
// surfaces. The Registry supplies a cached snapshot; this function never
// constructs a service or performs a bus call.
func bluetoothBody(r *Registry, h *PanelHost) *ui.Node {
	m := DefaultTheme().Metrics
	if h != nil {
		if hm := h.metrics(); hm.StandardControl > 0 {
			m = hm
		}
	}
	state := services.BluetoothState{}
	if r != nil {
		state = r.bluetoothState
	}
	if state.Prompt == nil && h != nil {
		clearBluetoothInput(h)
	}

	children := []*ui.Node{bluetoothAdapterCard(state, h, m)}
	if state.Prompt != nil {
		children = append(children, bluetoothPromptCard(h, *state.Prompt, m))
	}
	sections := state.Sections()
	if len(sections) == 0 {
		children = append(children, bluetoothEmptyCard(state, h, m))
	}
	for _, section := range sections {
		children = append(children, bluetoothSection(section, h, state, m))
	}
	if h != nil && h.errLabel != "" {
		errorRow := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
			{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError, TextRole: theme.RoleCaption},
		}}
		if h.bluetoothRetry != "" {
			errorRow.Children = append(errorRow.Children, bluetoothButton(
				bluetoothRetryAction, cardT(hostT(h), "common.retry", "Retry"), m, false))
		}
		children = append(children, errorRow)
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
}

func bluetoothAdapterCard(state services.BluetoothState, h *PanelHost, m theme.Metrics) *ui.Node {
	tr := hostT(h)
	title, detail := cardT(tr, "bt.title", "Bluetooth"), cardT(tr, "bt.ready", "Ready")
	if !state.Available {
		title, detail = cardT(tr, "bt.unavailable", "Bluetooth unavailable"), cardT(tr, "bt.no_bluez", "BlueZ is not available")
	} else if !state.Adapter.Powered {
		title, detail = cardT(tr, "bt.off", "Bluetooth is off"), cardT(tr, "bt.turn_on", "Turn it on to discover devices")
	} else if !state.AgentReady {
		detail = cardT(tr, "bt.no_agent", "Pairing prompts are unavailable")
	} else if state.Adapter.Pairable {
		detail = cardT(tr, "bt.ready_to_pair", "Ready to pair")
	}

	power := &ui.Node{
		Kind: ui.KindToggle, Width: ui.ToggleWidth, Height: ui.ToggleHeight,
		Value:  toggleValue(state.Available && state.Adapter.Powered),
		Action: bluetoothPowerAction, Name: cardT(tr, "bt.power_switch", "Bluetooth power"), Role: "switch", Focusable: true,
	}
	if !state.Available {
		power.State |= ui.StateDisabled
	}
	scanning := state.Adapter.Discovering
	if h != nil {
		scanning = h.bluetoothDiscovery
	}
	scanLabel := cardT(tr, "bt.scan", "Scan")
	if scanning {
		scanLabel = cardT(tr, "bt.stop_scan", "Stop scan")
	}
	scan := bluetoothButton(bluetoothScanAction, scanLabel, m, scanning)
	if !state.Available || !state.Adapter.Powered {
		scan.State |= ui.StateDisabled
	}
	controls := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: cardT(tr, "bt.power", "Power"), TextRole: theme.RoleCaption},
		power,
		scan,
	}}
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
		Shape: ui.ShapeCard,
		Children: []*ui.Node{
			{
				Kind: ui.KindColumn, Gap: theme.MarginM,
				Children: []*ui.Node{
					{Kind: ui.KindRow, Gap: theme.MarginL, Children: []*ui.Node{
						{Kind: ui.KindCapsule, Width: m.StandardControl, Height: m.StandardControl,
							Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium,
							Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "bluetooth", IconSize: m.IconNormal}}},
						{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
							{Kind: ui.KindText, Text: title, TextRole: theme.RoleTitle},
							{Kind: ui.KindText, Text: detail, TextRole: theme.RoleCaption},
						}},
					}},
					controls,
				},
			},
		},
	}
}

func bluetoothEmptyCard(state services.BluetoothState, h *PanelHost, m theme.Metrics) *ui.Node {
	tr := hostT(h)
	title, detail := cardT(tr, "bt.empty", "No Bluetooth devices found"), cardT(tr, "bt.hint.range", "Nothing is in range on this adapter")
	switch {
	case !state.Available:
		title, detail = cardT(tr, "bt.unavailable", "Bluetooth unavailable"), cardT(tr, "bt.reconnect_bluez", "Reconnect BlueZ to manage devices")
	case !state.Adapter.Powered:
		title, detail = cardT(tr, "bt.off", "Bluetooth is off"), cardT(tr, "bt.turn_on_radio", "Turn the radio on to discover devices")
	case state.Adapter.Discovering || (h != nil && h.bluetoothDiscovery):
		title, detail = cardT(tr, "bt.searching", "Searching for devices…"), cardT(tr, "bt.hint.appear", "Nearby devices will appear here")
	}
	return bluetoothNotice(title, detail, m)
}

func bluetoothNotice(title, detail string, m theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
		Shape: ui.ShapeCard, Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: title, TextRole: theme.RoleTitle},
			{Kind: ui.KindText, Text: detail, TextRole: theme.RoleCaption},
		}}}}
}

func bluetoothSection(section services.BluetoothSectionDevices, h *PanelHost, state services.BluetoothState, m theme.Metrics) *ui.Node {
	rows := make([]*ui.Node, 0, len(section.Devices))
	for _, device := range section.Devices {
		rows = append(rows, bluetoothDeviceRow(device, h, state, m))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: bluetoothSectionName(section.Name, hostT(h)), TextRole: theme.RoleTitle},
		{Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
			Shape: ui.ShapeCard, Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: rows}}},
	}}
}

func bluetoothSectionName(section services.BluetoothSection, tr translateFunc) string {
	switch section {
	case services.BluetoothSectionConnected:
		return cardT(tr, "bt.section.Connected", "Connected")
	case services.BluetoothSectionPaired:
		return cardT(tr, "bt.section.Paired", "Paired")
	case services.BluetoothSectionAvailable:
		return cardT(tr, "bt.section.Available", "Available")
	default:
		return cardT(tr, "bt.section.Devices", "Devices")
	}
}

func bluetoothDeviceRow(device services.BluetoothDevice, h *PanelHost, state services.BluetoothState, m theme.Metrics) *ui.Node {
	tr := hostT(h)
	label, action := bluetoothDeviceAction(device, tr)
	actionName := "bluetooth-" + action + ":" + string(device.ID)
	if device.Action != services.BluetoothActionNone {
		label = bluetoothActionProgress(device.Action, tr)
	}
	primary := bluetoothButton(actionName, label, m, false)
	if device.Action != services.BluetoothActionNone || !state.Available || !state.Adapter.Powered ||
		(action == "pair" && !state.AgentReady) {
		primary.State |= ui.StateDisabled
	}
	detailsAction := "bluetooth-details:" + string(device.ID)
	detailsLabel := fmt.Sprintf(cardT(tr, "bt.details_show", "Details for %s"), device.Alias)
	detailsIcon := "chevron_right"
	if h != nil && h.bluetoothDetails == device.ID {
		detailsIcon, detailsLabel = "chevron_left", fmt.Sprintf(cardT(tr, "bt.details_hide", "Hide details for %s"), device.Alias)
	}
	details := &ui.Node{Kind: ui.KindButton, Action: detailsAction, Name: detailsLabel, Role: "button",
		Focusable: true, Width: m.StandardControl, Height: m.StandardControl, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: detailsIcon, IconSize: m.IconNormal}}}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{primary, details}}
	leading := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: bluetoothDeviceGlyph(device.Icon), IconSize: m.IconNormal},
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: device.Alias},
			{Kind: ui.KindText, Text: bluetoothDeviceSummary(device), TextRole: theme.RoleCaption, Tabular: true},
		}},
	}}
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Height: m.StandardControl,
		Children: []*ui.Node{leading, trailing}}
	children := []*ui.Node{row}
	if h != nil && h.bluetoothDetails == device.ID {
		children = append(children, bluetoothDeviceDetails(device, state, h, m))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: children}
}

func bluetoothDeviceAction(device services.BluetoothDevice, tr translateFunc) (label, action string) {
	if device.Connected {
		return cardT(tr, "bt.action.Disconnect", "Disconnect"), "disconnect"
	}
	if device.Paired {
		return cardT(tr, "common.connect", "Connect"), "connect"
	}
	return cardT(tr, "bt.action.Pair", "Pair"), "pair"
}

func bluetoothActionProgress(action services.BluetoothAction, tr translateFunc) string {
	switch action {
	case services.BluetoothActionPair:
		return cardT(tr, "bt.progress.pairing", "Pairing…")
	case services.BluetoothActionConnect:
		return cardT(tr, "common.connecting", "Connecting…")
	case services.BluetoothActionDisconnect:
		return cardT(tr, "bt.progress.disconnecting", "Disconnecting…")
	case services.BluetoothActionTrust:
		return cardT(tr, "bt.progress.trust", "Updating trust…")
	case services.BluetoothActionForget:
		return cardT(tr, "bt.progress.forgetting", "Forgetting…")
	default:
		return cardT(tr, "bt.progress.working", "Working…")
	}
}

func bluetoothDeviceSummary(device services.BluetoothDevice) string {
	parts := make([]string, 0, 3)
	if device.Address != "" {
		parts = append(parts, device.Address)
	}
	if device.Battery != nil {
		parts = append(parts, strconv.Itoa(int(*device.Battery))+"%")
	}
	if device.RSSI != nil {
		parts = append(parts, strconv.Itoa(int(*device.RSSI))+" dBm")
	}
	return strings.Join(parts, " · ")
}

func bluetoothDeviceDetails(device services.BluetoothDevice, state services.BluetoothState, h *PanelHost, m theme.Metrics) *ui.Node {
	tr := hostT(h)
	rows := []*ui.Node{{Kind: ui.KindText, Text: cardT(tr, "bt.address", "Address") + "  " + orAbsent(device.Address), TextRole: theme.RoleCaption}}
	if device.Battery != nil {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf(cardT(tr, "bt.battery", "Battery  %d%%"), *device.Battery), TextRole: theme.RoleCaption, Tabular: true})
	}
	if device.RSSI != nil {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf(cardT(tr, "bt.signal", "Signal  %d dBm"), *device.RSSI), TextRole: theme.RoleCaption, Tabular: true})
	}
	trusted := &ui.Node{Kind: ui.KindToggle, Width: ui.ToggleWidth, Height: ui.ToggleHeight,
		Value: toggleValue(device.Trusted), Action: "bluetooth-trust:" + string(device.ID),
		Name: cardT(tr, "bt.trusted", "Trusted"), Role: "switch", Focusable: true}
	if !state.Available || !state.Adapter.Powered || device.Action != services.BluetoothActionNone {
		trusted.State |= ui.StateDisabled
	}
	rows = append(rows, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: cardT(tr, "bt.trusted", "Trusted"), TextRole: theme.RoleCaption}, trusted,
	}})
	rows = append(rows, &ui.Node{Kind: ui.KindText,
		Text: cardT(tr, "bt.trusted_hint", "Trusted permits service authorization and reconnect"), TextRole: theme.RoleCaption})
	if h.bluetoothForget == device.ID {
		rows = append(rows, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindText, Text: cardT(tr, "bt.forget_confirm", "Forget this device?"), TextRole: theme.RoleCaption},
			bluetoothButton("bluetooth-forget-cancel", cardT(tr, "common.cancel", "Cancel"), m, false),
			bluetoothButton("bluetooth-forget:"+string(device.ID), cardT(tr, "bt.forget", "Forget"), m, false),
		}})
	} else {
		rows = append(rows, bluetoothButton("bluetooth-forget-confirm:"+string(device.ID), cardT(tr, "bt.forget_device", "Forget device"), m, false))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Padding: theme.MarginXS, Children: rows}
}

func bluetoothDeviceGlyph(hint string) string {
	hint = strings.ToLower(strings.TrimSpace(hint))
	hint = strings.ReplaceAll(hint, "-", "_")
	switch hint {
	case "headset", "headphones":
		return "headphones"
	case "computer":
		return "desktop_windows"
	case "keyboard":
		return "keyboard"
	case "mouse":
		return "mouse"
	case "phone":
		return "smartphone"
	case "speaker", "audio_card", "audiocard":
		return "speaker"
	default:
		return "devices_other"
	}
}

func bluetoothButton(action, label string, m theme.Metrics, selected bool) *ui.Node {
	n := &ui.Node{Kind: ui.KindButton, Action: action, Name: label, Role: "button", Focusable: true,
		Height: m.StandardControl, Padding: m.ButtonPadding, Shape: ui.ShapeSmall,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}}}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

func bluetoothPromptCard(h *PanelHost, prompt services.BluetoothPrompt, m theme.Metrics) *ui.Node {
	tr := hostT(h)
	children := []*ui.Node{{Kind: ui.KindText, Text: bluetoothPromptTitle(prompt.Kind, tr), TextRole: theme.RoleTitle}}
	if prompt.Alias != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: prompt.Alias, TextRole: theme.RoleCaption})
	}
	switch prompt.Kind {
	case services.PairingPromptPIN, services.PairingPromptPasskey:
		field := bluetoothPromptField(h, prompt, m)
		children = append(children, field)
		children = append(children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
			bluetoothButton("bluetooth-prompt-cancel", cardT(tr, "common.cancel", "Cancel"), m, false),
			bluetoothButton("bluetooth-prompt-submit", cardT(tr, "common.submit", "Submit"), m, true),
		}})
	case services.PairingPromptDisplayPIN, services.PairingPromptDisplayPasskey:
		children = append(children,
			&ui.Node{Kind: ui.KindText, Text: prompt.Code, Tabular: true, TextRole: theme.RoleHeadline},
			&ui.Node{Kind: ui.KindText, Text: bluetoothDisplayDetail(prompt, tr), TextRole: theme.RoleCaption},
		)
	case services.PairingPromptConfirmation, services.PairingPromptAuthorization, services.PairingPromptServiceAuthorization:
		if prompt.Code != "" {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: prompt.Code, Tabular: true, TextRole: theme.RoleHeadline})
		}
		if prompt.UUID != "" {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf(cardT(tr, "bt.service", "Service %s"), prompt.UUID), TextRole: theme.RoleCaption})
		}
		children = append(children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
			bluetoothButton("bluetooth-prompt-reject", cardT(tr, "bt.reject", "Reject"), m, false),
			bluetoothButton("bluetooth-prompt-accept", cardT(tr, "bt.allow", "Allow"), m, true),
		}})
	}
	return &ui.Node{Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillAccent,
		Shape: ui.ShapeCard, Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: children}}}
}

func bluetoothPromptTitle(kind services.PairingPromptKind, tr translateFunc) string {
	switch kind {
	case services.PairingPromptPIN:
		return cardT(tr, "bt.prompt.enter_pin", "Enter PIN")
	case services.PairingPromptPasskey:
		return cardT(tr, "bt.prompt.enter_passkey", "Enter passkey")
	case services.PairingPromptDisplayPIN:
		return cardT(tr, "bt.prompt.check_pin", "Check this PIN")
	case services.PairingPromptDisplayPasskey:
		return cardT(tr, "bt.prompt.check_passkey", "Check this passkey")
	case services.PairingPromptConfirmation:
		return cardT(tr, "bt.prompt.confirm_passkey", "Confirm passkey")
	case services.PairingPromptAuthorization:
		return cardT(tr, "bt.prompt.authorize_device", "Authorize device")
	case services.PairingPromptServiceAuthorization:
		return cardT(tr, "bt.prompt.authorize_service", "Authorize service")
	default:
		return cardT(tr, "bt.prompt.title", "Bluetooth pairing")
	}
}

func bluetoothPromptField(h *PanelHost, prompt services.BluetoothPrompt, m theme.Metrics) *ui.Node {
	if h == nil {
		return &ui.Node{Kind: ui.KindTextField, Action: "bluetooth-prompt-input", Key: "bluetooth-prompt-input",
			Name: cardT(hostT(h), "bt.pairing_code", "Pairing code"), Role: "textbox", Focusable: true, Height: m.StandardControl}
	}
	if h.bluetoothPromptID != prompt.ID {
		clearBluetoothInput(h)
		h.bluetoothPromptID = prompt.ID
		h.bluetoothInput = ui.NewField("")
	}
	if h.bluetoothInput == nil {
		h.bluetoothInput = ui.NewField("")
	}
	h.bluetoothInput.Masked = prompt.Kind == services.PairingPromptPIN || prompt.Kind == services.PairingPromptPasskey
	n := h.bluetoothInput.Node(cardT(hostT(h), "bt.pairing_code", "Pairing code"))
	n.Action = "bluetooth-prompt-input"
	n.Key = "bluetooth-prompt-input"
	n.Height = m.StandardControl
	n.Tabular = true
	return n
}

func bluetoothDisplayDetail(prompt services.BluetoothPrompt, tr translateFunc) string {
	if prompt.Entered > 0 {
		return fmt.Sprintf(cardT(tr, "bt.prompt.digits", "%d of 6 digits entered on the other device"), prompt.Entered)
	}
	return cardT(tr, "bt.prompt.check_code", "Enter or check this code on the other device")
}

func clearBluetoothInput(h *PanelHost) {
	if h == nil {
		return
	}
	if h.bluetoothInput != nil {
		h.bluetoothInput.Clear()
	}
	h.bluetoothInput = nil
	h.bluetoothPromptID = 0
}

func bluetoothDeviceActionID(action, prefix string) (services.DeviceID, bool) {
	rest, ok := strings.CutPrefix(action, prefix)
	if !ok || rest == "" {
		return "", false
	}
	return services.DeviceID(rest), true
}

// activateBluetooth is called under Registry.mu. It records only host state
// there; every BlueZ operation itself runs through scheduleBluetooth.
func (h *PanelHost) activateBluetooth(r *Registry, n *ui.Node) bool {
	if h == nil || r == nil || n == nil {
		return false
	}
	if n.Action == "bluetooth-close" {
		r.closePanelLocked(h.id)
		return true
	}
	if n.Action == "bluetooth-prompt-input" {
		return true
	}
	if n.Action == bluetoothRetryAction {
		n = &ui.Node{Kind: ui.KindButton, Action: h.bluetoothRetry}
	}
	if n.Action == "bluetooth-forget-cancel" {
		h.bluetoothForget = ""
		r.rebuildPanel(h)
		return true
	}
	if id, ok := bluetoothDeviceActionID(n.Action, "bluetooth-details:"); ok {
		if h.bluetoothDetails == id {
			h.bluetoothDetails = ""
		} else {
			h.bluetoothDetails = id
		}
		r.rebuildPanel(h)
		return true
	}
	if id, ok := bluetoothDeviceActionID(n.Action, "bluetooth-forget-confirm:"); ok {
		h.bluetoothForget = id
		h.bluetoothDetails = id
		r.rebuildPanel(h)
		return true
	}
	svc := r.bluetooth
	if svc == nil {
		h.errLabel = "Bluetooth unavailable"
		r.rebuildPanel(h)
		return true
	}
	switch n.Action {
	case bluetoothPowerAction:
		want := n.Value == 0
		wasDiscovering := h.bluetoothDiscovery
		if !want {
			h.bluetoothDiscovery = false
		}
		h.errLabel, h.bluetoothRetry = "", ""
		r.scheduleBluetoothOrdered(h, bluetoothPowerAction, func() error { return svc.SetPowered(want) }, nil, func() {
			if !want && bluetoothBodyVisible(h) {
				h.bluetoothDiscovery = wasDiscovering
			}
		})
		return true
	case bluetoothScanAction:
		start := !h.bluetoothDiscovery
		h.bluetoothDiscovery = start
		h.errLabel, h.bluetoothRetry = "", ""
		r.scheduleBluetoothOrdered(h, bluetoothScanAction, func() error {
			if start {
				return svc.StartDiscovery()
			}
			return svc.StopDiscovery()
		}, nil, func() {
			if bluetoothBodyVisible(h) {
				h.bluetoothDiscovery = !start
			}
		})
		return true
	case "bluetooth-prompt-submit", "bluetooth-prompt-accept", "bluetooth-prompt-reject", "bluetooth-prompt-cancel":
		prompt := r.bluetoothState.Prompt
		if prompt == nil {
			return true
		}
		accept := n.Action == "bluetooth-prompt-submit" || n.Action == "bluetooth-prompt-accept"
		value := ""
		if h.bluetoothInput != nil {
			value = h.bluetoothInput.Text
		}
		if n.Action == "bluetooth-prompt-cancel" {
			accept = false
		}
		id := prompt.ID
		action := n.Action
		clearBluetoothInput(h)
		h.errLabel, h.bluetoothRetry = "", ""
		r.scheduleBluetooth(h, action, func() error {
			defer func() { value = "" }()
			if action == "bluetooth-prompt-cancel" || action == "bluetooth-prompt-reject" {
				return svc.CancelPrompt(id)
			}
			return svc.Respond(id, services.PairingResponse{Accept: accept, Value: value})
		}, func() {
			clearBluetoothInput(h)
			if current := r.bluetoothState.Prompt; current != nil && current.ID == id {
				r.bluetoothState.Prompt = nil
			}
		}, nil)
		return true
	}
	if id, ok := bluetoothDeviceActionID(n.Action, "bluetooth-forget:"); ok {
		h.errLabel, h.bluetoothRetry = "", ""
		r.scheduleBluetooth(h, n.Action, func() error { return svc.Forget(id) }, func() {
			h.bluetoothForget, h.bluetoothDetails = "", ""
		}, nil)
		return true
	}
	if id, ok := bluetoothDeviceActionID(n.Action, "bluetooth-trust:"); ok {
		want := n.Value == 0
		h.errLabel, h.bluetoothRetry = "", ""
		r.scheduleBluetooth(h, n.Action, func() error { return svc.SetTrusted(id, want) }, nil, nil)
		return true
	}
	for _, prefix := range []string{"bluetooth-pair:", "bluetooth-connect:", "bluetooth-disconnect:"} {
		if id, ok := bluetoothDeviceActionID(n.Action, prefix); ok {
			action := prefix
			h.errLabel, h.bluetoothRetry = "", ""
			r.scheduleBluetooth(h, n.Action, func() error {
				switch action {
				case "bluetooth-pair:":
					return svc.Pair(id)
				case "bluetooth-connect:":
					return svc.Connect(id)
				default:
					return svc.Disconnect(id)
				}
			}, nil, nil)
			return true
		}
	}
	return false
}

func (r *Registry) scheduleBluetooth(h *PanelHost, retry string, run func() error, success, failure func()) {
	r.runBluetooth(run, r.bluetoothCompletion(h, retry, success, failure))
}

func (r *Registry) scheduleBluetoothOrdered(h *PanelHost, retry string, run func() error, success, failure func()) {
	r.queueBluetooth(run, r.bluetoothCompletion(h, retry, success, failure))
}

func (r *Registry) bluetoothCompletion(h *PanelHost, retry string, success, failure func()) func(error) {
	return func(err error) {
		r.mu.Lock()
		if r.panelHosts[h.id] != h || !bluetoothBodyVisible(h) {
			r.mu.Unlock()
			return
		}
		if err != nil {
			if failure != nil {
				failure()
			}
			h.errLabel = err.Error()
			h.bluetoothRetry = retry
		} else {
			h.errLabel = ""
			h.bluetoothRetry = ""
			if success != nil {
				success()
			}
		}
		out := h.output
		r.rebuildPanel(h)
		r.mu.Unlock()
		r.publishSurface(out, panelSurfaceID(h.id))
	}
}

func (r *Registry) runBluetooth(run func() error, done func(error)) {
	go func() {
		err := run()
		if done != nil {
			done(err)
		}
	}()
}

func bluetoothBodyVisible(h *PanelHost) bool {
	return h != nil && (h.id == PanelBluetooth ||
		(h.id == PanelControlCenter && h.section == "bluetooth"))
}

func (r *Registry) startBluetoothDiscoveryLocked(h *PanelHost) {
	if !bluetoothBodyVisible(h) || h.bluetoothDiscovery || r.bluetooth == nil ||
		!r.bluetoothState.Available || !r.bluetoothState.Adapter.Powered {
		return
	}
	h.bluetoothDiscovery = true
	svc := r.bluetooth
	r.scheduleBluetoothOrdered(h, bluetoothScanAction, svc.StartDiscovery, nil, func() {
		if bluetoothBodyVisible(h) {
			h.bluetoothDiscovery = false
		}
	})
}

func (r *Registry) stopBluetoothDiscoveryLocked(h *PanelHost) {
	if !bluetoothBodyVisible(h) || !h.bluetoothDiscovery {
		return
	}
	h.bluetoothDiscovery = false
	if r.bluetooth == nil {
		return
	}
	svc := r.bluetooth
	r.scheduleBluetoothOrdered(h, bluetoothScanAction, svc.StopDiscovery, nil, func() {
		if bluetoothBodyVisible(h) {
			h.bluetoothDiscovery = true
		}
	})
}

func (r *Registry) leaveBluetoothBodyLocked(h *PanelHost) {
	if !bluetoothBodyVisible(h) {
		return
	}
	r.stopBluetoothDiscoveryLocked(h)
	r.cancelBluetoothPromptLocked(h)
}

func (r *Registry) cancelBluetoothPromptLocked(h *PanelHost) {
	if h == nil {
		return
	}
	prompt := r.bluetoothState.Prompt
	clearBluetoothInput(h)
	if prompt == nil {
		return
	}
	r.bluetoothState.Prompt = nil
	if r.bluetooth == nil {
		return
	}
	svc := r.bluetooth
	go func() { _ = svc.CancelPrompt(prompt.ID) }()
}

func (r *Registry) bluetoothHostVisibleLocked() bool {
	if h := r.panelHosts[PanelBluetooth]; h != nil && r.panelOpenLocked(PanelBluetooth) {
		return true
	}
	if h := r.panelHosts[PanelControlCenter]; h != nil && h.section == "bluetooth" &&
		r.panelOpenLocked(PanelControlCenter) {
		return true
	}
	return false
}

// queueBluetooth keeps service calls off Registry.mu while preserving their
// order across a root replacement. The queue is intentionally private to the
// one Registry-owned Bluetooth service.
func (r *Registry) queueBluetooth(run func() error, done func(error)) {
	r.bluetoothOpsMu.Lock()
	previous := r.bluetoothOpsTail
	next := make(chan struct{})
	r.bluetoothOpsTail = next
	r.bluetoothOpsMu.Unlock()
	go func() {
		if previous != nil {
			<-previous
		}
		err := run()
		close(next)
		if done != nil {
			done(err)
		}
	}()
}
