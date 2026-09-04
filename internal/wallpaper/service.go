package wallpaper

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"
)

// Op is one thing the panel or the registry can ask the service to do.
type Op uint8

const (
	// OpApply assigns Path to Token, which is a connector or AllOutputs.
	OpApply Op = iota
	OpPause
	OpResume
	// OpRestore stops our engine on Token and puts the last still on the
	// static fallback (D16).
	OpRestore
	// OpConnect and OpDisconnect carry compositor output events.
	OpConnect
	OpDisconnect
	// OpRefresh rescans the library.
	OpRefresh
)

// Command is one queued request. It is a value, so nothing the panel holds is
// shared with the service after the send.
type Command struct {
	Op    Op
	Token string
	Path  string
	Kind  Kind
}

// Capabilities is what is installed, probed once at start and projected into
// the picker's banners: without gSlapper the video tiles go inert, and without
// a static engine Restore has nowhere to go.
type Capabilities struct {
	GSlapper bool
	// Static is the chosen fallback binary name, empty when neither is present.
	Static string
}

// Engine is the side of the service that runs processes. It is an interface so
// the service can be tested without exec, a socket, or a compositor.
type Engine interface {
	// Apply puts one path on one output and returns a still for a video, which
	// may be empty when none could be extracted.
	Apply(job Job, set Settings) (preview string, err error)
	// Restore stops our engine on connector and shows still through the static
	// fallback. An empty still leaves the output blank.
	Restore(connector, still string) error
	SetPaused(connector string, paused bool) error
	Capabilities() Capabilities
}

// Snapshot is an immutable view of everything the picker draws.
type Snapshot struct {
	Library     *Library
	Connectors  []string
	Assignments map[string]Assignment
	Runtime     map[string]Runtime
	Caps        Capabilities
	Seed        string
	Err         string
	// Covered maps an output to the namespace of a foreign Background surface
	// painting over it. gSlapper reports itself as playing whether or not its
	// surface is visible, so without this an apply that nobody can see looks
	// exactly like one that worked (D18).
	Covered map[string]string
}

// ServiceConfig is what the registry hands the service at construction.
type ServiceConfig struct {
	Engine      Engine
	Settings    Settings
	Connectors  []string
	Roots       []string
	PersistPath string
	// Coverage reports which outputs a foreign wallpaper owns. It is injected
	// so the service stays free of any compositor dependency.
	Coverage func() (map[string]string, error)
	// CacheDir holds the generated previews. Empty disables the generator.
	CacheDir string
	// ThumbPace is the gap between two generated previews. Zero uses the
	// package default.
	ThumbPace time.Duration
}

type engineResult struct {
	job     Job
	preview string
	err     error
}

// Service owns the store, the library, and the engine, and is the only thing
// that mutates them. One loop goroutine serialises every state change; engine
// work runs on its own goroutine per job so a three-second socket wait on one
// output never stalls the other (D13/D14).
//
// Nothing here touches Wayland. The shell submits commands and reads
// snapshots, the same shape the notify and tray clients already use.
type Service struct {
	engine      Engine
	set         Settings
	roots       []string
	persistPath string

	cmds    chan Command
	results chan engineResult
	updates chan Snapshot
	quit    chan struct{}
	closing sync.Once
	work    sync.WaitGroup

	coverage func() (map[string]string, error)
	thumbs   *Thumbnailer
	stopWork context.CancelFunc

	// store, lib, caps, and covered are touched only by the loop goroutine.
	store   Store
	lib     *Library
	caps    Capabilities
	covered map[string]string

	mu   sync.Mutex
	snap Snapshot
	// cfgHook is the theme write-back. It is never called while mu is held:
	// it re-enters the registry, which takes its own lock.
	cfgHook func(source, seed string)
}

// NewService starts the service loop.
func NewService(cfg ServiceConfig) *Service {
	s := &Service{
		engine:      cfg.Engine,
		set:         cfg.Settings,
		roots:       slices.Clone(cfg.Roots),
		persistPath: cfg.PersistPath,
		coverage:    cfg.Coverage,
		cmds:        make(chan Command, 32),
		results:     make(chan engineResult, 32),
		// One slot, coalescing: a picker that is closed or slow must never
		// block an apply.
		updates: make(chan Snapshot, 1),
		quit:    make(chan struct{}),
	}
	s.store.SetConnectors(cfg.Connectors)
	if s.engine != nil {
		s.caps = s.engine.Capabilities()
	}
	if s.persistPath != "" {
		saved, err := LoadAssignments(s.persistPath)
		if err != nil {
			s.store.noteErr(err)
		}
		s.store.Adopt(saved)
	}
	s.lib = Scan(s.roots)
	s.refreshCoverage()
	s.publish()

	// Previews are generated slowly in the background rather than decoded on
	// demand: a library of several hundred wallpapers is tens of gigabytes of
	// pixels, and doing that work when the picker opens is what makes a
	// wallpaper chooser hang a desktop.
	if cfg.CacheDir != "" {
		ctx, cancel := context.WithCancel(context.Background())
		s.stopWork = cancel
		s.thumbs = NewThumbnailer(cfg.CacheDir, cfg.ThumbPace)
		go s.thumbs.Run(ctx)
		s.enqueueThumbs()
	}
	go s.run()
	s.reconcile()
	return s
}

// reconcile replays the saved assignment for every output that is connected
// right now. It is what makes a wallpaper survive a restart (D20): the seed is
// never written to the user's config file, so the theme is rebuilt from this
// replay rather than from disk.
func (s *Service) reconcile() {
	for connector, a := range s.store.All() {
		if !slices.Contains(s.store.Connectors(), connector) {
			continue
		}
		s.Enqueue(Command{Op: OpApply, Token: connector, Path: a.Path, Kind: a.Kind})
	}
}

// Updates carries published snapshots. It coalesces: a reader that misses one
// still sees the newest state.
func (s *Service) Updates() <-chan Snapshot { return s.updates }

// Snapshot returns the current state. The maps and slices are copies, so a
// caller cannot reach into the service through what it reads.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSnapshot(s.snap)
}

// SetConfigHook installs the theme write-back. The registry points it at the
// call that sets ThemeGen.Source and Seed and regenerates the palette.
func (s *Service) SetConfigHook(hook func(source, seed string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfgHook = hook
}

// Enqueue submits one command. It never blocks and never fails: a caller on
// the shell's dispatch path has nothing useful to do with either.
func (s *Service) Enqueue(c Command) {
	select {
	case <-s.quit:
	case s.cmds <- c:
	default:
	}
}

// Close stops the loop and waits for in-flight engine work to report back, so
// no goroutine outlives the service.
func (s *Service) Close() {
	s.closing.Do(func() {
		if s.stopWork != nil {
			s.stopWork()
		}
		close(s.quit)
		s.work.Wait()
	})
}

func (s *Service) run() {
	for {
		select {
		case <-s.quit:
			return
		case c := <-s.cmds:
			s.handle(c)
		case r := <-s.results:
			s.finish(r)
		case <-s.thumbProgress():
			// A preview landed on disk; the picker only picks it up on a
			// snapshot, so publish one.
			s.publish()
		}
	}
}

// thumbProgress is the generator's channel, or nil when there is no generator.
// A nil channel blocks forever, which is what makes the select above safe.
func (s *Service) thumbProgress() <-chan struct{} {
	if s.thumbs == nil {
		return nil
	}
	return s.thumbs.Progress()
}

// enqueueThumbs hands the current library to the generator.
func (s *Service) enqueueThumbs() {
	if s.thumbs == nil || s.lib == nil {
		return
	}
	s.thumbs.Enqueue(s.lib.All())
}

func (s *Service) handle(c Command) {
	switch c.Op {
	case OpApply:
		s.dispatch(s.store.Apply(c.Token, c.Path, c.Kind))
		return
	case OpPause, OpResume:
		s.setPaused(c.Token, c.Op == OpPause)
	case OpRestore:
		s.restore(c.Token)
	case OpConnect:
		s.dispatch(s.store.Reconnect(c.Token))
		return
	case OpDisconnect:
		s.store.Disconnect(c.Token)
	case OpRefresh:
		s.lib = Scan(s.roots)
		s.refreshCoverage()
		s.enqueueThumbs()
	}
	s.publish()
}

// dispatch runs each job on its own goroutine and publishes the starting
// state at once, so the picker shows work in flight rather than nothing.
func (s *Service) dispatch(jobs []Job) {
	s.publish()
	for _, job := range jobs {
		s.work.Add(1)
		go func(job Job) {
			defer s.work.Done()
			preview, err := s.engine.Apply(job, s.set)
			select {
			case s.results <- engineResult{job: job, preview: preview, err: err}:
			case <-s.quit:
			}
		}(job)
	}
}

func (s *Service) finish(r engineResult) {
	if r.err != nil {
		s.store.Fail(r.job, r.err)
		s.publish()
		return
	}
	before := s.store.SeedPath()
	if !s.store.Commit(r.job, r.preview) {
		// A stale generation: a newer apply already owns this output, so the
		// work is discarded rather than committed over it.
		return
	}
	s.persist()
	s.refreshCoverage()
	seed := s.store.SeedPath()
	s.publish()
	if seed != "" && seed != before {
		s.notifySeed(seed)
	}
}

// refreshCoverage re-reads which outputs a foreign wallpaper owns. A probe
// failure is not an error the user needs: it only means we cannot warn.
func (s *Service) refreshCoverage() {
	if s.coverage == nil {
		return
	}
	if covered, err := s.coverage(); err == nil {
		s.covered = covered
	}
}

// notifySeed calls the theme write-back outside the service lock.
func (s *Service) notifySeed(seed string) {
	s.mu.Lock()
	hook := s.cfgHook
	s.mu.Unlock()
	if hook != nil {
		hook("wallpaper", seed)
	}
}

func (s *Service) setPaused(connector string, paused bool) {
	a, ok := s.store.Assignment(connector)
	if !ok || a.Kind != KindVideo {
		// Pause is video-only; an image has no pipeline to hold.
		return
	}
	if err := s.engine.SetPaused(connector, paused); err != nil {
		s.store.noteRuntimeErr(connector, err)
		return
	}
	s.store.SetPlayback(connector, paused)
	s.persist()
}

func (s *Service) restore(token string) {
	targets := []string{token}
	if token == AllOutputs {
		targets = s.store.Connectors()
	}
	for _, connector := range targets {
		a, _ := s.store.Assignment(connector)
		if err := s.engine.Restore(connector, stillFor(a)); err != nil {
			s.store.noteRuntimeErr(connector, err)
			continue
		}
		s.store.SetRestored(connector)
	}
	s.persist()
}

// stillFor is the image Restore hands to the static fallback: the image
// itself, or the extracted still for a video. Empty leaves the output blank,
// which is the design's one intentional exception to gSlapper-first (D16).
func stillFor(a Assignment) string {
	if a.Kind == KindImage {
		return a.Path
	}
	return a.PreviewPath
}

func (s *Service) persist() {
	if s.persistPath == "" {
		return
	}
	if err := SaveAssignments(s.persistPath, s.store.All()); err != nil {
		s.store.noteErr(err)
	}
}

// publish rebuilds the snapshot and offers it to the updates channel,
// replacing an unread one rather than blocking on a closed picker.
func (s *Service) publish() {
	snap := Snapshot{
		Library:     s.lib,
		Connectors:  s.store.Connectors(),
		Assignments: s.store.All(),
		Runtime:     s.store.AllRuntime(),
		Caps:        s.caps,
		Seed:        s.store.SeedPath(),
		Err:         s.store.Err(),
		Covered:     maps.Clone(s.covered),
	}
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()

	for {
		select {
		case s.updates <- cloneSnapshot(snap):
			return
		default:
		}
		select {
		case <-s.updates: // drop the stale one and retry
		default:
			return
		}
	}
}

func cloneSnapshot(s Snapshot) Snapshot {
	out := s
	out.Connectors = slices.Clone(s.Connectors)
	out.Assignments = maps.Clone(s.Assignments)
	out.Runtime = maps.Clone(s.Runtime)
	out.Covered = maps.Clone(s.Covered)
	if out.Assignments == nil {
		out.Assignments = map[string]Assignment{}
	}
	if out.Runtime == nil {
		out.Runtime = map[string]Runtime{}
	}
	return out
}
