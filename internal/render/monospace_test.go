package render

import "testing"

// stubMonospace swaps the probe for the duration of the test. A family the
// verdicts map does not mention reads as false, mirroring a machine that does
// not have the font.
func stubMonospace(t *testing.T, verdicts map[string]bool) *int {
	t.Helper()
	var calls int
	saved := monospaceProbe
	monospaceProbe = func(family string) bool {
		calls++
		return verdicts[family]
	}
	t.Cleanup(func() { monospaceProbe = saved })
	return &calls
}

func TestMonospaceFamilySnapsToAVerifiedFace(t *testing.T) {
	tests := []struct {
		name     string
		family   string
		fallback string
		verdicts map[string]bool
		want     string
	}{
		{
			name:     "monospace configured family is kept",
			family:   "Fira Code",
			fallback: "Fira Code",
			verdicts: map[string]bool{"Fira Code": true},
			want:     "Fira Code",
		},
		{
			name:     "proportional configured family falls back",
			family:   "DejaVu Sans",
			fallback: "Fira Code",
			verdicts: map[string]bool{"Fira Code": true},
			want:     "Fira Code",
		},
		{
			name:     "generic family when the fallback is not monospace",
			family:   "DejaVu Sans",
			fallback: "Unreadable",
			verdicts: map[string]bool{"monospace": true},
			want:     "monospace",
		},
		{
			name:     "configured family kept when nothing verifies",
			family:   "DejaVu Sans",
			fallback: "Unreadable",
			verdicts: map[string]bool{},
			want:     "DejaVu Sans",
		},
		{
			name:     "empty configured family uses the fallback",
			family:   "",
			fallback: "Fira Code",
			verdicts: map[string]bool{"Fira Code": true},
			want:     "Fira Code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubMonospace(t, tt.verdicts)
			if got := MonospaceFamily(tt.family, tt.fallback); got != tt.want {
				t.Fatalf("MonospaceFamily(%q, %q) = %q, want %q",
					tt.family, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestMonospaceFamilyDoesNotProbeAnEqualFallback(t *testing.T) {
	calls := stubMonospace(t, map[string]bool{})
	if got := MonospaceFamily("Same", "Same"); got != "Same" {
		t.Fatalf("MonospaceFamily = %q, want the configured family", got)
	}
	if *calls != 2 {
		t.Fatalf("probe calls = %d, want 2 (family plus generic)", *calls)
	}
}
