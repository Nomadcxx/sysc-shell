package plugin

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestLegacyNamesMapToTheUnionOfTheirParts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name Capability
		want []Capability
	}{
		{CapState, []Capability{CapStateRead, CapStateWrite}},
		{CapWallpaper, []Capability{CapWallpaperRead, CapWallpaperWrite}},
		{CapScreenshot, []Capability{CapScreenshotCapture, CapScreenshotRead}},
		{CapPanels, []Capability{CapPanels, CapOutputsRead}},
		{CapFloatingSurfaces, []Capability{CapFloatingSurfaces, CapOutputsRead}},
		{CapFiles, []Capability{CapFiles}},
		{CapFilesBrowser, []Capability{CapFiles}},
		{CapStateRead, []Capability{CapStateRead}},
		{CapNotifications, []Capability{CapNotifications}},
	} {
		got := grantsOf(tc.name)
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s → %v, want %v", tc.name, got, want)
		}
	}
}

func TestNarrowGrantsAdmitOnlyTheirCalls(t *testing.T) {
	t.Parallel()
	env := func(minor int, caps ...Capability) CallEnv {
		return CallEnv{Granted: caps, Protocol: v1.Version{Major: 1, Minor: minor}}
	}
	for _, tc := range []struct {
		name  string
		env   CallEnv
		call  v1.CallKind
		admit bool
	}{
		{"state-read reads", env(19, CapStateRead), v1.CallStateGet, true},
		{"state-read lists", env(19, CapStateRead), v1.CallStateList, true},
		{"state-read cannot write", env(19, CapStateRead), v1.CallStateSet, false},
		{"state-write writes", env(19, CapStateWrite), v1.CallStateSet, true},
		{"state-write cannot read", env(19, CapStateWrite), v1.CallStateGet, false},
		{"legacy state still writes", env(16, CapState), v1.CallStateSet, true},
		{"wallpaper-read snapshots", env(19, CapWallpaperRead), v1.CallWallpaperSnapshot, true},
		{"wallpaper-read cannot set a mask", env(19, CapWallpaperRead), v1.CallWallpaperMaskSet, false},
		{"screenshot-read reads the directory", env(19, CapScreenshotRead), v1.CallScreenshotDirectory, true},
		{"screenshot-read cannot capture", env(19, CapScreenshotRead), v1.CallScreenshotStart, false},
		{"outputs-read reads output context", env(19, CapOutputsRead), v1.CallOutputContext, true},
		{"output context is gated at minor 19", env(19, CapNotifications), v1.CallOutputContext, false},
		{"panels still read output context", env(19, CapPanels), v1.CallOutputContext, true},
		{"output context stays open below minor 19", env(18, CapNotifications), v1.CallOutputContext, true},
		{"files:browser browses", env(19, CapFilesBrowser), v1.CallFilesBrowse, true},
	} {
		if got := tc.env.allows(classOf(tc.call)); got != tc.admit {
			t.Errorf("%s: allows = %v, want %v", tc.name, got, tc.admit)
		}
	}
}

func TestRefusalNamesTheDeclarationThePluginCanMake(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		minor int
		call  v1.CallKind
		want  []string
	}{
		{19, v1.CallStateSet, []string{`"state.set"`, `"state-write"`, "manifest.json", `{"capabilities":["state-write"]}`}},
		{16, v1.CallStateSet, []string{`"state.set"`, `"state"`, `{"capabilities":["state"]}`}},
		{19, v1.CallNotify, []string{`"notify"`, `{"capabilities":["notifications"]}`}},
	} {
		reply := NewDispatcher(CallEnv{Protocol: v1.Version{Major: 1, Minor: tc.minor}}).
			Handle(context.Background(), &v1.HostCall{ID: "x", Call: tc.call})
		if reply.OK {
			t.Fatalf("minor %d %s admitted with no grant", tc.minor, tc.call)
		}
		for _, want := range tc.want {
			if !strings.Contains(reply.Error, want) {
				t.Errorf("minor %d %s: refusal %q misses %q", tc.minor, tc.call, reply.Error, want)
			}
		}
	}
}

func TestNarrowNamesNeedTheirProtocolMinor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		minor int
		caps  []string
		err   string
	}{
		{16, []string{"state-read"}, `"state-read" requires protocol minor 19`},
		{16, []string{"files:browser"}, `"files:browser" requires protocol minor 19`},
		{19, []string{"state-read", "panels"}, ""},
		{19, []string{"files:browser"}, ""},
		{19, []string{"files:root"}, `scope "root"`},
		{19, []string{"files:"}, `scope ""`},
		{19, []string{"state:x"}, `takes no scope`},
		{19, []string{"state-reader"}, "not one this shell offers"},
		{16, []string{"state", "files"}, ""},
	} {
		var document map[string]any
		if err := json.Unmarshal([]byte(edit(t, "capabilities", tc.caps)), &document); err != nil {
			t.Fatal(err)
		}
		document["protocol"].(map[string]any)["minor"] = tc.minor
		manifest, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseManifest(manifest)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("minor %d %v: %v", tc.minor, tc.caps, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("minor %d %v: error %v, want one containing %q", tc.minor, tc.caps, err, tc.err)
		}
	}
}

func TestSameGrantsComparesWhatTheNamesAllow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b []string
		same bool
	}{
		{[]string{"files"}, []string{"files:browser"}, true},
		{[]string{"state"}, []string{"state-read", "state-write"}, true},
		{[]string{"state"}, []string{"state-read"}, false},
		{[]string{"panels", "notifications"}, []string{"notifications", "panels"}, true},
		{[]string{"panels"}, []string{"panels", "outputs-read"}, true},
		{[]string{"panels"}, []string{"notifications"}, false},
		{nil, nil, true},
	} {
		if got := SameGrants(tc.a, tc.b); got != tc.same {
			t.Errorf("SameGrants(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.same)
		}
	}
}

func TestDescribeCapabilitySpellsOutABroadName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"state":         "state (read and write)",
		"wallpaper":     "wallpaper (read and write)",
		"screenshot":    "screenshot (capture and read)",
		"state-read":    "state-read",
		"files:browser": "files:browser",
		"panels":        "panels",
	} {
		if got := DescribeCapability(name); got != want {
			t.Errorf("DescribeCapability(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTheHostSpeaksTheMinorThatIntroducedTheNarrowNames(t *testing.T) {
	t.Parallel()
	// A shell that accepts the narrowed names in a manifest must also accept
	// the minor such a manifest declares, or every such plugin is refused.
	if HostProtocolMinor < NarrowCapabilityMinor {
		t.Fatalf("host speaks minor %d; the narrowed names need %d", HostProtocolMinor, NarrowCapabilityMinor)
	}
	m, err := ParseManifest([]byte(`{"schema":1,"id":"org.example.probe","name":"Probe","version":"1.0.0",
"protocol":{"major":1,"minor":19},"exec":"bin/probe","capabilities":["state-read","outputs-read"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !HostSupports(m.Protocol) {
		t.Fatalf("a minor-%d manifest is refused by this host", m.Protocol.Minor)
	}
}
