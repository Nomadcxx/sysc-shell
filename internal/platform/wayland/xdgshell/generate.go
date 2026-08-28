// Package xdgshell holds the generated xdg-shell binding. Layer-shell
// references xdg_popup, so this package exists even though the proof creates no
// xdg surface.
//
// Upstream: https://gitlab.freedesktop.org/wayland/wayland-protocols
// Revision: tag 1.49, stable/xdg-shell/xdg-shell.xml
// SHA-256:  7ba7f9c8473deee674cb1f154a18abd0bb0cc072604fc055b0c15e459fc4c7df
//
// The -prefix flag is mandatory here: the layer-shell generator emits its
// xdg_popup references against the trimmed names.
package xdgshell

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg xdgshell -prefix xdg_ -o xdg_shell.go -i ../../../../protocols/xdg-shell.xml
