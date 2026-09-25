package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func TestRegistryRecordsCompositorBlur(t *testing.T) {
	t.Parallel()
	r := &Registry{}
	r.mu.Lock()
	if r.blurAvailableLocked() {
		t.Fatal("a registry that heard nothing reports blur; the zero answer must be no")
	}
	r.mu.Unlock()
	r.SetCapabilities(wayland.Capabilities{Blur: true})
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.blurAvailableLocked() {
		t.Fatal("blur was reported but not recorded")
	}
}
