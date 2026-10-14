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
				in.Settings.PauseLocked = true
				in.ScreensOff = true
				return in
			}(),
			want: map[string]Reasons{"DP-1": ReasonScreensOff.bit()},
		},
		{
			name: "screens off ignored when the setting is off",
			in: func() AutoInputs {
				in := base
				in.ScreensOff = true
				return in
			}(),
			want: map[string]Reasons{},
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
			name: "charge at the threshold does not pause",
			in: func() AutoInputs {
				in := base
				in.Settings.LowBatteryPause = 20
				in.Battery = AutoBattery{Present: true, OnBattery: true, Charge: 20, ChargeValid: true}
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

// TestEvaluateCoveredGeometry covers tiles whose view position is known, the
// shape every Niri release sends for floating windows and older releases
// send for tiled ones.
func TestEvaluateCoveredGeometry(t *testing.T) {
	output := AutoOutput{Connector: "DP-1", Area: AutoArea{W: 1000, H: 500}, OutputW: 1000, OutputH: 540, WorkspaceID: 3}
	settings := AutoSettings{PauseCovered: true}

	cases := []struct {
		name    string
		windows []AutoWindow
		want    bool
	}{
		{
			name:    "full cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 500, HasLayout: true, TileInView: true}},
			want:    true,
		},
		{
			name:    "exactly 95 percent covers",
			windows: []AutoWindow{{WorkspaceID: 3, W: 950, H: 475, HasLayout: true, TileInView: true}},
			want:    true,
		},
		{
			name:    "below 95 percent in width does not cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 940, H: 500, HasLayout: true, TileInView: true}},
			want:    false,
		},
		{
			name:    "below 95 percent in height does not cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 470, HasLayout: true, TileInView: true}},
			want:    false,
		},
		{
			name:    "maximised column scrolled half out of view does not cover",
			windows: []AutoWindow{{WorkspaceID: 3, X: -500, W: 1000, H: 500, HasLayout: true, TileInView: true}},
			want:    false,
		},
		{
			name:    "a floating window at cover size covers",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 500, HasLayout: true, TileInView: true, Floating: true}},
			want:    true,
		},
		{
			name:    "a floating window below cover size does not cover",
			windows: []AutoWindow{{WorkspaceID: 3, W: 800, H: 400, HasLayout: true, TileInView: true, Floating: true}},
			want:    false,
		},
		{
			name:    "window without layout never covers",
			windows: []AutoWindow{{WorkspaceID: 3, W: 1000, H: 500}},
			want:    false,
		},
		{
			name:    "other workspace does not cover",
			windows: []AutoWindow{{WorkspaceID: 4, W: 1000, H: 500, HasLayout: true, TileInView: true}},
			want:    false,
		},
		{
			name: "one covering window among others wins",
			windows: []AutoWindow{
				{WorkspaceID: 3, W: 400, H: 400, HasLayout: true, TileInView: true},
				{WorkspaceID: 3, W: 1000, H: 500, HasLayout: true, TileInView: true},
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

// TestEvaluateCoveredWithoutTilePosition is the Niri 26.04 shape: tiled
// windows, fullscreen ones included, carry a tile size and no view position.
// A tile the size of the whole output is a fullscreen window and covers; a
// tile that only spans the working area cannot be proven to be in view, so
// it does not.
func TestEvaluateCoveredWithoutTilePosition(t *testing.T) {
	cases := []struct {
		name    string
		outputW float64
		outputH float64
		windows []AutoWindow
		want    bool
	}{
		{
			name: "tile the size of the output covers", outputW: 1000, outputH: 540,
			windows: []AutoWindow{{WorkspaceID: 7, W: 1000, H: 540, HasLayout: true}},
			want:    true,
		},
		{
			name: "tile below 95 percent of the output does not cover", outputW: 1000, outputH: 540,
			windows: []AutoWindow{{WorkspaceID: 7, W: 900, H: 540, HasLayout: true}},
			want:    false,
		},
		{
			name: "tile the size of the working area does not cover", outputW: 1000, outputH: 1000,
			windows: []AutoWindow{{WorkspaceID: 7, W: 1000, H: 800, HasLayout: true}},
			want:    false,
		},
		{
			name: "unknown output size cannot match", outputW: 0, outputH: 0,
			windows: []AutoWindow{{WorkspaceID: 7, W: 1000, H: 540, HasLayout: true}},
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := AutoInputs{
				Settings: AutoSettings{PauseCovered: true},
				Outputs: []AutoOutput{{
					Connector: "DP-1", Area: AutoArea{W: 1000, H: 500},
					OutputW: tc.outputW, OutputH: tc.outputH, WorkspaceID: 7,
				}},
				Windows: tc.windows,
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
	// A 40px top bar: the working area starts at y=40. A positioned window
	// that covers the full output therefore covers the working area too.
	in := AutoInputs{
		Settings: AutoSettings{PauseCovered: true},
		Outputs: []AutoOutput{{
			Connector: "DP-1", Area: AutoArea{Y: 40, W: 1000, H: 500},
			OutputW: 1000, OutputH: 540, WorkspaceID: 1,
		}},
		Windows: []AutoWindow{{WorkspaceID: 1, W: 1000, H: 540, HasLayout: true, TileInView: true}},
	}
	if got := Evaluate(in, nil); len(got) != 1 {
		t.Fatalf("full-output window must cover the bar-adjusted area, got %v", got)
	}

	// A window that starts below the bar and fills the working area covers.
	in.Windows[0].Y = 40
	in.Windows[0].H = 500
	if got := Evaluate(in, nil); len(got) != 1 {
		t.Fatalf("working-area-sized window must cover, got %v", got)
	}

	// The same window positioned past the working area does not.
	in.Windows[0].Y = 540
	if got := Evaluate(in, nil); len(got) != 0 {
		t.Fatalf("a window below the working area must not cover, got %v", got)
	}
}

func TestEvaluateOverviewSuppressesCover(t *testing.T) {
	in := AutoInputs{
		Settings:     AutoSettings{PauseCovered: true},
		OverviewOpen: true,
		Outputs:      []AutoOutput{{Connector: "DP-1", Area: AutoArea{W: 1000, H: 500}, WorkspaceID: 1}},
		Windows:      []AutoWindow{{WorkspaceID: 1, W: 1000, H: 500, HasLayout: true, TileInView: true}},
	}
	if got := Evaluate(in, nil); len(got) != 0 {
		t.Fatalf("overview must clear coverage, got %v", got)
	}
	// The same window with the overview closed covers, so the assertion
	// above tests the suppression and not a window that never matched.
	in.OverviewOpen = false
	if got := Evaluate(in, nil); !got["DP-1"].Has(ReasonOccluded) {
		t.Fatalf("a full-size window without the overview must cover, got %v", got)
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
	window := AutoWindow{WorkspaceID: 1, W: 1000, H: 500, HasLayout: true, TileInView: true}
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
	moved.Windows = []AutoWindow{{WorkspaceID: 1, W: 400, H: 400, HasLayout: true, TileInView: true}}
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

func TestDebouncerPendingTracksTheGraceWindow(t *testing.T) {
	var d Debouncer
	now := time.Unix(1700000000, 0)
	d.Confirm("DP-1", true, now)
	if d.Pending() {
		t.Fatal("a covered output reported a pending uncover")
	}
	d.Confirm("DP-1", false, now)
	if !d.Pending() {
		t.Fatal("the uncover grace window was not reported pending")
	}
	if d.Confirm("DP-1", false, now.Add(UncoverDelay)) {
		t.Fatal("the grace window elapsed but the output still read covered")
	}
	if d.Pending() {
		t.Fatal("the grace window stayed pending after it elapsed")
	}
}

// TestDebouncerForgetsDepartedConnectors keeps a pending uncover grace from
// outliving the output it belongs to: the driver re-arms its wake-up while
// Pending is true, so a connector that never returns must not hold it open.
func TestDebouncerForgetsDepartedConnectors(t *testing.T) {
	var d Debouncer
	now := time.Unix(1700000000, 0)
	keep := AutoOutput{Connector: "DP-1", Area: AutoArea{W: 1000, H: 500}, OutputW: 1000, OutputH: 540, WorkspaceID: 1}
	gone := AutoOutput{Connector: "DP-2", Area: AutoArea{W: 1000, H: 500}, OutputW: 1000, OutputH: 540, WorkspaceID: 2}
	covered := []AutoWindow{
		{WorkspaceID: 1, W: 1000, H: 540, HasLayout: true},
		{WorkspaceID: 2, W: 1000, H: 540, HasLayout: true},
	}

	in := AutoInputs{
		Settings: AutoSettings{PauseCovered: true},
		Outputs:  []AutoOutput{keep, gone},
		Windows:  covered,
		Now:      now,
	}
	Evaluate(in, &d)

	// DP-2 stops being covered, starting its grace window; DP-1 stays put.
	in.Windows = covered[:1]
	in.Now = now.Add(10 * time.Millisecond)
	Evaluate(in, &d)
	if !d.Pending() {
		t.Fatal("the uncover grace window was not pending")
	}

	// DP-2 disconnects while its grace window is open.
	in.Outputs = []AutoOutput{keep}
	Evaluate(in, &d)
	if d.Pending() {
		t.Fatal("a departed connector kept the grace window pending")
	}
}
