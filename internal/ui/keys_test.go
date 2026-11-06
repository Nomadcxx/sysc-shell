package ui

import "testing"

func TestModsFromMaskUsesRealModifierBits(t *testing.T) {
	cases := []struct {
		dep, lat, lock uint32
		want           Mods
	}{
		{0, 0, 0, 0},
		{1 << 0, 0, 0, ModShift},
		{1 << 2, 0, 0, ModCtrl},
		{1 << 3, 0, 0, ModAlt},
		{1 << 6, 0, 0, ModSuper},
		{0, 0, 1 << 1, ModCapsLock},
		{0, 1 << 0, 0, ModShift},                     // a latched Shift counts
		{1 << 7, 0, 0, 0},                            // Mod5 is AltGr's level, not Alt
		{1<<0 | 1<<2, 0, 1 << 4, ModShift | ModCtrl}, // Mod2 (NumLock) is ignored
	}
	for _, tc := range cases {
		if got := ModsFromMask(tc.dep, tc.lat, tc.lock); got != tc.want {
			t.Errorf("mask %#x/%#x/%#x = %#x, want %#x", tc.dep, tc.lat, tc.lock, got, tc.want)
		}
	}
}

func TestFallbackKey(t *testing.T) {
	cases := []struct {
		name string
		code uint32
		mods Mods
		sym  uint32
		text string
	}{
		{"a", 30, 0, 'a', "a"},
		{"Shift+a", 30, ModShift, 'A', "A"},
		{"Caps+a", 30, ModCapsLock, 'A', "A"},
		{"Caps+Shift+a", 30, ModCapsLock | ModShift, 'a', "a"},
		{"Caps+1 is not a letter", 2, ModCapsLock, '1', "1"},
		{"Shift+1", 2, ModShift, '!', "!"},
		{"space", 57, 0, ' ', " "},
		{"BackSpace", 14, 0, SymBackSpace, ""},
		{"Delete", 111, 0, SymDelete, ""},
		{"Left", 105, 0, SymLeft, ""},
		{"Home", 102, 0, SymHome, ""},
		{"Return", 28, 0, SymReturn, ""},
		{"Shift+Tab", 15, ModShift, SymISOLeftTab, ""},
		{"Ctrl+a keeps sym and text", 30, ModCtrl, 'a', "a"},
	}
	for _, tc := range cases {
		got := FallbackKey(tc.code, tc.mods)
		if got.Sym != tc.sym || got.Text != tc.text || got.Code != tc.code || got.Mods != tc.mods {
			t.Errorf("%s: %+v, want sym %#x text %q", tc.name, got, tc.sym, tc.text)
		}
	}
}

func TestNavCodeFollowsTheResolvedSymbol(t *testing.T) {
	cases := []struct {
		name string
		k    KeyInput
		want uint32
	}{
		{"Tab on the Escape code", KeyInput{Code: 1, Sym: SymTab}, 15},
		{"Escape on the Tab code", KeyInput{Code: 15, Sym: SymEscape}, 1},
		{"ISO_Left_Tab is Tab", KeyInput{Code: 23, Sym: SymISOLeftTab}, 15},
		{"KP_Enter activates as Enter", KeyInput{Code: 96, Sym: SymKPEnter}, 28},
		{"Space by symbol", KeyInput{Code: 30, Sym: ' ', Text: " "}, 57},
		{"F2 by symbol", KeyInput{Code: 2, Sym: SymF2}, 60},
		{"arrow by symbol", KeyInput{Code: 30, Sym: SymDown}, 108},
		{"a letter on the Escape code is not Escape", KeyInput{Code: 1, Sym: 'a', Text: "a"}, 0},
		{"a letter keeps its code", KeyInput{Code: 30, Sym: 'q', Text: "q"}, 30},
		{"a modifier keeps its code", KeyInput{Code: 42, Sym: 0xffe1}, 42},
		{"no keymap keeps the raw code", KeyInput{Code: 1}, 1},
		{"US fallback round-trips", FallbackKey(15, ModShift), 15},
	}
	for _, tc := range cases {
		if got := tc.k.NavCode(); got != tc.want {
			t.Errorf("%s: NavCode(%+v) = %d, want %d", tc.name, tc.k, got, tc.want)
		}
	}
}
