// Package inhibit holds the generated zwp-keyboard-shortcuts-inhibit binding.
//
// Upstream: https://gitlab.freedesktop.org/wayland/wayland-protocols
// tag 1.45, unstable/keyboard-shortcuts-inhibit/
// keyboard-shortcuts-inhibit-unstable-v1.xml. It provides
// zwp_keyboard_shortcuts_inhibit_manager_v1 version 1, which Niri 26.04
// advertises (verified on the laptop with wayland-info, 2026-09-29). The
// region selector uses it so compositor keybinds cannot steal input while the
// selector holds the keyboard.
// SHA-256:  9117d9e8ec02e9a3c3c55803b41e1227e76986f555f7c12eeb29f796fa63e69b
package inhibit

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg inhibit -o inhibit.go -i ../../../../protocols/keyboard-shortcuts-inhibit-unstable-v1.xml
