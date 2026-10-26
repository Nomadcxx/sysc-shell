package wayland

import (
	"slices"
	"testing"
)

func allRequiredGlobals() map[string]uint32 {
	globals := make(map[string]uint32, len(interfaceMaximum))
	for _, name := range requiredSingletons {
		globals[name] = interfaceMaximum[name]
	}
	return globals
}

func TestProtocolReportNamesEveryMissingRequiredInterface(t *testing.T) {
	globals := allRequiredGlobals()
	delete(globals, "wl_seat")
	delete(globals, "wp_viewporter")

	report := protocolReport(globals, 17)
	want := []string{"wl_seat", "wp_viewporter"}
	if !slices.Equal(report.Missing, want) {
		t.Fatalf("missing = %v, want %v", report.Missing, want)
	}
	if report.Globals != 17 {
		t.Fatalf("globals = %d, want 17", report.Globals)
	}
	if len(report.Protocols) != len(requiredSingletons)-2 {
		t.Fatalf("protocols = %d, want %d", len(report.Protocols), len(requiredSingletons)-2)
	}
}

func TestProtocolReportCapsVersionsAtTheShellsMaximum(t *testing.T) {
	globals := allRequiredGlobals()
	globals["wl_compositor"] = 99
	globals["zwlr_layer_shell_v1"] = 1

	report := protocolReport(globals, len(globals))
	versions := make(map[string]uint32, len(report.Protocols))
	for _, protocol := range report.Protocols {
		versions[protocol.Name] = protocol.Version
	}
	if got := versions["wl_compositor"]; got != interfaceMaximum["wl_compositor"] {
		t.Fatalf("wl_compositor = %d, want %d", got, interfaceMaximum["wl_compositor"])
	}
	if got := versions["zwlr_layer_shell_v1"]; got != 1 {
		t.Fatalf("zwlr_layer_shell_v1 = %d, want 1", got)
	}
	if len(report.Missing) != 0 {
		t.Fatalf("missing = %v, want none", report.Missing)
	}
}

func TestProtocolReportSortsProtocolsAndMissingNames(t *testing.T) {
	globals := allRequiredGlobals()
	delete(globals, "wl_seat")
	delete(globals, "wl_shm")

	report := protocolReport(globals, len(globals))
	if !slices.IsSortedFunc(report.Protocols, func(a, b Protocol) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	}) {
		t.Fatalf("protocols are not sorted: %v", report.Protocols)
	}
	if !slices.IsSorted(report.Missing) {
		t.Fatalf("missing is not sorted: %v", report.Missing)
	}
}
