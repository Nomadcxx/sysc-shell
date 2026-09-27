package wayland

import (
	"os"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func loadResolver(t *testing.T, name string) *keymapResolver {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	k, err := newKeymapResolver(raw, "testdata/Compose.test")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

const (
	evY, evQ, evE, evA, ev2, evApostrophe, evEqual = 21, 16, 18, 30, 3, 40, 13
	maskShift, maskLock, maskCtrl, maskMod5        = 1 << 0, 1 << 1, 1 << 2, 1 << 7
)

func TestGermanLayoutResolves(t *testing.T) {
	k := loadResolver(t, "keymap-de.xkb")
	cases := []struct {
		name        string
		dep, locked uint32
		code        uint32
		wantText    string
	}{
		{"Y types z", 0, 0, evY, "z"},
		{"Shift+Y types Z", maskShift, 0, evY, "Z"},
		{"AltGr+Q types @", maskMod5, 0, evQ, "@"},
		{"AltGr+E types €", maskMod5, 0, evE, "€"},
		{"ä", 0, 0, evApostrophe, "ä"},
		{"Shift+2 types quote", maskShift, 0, ev2, "\""},
	}
	for _, tc := range cases {
		k.setMask(tc.dep, 0, tc.locked, 0)
		if _, got := k.resolve(tc.code, ui.ModsFromMask(tc.dep, 0, tc.locked)); got != tc.wantText {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

// Review focus 4: xkb-go v0.1.0 ignores Lock for alphabetic keys.
func TestCapsLockUppercasesLetters(t *testing.T) {
	k := loadResolver(t, "keymap-de.xkb")
	k.setMask(0, 0, maskLock, 0)
	if _, got := k.resolve(evA, ui.ModCapsLock); got != "A" {
		t.Fatalf("Caps+a = %q", got)
	}
	k.setMask(maskShift, 0, maskLock, 0)
	if _, got := k.resolve(evA, ui.ModCapsLock|ui.ModShift); got != "a" {
		t.Fatalf("Caps+Shift+a = %q", got)
	}
	k.setMask(0, 0, maskLock, 0)
	if _, got := k.resolve(ev2, ui.ModCapsLock); got != "2" {
		t.Fatalf("Caps+2 = %q", got)
	}
}

func TestDeadKeyComposes(t *testing.T) {
	k := loadResolver(t, "keymap-us-intl.xkb")
	k.setMask(0, 0, 0, 0)
	if sym, got := k.resolve(evApostrophe, 0); got != "" || sym != 0xfe51 {
		t.Fatalf("dead_acute = %#x %q, want dead_acute and no text", sym, got)
	}
	if _, got := k.resolve(evE, 0); got != "é" {
		t.Fatalf("dead_acute e = %q", got)
	}
}

func TestControlCharactersNeverReachText(t *testing.T) {
	k := loadResolver(t, "keymap-de.xkb")
	k.setMask(maskCtrl, 0, 0, 0)
	if sym, got := k.resolve(evA, ui.ModCtrl); sym != 'a' || got != "a" {
		t.Fatalf("Ctrl+a = %#x %q", sym, got)
	}
	for _, r := range func() string { _, s := k.resolve(28, ui.ModCtrl); return s }() {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("control character %U in text", r)
		}
	}
}

func TestMalformedKeymapIsAnError(t *testing.T) {
	if _, err := newKeymapResolver([]byte("xkb_keymap { nonsense"), ""); err == nil {
		t.Fatal("malformed keymap parsed")
	}
}

// Shift pressed inside a dead-key sequence is a modifier, not a cancel:
// dead_acute, Shift, E types É.
func TestModifierPressKeepsAComposeOpen(t *testing.T) {
	const evLeftShift = 42
	k := loadResolver(t, "keymap-us-intl.xkb")
	k.setMask(0, 0, 0, 0)
	k.resolve(evApostrophe, 0)
	if _, got := k.resolve(evLeftShift, 0); got != "" {
		t.Fatalf("Shift typed %q", got)
	}
	k.setMask(maskShift, 0, 0, 0)
	if _, got := k.resolve(evE, ui.ModShift); got != "É" {
		t.Fatalf("dead_acute Shift+E = %q, want É", got)
	}
}
