package wayland

import (
	"fmt"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// outputProxy pairs a bound wl_output with the registry global that identifies
// it. Milestone 2 keys every output host the same way: by global name, never by
// connector string, so a reconnect or rename cannot produce a duplicate host.
type outputProxy struct {
	global uint32
	proxy  *client.Output
}

// chooseOutput resolves the requested connector to a bound proxy after the
// roundtrips that deliver wl_output.name.
func (o *owner) chooseOutput() error {
	entry, err := o.rs.selectOutput(o.options.Output)
	if err != nil {
		return err
	}
	for _, bound := range o.outputProxies {
		if bound.global == entry.global {
			o.output = bound.proxy
			o.selectedGlobal = entry.global
			o.connector = entry.connector
			return nil
		}
	}
	return fmt.Errorf("wayland: output %q was advertised but not bound", entry.connector)
}

// releaseOutputs releases every bound output proxy.
func (o *owner) releaseOutputs() error {
	for _, bound := range o.outputProxies {
		if err := bound.proxy.Release(); err != nil {
			return err
		}
	}
	o.outputProxies = nil
	o.output = nil
	return nil
}
