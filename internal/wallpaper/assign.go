package wallpaper

import "slices"

// AllOutputs is the output-select token the picker sends for "All". It is not
// a connector name and never reaches an engine: the store expands it over the
// connectors that are live right now (D14). gSlapper's own `*` wildcard is
// deliberately unused, because one instance owning every output blanks the
// others as soon as a second wallpaper is assigned.
const AllOutputs = "all"

// State is what an output's engine is doing. It is runtime only: it describes
// a process, so it is rebuilt from scratch rather than persisted.
type State uint8

const (
	// StateStatic is a still on the fallback engine, or nothing at all.
	StateStatic State = iota
	StateStarting
	StatePlaying
	StatePaused
	StateError
)

// Assignment is what the user chose for one output. This is the persisted
// shape (D19); everything derived from a running process lives in Runtime.
type Assignment struct {
	Kind Kind
	Path string
	// PreviewPath is a still for a video, used for the theme seed and the
	// static fallback on Restore. Empty when we could not extract one.
	PreviewPath string
	// DesiredPlayback is what the user asked for, not what is happening: a
	// paused video that reconnects comes back paused.
	DesiredPlayback State
}

// Runtime is the live half of an output's wallpaper, discarded on disconnect.
type Runtime struct {
	State State
	// Socket is the gSlapper control socket we own for this output. It is the
	// only handle used to stop an instance, so an empty Socket means there is
	// nothing of ours to stop.
	Socket string
	// FallbackPID is an awww or swaybg process *we* started. A fallback we did
	// not start is left alone (D17/D18).
	FallbackPID int
	Err         string
}

// Job is one output's share of an apply. The generation is carried through
// the engine work and back, so a slow apply that lands after a newer one
// cannot commit over it.
type Job struct {
	Connector string
	Gen       uint64
	Path      string
	Kind      Kind
}

// Store holds the assignments, the live runtime, and the per-connector
// generation counter. It is a plain value with methods: the service owns the
// only instance and serialises access, so the store takes no lock of its own.
type Store struct {
	connectors []string
	assigned   map[string]Assignment
	runtime    map[string]Runtime
	gen        map[string]uint64
	seed       string
}

func (s *Store) ensure() {
	if s.assigned == nil {
		s.assigned = make(map[string]Assignment)
		s.runtime = make(map[string]Runtime)
		s.gen = make(map[string]uint64)
	}
}

// SetConnectors replaces the live output list. Startup reconcile calls it once
// with what the compositor reports; hotplug uses Disconnect and Reconnect.
func (s *Store) SetConnectors(names []string) {
	s.ensure()
	s.connectors = slices.Clone(names)
}

// Connectors returns the live output list.
func (s *Store) Connectors() []string {
	return slices.Clone(s.connectors)
}

// Assignment returns what is assigned to one output, connected or not.
func (s *Store) Assignment(connector string) (Assignment, bool) {
	s.ensure()
	a, ok := s.assigned[connector]
	return a, ok
}

// Runtime returns the live state of one output. An output we are doing nothing
// for reports the zero value, which is StateStatic.
func (s *Store) Runtime(connector string) Runtime {
	s.ensure()
	return s.runtime[connector]
}

// SeedPath is the image the theme should be generated from: the last image
// applied, or the last still extracted from a video. A video with no still
// leaves it alone rather than clearing it, so a theme never collapses to the
// fallback palette just because one file had no extractable frame (D15).
func (s *Store) SeedPath() string { return s.seed }

// Apply opens one generation per targeted output and returns the work to do.
// token is either AllOutputs or a single connector name. Nothing is committed
// here: the caller runs the engine and reports back with Commit or Fail.
func (s *Store) Apply(token, path string, kind Kind) []Job {
	s.ensure()
	targets := []string{token}
	if token == AllOutputs {
		targets = s.connectors
	} else if !slices.Contains(s.connectors, token) {
		// An output that is not connected has nothing to apply to. The
		// assignment it already holds is untouched.
		return nil
	}

	jobs := make([]Job, 0, len(targets))
	for _, c := range targets {
		s.gen[c]++
		rt := s.runtime[c]
		rt.State = StateStarting
		rt.Err = ""
		s.runtime[c] = rt
		jobs = append(jobs, Job{Connector: c, Gen: s.gen[c], Path: path, Kind: kind})
	}
	return jobs
}

// current reports whether r is still the newest apply for its output.
func (s *Store) current(j Job) bool {
	s.ensure()
	return s.gen[j.Connector] == j.Gen
}

// Commit records a successful apply. preview is the still extracted for a
// video, empty for an image or when extraction failed. It returns false for a
// stale generation, which the caller treats as work to discard rather than an
// error to show.
func (s *Store) Commit(j Job, preview string) bool {
	if !s.current(j) {
		return false
	}
	prior := s.assigned[j.Connector]
	a := Assignment{Kind: j.Kind, Path: j.Path, PreviewPath: preview, DesiredPlayback: StatePlaying}
	if j.Kind == KindImage {
		a.DesiredPlayback = StateStatic
	} else if prior.Path == j.Path && prior.DesiredPlayback == StatePaused {
		// Re-applying the file that is already there keeps a user's pause.
		a.DesiredPlayback = StatePaused
	}
	s.assigned[j.Connector] = a

	rt := s.runtime[j.Connector]
	rt.State = a.DesiredPlayback
	rt.Err = ""
	s.runtime[j.Connector] = rt

	if seed := seedFor(a); seed != "" {
		s.seed = seed
	}
	return true
}

// Fail records a refused apply. The output keeps whatever it had: a partial
// All leaves the outputs that worked changed and the rest exactly as they were
// (D14), so a failure never blanks a desktop.
func (s *Store) Fail(j Job, err error) bool {
	if !s.current(j) {
		return false
	}
	rt := s.runtime[j.Connector]
	rt.State = StateError
	if err != nil {
		rt.Err = err.Error()
	}
	s.runtime[j.Connector] = rt
	return true
}

// seedFor is the image a theme can be generated from, or "" for a video whose
// still is missing.
func seedFor(a Assignment) string {
	if a.Kind == KindImage {
		return a.Path
	}
	return a.PreviewPath
}

// Disconnect drops an output from the live list and discards its runtime while
// keeping the assignment, so a monitor that comes back gets its wallpaper back
// rather than an empty desktop (D20).
func (s *Store) Disconnect(connector string) {
	s.ensure()
	s.connectors = slices.DeleteFunc(s.connectors, func(c string) bool { return c == connector })
	delete(s.runtime, connector)
}

// Reconnect returns the output to the live list and replays its saved
// assignment. An output we have never seen assigned returns no work: a new
// monitor stays untouched until the user picks something for it (D20).
func (s *Store) Reconnect(connector string) []Job {
	s.ensure()
	if !slices.Contains(s.connectors, connector) {
		s.connectors = append(s.connectors, connector)
	}
	a, ok := s.assigned[connector]
	if !ok {
		return nil
	}
	return s.Apply(connector, a.Path, a.Kind)
}
