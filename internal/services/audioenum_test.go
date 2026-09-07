package services

import (
	"os"
	"testing"
)

func TestPwdumpParsesSinksSourcesAndStreams(t *testing.T) {
	data, err := os.ReadFile("testdata/pw-dump.json")
	if err != nil {
		t.Skip("no live fixture captured yet")
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
