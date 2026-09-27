package wayland

// Capabilities are the optional compositor features the shell styles around.
// They are compositor-wide, not per output, so they reach the application
// through Callbacks rather than through a host.
type Capabilities struct {
	// Blur reports that ext_background_effect_v1 will blur behind a region
	// a surface publishes.
	Blur bool
}

// blurCapabilityMask is the blur bit on the wire. The 1.45 XML declares it
// as 0, which no bitfield flag can carry; Niri sends 1.
const blurCapabilityMask = 1

func blurCapable(flags uint32) bool { return flags&blurCapabilityMask != 0 }

// capabilityState remembers the last capabilities reported, so a compositor
// that repeats its event does not restyle every surface.
type capabilityState struct {
	known   bool
	current Capabilities
}

// update records one capabilities event and reports it when it is the first
// or differs from the last. report may be nil.
func (s *capabilityState) update(flags uint32, report func(Capabilities)) {
	next := Capabilities{Blur: blurCapable(flags)}
	if s.known && next == s.current {
		return
	}
	s.known, s.current = true, next
	if report != nil {
		report(next)
	}
}
