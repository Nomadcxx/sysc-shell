package wallpaper

import (
	"reflect"
	"testing"
	"time"
)

func TestEvaluateReasonSources(t *testing.T) {
	area := AutoArea{W: 1920, H: 1080}
	base := AutoInputs{Outputs: []AutoOutput{{Connector: "DP-1", Area: area, WorkspaceID: 7}}}

	cases := []struct {
		name string
		in   AutoInputs
		want map[string]Reasons
	}{
		{
			name: "idle plays",
			in:   base,
			want: map[string]Reasons{},
		},
		{
			name: "locked",
			in: func() AutoInputs {
				in := base
				in.Settings.PauseLocked = true
				in.Locked = true
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonLock.bit()},
		},
		{
			name: "locked ignored when the setting is off",
			in: func() AutoInputs {
				in := base
				in.Locked = true
				return in
			}(),
			want: map[string]Reasons{},
		},
		{
			name: "lock background",
			in: func() AutoInputs {
				in := base
				in.Settings.PauseLocked = true
				in.LockBackground = true
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonLockBackground.bit()},
		},
		{
			name: "screens off",
			in: func() AutoInputs {
				in := base
				in.ScreensOff = true
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonScreensOff.bit()},
		},
		{
			name: "on battery pause policy",
			in: func() AutoInputs {
				in := base
				in.Settings.OnBattery = OnBatteryPause
				in.Battery = AutoBattery{Present: true, OnBattery: true, Charge: 80, ChargeValid: true}
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonBattery.bit()},
		},
		{
			name: "limit policy does not pause",
			in: func() AutoInputs {
				in := base
				in.Settings.OnBattery = OnBatteryLimit
				in.Battery = AutoBattery{Present: true, OnBattery: true, Charge: 80, ChargeValid: true}
				return in
			}(),
			want: map[string]Reasons{},
		},
		{
			name: "low battery pauses under any policy",
			in: func() AutoInputs {
				in := base
				in.Settings.OnBattery = OnBatteryKeep
				in.Settings.LowBatteryPause = 20
				in.Battery = AutoBattery{Present: true, OnBattery: true, Charge: 15, ChargeValid: true}
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonLowBattery.bit()},
		},
		{
			name: "low battery threshold disabled",
			in: func() AutoInputs {
				in := base
				in.Settings.LowBatteryPause = 0
				in.Battery = AutoBattery{Present: true, OnBattery: true, Charge: 5, ChargeValid: true}
				return in
			}(),
			want: map[string]Reasons{},
		},
		{
			name: "low battery and pause policy compose",
			in: func() AutoInputs {
				in := base
				in.Settings.OnBattery = OnBatteryPause
				in.Settings.LowBatteryPause = 20
				in.Battery = AutoBattery{Present: true, OnBattery: true, Charge: 15, ChargeValid: true}
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonLowBattery.bit() | ReasonBattery.bit()},
		},
		{
			name: "ac power clears battery reasons",
			in: func() AutoInputs {
				in := base
				in.Settings.OnBattery = OnBatteryPause
				in.Settings.LowBatteryPause = 20
				in.Battery = AutoBattery{Present: true, OnBattery: false, Charge: 15, ChargeValid: true}
				return in
			}(),
			want: map[string]Reasons{},
		},
		{
			name: "missing battery reads as ac",
			in: func() AutoInputs {
				in := base
				in.Settings.OnBattery = OnBatteryPause
				in.Battery = AutoBattery{}
				return in
			}(),
			want: map[string]Reasons{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.in, nil)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Evaluate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateCoveredGeometry(t *testing.T) {
	output := AutoOutput{Connector: "DP-1", Area: AutoArea{W: 1000, H: 500}, WorkspaceID: 3}
	settings := AutoSettings{PauseCovered: true}

	cases := []struct {
		name    string
		windows []AutoWindow
		want    bool
	}{
		{
			name:    "full cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 500, HasLayout: true}},
			want:    true,
		},
		{
			name:    "exactly 95 percent covers",
			windows: []AutoWindow{{WorkspaceID: 3, W: 950, H: 475, HasLayout: true}},
			want:    true,
		},
		{
			name:    "below 95 percent in width does not cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 940, H: 500, HasLayout: true}},
			want:    false,
		},
		{
			name:    "below 95 percent in height does not cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 470, HasLayout: true}},
			want:    false,
		},
		{
			name:    "floating windows never cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 500, HasLayout: true, Floating: true}},
			want:    false,
		},
		{
			name:    "window without layout never covers",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 500}},
			want:    false,
		},
		{
			name:    "other workspace does not cover",
			windows: []AutoWindow{{WorkspaceID: 4, W: 1000, H: 500, HasLayout: true}},
			want:    false,
		},
		{
			name: "one covering window among others wins",
			windows: []AutoWindow{
				{WorkspaceID: 3, W: 400, H: 400, HasLayout: true},
				{WorkspaceID: 3, W: 1000, H: 500, HasLayout: true},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := AutoInputs{
				Settings: settings,
				Outputs:  []AutoOutput{output},
				Windows:  tc.windows,
			}
			got := Evaluate(in, nil)
			_, covered := got["DP-1"]
			if covered != tc.want {
				t.Fatalf("covered = %v, want %v (reasons %v)", covered, tc.want, got)
			}
		})
	}
}

func TestEvaluateCoveredRespectsBarOffset(t *testing.T) {
	// A 40px top bar: the working area starts at y=40 and the only window
	// covers the full output (0..1080), so it covers the working area too.
	in := AutoInputs{
		Settings: AutoSettings{PauseCovered: true},
		Outputs:  []AutoOutput{{Connector: "DP-1", Area: AutoArea{Y: 40, W: 1000, H: 460}, WorkspaceID: 1}},
		Windows:  []AutoWindow{{WorkspaceID: 1, W: 1000, H: 500, HasLayout: true}},
	}
	if got := Evaluate(in, nil); len(got) != 1 {
		t.Fatalf("full-output window must cover the bar-adjusted area, got %v", got)
	}

	// The same window floating is not enough.
	in.Windows[0].Floating = true
	if got := Evaluate(in, nil); len(got) != 0 {
		t.Fatalf("floating window must not cover, got %v", got)
	}
}

func TestEvaluateOverviewSuppressesCover(t *testing.T) {
	in := AutoInputs{
		Settings:     AutoSettings{PauseCovered: true},
		OverviewOpen: true,
		Outputs:      []AutoOutput{{Connector: "DP-1", Area: AutoArea{W: 1000, H: 500}, WorkspaceID: 1}},
		Windows:      []AutoWindow{{WorkspaceID: 1, W: 1000, H: 500, HasLayout: true}},
	}
	if got := Evaluate(in, nil); len(got) != 0 {
		t.Fatalf("overview must clear coverage, got %v", got)
	}
}

func TestEvaluateLeavesManualOut(t *testing.T) {
	in := AutoInputs{Outputs: []AutoOutput{{Connector: "DP-1", Area: AutoArea{W: 100, H: 100}, WorkspaceID: 1}}}
	for connector, reasons := range Evaluate(in, nil) {
		if reasons.Auto() != reasons {
			t.Fatalf("Evaluate produced manual for %s: %v", connector, reasons)
		}
	}
}

func TestDebouncerUncoverGrace(t *testing.T) {
	var d Debouncer
	start := time.Unix(1000, 0)

	if d.Confirm("DP-1", false, start) {
		t.Fatal("an output that was never covered must not report coverage")
	}
	if !d.Confirm("DP-1", true, start) {
		t.Fatal("cover must apply immediately")
	}
	if !d.Confirm("DP-1", false, start.Add(100*time.Millisecond)) {
		t.Fatal("uncover inside the grace window must keep coverage")
	}
	if !d.Confirm("DP-1", true, start.Add(150*time.Millisecond)) {
		t.Fatal("re-cover inside the grace window keeps coverage")
	}
	if !d.Confirm("DP-1", false, start.Add(200*time.Millisecond)) {
		t.Fatal("grace window restarts after a re-cover")
	}
	if d.Confirm("DP-1", false, start.Add(500*time.Millisecond)) {
		t.Fatal("uncover must clear after the grace window")
	}
}

func TestDebouncerFlowsThroughEvaluate(t *testing.T) {
	window := AutoWindow{WorkspaceID: 1, W: 1000, H: 500, HasLayout: true}
	base := AutoInputs{
		Settings: AutoSettings{PauseCovered: true},
		Outputs:  []AutoOutput{{Connector: "DP-1", Area: AutoArea{W: 1000, H: 500}, WorkspaceID: 1}},
		Windows:  []AutoWindow{window},
	}
	var d Debouncer
	now := time.Unix(2000, 0)

	if got := Evaluate(base, &d); !got["DP-1"].Has(ReasonOccluded) {
		t.Fatalf("covered frame must occlude, got %v", got)
	}

	moved := base
	moved.Windows = []AutoWindow{{WorkspaceID: 1, W: 400, H: 400, HasLayout: true}}
	moved.Now = now.Add(50 * time.Millisecond)
	if got := Evaluate(moved, &d); !got["DP-1"].Has(ReasonOccluded) {
		t.Fatalf("uncover inside the grace window must stay occluded, got %v", got)
	}
	moved.Now = now.Add(400 * time.Millisecond)
	if got := Evaluate(moved, &d); len(got) != 0 {
		t.Fatalf("uncover after the grace window must clear, got %v", got)
	}
}

func TestBatteryFPS(t *testing.T) {
	limit := AutoSettings{OnBattery: OnBatteryLimit}
	if got := BatteryFPS(limit, true); got != 10 {
		t.Fatalf("battery fps = %d, want 10", got)
	}
	if got := BatteryFPS(limit, false); got != 20 {
		t.Fatalf("ac fps = %d, want 20", got)
	}
	for _, policy := range []string{OnBatteryKeep, OnBatteryPause, ""} {
		if got := BatteryFPS(AutoSettings{OnBattery: policy}, true); got != 0 {
			t.Fatalf("policy %q fps = %d, want 0", policy, got)
		}
	}
}
