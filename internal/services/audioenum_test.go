package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/fakebin"
)

func TestPwdumpIgnoresHeterogeneousUnrelatedMetadata(t *testing.T) {
	data := []byte(`[
		{"id":31,"props":{"metadata.name":"settings"},"metadata":[
			{"key":"log.level","value":2},
			{"key":"clock.force-quantum","value":false}
		]},
		{"id":40,"props":{"metadata.name":"default"},"metadata":[
			{"key":"default.audio.sink","value":{"name":"sink.main"}},
			{"key":"default.audio.source","value":{"name":"source.main"}}
		]},
		{"id":45,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.main","node.description":"Main output"}}},
		{"id":46,"info":{"props":{"media.class":"Audio/Source","node.name":"source.main","node.description":"Main input"}}},
		{"id":47,"info":{"props":{"media.class":"Stream/Output/Audio","node.name":"stream.player","application.name":"Player"}}}
	]`)

	snap, err := parsePwdump(data)
	if err != nil {
		t.Fatalf("parsePwdump rejected unrelated metadata: %v", err)
	}
	if len(snap.Sinks) != 1 || !snap.Sinks[0].Default {
		t.Fatalf("sinks = %+v, want the default output", snap.Sinks)
	}
	if len(snap.Sources) != 1 || !snap.Sources[0].Default {
		t.Fatalf("sources = %+v, want the default input", snap.Sources)
	}
	if len(snap.Streams) != 1 {
		t.Fatalf("streams = %+v, want the valid playback stream", snap.Streams)
	}
}

func TestMixerPollExposesReadFailure(t *testing.T) {
	a := NewAudio(0, "/bin/true")
	a.mixer = AudioSnapshot{Sinks: []AudioNode{{ID: 45, Name: "sink.main"}}}
	a.hasMixer = true
	a.dumpBin = "/bin/false"

	a.pollMixer()

	if err := a.MixerError(); err == nil {
		t.Fatal("MixerError = nil after pw-dump failed")
	}
	if got := a.Mixer(); len(got.Sinks) != 1 || got.Sinks[0].ID != 45 {
		t.Fatalf("Mixer = %+v, want the last valid snapshot with the poll error", got)
	}
}

func TestPwdumpParsesSinksSourcesAndStreams(t *testing.T) {
	data, err := os.ReadFile("testdata/pw-dump.json")
	if err != nil {
		t.Skip("testdata/pw-dump.json is absent; a captured pw-dump fixture would run this")
	}
	snap, err := parsePwdump(data)
	if err != nil {
		t.Fatalf("parsePwdump: %v", err)
	}
	if len(snap.Sinks) == 0 {
		t.Fatal("no sinks parsed from a live dump that has them")
	}
	for _, n := range snap.Sinks {
		if n.Description == "" {
			t.Errorf("sink %d has no description: the panel names devices by it", n.ID)
		}
	}
}

func TestCubicVolumeMatchesWpctlDisplay(t *testing.T) {
	// Live fact recorded 2026-09-07: node 45 stores channelVolumes 0.1355 and
	// wpctl shows 0.51. Reading raw would paint 14% where the shell paints 51%.
	if got := cubicPercent(0.1355); got != 51 {
		t.Errorf("cubicPercent(0.1355) = %d, want 51", got)
	}
	if got := cubicPercent(0); got != 0 {
		t.Errorf("cubicPercent(0) = %d, want 0", got)
	}
	if got := cubicPercent(1); got != 100 {
		t.Errorf("cubicPercent(1) = %d, want 100", got)
	}
}

func TestParsePulseRoutingMapsPipeWireObjectIDs(t *testing.T) {
	inputs := []byte(`[
		{"index":973016,"sink":58,"properties":{"object.id":"82"}}
	]`)
	sinks := []byte(`[
		{"index":58,"name":"sink.headphones","properties":{"object.id":"46"}},
		{"index":61,"name":"sink.unmapped","properties":{"object.id":"77"}}
	]`)
	routing, err := parsePulseRouting(inputs, sinks)
	if err != nil {
		t.Fatalf("parsePulseRouting: %v", err)
	}
	snap := mergePulseRouting(AudioSnapshot{
		Sinks:   []AudioNode{{ID: 46}, {ID: 77}, {ID: 90}},
		Streams: []AudioNode{{ID: 82}, {ID: 116}},
	}, routing)
	if !snap.Streams[0].Routeable || snap.Streams[0].OutputID != 46 {
		t.Errorf("mapped stream = %+v, want routable on output 46", snap.Streams[0])
	}
	if snap.Streams[1].Routeable || snap.Streams[1].OutputID != 0 {
		t.Errorf("unmapped stream = %+v, want routing disabled", snap.Streams[1])
	}
	if !snap.Sinks[0].Routeable || !snap.Sinks[1].Routeable || snap.Sinks[2].Routeable {
		t.Errorf("sink routing = %+v, want only PulseAudio-backed sinks enabled", snap.Sinks)
	}
}

func TestParsePulseRoutingRejectsAmbiguousObjectIDs(t *testing.T) {
	inputs := []byte(`[
		{"index":10,"sink":58,"properties":{"object.id":"82"}},
		{"index":11,"sink":58,"properties":{"object.id":"82"}}
	]`)
	sinks := []byte(`[{"index":58,"name":"sink.main","properties":{"object.id":"46"}}]`)
	if _, err := parsePulseRouting(inputs, sinks); err == nil {
		t.Fatal("parsePulseRouting accepted two PulseAudio inputs for one PipeWire object ID")
	}
}

func TestParsePulseRoutingRejectsDuplicateSinkInputIndexes(t *testing.T) {
	inputs := []byte(`[
		{"index":10,"sink":58,"properties":{"object.id":"82"}},
		{"index":10,"sink":58,"properties":{"object.id":"83"}}
	]`)
	sinks := []byte(`[{"index":58,"name":"sink.main","properties":{"object.id":"46"}}]`)
	if _, err := parsePulseRouting(inputs, sinks); err == nil {
		t.Fatal("parsePulseRouting accepted two PipeWire streams for one PulseAudio sink-input index")
	}
}

func TestPollMixerProjectsPulseRoutesAndKeepsVolumeWhenPactlFails(t *testing.T) {
	dump := `[{"id":46,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.main","node.description":"Main output"},"params":{"Props":[{"channelVolumes":[0.125],"mute":false}]}}},{"id":34,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.other","node.description":"Other output"}}},{"id":82,"info":{"props":{"media.class":"Stream/Output/Audio","node.name":"stream.player"}}},{"id":116,"info":{"props":{"media.class":"Stream/Output/Audio","node.name":"stream.unmapped"}}}]`
	commands := newFakeAudioRoutingCommands(t,
		dump,
		`[{"index":973016,"sink":58,"properties":{"object.id":"82"}}]`,
		`[{"index":58,"name":"sink.main","properties":{"object.id":"46"}},{"index":86,"name":"sink.other","properties":{"object.id":"34"}}]`,
	)
	a := &Audio{dumpBin: commands.dumpBin, pactlBin: commands.pactlBin, mixerCh: make(chan AudioSnapshot, 1)}
	a.pollMixer()
	snap := a.Mixer()
	if !a.MixerReady() || len(snap.Streams) != 2 || !snap.Streams[0].Routeable || snap.Streams[0].OutputID != 46 || snap.Streams[1].Routeable {
		t.Fatalf("mixer routes = %+v, want stream 82 mapped to 46 and stream 116 disabled", snap.Streams)
	}
	var mainOutput AudioNode
	for _, sink := range snap.Sinks {
		if sink.ID == 46 {
			mainOutput = sink
		}
		if !sink.Routeable {
			t.Errorf("sink %d is not routeable", sink.ID)
		}
	}
	if len(snap.Sinks) != 2 || mainOutput.ID == 0 || mainOutput.Level != 50 {
		t.Fatalf("mixer sinks = %+v, want routable outputs and PipeWire volume retained", snap.Sinks)
	}

	a = &Audio{dumpBin: commands.dumpBin, pactlBin: "/bin/false", mixerCh: make(chan AudioSnapshot, 1)}
	a.pollMixer()
	snap = a.Mixer()
	mainOutput = AudioNode{}
	for _, sink := range snap.Sinks {
		if sink.ID == 46 {
			mainOutput = sink
		}
	}
	if !a.MixerReady() || snap.Error != "" || len(snap.Sinks) != 2 || mainOutput.Level != 50 || snap.Streams[0].Routeable {
		t.Fatalf("mixer after pactl failure = %+v, want volume state retained and routing disabled", snap)
	}
}

func TestMoveStreamUsesFreshValidatedPulseIndices(t *testing.T) {
	commands := newFakeAudioRoutingCommands(t,
		`[{"id":46,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.main"}}},{"id":34,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.other"}}},{"id":82,"info":{"props":{"media.class":"Stream/Output/Audio","node.name":"stream.player"}}}]`,
		`[{"index":973016,"sink":58,"properties":{"object.id":"82"}}]`,
		`[{"index":58,"name":"sink.main","properties":{"object.id":"46"}},{"index":86,"name":"sink.other","properties":{"object.id":"34"}}]`,
	)
	a := &Audio{dumpBin: commands.dumpBin, pactlBin: commands.pactlBin}
	if err := a.MoveStream(82, 34); err != nil {
		t.Fatalf("MoveStream: %v", err)
	}
	got := commands.log(t)
	want := []string{
		"-f json list sink-inputs",
		"-f json list sinks",
		"move-sink-input 973016 86",
	}
	if got != strings.Join(want, "\n")+"\n" {
		t.Fatalf("pactl log = %q, want only the selected input moved to sink 86", got)
	}
}

func TestMoveStreamRefusesStaleOrUnmappedObjects(t *testing.T) {
	tests := []struct {
		name    string
		stream  int
		sink    int
		dump    string
		inputs  string
		sinks   string
		wantErr string
	}{
		{
			name: "stream no longer exists", stream: 116, sink: 34,
			inputs:  `[{"index":973016,"sink":58,"properties":{"object.id":"82"}}]`,
			sinks:   `[{"index":86,"name":"sink.other","properties":{"object.id":"34"}}]`,
			wantErr: "stream 116 is unavailable",
		},
		{
			name: "destination disappeared from PulseAudio", stream: 82, sink: 34,
			inputs:  `[{"index":973016,"sink":58,"properties":{"object.id":"82"}}]`,
			sinks:   `[{"index":58,"name":"sink.main","properties":{"object.id":"46"}}]`,
			wantErr: "output 34 is unavailable",
		},
		{
			name: "stream has no PulseAudio mapping", stream: 116, sink: 34,
			dump:    `[{"id":46,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.main"}}},{"id":34,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.other"}}},{"id":116,"info":{"props":{"media.class":"Stream/Output/Audio","node.name":"stream.other"}}}]`,
			inputs:  `[{"index":973016,"sink":58,"properties":{"object.id":"82"}}]`,
			sinks:   `[{"index":58,"name":"sink.main","properties":{"object.id":"46"}},{"index":86,"name":"sink.other","properties":{"object.id":"34"}}]`,
			wantErr: "stream 116 has no PulseAudio route",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump := tt.dump
			if dump == "" {
				dump = `[{"id":46,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.main"}}},{"id":34,"info":{"props":{"media.class":"Audio/Sink","node.name":"sink.other"}}},{"id":82,"info":{"props":{"media.class":"Stream/Output/Audio","node.name":"stream.player"}}}]`
			}
			commands := newFakeAudioRoutingCommands(t,
				dump,
				tt.inputs, tt.sinks,
			)
			a := &Audio{dumpBin: commands.dumpBin, pactlBin: commands.pactlBin}
			err := a.MoveStream(tt.stream, tt.sink)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("MoveStream error = %v, want it to contain %q", err, tt.wantErr)
			}
			if got := commands.log(t); strings.Contains(got, "move-sink-input") {
				t.Fatalf("pactl log = %q, stale request was moved", got)
			}
		})
	}
}

type fakeAudioRoutingCommands struct {
	dumpBin  string
	pactlBin string
	logPath  string
}

func newFakeAudioRoutingCommands(t *testing.T, dump, inputs, sinks string) fakeAudioRoutingCommands {
	t.Helper()
	dir := t.TempDir()
	for name, value := range map[string]string{
		"dump.json": dump, "inputs.json": inputs, "sinks.json": sinks,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dumpBin := filepath.Join(dir, "pw-dump")
	if err := fakebin.Write(dumpBin, "#!/bin/sh\ncat '"+filepath.Join(dir, "dump.json")+"'\n", 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "pactl.log")
	pactlBin := filepath.Join(dir, "pactl")
	script := `#!/bin/sh
echo "$*" >> '` + logPath + `'
case "$*" in
  '-f json list sink-inputs') cat '` + filepath.Join(dir, "inputs.json") + `' ;;
  '-f json list sinks') cat '` + filepath.Join(dir, "sinks.json") + `' ;;
  'move-sink-input '*) ;;
  *) exit 2 ;;
esac
`
	if err := fakebin.Write(pactlBin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	return fakeAudioRoutingCommands{dumpBin: dumpBin, pactlBin: pactlBin, logPath: logPath}
}

func (c fakeAudioRoutingCommands) log(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(c.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func TestHotUnplugBetweenTwoDumps(t *testing.T) {
	a := readFixture(t, "testdata/pw-dump.json")
	b := readFixture(t, "testdata/pw-dump-unplugged.json")
	sa, _ := parsePwdump(a)
	sb, _ := parsePwdump(b)
	got := countNodes(sa) - countNodes(sb)
	if got != 1 {
		t.Errorf("unplugged fixture drops %d streams/nodes, want 1", got)
	}
}

func TestPwdumpMarksTheDefaultSink(t *testing.T) {
	snap, err := parsePwdump(readFixture(t, "testdata/pw-dump.json"))
	if err != nil {
		t.Fatal(err)
	}
	var defaults int
	for _, n := range snap.Sinks {
		if n.Default {
			defaults++
			if n.ID != 45 {
				t.Errorf("default sink id = %d, want 45 (the live default.audio.sink)", n.ID)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("default sinks = %d, want 1", defaults)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func countNodes(s AudioSnapshot) int {
	return len(s.Sinks) + len(s.Sources) + len(s.Streams)
}
