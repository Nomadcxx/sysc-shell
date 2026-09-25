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
