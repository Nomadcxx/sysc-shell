package config

import (
	"math"
	"slices"
)

// Pointer contains only settings explicitly changed in the input page. Nil
// fields leave the compositor's configuration in charge, including defaults.
type Pointer struct {
	NaturalScroll           *bool    `json:"natural-scroll,omitempty"`
	LeftHanded              *bool    `json:"left-handed,omitempty"`
	Tap                     *bool    `json:"tap,omitempty"`
	DWT                     *bool    `json:"dwt,omitempty"`
	DisabledOnExternalMouse *bool    `json:"disabled-on-external-mouse,omitempty"`
	AccelSpeed              *float64 `json:"accel-speed,omitempty"`
	ScrollFactor            *float64 `json:"scroll-factor,omitempty"`
	AccelProfile            *string  `json:"accel-profile,omitempty"`
	ScrollMethod            *string  `json:"scroll-method,omitempty"`
	ClickMethod             *string  `json:"click-method,omitempty"`
}

// Input stores the shell's overrides separately from the user's KDL sections.
type Input struct {
	Touchpad Pointer `json:"touchpad,omitzero"`
	Mouse    Pointer `json:"mouse,omitzero"`
}

func (p Pointer) IsZero() bool { return p == (Pointer{}) }
func (in Input) IsZero() bool  { return in.Touchpad.IsZero() && in.Mouse.IsZero() }

// ValidateInput applies the same limits as Niri's device configuration. The
// managed include writer and JSON reader share it to reject invalid changes
// before they reach either configuration file.
func ValidateInput(in Input) error {
	for _, field := range []struct {
		name string
		set  bool
	}{
		{"tap", in.Mouse.Tap != nil},
		{"dwt", in.Mouse.DWT != nil},
		{"disabled-on-external-mouse", in.Mouse.DisabledOnExternalMouse != nil},
		{"click-method", in.Mouse.ClickMethod != nil},
	} {
		if field.set {
			return pathErr("input.mouse."+field.name, "is only supported for touchpads")
		}
	}
	for _, dev := range []struct {
		name string
		p    Pointer
	}{{"touchpad", in.Touchpad}, {"mouse", in.Mouse}} {
		path := "input." + dev.name + "."
		if v := dev.p.AccelSpeed; v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < -1 || *v > 1) {
			return pathErr(path+"accel-speed", "must be finite and in -1..1")
		}
		if v := dev.p.ScrollFactor; v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 100) {
			return pathErr(path+"scroll-factor", "must be finite and in 0..100")
		}
		for _, enum := range []struct {
			name    string
			value   *string
			options []string
		}{
			{"accel-profile", dev.p.AccelProfile, []string{"adaptive", "flat"}},
			{"scroll-method", dev.p.ScrollMethod, []string{"no-scroll", "two-finger", "edge", "on-button-down"}},
			{"click-method", dev.p.ClickMethod, []string{"clickfinger", "button-areas"}},
		} {
			if enum.value != nil && !slices.Contains(enum.options, *enum.value) {
				return pathErr(path+enum.name, "%q is not a valid option", *enum.value)
			}
		}
	}
	return nil
}

// Clone gives the Settings draft its own optional values.
func (in Input) Clone() Input {
	return Input{Touchpad: in.Touchpad.clone(), Mouse: in.Mouse.clone()}
}

func (p Pointer) clone() Pointer {
	return Pointer{
		NaturalScroll: cloneInputValue(p.NaturalScroll), LeftHanded: cloneInputValue(p.LeftHanded), Tap: cloneInputValue(p.Tap), DWT: cloneInputValue(p.DWT), DisabledOnExternalMouse: cloneInputValue(p.DisabledOnExternalMouse),
		AccelSpeed: cloneInputValue(p.AccelSpeed), ScrollFactor: cloneInputValue(p.ScrollFactor), AccelProfile: cloneInputValue(p.AccelProfile), ScrollMethod: cloneInputValue(p.ScrollMethod), ClickMethod: cloneInputValue(p.ClickMethod),
	}
}

func cloneInputValue[T any](v *T) *T {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
