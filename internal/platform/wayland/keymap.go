package wayland

import (
	"context"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	xkb "github.com/thegrumpylion/xkb-go"
)

// keymapResolver turns evdev codes into keysyms and text through the
// compositor's keymap. xkb-go is pinned at v0.1.0; two of its defects are
// worked around here and recorded in the design doc:
//   - Keymap.ModGetIndex returns -1 for every name on a parsed keymap, so
//     modifier masks are passed through by fixed real-modifier bit.
//   - Caps Lock does not select the capital level of alphabetic keys, so
//     letters are cased here.
type keymapResolver struct {
	state   *xkb.State
	compose *xkb.ComposeState
}

// newKeymapResolver parses an xkb_v1 keymap. composeFile names a compose
// table; "" loads the locale's. A compose table that fails to load leaves
// compose off, never the keymap.
func newKeymapResolver(text []byte, composeFile string) (*keymapResolver, error) {
	ctx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	km, err := ctx.NewKeymapFromString(text, xkb.KeymapFormatTextV1)
	if err != nil {
		return nil, err
	}
	k := &keymapResolver{state: km.NewState()}
	var table *xkb.ComposeTable
	if composeFile != "" {
		table, err = ctx.NewComposeTableFromFile(composeFile, "C", xkb.ComposeCompileNoFlags)
	} else {
		table, err = ctx.NewComposeTableFromLocale(composeLocale(), xkb.ComposeCompileNoFlags)
	}
	if err == nil && table != nil {
		k.compose = table.NewState(xkb.ComposeStateNoFlags)
	}
	return k, nil
}

// composeLocale follows the C library's precedence for LC_CTYPE.
func composeLocale() string {
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if s := os.Getenv(v); s != "" {
			return s
		}
	}
	return "C"
}

func (k *keymapResolver) setMask(depressed, latched, locked, group uint32) {
	k.state.UpdateMask(xkb.ModMask(depressed), xkb.ModMask(latched), xkb.ModMask(locked), 0, 0, xkb.Group(group))
}

// resetCompose abandons an open compose sequence.
func (k *keymapResolver) resetCompose() {
	if k.compose != nil {
		k.compose.Reset()
	}
}

// peek returns the keysym for a code without touching compose state.
func (k *keymapResolver) peek(code uint32) (uint32, string) {
	return uint32(k.state.KeyGetOneSym(xkb.Keycode(code + 8))), ""
}

// resolve returns the keysym and text for an evdev code under the current
// mask. Text is "" for non-printing keys, while a compose sequence is open,
// and never carries a control character.
func (k *keymapResolver) resolve(code uint32, mods ui.Mods) (uint32, string) {
	kc := xkb.Keycode(code + 8)
	sym := k.state.KeyGetOneSym(kc)
	text := k.state.KeyGetUTF8(kc)
	// A modifier press inside a sequence (Shift for a capital) must not
	// reach compose: xkb-go cancels on any keysym the table lacks.
	if k.compose != nil && !mods.Has(ui.ModCtrl) && !isModifierSym(uint32(sym)) {
		if k.compose.Feed(sym) == xkb.ComposeFeedAccepted {
			switch k.compose.GetStatus() {
			case xkb.ComposeComposing:
				return uint32(sym), ""
			case xkb.ComposeComposed:
				text = k.compose.GetUTF8()
				if s := k.compose.GetOneSym(); s != xkb.KeyNoSymbol {
					sym = s
				}
				k.compose.Reset()
			case xkb.ComposeCancelled:
				k.compose.Reset()
				return uint32(sym), ""
			}
		}
	}
	return uint32(sym), stripControl(caseForLock(text, mods))
}

// isModifierSym reports the keysyms that only change state: Shift through
// Hyper, the ISO level and group shifts, Mode_switch and Num_Lock.
func isModifierSym(sym uint32) bool {
	return sym >= 0xffe1 && sym <= 0xffee || sym >= 0xfe01 && sym <= 0xfe13 ||
		sym == 0xff7e || sym == 0xff7f
}

// caseForLock applies Caps Lock to a single cased letter: upper with Lock
// alone, lower with Lock and Shift. xkb-go v0.1.0 leaves this undone.
func caseForLock(text string, mods ui.Mods) string {
	if !mods.Has(ui.ModCapsLock) {
		return text
	}
	r, size := utf8.DecodeRuneInString(text)
	if size != len(text) || !unicode.IsLetter(r) || unicode.ToUpper(r) == unicode.ToLower(r) {
		return text
	}
	if mods.Has(ui.ModShift) {
		return string(unicode.ToLower(r))
	}
	return string(unicode.ToUpper(r))
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
