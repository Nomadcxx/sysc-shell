// Package xdgactivation holds the generated xdg-activation binding.
//
// Upstream: https://gitlab.freedesktop.org/wayland/wayland-protocols
// Revision: 1.49, staging/xdg-activation/xdg-activation-v1.xml
// SHA-256:  d8418be2d5738d50aff788bef1c7574f33f26659aa045447ff2ef9b78c58fe01
package xdgactivation

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.3.3 -pkg xdgactivation -o xdg_activation.go -i ../../../../protocols/xdg-activation-v1.xml
