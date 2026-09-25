package wayland

import "testing"

// The 1.45 XML declares blur as 0 in a bitfield, which no flag can carry.
// Niri sends 1, and that is the bit the shell tests.
func TestBlurCapabilityUsesTheCorrectedMask(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flags uint32
		want  bool
	}{{0, false}, {1, true}, {3, true}, {2, false}} {
		if got := blurCapable(tc.flags); got != tc.want {
			t.Errorf("blurCapable(%d) = %v, want %v", tc.flags, got, tc.want)
		}
	}
}

// Capabilities are reported on the first event and afterwards only when
// they change, so a compositor repeating itself does not restyle every bar.
func TestCapabilityReportsOnlyChanges(t *testing.T) {
	t.Parallel()
	var st capabilityState
	var got []Capabilities
	report := func(c Capabilities) { got = append(got, c) }
	for _, flags := range []uint32{0, 0, 1, 1, 0} {
		st.update(flags, report)
	}
	want := []Capabilities{{Blur: false}, {Blur: true}, {Blur: false}}
	if len(got) != len(want) {
		t.Fatalf("reported %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("report %d = %v, want %v", i, got[i], want[i])
		}
	}
	if !st.known || st.current.Blur {
		t.Fatalf("state = %+v, want known and no blur", st)
	}
}

func TestCapabilityReportToleratesNoCallback(t *testing.T) {
	t.Parallel()
	var st capabilityState
	st.update(1, nil)
	if !st.current.Blur {
		t.Fatal("state did not record blur with no callback installed")
	}
}
