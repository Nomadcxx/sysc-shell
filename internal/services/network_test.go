package services

import "testing"

func TestSignalBandMatchesReferenceThresholds(t *testing.T) {
	cases := []struct {
		signal uint8
		band   int
	}{{100, 4}, {80, 4}, {79, 3}, {60, 3}, {59, 2}, {35, 2}, {34, 1}, {15, 1}, {14, 0}, {0, 0}}
	for _, c := range cases {
		if got := SignalBand(c.signal); got != c.band {
			t.Errorf("SignalBand(%d) = %d, want %d", c.signal, got, c.band)
		}
	}
}
