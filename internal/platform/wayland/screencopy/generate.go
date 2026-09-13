// Package screencopy holds the generated wlr-screencopy binding.
//
// Upstream: https://gitlab.freedesktop.org/wlroots/wlr-protocols
// unstable/wlr-screencopy-unstable-v1.xml. It provides
// zwlr_screencopy_manager_v1 version 3, which is the version Niri 26.04
// advertises.
// SHA-256:  131b8f9b4aad0c8a9cf705e90d2a1511a5ca0c477637fd3400cf1cc4fa963fb8
package screencopy

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg screencopy -o screencopy.go -i ../../../../protocols/wlr-screencopy-unstable-v1.xml
