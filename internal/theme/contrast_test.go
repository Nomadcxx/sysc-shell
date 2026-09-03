package theme

import (
	"math"
	"testing"
)

func TestParseColorRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want Color
	}{
		{"#000000", Color{A: 0xff}},
		{"#ffffff", Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}},
		{"#1d2025", Color{R: 0x1d, G: 0x20, B: 0x25, A: 0xff}},
		{"#1d202580", Color{R: 0x1d, G: 0x20, B: 0x25, A: 0x80}},
		{"#ABCDEF", Color{R: 0xab, G: 0xcd, B: 0xef, A: 0xff}},
	} {
		got, err := ParseColor(tc.in)
		if err != nil {
			t.Errorf("ParseColor(%q) = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseColor(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
		if back, err := ParseColor(got.Hex()); err != nil || back != got {
			t.Errorf("%q did not survive a round trip: %q -> %+v (%v)", tc.in, got.Hex(), back, err)
		}
	}
}

func TestParseColorRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "#", "#fff", "ffffff", "#gggggg", "#1234567", "blue", "#12345"} {
		if _, err := ParseColor(in); err == nil {
			t.Errorf("ParseColor(%q) = nil error, want a rejection", in)
		}
	}
}

func TestContrastRatioMatchesWCAG(t *testing.T) {
	t.Parallel()
	black := Color{A: 0xff}
	white := Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if got := ContrastRatio(black, white); math.Abs(got-21) > 0.01 {
		t.Errorf("black on white = %.4f, want 21", got)
	}
	if got := ContrastRatio(white, white); math.Abs(got-1) > 0.0001 {
		t.Errorf("white on white = %.4f, want 1", got)
	}
	if ContrastRatio(black, white) != ContrastRatio(white, black) {
		t.Error("ContrastRatio is not symmetric")
	}
	// A published reference value: #767676 is the lightest grey that still
	// reaches 4.5:1 against white.
	grey, _ := ParseColor("#767676")
	if got := ContrastRatio(grey, white); got < 4.5 || got > 4.6 {
		t.Errorf("#767676 on white = %.3f, want just over 4.5", got)
	}
}

func TestEnsureContrastLeavesAPassingPairAlone(t *testing.T) {
	t.Parallel()
	fg, _ := ParseColor("#ffffff")
	bg, _ := ParseColor("#000000")
	if got := EnsureContrast(fg, bg, 4.5); got != fg {
		t.Errorf("EnsureContrast changed a passing pair: %+v -> %+v", fg, got)
	}
}

func TestEnsureContrastReachesTheRequestedRatio(t *testing.T) {
	t.Parallel()
	backgrounds := []string{"#1d2025", "#ffffff", "#000000", "#1f7ab5", "#ff5449", "#7f7f7f", "#e6e6e6"}
	foregrounds := []string{"#1d2025", "#808080", "#ffffff", "#0080ff", "#9aa0a6"}
	for _, ratio := range []float64{3.0, 4.5, 7.0} {
		for _, b := range backgrounds {
			bg, _ := ParseColor(b)
			for _, f := range foregrounds {
				fg, _ := ParseColor(f)
				got := EnsureContrast(fg, bg, ratio)
				reachable := math.Max(
					ContrastRatio(Color{A: fg.A}, bg),
					ContrastRatio(Color{R: 0xff, G: 0xff, B: 0xff, A: fg.A}, bg))
				if reachable < ratio {
					// Unreachable: the best endpoint is the honest answer.
					if ContrastRatio(got, bg) < reachable-0.01 {
						t.Errorf("%s on %s at %.1f: got %.2f, best reachable %.2f",
							f, b, ratio, ContrastRatio(got, bg), reachable)
					}
					continue
				}
				if r := ContrastRatio(got, bg); r < ratio {
					t.Errorf("%s on %s at %.1f: repaired to %s = %.3f, below target",
						f, b, ratio, got.Hex(), r)
				}
			}
		}
	}
}

func TestEnsureContrastMakesTheSmallestChange(t *testing.T) {
	t.Parallel()
	bg, _ := ParseColor("#1d2025")
	fg, _ := ParseColor("#5a5f66") // too dim on this surface
	got := EnsureContrast(fg, bg, 4.5)
	if ContrastRatio(got, bg) < 4.5 {
		t.Fatalf("did not reach the target: %.3f", ContrastRatio(got, bg))
	}
	// One step back toward the original must fall short, or the search
	// overshot and the repair is not minimal.
	back := Color{
		R: uint8(int(got.R) - sign(int(got.R)-int(fg.R))),
		G: uint8(int(got.G) - sign(int(got.G)-int(fg.G))),
		B: uint8(int(got.B) - sign(int(got.B)-int(fg.B))),
		A: got.A,
	}
	if back != got && ContrastRatio(back, bg) >= 4.5 {
		t.Errorf("repair overshot: %s reaches %.3f but %s already reaches %.3f",
			got.Hex(), ContrastRatio(got, bg), back.Hex(), ContrastRatio(back, bg))
	}
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func TestEnsureContrastIsDeterministic(t *testing.T) {
	t.Parallel()
	fg, _ := ParseColor("#5a5f66")
	bg, _ := ParseColor("#1d2025")
	first := EnsureContrast(fg, bg, 4.5)
	for range 16 {
		if got := EnsureContrast(fg, bg, 4.5); got != first {
			t.Fatalf("EnsureContrast is not deterministic: %+v then %+v", first, got)
		}
	}
	// Repairing an already-repaired colour is a no-op, so resolving twice
	// cannot drift the palette.
	if got := EnsureContrast(first, bg, 4.5); got != first {
		t.Errorf("repair is not idempotent: %+v -> %+v", first, got)
	}
}

func TestEnsureContrastPreservesAlpha(t *testing.T) {
	t.Parallel()
	bg, _ := ParseColor("#1d2025")
	for _, alpha := range []uint8{0x00, 0x40, 0x80, 0xcc, 0xff} {
		fg := Color{R: 0x5a, G: 0x5f, B: 0x66, A: alpha}
		if got := EnsureContrast(fg, bg, 4.5); got.A != alpha {
			t.Errorf("alpha %#02x became %#02x", alpha, got.A)
		}
		// The unreachable path returns an endpoint; it must carry alpha too.
		if got := EnsureContrast(fg, bg, 21); got.A != alpha {
			t.Errorf("unreachable repair dropped alpha %#02x -> %#02x", alpha, got.A)
		}
	}
}

func TestEnsureContrastReturnsTheBestEndpointWhenUnreachable(t *testing.T) {
	t.Parallel()
	// Nothing reaches 7:1 against a saturated mid blue; pure black is the
	// closest a foreground can get, and a frame still has to paint.
	bg, _ := ParseColor("#0080ff")
	fg, _ := ParseColor("#0b1016")
	got := EnsureContrast(fg, bg, 7.0)
	black := Color{A: 0xff}
	if got != black {
		t.Errorf("EnsureContrast = %s, want the black endpoint %s", got.Hex(), black.Hex())
	}
	if ContrastRatio(got, bg) >= 7.0 {
		t.Fatal("test premise is wrong: 7:1 is reachable against this background")
	}
}

func TestEnsureContrastPicksTheReachableDirection(t *testing.T) {
	t.Parallel()
	// White on this red tops out at 3.17:1, so a 4.5 request has to travel
	// toward black even though the foreground starts at white.
	bg, _ := ParseColor("#ff5449")
	white, _ := ParseColor("#ffffff")
	got := EnsureContrast(white, bg, 4.5)
	if ContrastRatio(got, bg) < 4.5 {
		t.Fatalf("did not reach 4.5: %s = %.3f", got.Hex(), ContrastRatio(got, bg))
	}
	if got.Luminance() > bg.Luminance() {
		t.Errorf("repair went toward white; %s is lighter than the background", got.Hex())
	}
}
