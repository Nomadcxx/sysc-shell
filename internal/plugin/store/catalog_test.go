package store

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestResolve(t *testing.T) {
	t.Parallel()
	asset := map[string]catalog.Asset{"linux-amd64": validAsset()}
	tooNew := v1Version(1, plugin.HostProtocolMinor+1)
	cases := []struct {
		name    string
		entry   catalog.Entry
		arch    string
		compat  Compat
		version string
		noAsset bool
	}{
		{"tip fits", entryFor(catalog.Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: asset}), "amd64", Compatible, "1.4.0", false},
		{"tip too new, older fits", entryFor(
			catalog.Release{Version: "2.0.0", Protocol: tooNew, Assets: asset},
			catalog.Release{Version: "1.3.2", Protocol: v1Version(1, 0), Assets: asset},
			catalog.Release{Version: "1.2.0", Protocol: v1Version(1, 0), Assets: asset},
		), "amd64", HeldBack, "1.3.2", false},
		{"nothing fits", entryFor(catalog.Release{Version: "2.0.0", Protocol: v1Version(2, 0), Assets: asset}), "amd64", Incompatible, "", false},
		{"no asset for arch", entryFor(catalog.Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: asset}), "arm64", Incompatible, "", true},
	}
	for _, c := range cases {
		got := Resolve(c.entry, c.arch)
		if got.Compat != c.compat {
			t.Errorf("%s: compat %s, want %s", c.name, got.Compat, c.compat)
		}
		if c.version == "" && got.Release != nil || c.version != "" && (got.Release == nil || got.Release.Version != c.version) {
			t.Errorf("%s: release %+v, want %q", c.name, got.Release, c.version)
		}
		if got.Needs != c.entry.Protocol {
			t.Errorf("%s: needs %+v, want the tip's %+v", c.name, got.Needs, c.entry.Protocol)
		}
		if got.NoAsset != c.noAsset {
			t.Errorf("%s: NoAsset %v, want %v", c.name, got.NoAsset, c.noAsset)
		}
	}
}
