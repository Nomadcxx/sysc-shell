package services

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func readPrivacyFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/privacy-pw-dump.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParsePrivacyDumpClassifiesCaptureFromDeviceMetadata(t *testing.T) {
	t.Parallel()
	snapshot, err := parsePrivacyDump(readPrivacyFixture(t))
	if err != nil {
		t.Fatalf("parsePrivacyDump: %v", err)
	}
	if !snapshot.Known || !snapshot.MicrophoneActive || !snapshot.MicrophoneUnknown {
		t.Fatalf("audio capture = %+v, want a known mic and an unclassified active path", snapshot)
	}
	want := []PrivacyVideoPath{
		{SourceNodeID: 3, Camera: true, ApplicationName: "Cheese"},
		{SourceNodeID: 11},
	}
	if !slices.Equal(snapshot.VideoPaths, want) {
		t.Fatalf("video paths = %+v, want %+v", snapshot.VideoPaths, want)
	}
	if want := []string{"Jitsi Meet"}; !slices.Equal(snapshot.MicrophoneApps, want) {
		t.Fatalf("microphone applications = %q, want %q", snapshot.MicrophoneApps, want)
	}
}

func TestAudioSourceKindRecognizesPipeWireCanonicalFormFactors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		formFactor string
		want       audioSourceClassification
	}{
		{formFactor: "hands-free", want: privacyMicrophone},
		{formFactor: "headphone", want: privacyNotMicrophone},
	} {
		t.Run(tc.formFactor, func(t *testing.T) {
			if got := audioSourceKind(tc.formFactor); got != tc.want {
				t.Fatalf("audioSourceKind(%q) = %v, want %v", tc.formFactor, got, tc.want)
			}
		})
	}
}

func TestParsePrivacyDumpSortsDeduplicatesAndSanitizesApplications(t *testing.T) {
	t.Parallel()
	input := `[
		{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source","device.form-factor":"microphone"}}},
		{"id":2,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Audio","application.name":"Zulu\nBridge"}}},
		{"id":3,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Audio","application.name":"Alpha"}}},
		{"id":4,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Audio","application.name":"Zulu\nBridge"}}},
		{"id":5,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":2}},
		{"id":6,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":3}},
		{"id":7,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":4}}
	]`
	snapshot, err := parsePrivacyDump([]byte(input))
	if err != nil {
		t.Fatalf("parsePrivacyDump: %v", err)
	}
	want := []string{"Alpha", "Zulu Bridge"}
	if !slices.Equal(snapshot.MicrophoneApps, want) {
		t.Fatalf("microphone applications = %q, want %q", snapshot.MicrophoneApps, want)
	}
}

func TestParsePrivacyDumpEmptyGraphIsKnownInactive(t *testing.T) {
	t.Parallel()
	snapshot, err := parsePrivacyDump([]byte(`[]`))
	if err != nil {
		t.Fatalf("parse empty graph: %v", err)
	}
	if !snapshot.Known || snapshot.MicrophoneActive || snapshot.MicrophoneUnknown || len(snapshot.VideoPaths) != 0 {
		t.Fatalf("empty graph = %+v, want known inactive", snapshot)
	}
}

func TestParsePrivacyDumpRejectsMalformedOrNullSnapshot(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"not":"an array"}`, `null`} {
		if _, err := parsePrivacyDump([]byte(input)); err == nil {
			t.Errorf("parse %s succeeded, want a decode error", input)
		}
	}
}

func TestParsePrivacyDumpUnknownConsumerClassesStayUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		props string
	}{
		{name: "missing", props: `{}`},
		{name: "unrecognized", props: `{"media.class":"Stream/Unknown"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := `[
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source","device.form-factor":"microphone"}}},
				{"id":2,"type":"PipeWire:Interface:Node","info":{"state":"running","props":` + tc.props + `}},
				{"id":3,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Video/Source","device.api":"v4l2"}}},
				{"id":4,"type":"PipeWire:Interface:Node","info":{"state":"running","props":` + tc.props + `}},
				{"id":5,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":2}},
				{"id":6,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":3,"input-node-id":4}}
			]`
			snapshot, err := parsePrivacyDump([]byte(input))
			if err != nil {
				t.Fatalf("parsePrivacyDump: %v", err)
			}
			if !snapshot.Known || snapshot.MicrophoneActive || !snapshot.MicrophoneUnknown {
				t.Fatalf("audio capture = %+v, want unknown without positive activity", snapshot)
			}
			want := []PrivacyVideoPath{{SourceNodeID: 3}}
			if !slices.Equal(snapshot.VideoPaths, want) {
				t.Fatalf("video paths = %+v, want unclassified path %+v", snapshot.VideoPaths, want)
			}
		})
	}
}

func TestParsePrivacyDumpIncompleteCaptureLinksStayUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		input                 string
		wantMicrophoneUnknown bool
		wantVideoPaths        []PrivacyVideoPath
	}{
		{
			name: "missing audio consumer",
			input: `[
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source"}}},
				{"id":2,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":99}}
			]`,
			wantMicrophoneUnknown: true,
		},
		{
			name: "known headphone source stays inactive",
			input: `[
				{"id":100,"type":"PipeWire:Interface:Device","info":{"props":{"device.form-factor":"headphone"}}},
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source","device.id":"100"}}},
				{"id":2,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":99}}
			]`,
		},
		{
			name: "suspended microphone source stays inactive",
			input: `[
				{"id":100,"type":"PipeWire:Interface:Device","info":{"props":{"device.form-factor":"microphone"}}},
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"suspended","props":{"media.class":"Audio/Source","device.id":"100"}}},
				{"id":2,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":99}}
			]`,
		},
		{
			name: "missing video consumer",
			input: `[
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Video/Source"}}},
				{"id":2,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":99}}
			]`,
			wantVideoPaths: []PrivacyVideoPath{{SourceNodeID: 1}},
		},
		{
			name: "suspended video source stays inactive",
			input: `[
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"suspended","props":{"media.class":"Video/Source","device.api":"v4l2"}}},
				{"id":2,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":99}}
			]`,
		},
		{
			name: "unknown consumers missing state",
			input: `[
				{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source"}}},
				{"id":2,"type":"PipeWire:Interface:Node","info":{"props":{"media.class":"Stream/Unknown"}}},
				{"id":3,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Video/Source"}}},
				{"id":4,"type":"PipeWire:Interface:Node","info":{"props":{"media.class":"Stream/Unknown"}}},
				{"id":5,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":2}},
				{"id":6,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":3,"input-node-id":4}}
			]`,
			wantMicrophoneUnknown: true,
			wantVideoPaths:        []PrivacyVideoPath{{SourceNodeID: 3}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := parsePrivacyDump([]byte(tc.input))
			if err != nil {
				t.Fatalf("parsePrivacyDump: %v", err)
			}
			if !snapshot.Known || snapshot.MicrophoneActive || snapshot.MicrophoneUnknown != tc.wantMicrophoneUnknown {
				t.Fatalf("audio state = %+v, want known non-active, microphone unknown=%v", snapshot, tc.wantMicrophoneUnknown)
			}
			if !slices.Equal(snapshot.VideoPaths, tc.wantVideoPaths) {
				t.Fatalf("video paths = %+v, want %+v", snapshot.VideoPaths, tc.wantVideoPaths)
			}
		})
	}
}

func TestPrivacyServiceReportsUnknownAfterCommandFailureAndRecovers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "pw-dump")
	result := filepath.Join(dir, "result")
	fail := filepath.Join(dir, "fail")
	if err := os.WriteFile(result, readPrivacyFixture(t), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ -f '" + fail + "' ]; then exit 1; fi\ncat '" + result + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := NewPrivacy(15*time.Millisecond, bin)
	defer p.Close()

	waitPrivacy(t, p, func(snapshot PrivacySnapshot) bool { return snapshot.Known && snapshot.MicrophoneActive })
	if err := os.WriteFile(fail, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitPrivacy(t, p, func(snapshot PrivacySnapshot) bool { return !snapshot.Known })
	if err := os.WriteFile(result, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	waitPrivacy(t, p, func(snapshot PrivacySnapshot) bool {
		return snapshot.Known && !snapshot.MicrophoneActive && !snapshot.MicrophoneUnknown && len(snapshot.VideoPaths) == 0
	})
	p.Close()
	select {
	case <-p.done:
	default:
		t.Fatal("Close returned before the poller stopped")
	}
}

func TestPrivacyUnavailableCommandStaysUnknownAndCloses(t *testing.T) {
	t.Parallel()
	p := NewPrivacy(time.Millisecond, filepath.Join(t.TempDir(), "missing-pw-dump"))
	defer p.Close()
	if snapshot := p.Snapshot(); snapshot.Known {
		t.Fatalf("snapshot = %+v, want unknown without pw-dump", snapshot)
	}
	p.Close()
	select {
	case <-p.done:
	default:
		t.Fatal("Close returned before the poller stopped")
	}
}

func waitPrivacy(t *testing.T, p *Privacy, match func(PrivacySnapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if match(p.Snapshot()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("privacy snapshot did not reach expected state; latest=%+v", p.Snapshot())
}
