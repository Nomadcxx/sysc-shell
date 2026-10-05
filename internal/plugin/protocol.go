package plugin

import v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"

// The newest protocol this host speaks. The supervisor refuses anything newer,
// and the plugin store resolves catalog releases against the same ceiling, so
// a release the store offers is one the supervisor will start.
const (
	HostProtocolMajor = v1.ProtocolMajor
	HostProtocolMinor = v1.ProtocolMinor
)

// HostSupports reports whether a plugin declaring v can run on this host.
func HostSupports(v v1.Version) bool {
	return v.Major == HostProtocolMajor && v.Minor >= 0 && v.Minor <= HostProtocolMinor
}

// hostSupportedVersions is host.hello's Supported list, best first. Every
// 1.x minor this host still speaks is named so a plugin compiled against an
// older plugin/v1 can pick its own minor instead of falling back to 7.
func hostSupportedVersions() []v1.Version {
	out := make([]v1.Version, 0, HostProtocolMinor+1)
	for m := HostProtocolMinor; m >= 0; m-- {
		out = append(out, v1.Version{Major: HostProtocolMajor, Minor: m})
	}
	return out
}
