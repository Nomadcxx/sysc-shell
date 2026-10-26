package wayland

import (
	"context"
	"slices"
	"sort"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// Protocol is one interface the shell uses, at the version the compositor
// advertises and this package would bind. Required interfaces are the ones the
// shell cannot start without; the rest are optional features.
type Protocol struct {
	Name     string
	Version  uint32
	Required bool
}

// ProtocolReport is what a compositor advertised in one probe. Globals counts
// every advertised interface, including ones the shell does not use, so a
// report can distinguish a barren compositor from a filtered list.
type ProtocolReport struct {
	Protocols []Protocol
	Missing   []string
	Globals   int
}

// ProbeProtocols opens a short-lived Wayland connection, reads the registry and
// closes it. It does not bind a global or map a surface, so it is safe to run
// while the shell is running. The context bounds the wait for the compositor's
// first roundtrip; a compositor that stops answering fails the probe rather
// than holding the caller forever.
func ProbeProtocols(ctx context.Context) (ProtocolReport, error) {
	type result struct {
		report ProtocolReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		display, err := client.Connect("")
		if err != nil {
			done <- result{err: err}
			return
		}
		defer display.Context().Close()

		registry, err := display.GetRegistry()
		if err != nil {
			done <- result{err: err}
			return
		}
		globals := make(map[string]uint32)
		total := 0
		registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
			total++
			globals[e.Interface] = e.Version
		})
		if err := display.Roundtrip(); err != nil {
			done <- result{err: err}
			return
		}
		done <- result{report: protocolReport(globals, total)}
	}()

	select {
	case <-ctx.Done():
		return ProtocolReport{}, ctx.Err()
	case got := <-done:
		return got.report, got.err
	}
}

// protocolReport turns the advertised versions into the report the doctor
// prints. It is pure so the required-interface rule is testable without a
// compositor.
func protocolReport(globals map[string]uint32, total int) ProtocolReport {
	names := make([]string, 0, len(interfaceMaximum))
	for name := range interfaceMaximum {
		names = append(names, name)
	}
	sort.Strings(names)

	report := ProtocolReport{Globals: total}
	for _, name := range names {
		server, ok := globals[name]
		if !ok {
			if slices.Contains(requiredSingletons, name) {
				report.Missing = append(report.Missing, name)
			}
			continue
		}
		version, _ := bindVersion(name, server)
		report.Protocols = append(report.Protocols, Protocol{
			Name:     name,
			Version:  version,
			Required: slices.Contains(requiredSingletons, name),
		})
	}
	sort.Strings(report.Missing)
	return report
}
