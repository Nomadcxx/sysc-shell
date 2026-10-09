package gamma

import (
	"encoding/binary"
	"testing"
)

func TestRampFileStoresContiguousChannels(t *testing.T) {
	f, err := RampFile(2, []uint16{1, 2}, []uint16{3, 4}, []uint16{5, 6})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got := make([]byte, 12)
	if _, err := f.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	want := []uint16{1, 2, 3, 4, 5, 6}
	for i, value := range want {
		if actual := binary.NativeEndian.Uint16(got[i*2:]); actual != value {
			t.Fatalf("ramp value %d = %d, want %d", i, actual, value)
		}
	}
}

func TestRampFileRejectsUnboundedSize(t *testing.T) {
	if _, err := RampFile(MaxRampEntries+1, nil, nil, nil); err == nil {
		t.Fatal("oversized compositor ramp was accepted")
	}
}
