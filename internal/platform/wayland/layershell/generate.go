// Package layershell holds the generated wlr-layer-shell binding.
//
// Upstream: https://gitlab.freedesktop.org/wlroots/wlr-protocols
// Revision: 2b8d43325b7012cc3f9b55c08d26e50e42beac7d, the commit adding
// set_exclusive_edge, at unstable/wlr-layer-shell-unstable-v1.xml. It provides
// zwlr_layer_shell_v1 version 5, which is the version Niri 26.04 advertises.
// SHA-256:  87e0b9c837aecd6977f76f3c47d73088b7159871f5d979dc1840f6cadb5e2ed8
package layershell

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg layershell -xdg-shell-import github.com/Nomadcxx/sysc-shell/internal/platform/wayland/xdgshell -o layer_shell.go -i ../../../../protocols/wlr-layer-shell-unstable-v1.xml
