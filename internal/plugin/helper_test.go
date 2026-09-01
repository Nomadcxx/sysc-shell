package plugin

import (
	"encoding/json"
	"os"
	"testing"
)

// TestMain intercepts the helper re-execution before the testing package parses
// flags, so a fake plugin is this same binary behaving differently rather than
// a second program to build and ship.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == HelperFlag {
		os.Exit(HelperServe(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// installHelper writes a plugin directory whose entry point runs this test
// binary in the given helper mode.
func installHelper(t *testing.T, mode string) Manifest {
	t.Helper()
	return installHelperWith(t, mode, timerManifest)
}

func installHelperWith(t *testing.T, mode, manifest string) Manifest {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	m, err := WriteHelperPlugin(t.TempDir(), self, mode, manifest)
	if err != nil {
		t.Fatalf("WriteHelperPlugin for helper %q: %v", mode, err)
	}
	return m
}

// jsonOf is a small helper for building raw payloads in tests.
func jsonOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// errDiscovery stands in for a rejection produced by the scan.
var errDiscovery = errTest("manifest.json is malformed")

type errTest string

func (e errTest) Error() string { return string(e) }
