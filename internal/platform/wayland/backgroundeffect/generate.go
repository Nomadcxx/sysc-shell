// Package backgroundeffect holds the generated ext-background-effect binding.
//
// Upstream: https://gitlab.freedesktop.org/wayland/wayland-protocols
// staging/ext-background-effect/ext-background-effect-v1.xml as released in
// wayland-protocols 1.45. It provides ext_background_effect_manager_v1
// version 1, which Niri advertises from 26.04.
// SHA-256:  9aa5011f38752c0146014f8569f3d3981db7dbb8c04e578a80beef6c98fa9653
//
// This revision declares the blur capability as value 0 in a bitfield, which
// no flag can carry. The corrected wire mask is 1, and that is what Niri
// sends, so callers test flags&1 and never the generated constant.
package backgroundeffect

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg backgroundeffect -o background_effect.go -i ../../../../protocols/ext-background-effect-v1.xml
