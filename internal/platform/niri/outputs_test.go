package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

// Output fixtures written from the niri-ipc 26.04 (8ed0da4) schema: every field
// name and type below is the serde form of niri_ipc::Output and its Mode and
// LogicalOutput. They are synthesized from the schema rather than captured from
// a live session, so they carry no user data.
const (
	modeLaptop60  = `{"width":1920,"height":1200,"refresh_rate":60000,"is_preferred":false}`
	modeLaptop120 = `{"width":1920,"height":1200,"refresh_rate":120030,"is_preferred":true}`

	// A laptop panel, mapped, plus a second output that niri knows but has not
	// mapped: current_mode and logical are both null there.
	replyOutputs = `{"Ok":{"Outputs":{` +
		`"DP-1":{"name":"DP-1","make":"FooCorp","model":"Bar 27","serial":null,` +
		`"physical_size":null,"modes":[` + modeLaptop60 + `,` + modeLaptop120 + `],` +
		`"current_mode":1,"is_custom_mode":false,"vrr_supported":true,"vrr_enabled":false,` +
		`"logical":{"x":0,"y":0,"width":1920,"height":1200,"scale":1.0,"transform":"Normal"}},` +
		`"eDP-1":{"name":"eDP-1","make":"Acme","model":"Laptop Panel","serial":"PANEL-1",` +
		`"physical_size":[300,190],"modes":[{"width":2560,"height":1600,"refresh_rate":60000,"is_preferred":true}],` +
		`"current_mode":null,"is_custom_mode":false,"vrr_supported":false,"vrr_enabled":false,` +
		`"logical":null}}}}`
)

func TestOutputsProjectsTheQueryReply(t *testing.T) {
	t.Parallel()
	f := startFakeNiri(t, replyOutputs)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outs, err := Outputs(ctx, f.path)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}

	// The query is a bare JSON string on a short-lived connection, not an
	// EventStream handshake.
	if got := <-f.requests; strings.TrimSpace(got) != `"Outputs"` {
		t.Fatalf("wrote %q, want %q", got, `"Outputs"`)
	}

	if len(outs) != 2 {
		t.Fatalf("got %d outputs, want 2: %+v", len(outs), outs)
	}
	// A HashMap reply has no order; the projection sorts so a snapshot diff
	// does not report churn.
	if outs[0].Name != "DP-1" || outs[1].Name != "eDP-1" {
		t.Fatalf("outputs are not sorted by name: %q, %q", outs[0].Name, outs[1].Name)
	}

	dp := outs[0]
	if dp.Make != "FooCorp" || dp.Model != "Bar 27" {
		t.Errorf("make/model = %q/%q", dp.Make, dp.Model)
	}
	if dp.Serial != "" {
		t.Errorf("serial = %q, want empty for null", dp.Serial)
	}
	if dp.HasPhysicalSize {
		t.Errorf("physical size present for null")
	}
	// Refresh stays integer millihertz in the domain.
	if len(dp.Modes) != 2 {
		t.Fatalf("got %d modes, want 2", len(dp.Modes))
	}
	if dp.Modes[1] != (Mode{Width: 1920, Height: 1200, RefreshMilliHz: 120030, IsPreferred: true}) {
		t.Errorf("mode = %+v", dp.Modes[1])
	}
	if dp.CurrentMode == nil || *dp.CurrentMode != 1 {
		t.Fatalf("current mode = %v, want index 1", dp.CurrentMode)
	}
	if !dp.VRRSupported || dp.VRREnabled {
		t.Errorf("vrr supported/enabled = %v/%v", dp.VRRSupported, dp.VRREnabled)
	}
	if dp.Logical == nil {
		t.Fatal("logical is nil for a mapped output")
	}
	if *dp.Logical != (LogicalOutput{X: 0, Y: 0, Width: 1920, Height: 1200, Scale: 1, Transform: TransformNormal}) {
		t.Errorf("logical = %+v", *dp.Logical)
	}

	edp := outs[1]
	if edp.Serial != "PANEL-1" {
		t.Errorf("serial = %q", edp.Serial)
	}
	if !edp.HasPhysicalSize || edp.PhysicalWidth != 300 || edp.PhysicalHeight != 190 {
		t.Errorf("physical size = %d x %d present=%v", edp.PhysicalWidth, edp.PhysicalHeight, edp.HasPhysicalSize)
	}
	// An absent current mode is not index zero: the output is disabled.
	if edp.CurrentMode != nil {
		t.Errorf("current mode = %v, want nil for a disabled output", *edp.CurrentMode)
	}
	if edp.Logical != nil {
		t.Errorf("logical = %+v, want nil for an unmapped output", *edp.Logical)
	}
}

func TestOutputsRejectsUnusableReplies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{
			"current mode index past the mode list",
			`{"Ok":{"Outputs":{"DP-1":{"name":"DP-1","modes":[` + modeLaptop60 + `],` +
				`"current_mode":5,"logical":null}}}}`,
			"mode index",
		},
		{
			"negative current mode index",
			`{"Ok":{"Outputs":{"DP-1":{"name":"DP-1","modes":[` + modeLaptop60 + `],` +
				`"current_mode":-1,"logical":null}}}}`,
			"mode index",
		},
		{
			"current mode present with no mode list",
			`{"Ok":{"Outputs":{"DP-1":{"name":"DP-1","modes":[],"current_mode":0,"logical":null}}}}`,
			"mode index",
		},
		{
			"transform niri does not define",
			`{"Ok":{"Outputs":{"DP-1":{"name":"DP-1","modes":[],"current_mode":null,` +
				`"logical":{"x":0,"y":0,"width":1,"height":1,"scale":1,"transform":"45"}}}}}`,
			"transform",
		},
		{
			"empty output name",
			`{"Ok":{"Outputs":{"":{"name":"","modes":[],"current_mode":null,"logical":null}}}}`,
			"name",
		},
		{
			"malformed reply",
			`{"Ok":{"Outputs":`,
			"decode",
		},
		{
			"no result",
			`{}`,
			"no result",
		},
		{
			"compositor refusal",
			`{"Err":"invalid request"}`,
			"invalid request",
		},
		{
			// A line past the transport bound must fail as a transport error,
			// not as a decode error.
			"line past the transport bound",
			`{"Ok":{"Outputs":{"DP-1":{"name":"` + strings.Repeat("x", maxLine) + `"}}}}`,
			"exceeds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := startFakeNiri(t, tc.reply)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := Outputs(ctx, f.path)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestOutputsIgnoresAdditiveFields(t *testing.T) {
	t.Parallel()
	// niri adds fields to Output over time; an unknown one must not stop a
	// reply from projecting.
	f := startFakeNiri(t, `{"Ok":{"Outputs":{"DP-1":{"name":"DP-1","make":"M","model":"D",`+
		`"future_field":{"nested":[1,2,3]},"modes":[{"width":1,"height":1,"refresh_rate":1,`+
		`"is_preferred":false,"future":null}],"current_mode":0,"logical":{"x":0,"y":0,"width":1,`+
		`"height":1,"scale":1,"transform":"Flipped90","more":true}}}}}`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outs, err := Outputs(ctx, f.path)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 1 || outs[0].Logical.Transform != TransformFlipped90 {
		t.Fatalf("outputs = %+v", outs)
	}
}

func TestOutputsStopsOnCancellationAndSilence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		cancel  bool
		timeout time.Duration
	}{
		{"cancelled before the reply", true, 2 * time.Second},
		{"peer never replies", false, 150 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			socket := startSilentNiri(t)

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			if tc.cancel {
				cancel()
			}
			_, err := Outputs(ctx, socket)
			if err == nil {
				t.Fatal("want an error")
			}
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				t.Fatalf("error %v does not wrap the context error", err)
			}
		})
	}
}

func TestSetOutputWritesValidatedRequests(t *testing.T) {
	t.Parallel()
	sample := Output{Name: "DP-1", Modes: []Mode{
		{Width: 1920, Height: 1200, RefreshMilliHz: 60000},
		{Width: 1920, Height: 1200, RefreshMilliHz: 120030, IsPreferred: true},
	}}
	scale, err := ScaleValue(1.25)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := ModeValue(sample, sample.Modes[1])
	if err != nil {
		t.Fatal(err)
	}
	transform, err := TransformValue("90")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		output string
		change OutputChange
		want   string
		reply  string
		out    Outcome
		fail   bool
	}{
		{
			"scale", "eDP-1", scale,
			`{"Output":{"output":"eDP-1","action":{"Scale":{"scale":{"Specific":1.25}}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			"automatic scale", "eDP-1", ScaleAuto(),
			`{"Output":{"output":"eDP-1","action":{"Scale":{"scale":"Automatic"}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			// Niri's ConfiguredMode takes the refresh in hertz; the domain keeps
			// integer millihertz, so the conversion belongs at this boundary.
			"mode", "DP-1", mode,
			`{"Output":{"output":"DP-1","action":{"Mode":{"mode":{"Specific":` +
				`{"width":1920,"height":1200,"refresh":120.03}}}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			"automatic mode", "DP-1", ModeAuto(),
			`{"Output":{"output":"DP-1","action":{"Mode":{"mode":"Automatic"}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			"transform", "DP-1", transform,
			`{"Output":{"output":"DP-1","action":{"Transform":{"transform":"90"}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			"position", "DP-2",
			mustPosition(t, 1920, 0),
			`{"Output":{"output":"DP-2","action":{"Position":{"position":{"Specific":{"x":1920,"y":0}}}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			"automatic position", "DP-2", PositionAuto(),
			`{"Output":{"output":"DP-2","action":{"Position":{"position":"Automatic"}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			"variable refresh rate", "DP-1", VRRValue(true, true),
			`{"Output":{"output":"DP-1","action":{"Vrr":{"vrr":{"vrr":true,"on_demand":true}}}}}`,
			`{"Ok":{"OutputConfigChanged":"Applied"}}`, OutcomeApplied, false,
		},
		{
			// A missing output means the change is stored for a later
			// connection, which is not the same as the change taking effect.
			"missing output", "DP-9", scale,
			`{"Output":{"output":"DP-9","action":{"Scale":{"scale":{"Specific":1.25}}}}}`,
			`{"Ok":{"OutputConfigChanged":"OutputWasMissing"}}`, OutcomeOutputMissing, false,
		},
		{
			"compositor refusal", "DP-1", scale,
			`{"Output":{"output":"DP-1","action":{"Scale":{"scale":{"Specific":1.25}}}}}`,
			`{"Err":"no such output"}`, "", true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := startFakeNiri(t, tc.reply)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			out, err := SetOutput(ctx, f.path, tc.output, tc.change)
			if tc.fail {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("SetOutput: %v", err)
			}
			if out != tc.out {
				t.Fatalf("outcome = %q, want %q", out, tc.out)
			}
			assertRequest(t, <-f.requests, tc.want)
		})
	}
}

func TestSetOutputRejectsUnusableChanges(t *testing.T) {
	t.Parallel()
	sample := Output{Name: "DP-1", Modes: []Mode{{Width: 1920, Height: 1200, RefreshMilliHz: 60000}}}

	for _, tc := range []struct {
		name string
		out  string
		make func() (OutputChange, error)
	}{
		{"zero scale", "DP-1", func() (OutputChange, error) { return ScaleValue(0) }},
		{"negative scale", "DP-1", func() (OutputChange, error) { return ScaleValue(-1) }},
		// niri's scale is FloatOrInt<0, 10>.
		{"scale above ten", "DP-1", func() (OutputChange, error) { return ScaleValue(10.5) }},
		{"not a number", "DP-1", func() (OutputChange, error) { return ScaleValue(math.NaN()) }},
		{"infinite scale", "DP-1", func() (OutputChange, error) { return ScaleValue(math.Inf(1)) }},
		{"undefined transform", "DP-1", func() (OutputChange, error) { return TransformValue("45") }},
		{"unadvertised mode", "DP-1", func() (OutputChange, error) {
			return ModeValue(sample, Mode{Width: 3840, Height: 2160, RefreshMilliHz: 60000})
		}},
		{"mode with no refresh", "DP-1", func() (OutputChange, error) {
			return ModeValue(sample, Mode{Width: 1920, Height: 1200})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.make(); err == nil {
				t.Fatal("want a rejected change")
			}
		})
	}

	for _, tc := range []struct {
		name   string
		output string
	}{
		{"empty output name", ""},
		{"output name with a space", "DP 1"},
		{"output name past the limit", strings.Repeat("D", maxOutputName+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			change, err := ScaleValue(1)
			if err != nil {
				t.Fatal(err)
			}
			// A rejected request never reaches the socket.
			f := startFakeNiri(t, `{"Ok":{"OutputConfigChanged":"Applied"}}`)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := SetOutput(ctx, f.path, tc.output, change); err == nil {
				t.Fatal("want a rejected output name")
			}
			select {
			case got := <-f.requests:
				t.Fatalf("wrote %q for a rejected name", got)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

func TestSetOutputCarriesTheExtremeCoordinates(t *testing.T) {
	t.Parallel()
	// The domain is i32, so a coordinate cannot overflow inside it; the checked
	// arithmetic that can overflow lives where relative positions are computed.
	change := mustPosition(t, math.MinInt32, math.MaxInt32)
	f := startFakeNiri(t, `{"Ok":{"OutputConfigChanged":"Applied"}}`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := SetOutput(ctx, f.path, "DP-2", change); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	assertRequest(t, <-f.requests,
		`{"Output":{"output":"DP-2","action":{"Position":{"position":{"Specific":`+
			`{"x":-2147483648,"y":2147483647}}}}}}`)
}

func TestLoadConfigFileWritesAnAbsolutePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body LoadConfigFile
		want string
		fail bool
	}{
		{"with a path", LoadConfigFile{Path: "/home/u/.config/niri/config.kdl"},
			`{"Action":{"LoadConfigFile":{"path":"/home/u/.config/niri/config.kdl"}}}`, false},
		{"reload the current file", LoadConfigFile{},
			`{"Action":{"LoadConfigFile":{"path":null}}}`, false},
		{"relative path", LoadConfigFile{Path: "config.kdl"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := startFakeNiri(t, replyOK)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := Action(ctx, f.path, tc.body)
			if tc.fail {
				if err == nil {
					t.Fatal("want a rejected relative path")
				}
				return
			}
			if err != nil {
				t.Fatalf("Action: %v", err)
			}
			assertRequest(t, <-f.requests, tc.want)
		})
	}
}

func mustPosition(t *testing.T, x, y int32) OutputChange {
	t.Helper()
	change, err := PositionValue(x, y)
	if err != nil {
		t.Fatal(err)
	}
	return change
}

// assertRequest compares a request line as JSON, so field order is free.
func assertRequest(t *testing.T, got, want string) {
	t.Helper()
	var gotAny, wantAny any
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &gotAny); err != nil {
		t.Fatalf("request %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantAny); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(gotAny)
	wantJSON, _ := json.Marshal(wantAny)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("wrote %s, want %s", gotJSON, wantJSON)
	}
}

// startSilentNiri accepts one connection, records nothing and never replies, so
// the client has to stop on its own deadline.
func startSilentNiri(t *testing.T) string {
	t.Helper()
	socket := filepath.Join(sockpath.Dir(t, "niri"), "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the request so the client's write cannot block, then stay quiet.
		_, _ = bufio.NewReader(conn).ReadString('\n')
		<-time.After(2 * time.Second)
	}()
	return socket
}
