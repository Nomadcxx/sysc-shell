package wallpaper

import (
	"slices"
	"testing"
)

func TestParseList(t *testing.T) {
	got := ParseList("effect fire 0\neffect fire-text 1\neffect rain 0\ntheme  nord nord,default\ntheme  dracula\nversion 1.0.3\n")
	// Text effects need artwork the shell no longer supplies, so the catalog
	// never offers them.
	if !slices.Equal(got.Effects, []string{"fire", "rain"}) {
		t.Fatalf("effects = %v, want fire and rain without the text effect", got.Effects)
	}
	if !slices.Equal(got.Themes, []string{"nord", "dracula"}) {
		t.Fatalf("themes = %v", got.Themes)
	}
}

func TestParseListEmptyIsNotACatalog(t *testing.T) {
	if got := ParseList("usage: sysc-terminal\n"); len(got.Effects) != 0 {
		t.Fatalf("help text parsed as catalog: %+v", got)
	}
}

func TestTerminalBlurCapability(t *testing.T) {
	for _, input := range []string{"capability blur 64\n", "capability blur -1\n", "capability blur nope\n", "capability blur 1000\n", ""} {
		got := ParseList("effect fire 0\n" + input)
		want := 0
		if input == "capability blur 64\n" {
			want = 64
		}
		if got.BlurMax != want {
			t.Fatalf("%q: blur limit %d, want %d", input, got.BlurMax, want)
		}
	}
}
