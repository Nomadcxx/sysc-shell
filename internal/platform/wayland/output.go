package wayland

import "fmt"

// chooseOutput resolves the requested connector to the host that will carry the
// bar, after the roundtrips that deliver wl_output.name.
//
// Host identity is the registry global name, never the connector string: a
// connector can disappear and return as a different monitor, so the connector
// is only a lookup attribute.
func (o *owner) chooseOutput() error {
	entry, err := o.rs.selectOutput(o.options.Output)
	if err != nil {
		return err
	}
	h, ok := o.hosts.get(entry.global)
	if !ok {
		return fmt.Errorf("wayland: output %q was advertised but not bound", entry.connector)
	}
	o.selected = h
	return nil
}
