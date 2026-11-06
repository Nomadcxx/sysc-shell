package ui

import (
	"unicode"
	"unicode/utf8"
)

// Mods is the modifier state a key was resolved under. AltGr is not here:
// it selects a keymap level, so its effect is already in Sym and Text.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
	ModCapsLock
)

func (m Mods) Has(x Mods) bool { return m&x != 0 }

// Real modifier bits are fixed by XKB: Shift 0, Lock 1, Control 2, Mod1 3,
// Mod2 4, Mod3 5, Mod4 6, Mod5 7. Every keymap maps Alt to Mod1 and Super to
// Mod4 in practice, and decoding by bit needs no keymap at all.
const (
	realShift = 1 << 0
	realLock  = 1 << 1
	realCtrl  = 1 << 2
	realMod1  = 1 << 3
	realMod4  = 1 << 6
)

// ModsFromMask decodes wl_keyboard.modifiers. Depressed and latched count as
// held; only Lock is read from the locked mask.
func ModsFromMask(depressed, latched, locked uint32) Mods {
	held := depressed | latched
	var m Mods
	if held&realShift != 0 {
		m |= ModShift
	}
	if held&realCtrl != 0 {
		m |= ModCtrl
	}
	if held&realMod1 != 0 {
		m |= ModAlt
	}
	if held&realMod4 != 0 {
		m |= ModSuper
	}
	if locked&realLock != 0 {
		m |= ModCapsLock
	}
	return m
}

// KeyInput is one resolved key: its evdev code, the keysym and text the
// active layout gives it, and the modifiers it was pressed under.
type KeyInput struct {
	Code   uint32
	Sym    uint32
	Text   string
	Mods   Mods
	Serial uint32
}

// X11 keysyms (keysymdef.h) the shell matches on. Printable ASCII keysyms
// equal their code points.
const (
	SymBackSpace  uint32 = 0xff08
	SymTab        uint32 = 0xff09
	SymReturn     uint32 = 0xff0d
	SymEscape     uint32 = 0xff1b
	SymHome       uint32 = 0xff50
	SymLeft       uint32 = 0xff51
	SymUp         uint32 = 0xff52
	SymRight      uint32 = 0xff53
	SymDown       uint32 = 0xff54
	SymPageUp     uint32 = 0xff55
	SymPageDown   uint32 = 0xff56
	SymEnd        uint32 = 0xff57
	SymKPEnter    uint32 = 0xff8d
	SymF2         uint32 = 0xffbf
	SymISOLeftTab uint32 = 0xfe20
	SymDelete     uint32 = 0xffff
)

var fallbackSyms = map[uint32]uint32{
	1: SymEscape, 14: SymBackSpace, 15: SymTab, 28: SymReturn, 96: SymKPEnter,
	102: SymHome, 103: SymUp, 104: SymPageUp, 105: SymLeft, 106: SymRight,
	107: SymEnd, 108: SymDown, 109: SymPageDown, 111: SymDelete,
}

// navCodes names each navigation keysym by the US evdev code the shell's key
// handlers switch on. KP_Enter and ISO_Left_Tab share their main keys' codes.
var navCodes = map[uint32]uint32{
	SymEscape: 1, SymBackSpace: 14, SymTab: 15, SymISOLeftTab: 15, SymReturn: 28,
	SymKPEnter: 28, ' ': 57, SymF2: 60, SymHome: 102, SymUp: 103, SymPageUp: 104,
	SymLeft: 105, SymRight: 106, SymEnd: 107, SymDown: 108, SymPageDown: 109,
	SymDelete: 111,
}

// NavCode is the code a key navigates as. The layout decides: a key that
// resolves to Tab is Tab whatever its evdev code, and a navigation code the
// layout gives another symbol navigates as nothing (0), so a virtual keyboard
// that sends Tab on code 1 does not act as Escape. Other keys keep their code,
// as does any key with no resolved symbol.
func (k KeyInput) NavCode() uint32 {
	if code, ok := navCodes[k.Sym]; ok {
		return code
	}
	if k.Sym == 0 {
		return k.Code
	}
	for _, code := range navCodes {
		if code == k.Code {
			return 0
		}
	}
	return k.Code
}

// FallbackKey resolves an evdev code through the US table. It is the whole
// resolver when the compositor sent no usable keymap, and what the shell's
// raw-code test entry point uses.
func FallbackKey(code uint32, mods Mods) KeyInput {
	k := KeyInput{Code: code, Mods: mods}
	if sym, ok := fallbackSyms[code]; ok {
		k.Sym = sym
		if sym == SymTab && mods.Has(ModShift) {
			k.Sym = SymISOLeftTab
		}
		return k
	}
	plain, _ := EvdevText(code, false)
	letter := len(plain) == 1 && plain[0] >= 'a' && plain[0] <= 'z'
	shift := mods.Has(ModShift)
	if letter && mods.Has(ModCapsLock) {
		shift = !shift
	}
	text, ok := EvdevText(code, shift)
	if !ok {
		return k
	}
	k.Text = text
	if r, size := utf8.DecodeRuneInString(text); size == len(text) && r < unicode.MaxASCII {
		k.Sym = uint32(r)
	}
	return k
}
