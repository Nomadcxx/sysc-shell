package wallpaper

import (
	"slices"
	"testing"
)

func TestAwwwImgArgs(t *testing.T) {
	args := awwwImgArgs("/w/a.png", "DP-1")
	want := []string{"awww", "img", "--outputs", "DP-1", "/w/a.png"}
	if !slices.Equal(args, want) {
		t.Fatalf("got %v, want %v", args, want)
	}
}

func TestSwaybgArgs(t *testing.T) {
	args := swaybgArgs("/w/a.png", "DP-1")
	want := []string{"swaybg", "-o", "DP-1", "-i", "/w/a.png", "-m", "fill"}
	if !slices.Equal(args, want) {
		t.Fatalf("got %v, want %v", args, want)
	}
}

func TestFallbackIsPerConnector(t *testing.T) {
	// There is no all-outputs form at this layer: the service calls once per
	// connector, so a fan-out failure is per output (D14).
	one := awwwImgArgs("/w/a.png", "DP-1")
	two := awwwImgArgs("/w/a.png", "DP-3")
	if slices.Equal(one, two) {
		t.Fatal("each connector must get its own argv")
	}
	for _, args := range [][]string{one, two} {
		if slices.Contains(args, "*") || slices.Contains(args, "all") {
			t.Fatalf("%v must not carry a wildcard", args)
		}
	}
}

func TestPickFallback(t *testing.T) {
	both := func(name string) bool { return name == "awww" || name == "swaybg" }
	if got, err := pickFallback(both); err != nil || got != "awww" {
		t.Fatalf("got %q, %v; awww is preferred when present", got, err)
	}
	onlySwaybg := func(name string) bool { return name == "swaybg" }
	if got, err := pickFallback(onlySwaybg); err != nil || got != "swaybg" {
		t.Fatalf("got %q, %v; swaybg is the second choice", got, err)
	}
	none := func(string) bool { return false }
	if _, err := pickFallback(none); err == nil {
		t.Fatal("no static engine on PATH must be an error, not a silent no-op")
	}
}
