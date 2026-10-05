package displays

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// mode projects one niri mode into the shell's own domain.
func mode(m niri.Mode) Mode {
	return Mode{Width: m.Width, Height: m.Height, RefreshMilliHz: m.RefreshMilliHz, Preferred: m.IsPreferred}
}

// advertised lists the modes an output currently offers.
func advertised(out niri.Output) []Mode {
	modes := make([]Mode, 0, len(out.Modes))
	for _, m := range out.Modes {
		modes = append(modes, mode(m))
	}
	return modes
}

// wireMode is the inverse projection, used to ask niri for a specific mode.
func wireMode(m ModePolicy) niri.Mode {
	return niri.Mode{Width: m.Width, Height: m.Height, RefreshMilliHz: m.RefreshMilliHz}
}

func outputName(out niri.Output) Name {
	return Name{Connector: out.Name, Make: out.Make, Model: out.Model, Serial: out.Serial}
}

// currentMode reports the mode an output is actually running, if it is one the
// shell can ask for again. A custom (modeline) mode has no advertised
// equivalent, so it is honestly unknown rather than approximated.
func currentMode(out niri.Output) *ModePolicy {
	if out.CurrentMode == nil || out.IsCustomMode {
		return nil
	}
	if *out.CurrentMode < 0 || *out.CurrentMode >= len(out.Modes) {
		return nil
	}
	m := out.Modes[*out.CurrentMode]
	return &ModePolicy{Width: m.Width, Height: m.Height, RefreshMilliHz: m.RefreshMilliHz}
}

// effectivePolicy is the policy the running compositor actually has, plus the
// logical geometry to fall back on. A nil field means "not honestly knowable":
// an unmapped output has no geometry, and VRR that is currently off does not
// say whether it is off or merely on demand with nothing to refresh.
func effectivePolicy(out niri.Output) (Policy, *Rect) {
	var p Policy
	var rect *Rect
	if out.Logical != nil {
		scale := out.Logical.Scale
		p.Scale = &scale
		transform := string(out.Logical.Transform)
		p.Transform = &transform
		position := Position{X: out.Logical.X, Y: out.Logical.Y}
		p.Position = &position
		rect = &Rect{Position: position, Width: int32(out.Logical.Width), Height: int32(out.Logical.Height)}
	}
	p.Mode = currentMode(out)
	// Always-on is the closest restorable approximation of a VRR that is
	// currently on: on demand is a subset of it, and an output left on is
	// usable where a missing value would leave the field unknown.
	if out.VRREnabled {
		p.VRR = &VRRPolicy{}
	}
	return p, rect
}

// beforeState snapshots one active output for a ticket. Every output is saved,
// not only the ones the draft touches, so recovery can verify what it did not
// change as well as undo what it did.
func beforeState(out niri.Output) OutputState {
	policy, rect := effectivePolicy(out)
	return OutputState{Name: outputName(out), Policy: policy, Logical: rect}
}

// planOutput turns a target policy into the commands that make the running
// compositor match it, split into the two phases the design requires. Geometry
// first: a position is expressed in logical pixels, so it cannot be planned
// honestly until the mode, scale and transform have taken effect.
//
// A field the draft leaves nil is still pinned, to the value the managed file
// implies by omitting it. That is what makes a preview honest: the file has no
// mode child, so niri picks the preferred mode; the file has no transform
// child, so the reloaded transform is Normal.
func planOutput(out niri.Output, want Policy) (geometry, layout []niri.OutputChange, err error) {
	if want.VRR != nil && !out.VRRSupported {
		return nil, nil, fmt.Errorf("displays: %s does not support variable refresh rate", out.Name)
	}
	if want.Scale != nil {
		if err := ValidateScale(*want.Scale); err != nil {
			return nil, nil, err
		}
	}
	if want.Transform != nil {
		if err := ParseTransformNative(*want.Transform); err != nil {
			return nil, nil, err
		}
	}
	if want.Mode != nil {
		change, cerr := niri.ModeValue(out, wireMode(*want.Mode))
		if cerr != nil {
			return nil, nil, cerr
		}
		geometry = append(geometry, change)
	} else {
		geometry = append(geometry, niri.ModeAuto())
	}
	if want.Scale != nil {
		change, cerr := niri.ScaleValue(*want.Scale)
		if cerr != nil {
			return nil, nil, cerr
		}
		geometry = append(geometry, change)
	} else {
		geometry = append(geometry, niri.ScaleAuto())
	}
	if want.Transform != nil {
		change, cerr := niri.TransformValue(*want.Transform)
		if cerr != nil {
			return nil, nil, cerr
		}
		geometry = append(geometry, change)
	} else {
		change, cerr := niri.TransformValue(string(niri.TransformNormal))
		if cerr != nil {
			return nil, nil, cerr
		}
		geometry = append(geometry, change)
	}
	if want.Position != nil {
		change, cerr := niri.PositionValue(want.Position.X, want.Position.Y)
		if cerr != nil {
			return nil, nil, cerr
		}
		layout = append(layout, change)
	} else {
		layout = append(layout, niri.PositionAuto())
	}
	if want.VRR != nil {
		layout = append(layout, niri.VRRValue(true, want.VRR.OnDemand))
	} else {
		layout = append(layout, niri.VRRValue(false, false))
	}
	return geometry, layout, nil
}

// restorePolicy is the policy that reproduces a pre-preview output. A mode the
// hardware no longer advertises falls back to niri's automatic choice rather
// than failing the whole restoration over a mode the user cannot select any
// more.
func restorePolicy(out niri.Output, before OutputState) Policy {
	p := before.Policy
	if p.Mode != nil {
		if _, ok := out.ModeIndex(wireMode(*p.Mode)); !ok {
			p.Mode = nil
		}
	}
	if p.Scale != nil {
		if err := ValidateScale(*p.Scale); err != nil {
			p.Scale = nil
		}
	}
	return p
}

// verifyOutput checks what the draft actually pinned. Anything the draft left
// to niri is accepted as niri reports it: an automatic mode, a custom mode, or
// on-demand VRR that is currently off are all correct outcomes rather than
// failures.
func verifyOutput(out niri.Output, want Policy) error {
	if want.Mode != nil {
		got := currentMode(out)
		if got == nil {
			return fmt.Errorf("displays: %s is not running a selectable mode", out.Name)
		}
		if *got != *want.Mode {
			return fmt.Errorf("displays: %s is running %s, not %s", out.Name, got, want.Mode)
		}
	}
	if want.Scale != nil {
		if out.Logical == nil {
			return fmt.Errorf("displays: %s is not mapped, so scale %v cannot be observed", out.Name, *want.Scale)
		}
		if out.Logical.Scale != *want.Scale {
			return fmt.Errorf("displays: %s is at scale %v, not %v", out.Name, out.Logical.Scale, *want.Scale)
		}
	}
	if want.Transform != nil {
		if out.Logical == nil {
			return fmt.Errorf("displays: %s is not mapped, so transform %s cannot be observed", out.Name, *want.Transform)
		}
		if string(out.Logical.Transform) != *want.Transform {
			return fmt.Errorf("displays: %s is transformed %s, not %s", out.Name, out.Logical.Transform, *want.Transform)
		}
	}
	if want.Position != nil {
		if out.Logical == nil {
			return fmt.Errorf("displays: %s is not mapped, so position %d,%d cannot be observed", out.Name, want.Position.X, want.Position.Y)
		}
		if out.Logical.X != want.Position.X || out.Logical.Y != want.Position.Y {
			return fmt.Errorf("displays: %s sits at %d,%d, not %d,%d", out.Name, out.Logical.X, out.Logical.Y, want.Position.X, want.Position.Y)
		}
	}
	if want.VRR != nil && !want.VRR.OnDemand && out.VRREnabled != want.VRR.OnDemand {
		return fmt.Errorf("displays: %s has variable refresh rate off, not on", out.Name)
	}
	return nil
}
