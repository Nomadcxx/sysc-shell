// Package gamma holds the generated wlr-gamma-control binding.
//
// Upstream: https://gitlab.freedesktop.org/wlroots/wlr-protocols
// unstable/wlr-gamma-control-unstable-v1.xml at commit
// 29d4a59df8cbbc719fea9fe84689a45569410a86. It provides
// zwlr_gamma_control_manager_v1 version 1, which is the version Niri 26.04
// advertises. This package is in-tree because the sysc-wayland gamma package
// (Nomadcxx/sysc-wayland#33) is not yet tagged; see
// docs/plans/2026-10-09-night-light.md.
// SHA-256: 4065cbc291a80348b7ef311168fbfb5cf245efe977a6dc32211291ef1a9529a1
package gamma

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg gamma -o gamma.go -i ../../../../protocols/wlr-gamma-control-unstable-v1.xml
