package config

import "fmt"

// Resolve produces one bar policy per connector, in the order supplied.
//
// It runs before the live configuration is replaced. Resolving every connected
// output first is what prevents half the outputs adopting new policy while the
// rest keep the old one: a candidate that is valid globally but unresolvable
// for one connected output is rejected whole, and nothing is applied.
func Resolve(cfg Config, connectors []string) ([]Bar, error) {
	out := make([]Bar, 0, len(connectors))
	for _, name := range connectors {
		bar := cfg.ForConnector(name)
		if err := validateBar(bar, name); err != nil {
			return nil, fmt.Errorf("resolving %s: %w", name, err)
		}
		out = append(out, bar)
	}
	return out, nil
}
