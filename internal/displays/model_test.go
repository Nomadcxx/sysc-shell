package displays

import (
	"math"
	"strings"
	"testing"
)

// rect builds a Rect without an embedded-literal promoted field, which the
// module's Go version rejects in a composite literal.
func rect(x, y, w, h int32) Rect {
	return Rect{Position: Position{X: x, Y: y}, Width: w, Height: h}
}

func TestValidateScale(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   float64
		ok   bool
	}{
		{"preset 1", 1, true},
		{"preset 1.25", 1.25, true},
		{"between presets", 1.3, true},
		{"just under the bound", 9.999, true},
		{"zero", 0, false},
		{"negative", -1, false},
		{"niri's exclusive upper bound", 10, false},
		{"above the bound", 10.001, false},
		{"NaN", math.NaN(), false},
		{"positive infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateScale(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("ValidateScale(%v) = %v, want nil", tc.in, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("ValidateScale(%v) = nil, want an error", tc.in)
			}
		})
	}
}

// A scale that is not a preset is legal, because niri's own bound is a range
// and a saved value must never be snapped to the nearest preset.
func TestIsPreset(t *testing.T) {
	for _, f := range ScalePresets {
		if !IsPreset(f) {
			t.Errorf("IsPreset(%v) = false for a value in ScalePresets", f)
		}
	}
	for _, f := range []float64{1.3, 0.5, 3, 2.5} {
		if IsPreset(f) {
			t.Errorf("IsPreset(%v) = true, want false", f)
		}
	}
}

func TestValidateMode(t *testing.T) {
	advertised := []Mode{
		{Width: 3440, Height: 1440, RefreshMilliHz: 60030, Preferred: true},
		{Width: 3440, Height: 1440, RefreshMilliHz: 120030},
		{Width: 1920, Height: 1080, RefreshMilliHz: 60000},
	}
	for _, tc := range []struct {
		name string
		want ModePolicy
		ok   bool
	}{
		{"advertised, same millihertz", ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 60000}, true},
		{"advertised, rounded from hertz", ModePolicy{Width: 3440, Height: 1440, RefreshMilliHz: 120030}, true},
		{"zero refresh is not a mode", ModePolicy{Width: 1920, Height: 1080}, false},
		{"height not advertised", ModePolicy{Width: 1920, Height: 1200, RefreshMilliHz: 60000}, false},
		{"refresh not advertised", ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 60001}, false},
		{"custom from a plain advertised mode", ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 60000, Custom: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMode(advertised, tc.want)
			if tc.ok != (err == nil) {
				t.Fatalf("ValidateMode(%v) = %v, want ok=%v", tc.want, err, tc.ok)
			}
		})
	}
}

// A custom mode is only legal where the output already advertises one,
// because the shell has nothing else to describe the shape with.
func TestValidateModeCustomFromCustom(t *testing.T) {
	advertised := []Mode{{Width: 1920, Height: 1080, RefreshMilliHz: 60000, Custom: true}}
	if err := ValidateMode(advertised, ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 60000, Custom: true}); err != nil {
		t.Fatalf("custom mode on a custom-advertising output: %v", err)
	}
}

func TestValidateArrangement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rects map[string]Rect
		ok    bool
	}{
		{"one output alone", map[string]Rect{"DP-1": rect(0, 0, 1920, 1080)}, true},
		{
			"side by side touching",
			map[string]Rect{
				"eDP-1": rect(0, 0, 1920, 1080),
				"DP-1":  rect(1920, 0, 3440, 1440),
			},
			true,
		},
		{
			"stacked touching",
			map[string]Rect{
				"eDP-1": rect(0, 0, 1920, 1080),
				"DP-1":  rect(0, 1080, 1920, 1080),
			},
			true,
		},
		{
			"overlapping",
			map[string]Rect{
				"eDP-1": rect(0, 0, 1920, 1080),
				"DP-1":  rect(100, 0, 1920, 1080),
			},
			false,
		},
		{
			"corner contact is not adjacency",
			map[string]Rect{
				"eDP-1": rect(0, 0, 1920, 1080),
				"DP-1":  rect(1920, 1080, 1920, 1080),
			},
			false,
		},
		{
			// A vertical gap with the outputs still overlapping horizontally
			// is fine: the pointer crosses the gap without leaving either
			// output, so only full separation needs an edge to share.
			"a vertical gap with horizontal overlap",
			map[string]Rect{
				"eDP-1": rect(0, 0, 1920, 1080),
				"DP-1":  rect(0, 2000, 1920, 1080),
			},
			true,
		},
		{
			"far apart with no shared edge",
			map[string]Rect{
				"eDP-1": rect(0, 0, 1920, 1080),
				"DP-1":  rect(9000, 5000, 1920, 1080),
			},
			false,
		},
		{
			"zero size",
			map[string]Rect{"DP-1": rect(0, 0, 0, 1080)},
			false,
		},
		{
			"negative size",
			map[string]Rect{"DP-1": rect(0, 0, 1920, -1)},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArrangement(tc.rects)
			if tc.ok != (err == nil) {
				t.Fatalf("ValidateArrangement = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

// The arrangement check must not depend on map iteration order, so it is run
// again on a reversed insertion order to catch a one-sided comparison.
func TestValidateArrangementIsOrderIndependent(t *testing.T) {
	forward := map[string]Rect{
		"a": rect(0, 0, 1920, 1080),
		"b": rect(1920, 0, 1920, 1080),
		"c": rect(3840, 0, 1920, 1080),
	}
	reversed := map[string]Rect{
		"c": rect(3840, 0, 1920, 1080),
		"b": rect(1920, 0, 1920, 1080),
		"a": rect(0, 0, 1920, 1080),
	}
	for i := 0; i < 20; i++ {
		if err := ValidateArrangement(forward); err != nil {
			t.Fatalf("valid chain rejected: %v", err)
		}
		if err := ValidateArrangement(reversed); err != nil {
			t.Fatalf("valid chain rejected: %v", err)
		}
	}
}

func TestModePolicyString(t *testing.T) {
	for _, tc := range []struct {
		in   ModePolicy
		want string
	}{
		{ModePolicy{Width: 3440, Height: 1440, RefreshMilliHz: 60030}, "3440x1440@60.030"},
		{ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 120030}, "1920x1080@120.030"},
		{ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 1000}, "1920x1080@1.000"},
	} {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("ModePolicy(%v).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Policy.String must name every absent field, so a blank line in a diff never
// reads as a claim that a field is automatic or off.
func TestPolicyStringNamesEveryField(t *testing.T) {
	scale := 1.5
	for _, tc := range []struct {
		name string
		p    Policy
		want []string
	}{
		{"empty", Policy{}, []string{"automatic", "off"}},
		{"scale only", Policy{Scale: &scale}, []string{"1.5", "mode automatic", "transform normal", "position automatic", "vrr off"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.String()
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("Policy.String() = %q, missing %q", got, w)
				}
			}
		})
	}
}

func TestPolicyEqual(t *testing.T) {
	a, b := 1.5, 1.5
	mode := ModePolicy{Width: 1920, Height: 1080, RefreshMilliHz: 60030}
	pos := Position{X: 1, Y: 2}
	base := Policy{Scale: &a, Mode: &mode, Position: &pos}
	same := Policy{Scale: &b, Mode: &mode, Position: &pos}
	if !base.Equal(same) {
		t.Errorf("policies with equal values compared unequal")
	}

	other := 2.0
	if base.Equal(Policy{Scale: &other, Mode: &mode, Position: &pos}) {
		t.Errorf("policies with different scales compared equal")
	}
	if base.Equal(Policy{Scale: &a, Mode: &mode}) {
		t.Errorf("policy with a position compared equal to one without")
	}
	if !(Policy{}).Equal(Policy{}) {
		t.Errorf("an all-nil policy must equal the zero policy")
	}
}
