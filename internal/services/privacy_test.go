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
		{SourceNodeID: 3, Camera: true},
		{SourceNodeID: 11},
	}
	if !slices.Equal(snapshot.VideoPaths, want) {
		t.Fatalf("video paths = %+v, want %+v", snapshot.VideoPaths, want)
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

func TestParsePrivacyDumpKeepsUnclassifiedRunningCaptureUnknown(t *testing.T) {
	t.Parallel()
	input := `[
		{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source"}}},
		{"id":2,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{}}},
		{"id":3,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Video/Source"}}},
		{"id":4,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{}}},
		{"id":5,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":2}},
		{"id":6,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":3,"input-node-id":4}}
	]`
	snapshot, err := parsePrivacyDump([]byte(input))
	if err != nil {
		t.Fatalf("parsePrivacyDump: %v", err)
	}
	if !snapshot.Known || !snapshot.MicrophoneUnknown {
		t.Fatalf("unclassified audio path = %+v, want microphone unknown", snapshot)
	}
	want := []PrivacyVideoPath{{SourceNodeID: 3}}
	if !slices.Equal(snapshot.VideoPaths, want) {
		t.Fatalf("unclassified video paths = %+v, want %+v", snapshot.VideoPaths, want)
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
