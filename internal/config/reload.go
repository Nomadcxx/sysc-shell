package config

import "fmt"

// Output is one connected output as bar policy sees it: its connector and
// its logical size, zero while the size is not yet known.
type Output struct {
	Connector     string
	Width, Height int
}

// Resolve produces one bar policy per connector, in the order supplied.
//
// It runs before the live configuration is replaced. Resolving every connected
// output first is what prevents half the outputs adopting new policy while the
// rest keep the old one: a candidate that is valid globally but unresolvable
// for one connected output is rejected whole, and nothing is applied.
func Resolve(cfg Config, connectors []string) ([]Bar, error) {
	outputs := make([]Output, 0, len(connectors))
	for _, name := range connectors {
		outputs = append(outputs, Output{Connector: name})
	}
	return ResolveOutputs(cfg, outputs)
}

// ResolveOutputs is Resolve with each output's logical size, so a bar the
// standard density sizes per output resolves to its row (see BarFor).
func ResolveOutputs(cfg Config, outputs []Output) ([]Bar, error) {
	out := make([]Bar, 0, len(outputs))
	for _, o := range outputs {
		bar := cfg.BarFor(o.Connector, o.Width, o.Height)
		if err := validateBar(bar, o.Connector); err != nil {
			return nil, fmt.Errorf("resolving %s: %w", o.Connector, err)
		}
		out = append(out, bar)
	}
	return out, nil
}
