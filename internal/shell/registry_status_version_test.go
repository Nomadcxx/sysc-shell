package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/buildinfo"
)

func TestStatusVersionIsTheStampLine(t *testing.T) {
	var nilRegistry *Registry
	if got := nilRegistry.Status()["version"]; got != buildinfo.Line() {
		t.Fatalf("nil registry version = %v, want %q", got, buildinfo.Line())
	}

	r := &Registry{}
	if got := r.Status()["version"]; got != buildinfo.Line() {
		t.Fatalf("version = %v, want %q", got, buildinfo.Line())
	}
}
