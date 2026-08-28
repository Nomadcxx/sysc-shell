// Package wayland owns the Wayland connection and every proxy created from it.
// One goroutine performs all Wayland work; other goroutines communicate through
// channels. This file holds the pure registry state, which is testable without
// a compositor.
package wayland

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// formatARGB8888 is the wl_shm format the painter writes. Niri advertises it,
// but it arrives last in the format list, so presence must be confirmed rather
// than assumed.
const formatARGB8888 = uint32(client.ShmFormatArgb8888)

// interfaceMaximum caps each global at the version this package implements.
// The generated bindings export no per-interface version constant, so the
// client maximum is an explicit table owned here.
var interfaceMaximum = map[string]uint32{
	"wl_compositor":                  6,
	"wl_shm":                         1,
	"wl_seat":                        7,
	"wl_output":                      4,
	"zwlr_layer_shell_v1":            4,
	"wp_fractional_scale_manager_v1": 1,
	"wp_viewporter":                  1,
}

// requiredSingletons must all be present before the proof can start. The
// fractional-scale manager and viewporter are required rather than optional:
// the proof exists to qualify that path, so an integer-scale fallback would let
// it pass without proving its architecture.
var requiredSingletons = []string{
	"wl_compositor",
	"wl_shm",
	"wl_seat",
	"zwlr_layer_shell_v1",
	"wp_fractional_scale_manager_v1",
	"wp_viewporter",
}

// bindVersion reports the version to bind an interface at, which is the lower
// of the server's version and this package's maximum, and whether the proof
// uses the interface at all.
func bindVersion(iface string, server uint32) (uint32, bool) {
	maximum, ok := interfaceMaximum[iface]
	if !ok {
		return 0, false
	}
	return min(server, maximum), true
}

// globalEntry is a bound singleton global.
type globalEntry struct {
	global  uint32
	version uint32
}

// outputEntry is one output host. Its identity is the wl_registry global name;
// the connector string is an attribute that a reconnect or rename may reuse.
type outputEntry struct {
	global    uint32
	version   uint32
	connector string
}

// registryState records what the compositor advertises. Wayland owns output
// existence: only wl_registry.global and global_remove create or destroy hosts.
type registryState struct {
	singletons map[string]globalEntry
	outputs    map[uint32]*outputEntry
	arrival    []uint32
	formats    map[uint32]struct{}
}

func newRegistryState() *registryState {
	return &registryState{
		singletons: make(map[string]globalEntry),
		outputs:    make(map[uint32]*outputEntry),
		formats:    make(map[uint32]struct{}),
	}
}

// addGlobal records an advertised global and reports the version to bind at.
func (r *registryState) addGlobal(global uint32, iface string, server uint32) (uint32, bool) {
	version, ok := bindVersion(iface, server)
	if !ok {
		return 0, false
	}
	if iface == "wl_output" {
		if _, exists := r.outputs[global]; !exists {
			r.outputs[global] = &outputEntry{global: global, version: version}
			r.arrival = append(r.arrival, global)
		}
		return version, true
	}
	r.singletons[iface] = globalEntry{global: global, version: version}
	return version, true
}

// removeGlobal forgets a global. It reports the output host to destroy when the
// removed global was one.
func (r *registryState) removeGlobal(global uint32) (*outputEntry, bool) {
	if out, ok := r.outputs[global]; ok {
		delete(r.outputs, global)
		r.arrival = slices.DeleteFunc(r.arrival, func(g uint32) bool { return g == global })
		return out, true
	}
	for iface, entry := range r.singletons {
		if entry.global == global {
			delete(r.singletons, iface)
			break
		}
	}
	return nil, false
}

// setOutputName records a wl_output.name event, which version 4 supplies
// directly. The proof never correlates connectors through Niri IPC.
func (r *registryState) setOutputName(global uint32, connector string) {
	if out, ok := r.outputs[global]; ok {
		out.connector = connector
	}
}

func (r *registryState) addFormat(format uint32) { r.formats[format] = struct{}{} }

func (r *registryState) hasFormat(format uint32) bool {
	_, ok := r.formats[format]
	return ok
}

// missingRequired names every interface the proof needs and has not seen.
func (r *registryState) missingRequired() []string {
	var missing []string
	for _, iface := range requiredSingletons {
		if _, ok := r.singletons[iface]; !ok {
			missing = append(missing, iface)
		}
	}
	if len(r.outputs) == 0 {
		missing = append(missing, "wl_output")
	}
	return missing
}

var errNoOutput = errors.New("wayland: no output matched")

// selectOutput picks the requested connector, or the first output that arrived
// when no connector was requested.
func (r *registryState) selectOutput(want string) (*outputEntry, error) {
	if len(r.arrival) == 0 {
		return nil, fmt.Errorf("%w: the compositor advertised no output", errNoOutput)
	}
	if want == "" {
		return r.outputs[r.arrival[0]], nil
	}
	for _, global := range r.arrival {
		if out := r.outputs[global]; out.connector == want {
			return out, nil
		}
	}
	names := make([]string, 0, len(r.arrival))
	for _, global := range r.arrival {
		names = append(names, r.outputs[global].connector)
	}
	return nil, fmt.Errorf("%w: no output is named %q; the compositor reported %v", errNoOutput, want, names)
}
