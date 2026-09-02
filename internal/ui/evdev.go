package ui

import "unicode"

// EvdevText maps a US QWERTY evdev code to a character. This session has no
// IME, and niri does not turn latin keys into text-input-v3 commits, so a
// text field that only listened for IME ate every letter.
//
// ponytail: US QWERTY only. Upgrade is parsing the wl_keyboard keymap.
func EvdevText(key uint32, shift bool) (string, bool) {
	if key == 57 {
		return " ", true
	}
	plain, shifted, ok := evdevPair(key)
	if !ok {
		return "", false
	}
	if shift {
		return shifted, true
	}
	return plain, true
}

func evdevPair(key uint32) (plain, shifted string, ok bool) {
	switch key {
	case 2:
		return "1", "!", true
	case 3:
		return "2", "@", true
	case 4:
		return "3", "#", true
	case 5:
		return "4", "$", true
	case 6:
		return "5", "%", true
	case 7:
		return "6", "^", true
	case 8:
		return "7", "&", true
	case 9:
		return "8", "*", true
	case 10:
		return "9", "(", true
	case 11:
		return "0", ")", true
	case 12:
		return "-", "_", true
	case 13:
		return "=", "+", true
	case 16:
		return letter("q")
	case 17:
		return letter("w")
	case 18:
		return letter("e")
	case 19:
		return letter("r")
	case 20:
		return letter("t")
	case 21:
		return letter("y")
	case 22:
		return letter("u")
	case 23:
		return letter("i")
	case 24:
		return letter("o")
	case 25:
		return letter("p")
	case 26:
		return "[", "{", true
	case 27:
		return "]", "}", true
	case 30:
		return letter("a")
	case 31:
		return letter("s")
	case 32:
		return letter("d")
	case 33:
		return letter("f")
	case 34:
		return letter("g")
	case 35:
		return letter("h")
	case 36:
		return letter("j")
	case 37:
		return letter("k")
	case 38:
		return letter("l")
	case 39:
		return ";", ":", true
	case 40:
		return "'", "\"", true
	case 41:
		return "`", "~", true
	case 43:
		return "\\", "|", true
	case 44:
		return letter("z")
	case 45:
		return letter("x")
	case 46:
		return letter("c")
	case 47:
		return letter("v")
	case 48:
		return letter("b")
	case 49:
		return letter("n")
	case 50:
		return letter("m")
	case 51:
		return ",", "<", true
	case 52:
		return ".", ">", true
	case 53:
		return "/", "?", true
	}
	return "", "", false
}

func letter(s string) (string, string, bool) {
	r := []rune(s)[0]
	return s, string(unicode.ToUpper(r)), true
}
