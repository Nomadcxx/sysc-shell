package store

import (
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

type Compat string

const (
	Compatible   Compat = "compatible"
	HeldBack     Compat = "held back"
	Incompatible Compat = "incompatible"
)

// Resolution is what this host would install from one entry.
type Resolution struct {
	// Release is nil when nothing is installable.
	Release *catalog.Release
	Compat  Compat
	// Needs is the newest release's protocol, which the manager names when
	// the row is held back or incompatible.
	Needs v1.Version
	// NoAsset is true when nothing resolved but at least one candidate's
	// protocol is supported: the blocker is the missing linux-<arch> asset,
	// not the protocol ceiling named by Needs.
	NoAsset bool
}

// Resolve picks the newest release this host can run on arch. It stays in the
// store, rather than the public catalog package, because it depends on the
// host's own protocol ceiling.
func Resolve(e catalog.Entry, arch string) Resolution {
	key := "linux-" + arch
	candidates := append([]catalog.Release{e.Release}, e.Releases...)
	var best *catalog.Release
	protocolOK := false
	for i := range candidates {
		r := &candidates[i]
		if !plugin.HostSupports(r.Protocol) {
			continue
		}
		protocolOK = true
		if _, ok := r.Assets[key]; !ok {
			continue
		}
		if best == nil || catalog.Newer(r.Version, best.Version) {
			best = r
		}
	}
	res := Resolution{Release: best, Needs: e.Protocol}
	switch {
	case best == nil:
		res.Compat = Incompatible
		res.NoAsset = protocolOK
	case best.Version == e.Version:
		res.Compat = Compatible
	default:
		res.Compat = HeldBack
	}
	return res
}
