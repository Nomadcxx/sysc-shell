package wallpaper

import "testing"

func TestParseList(t *testing.T) {
	got := ParseList("effect fire 0\neffect fire-text 1\ntheme  nord nord,default\ntheme  dracula\nversion 1.0.3\n")
	if len(got.Effects) != 2 {
		t.Fatalf("effects = %+v", got.Effects)
	}
	if got.Effects[0] != (EffectInfo{ID: "fire"}) {
		t.Fatalf("fire = %+v", got.Effects[0])
	}
	if got.Effects[1] != (EffectInfo{ID: "fire-text", Text: true}) {
		t.Fatalf("fire-text = %+v", got.Effects[1])
	}
	if len(got.Themes) != 2 || got.Themes[0] != "nord" || got.Themes[1] != "dracula" {
		t.Fatalf("themes = %v", got.Themes)
	}
}

func TestParseListEmptyIsNotACatalog(t *testing.T) {
	if got := ParseList("usage: sysc-terminal\n"); len(got.Effects) != 0 {
		t.Fatalf("help text parsed as catalog: %+v", got)
	}
}
