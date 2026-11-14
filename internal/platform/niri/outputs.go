package niri

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// requestOutputs is the Outputs query, written as a JSON string and a newline.
const requestOutputs = `"Outputs"`

// maxOutputName bounds a connector name. The name also becomes a node name in
// the native config, so an unbounded value is refused at the boundary rather
// than carried into a file.
const maxOutputName = 255

// Transform is niri's counter-clockwise output rotation. The values are the
// serde names in niri-ipc, which are also the native config's strings.
type Transform string

const (
	TransformNormal     Transform = "Normal"
	Transform90         Transform = "90"
	Transform180        Transform = "180"
	Transform270        Transform = "270"
	TransformFlipped    Transform = "Flipped"
	TransformFlipped90  Transform = "Flipped90"
	TransformFlipped180 Transform = "Flipped180"
	TransformFlipped270 Transform = "Flipped270"
)

var transforms = []Transform{
	TransformNormal, Transform90, Transform180, Transform270,
	TransformFlipped, TransformFlipped90, TransformFlipped180, TransformFlipped270,
}

// Mode is one mode an output advertises. RefreshMilliHz is niri's integer
// millihertz; converting to hertz is left to the wire and the native config,
// which each want hertz.
type Mode struct {
	Width, Height  uint16
	RefreshMilliHz uint32
	IsPreferred    bool
}

// LogicalOutput is one output as the compositor mapped it.
type LogicalOutput struct {
	X, Y      int32
	Width     uint32
	Height    uint32
	Scale     float64
	Transform Transform
}

// Output is one connected output.
type Output struct {
	Name  string
	Make  string
	Model string
	// Serial is empty when niri does not know it; the wire value is optional.
	Serial string
	// HasPhysicalSize is false when niri reports no physical size, which keeps
	// a real zero distinguishable from a missing value.
	HasPhysicalSize bool
	PhysicalWidth   uint32
	PhysicalHeight  uint32
	Modes           []Mode
	CurrentMode     *int
	IsCustomMode    bool
	VRRSupported    bool
	VRREnabled      bool
	// Logical is nil when the output is not mapped to any logical output.
	Logical *LogicalOutput
}

// ModeIndex returns where m sits in the advertised modes. A mode is identified
// by its width, height and refresh. IsPreferred describes the output's mode
// list rather than the mode, so a mode read back from an output and offered
// straight back to ModeValue has to match even though this shell is not told
// which of the output's modes it prefers.
func (o Output) ModeIndex(m Mode) (int, bool) {
	for i, candidate := range o.Modes {
		if candidate.Width == m.Width && candidate.Height == m.Height &&
			candidate.RefreshMilliHz == m.RefreshMilliHz {
			return i, true
		}
	}
	return 0, false
}

// Outcome is the compositor's acknowledgement of an output change. Neither
// value reports a hardware result: a change is queued and reported applied
// before the panel shows it.
type Outcome string

// The values are niri's own OutputConfigChanged names, so reading the
// acknowledgement cannot disagree with the wire.
const (
	// OutcomeApplied means the output was connected and the change was queued.
	OutcomeApplied Outcome = "Applied"
	// OutcomeOutputMissing means the output was not connected, so the change
	// was stored for a later connection. It is not a success.
	OutcomeOutputMissing Outcome = "OutputWasMissing"
)

// Outputs queries every connected output. The result is sorted by name because
// the wire reply is a hash map and an unsorted projection would report churn
// between identical states.
func Outputs(ctx context.Context, socketPath string) ([]Output, error) {
	ok, err := call(ctx, socketPath, []byte(requestOutputs))
	if err != nil {
		return nil, err
	}
	var reply struct {
		Outputs map[string]wireOutput `json:"Outputs"`
	}
	if err := json.Unmarshal(ok, &reply); err != nil {
		return nil, fmt.Errorf("niri: decode outputs reply: %w", err)
	}

	outs := make([]Output, 0, len(reply.Outputs))
	for key, w := range reply.Outputs {
		out, err := w.project()
		if err != nil {
			return nil, fmt.Errorf("niri: output %q: %w", key, err)
		}
		outs = append(outs, out)
	}
	slices.SortFunc(outs, func(a, b Output) int { return strings.Compare(a.Name, b.Name) })
	return outs, nil
}

// OutputChange is one validated change to a single output. It is sealed: the
// constructors below are the only way to build one, so anything that reaches
// the socket has already passed the bounds niri itself enforces.
type OutputChange interface {
	outputAction() any
}

type outputChange struct{ action any }

func (c outputChange) outputAction() any { return c.action }

// ScaleAuto lets the compositor pick the scale.
func ScaleAuto() OutputChange {
	return outputChange{map[string]any{"Scale": map[string]any{"scale": "Automatic"}}}
}

// ScaleValue returns a scale change. Niri's scale is FloatOrInt<0, 10>, so the
// value must be finite and within (0, 10].
func ScaleValue(f float64) (OutputChange, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f > 10 {
		return nil, fmt.Errorf("niri: scale %v is outside (0, 10]", f)
	}
	return outputChange{map[string]any{"Scale": map[string]any{"scale": map[string]any{"Specific": f}}}}, nil
}

// ModeAuto lets the compositor pick the mode.
func ModeAuto() OutputChange {
	return outputChange{map[string]any{"Mode": map[string]any{"mode": "Automatic"}}}
}

// ModeValue returns a mode change. The mode must be one out advertises: niri
// would otherwise store a custom mode, which is a different request with its
// own validation. The advertised refresh is never zero, so a zero here means
// the mode did not come from out.
func ModeValue(out Output, m Mode) (OutputChange, error) {
	if m.RefreshMilliHz == 0 {
		return nil, fmt.Errorf("niri: mode %dx%d has no refresh rate", m.Width, m.Height)
	}
	if _, ok := out.ModeIndex(m); !ok {
		return nil, fmt.Errorf("niri: mode %dx%d@%d is not advertised by %s",
			m.Width, m.Height, m.RefreshMilliHz, out.Name)
	}
	return outputChange{map[string]any{"Mode": map[string]any{"mode": map[string]any{
		"Specific": wireConfiguredMode{
			Width:   m.Width,
			Height:  m.Height,
			Refresh: float64(m.RefreshMilliHz) / 1000,
		},
	}}}}, nil
}

// wireConfiguredMode is niri's ConfiguredMode, whose refresh is hertz.
type wireConfiguredMode struct {
	Width   uint16  `json:"width"`
	Height  uint16  `json:"height"`
	Refresh float64 `json:"refresh"`
}

// TransformValue returns a transform change, refusing a rotation niri does not
// define rather than letting the compositor reject the whole request.
func TransformValue(name string) (OutputChange, error) {
	t := Transform(name)
	if !slices.Contains(transforms, t) {
		return nil, fmt.Errorf("niri: %q is not a known transform", name)
	}
	return outputChange{map[string]any{"Transform": map[string]any{"transform": name}}}, nil
}

// PositionAuto lets the compositor place the output.
func PositionAuto() OutputChange {
	return outputChange{map[string]any{"Position": map[string]any{"position": "Automatic"}}}
}

// PositionValue returns an explicit logical position. The coordinates are
// already i32, so they cannot overflow here; the checked arithmetic that can
// overflow belongs where a position is derived from measured sizes.
func PositionValue(x, y int32) (OutputChange, error) {
	return outputChange{map[string]any{"Position": map[string]any{
		"position": map[string]any{"Specific": wireConfiguredPosition{X: x, Y: y}},
	}}}, nil
}

type wireConfiguredPosition struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// VRRValue returns a variable refresh rate change. Niri's rule is that
// on_demand only matters while vrr is on.
func VRRValue(enabled, onDemand bool) OutputChange {
	return outputChange{map[string]any{"Vrr": map[string]any{
		"vrr": wireVrr{Vrr: enabled, OnDemand: onDemand},
	}}}
}

type wireVrr struct {
	Vrr      bool `json:"vrr"`
	OnDemand bool `json:"on_demand"`
}

// SetOutput applies one change to one output and returns the compositor's
// acknowledgement. The acknowledgement is a queued result, so a caller that
// needs the hardware state must query Outputs again.
func SetOutput(ctx context.Context, socketPath, output string, change OutputChange) (Outcome, error) {
	if err := checkOutputName(output); err != nil {
		return "", err
	}
	if change == nil {
		return "", errors.New("niri: no output change")
	}
	payload, err := json.Marshal(map[string]any{"Output": map[string]any{
		"output": output,
		"action": change.outputAction(),
	}})
	if err != nil {
		return "", fmt.Errorf("niri: encode output action: %w", err)
	}
	ok, err := call(ctx, socketPath, payload)
	if err != nil {
		return "", err
	}
	var reply struct {
		Changed string `json:"OutputConfigChanged"`
	}
	if err := json.Unmarshal(ok, &reply); err != nil {
		return "", fmt.Errorf("niri: decode output reply: %w", err)
	}
	switch Outcome(reply.Changed) {
	case OutcomeApplied:
		return OutcomeApplied, nil
	case OutcomeOutputMissing:
		return OutcomeOutputMissing, nil
	}
	// An acknowledgement this package does not know is not a success.
	return "", fmt.Errorf("niri: unrecognised output acknowledgement %q", reply.Changed)
}

// checkOutputName refuses a name the compositor would treat as a different
// output, or that could not be written into a native config node.
func checkOutputName(name string) error {
	switch {
	case name == "":
		return errors.New("niri: output name is empty")
	case len(name) > maxOutputName:
		return fmt.Errorf("niri: output name is %d bytes, over the %d byte limit", len(name), maxOutputName)
	case !utf8.ValidString(name):
		return fmt.Errorf("niri: output name is not valid UTF-8")
	case strings.ContainsAny(name, " \t\n\r"):
		return fmt.Errorf("niri: output name %q contains whitespace", name)
	}
	return nil
}

// wireOutput is the serde form of niri_ipc::Output. Fields this package does
// not project are absent, and an unknown field in a reply decodes and is
// ignored, so niri can add output fields without breaking the query.
type wireOutput struct {
	Name         string       `json:"name"`
	Make         string       `json:"make"`
	Model        string       `json:"model"`
	Serial       *string      `json:"serial"`
	PhysicalSize *[2]uint32   `json:"physical_size"`
	Modes        []wireMode   `json:"modes"`
	CurrentMode  *int         `json:"current_mode"`
	IsCustomMode bool         `json:"is_custom_mode"`
	VRRSupported bool         `json:"vrr_supported"`
	VRREnabled   bool         `json:"vrr_enabled"`
	Logical      *wireLogical `json:"logical"`
}

type wireMode struct {
	Width       uint16 `json:"width"`
	Height      uint16 `json:"height"`
	RefreshRate uint32 `json:"refresh_rate"`
	IsPreferred bool   `json:"is_preferred"`
}

type wireLogical struct {
	X         int32   `json:"x"`
	Y         int32   `json:"y"`
	Width     uint32  `json:"width"`
	Height    uint32  `json:"height"`
	Scale     float64 `json:"scale"`
	Transform string  `json:"transform"`
}

// project turns one wire output into the domain, refusing a value the domain
// cannot represent. A current mode outside the mode list is refused rather
// than clamped, because clamping would report a mode the output is not using.
func (w wireOutput) project() (Output, error) {
	if err := checkOutputName(w.Name); err != nil {
		return Output{}, err
	}
	modes := make([]Mode, 0, len(w.Modes))
	for _, m := range w.Modes {
		modes = append(modes, Mode{
			Width:          m.Width,
			Height:         m.Height,
			RefreshMilliHz: m.RefreshRate,
			IsPreferred:    m.IsPreferred,
		})
	}

	out := Output{
		Name:         w.Name,
		Make:         w.Make,
		Model:        w.Model,
		Modes:        modes,
		IsCustomMode: w.IsCustomMode,
		VRRSupported: w.VRRSupported,
		VRREnabled:   w.VRREnabled,
	}
	if w.Serial != nil {
		out.Serial = *w.Serial
	}
	if w.PhysicalSize != nil {
		out.HasPhysicalSize = true
		out.PhysicalWidth = w.PhysicalSize[0]
		out.PhysicalHeight = w.PhysicalSize[1]
	}
	if w.CurrentMode != nil {
		if *w.CurrentMode < 0 || *w.CurrentMode >= len(modes) {
			return Output{}, fmt.Errorf("current mode index %d is outside its %d advertised modes",
				*w.CurrentMode, len(modes))
		}
		index := *w.CurrentMode
		out.CurrentMode = &index
	}
	if w.Logical != nil {
		t := Transform(w.Logical.Transform)
		if !slices.Contains(transforms, t) {
			return Output{}, fmt.Errorf("logical transform %q is not a known transform", w.Logical.Transform)
		}
		out.Logical = &LogicalOutput{
			X:         w.Logical.X,
			Y:         w.Logical.Y,
			Width:     w.Logical.Width,
			Height:    w.Logical.Height,
			Scale:     w.Logical.Scale,
			Transform: t,
		}
	}
	return out, nil
}
