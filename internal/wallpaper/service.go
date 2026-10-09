package wallpaper

import (
	"context"
	"errors"
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
	// OpRefreshTerminalCatalog re-reads sysc-terminal's installed registry.
	OpRefreshTerminalCatalog
)

// Command is one queued request. It is a value, so nothing the panel holds is
// shared with the service after the send.
type Command struct {
	Op     Op
	Token  string
	Path   string
	Kind   Kind
	Effect string
	Theme  string
}

// Capabilities is what is installed, probed once at start and projected into
// the picker's banners: without gSlapper the video tiles go inert, and without
// a static engine Restore has nowhere to go.
type Capabilities struct {
	GSlapper bool
	// Terminal is true when sysc-terminal answers --list. KindEffect uses it
	// and nothing else does.
	Terminal bool
	// Catalog is the --list registry, filled only when Terminal is true.
	Catalog Catalog
	// Statics are the installed static fallback binaries in preference order.
	// The picker names every one; Restore uses the first.
	Statics []string
}

// Static is the fallback Restore hands off to, empty when none is installed.
func (c Capabilities) Static() string {
	if len(c.Statics) == 0 {
		return ""
	}
	return c.Statics[0]
}

// EngineGSlapper is the name gSlapper is recorded under in Runtime.Engine. The
// static engines are recorded under their binary name, which is what Statics
// already holds.
const EngineGSlapper = "gslapper"
const EngineTerminal = "sysc-terminal"

// EngineFor names the engine an apply of kind will use, or "" when nothing
// installed can paint it -- a video without gSlapper, an effect without
// sysc-terminal, or anything at all with no engine installed.
//
// This is the one statement of that policy: the engine branches on it, and the
// picker reports it. An earlier version had the picker infer the engine from
// Runtime.Socket and Runtime.FallbackPID, which nothing outside the engine's
// own private handles ever writes, so no engine was ever named.
func (c Capabilities) EngineFor(kind Kind) string {
	if kind == KindEffect {
		if c.Terminal {
			return EngineTerminal
		}
		return ""
	}
	if c.GSlapper {
		return EngineGSlapper
	}
	if kind == KindVideo {
		return ""
	}
	return c.Static()
}

// Engine is the side of the service that runs processes. It is an interface so
// the service can be tested without exec, a socket, or a compositor.
type Engine interface {
	// AdvanceGeneration announces the newest requested state for connector.
	// Pending older applies must not paint after it.
	AdvanceGeneration(connector string, generation uint64)
	// Apply puts one path on one output and returns a still for a video, which
	// may be empty when none could be extracted.
	Apply(job Job, set Settings) (preview string, err error)
	// Restore stops our engine on connector and shows still through the static
	// fallback. An empty still leaves the output blank.
	Restore(connector, still string) error
	SetPaused(connector string, paused bool) error
	Capabilities() Capabilities
	RefreshTerminalCatalog() Capabilities
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
	// ThumbsDone and ThumbsTotal report preview generation. A first run over a
	// real library takes minutes; without them the picker is a wall of glyphs
	// with no explanation.
	ThumbsDone  int
	ThumbsTotal int
	// Scanning is true while the library index is being built.
	Scanning bool
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
	// ConfigHook is the theme write-back, given here rather than installed
	// afterwards so the seed that startup reconcile produces cannot be
	// published before anyone is listening.
	ConfigHook func(source, seed string)
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
	done    chan struct{}
	closing sync.Once
	work    sync.WaitGroup

	coverage func() (map[string]string, error)
	thumbs   *Thumbnailer
	stopWork context.CancelFunc

	// store, lib, caps, and covered are touched only by the loop goroutine.
	store     Store
	lib       *Library
	caps      Capabilities
	covered   map[string]string
	restarted map[string]bool

	mu   sync.Mutex
	snap Snapshot
	// cfgHook is the theme write-back. It is never called while mu is held:
	// it re-enters the registry, which takes its own lock.
	cfgHook func(source, seed string)
	// notifiedSeed is what cfgHook was last given. The store's own seed is
	// primed by Adopt before reconcile runs, so comparing a commit against
	// that one treats a restored assignment as already applied and leaves the
	// palette at its defaults for the whole session.
	notifiedSeed string
}

// NewService starts the service loop.
func NewService(cfg ServiceConfig) *Service {
	s := &Service{
		engine:      cfg.Engine,
		restarted:   make(map[string]bool),
		set:         cfg.Settings,
		roots:       slices.Clone(cfg.Roots),
		persistPath: cfg.PersistPath,
		coverage:    cfg.Coverage,
		cfgHook:     cfg.ConfigHook,
		cmds:        make(chan Command, 32),
		results:     make(chan engineResult, 32),
		// One slot, coalescing: a picker that is closed or slow must never
		// block an apply.
		updates: make(chan Snapshot, 1),
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
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
		s.Enqueue(Command{Op: OpApply, Token: connector, Path: a.Path, Kind: a.Kind, Effect: a.Effect, Theme: a.Theme})
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
// Prefer ServiceConfig.ConfigHook: a hook installed after NewService can miss
// the seed that startup reconcile publishes.
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

// RefreshTerminalCatalog asks the service loop to refresh the installed
// sysc-terminal registry and publish the updated capabilities.
func (s *Service) RefreshTerminalCatalog() {
	s.Enqueue(Command{Op: OpRefreshTerminalCatalog})
}

// Close stops the loop and waits for in-flight engine work to report back, so
// no goroutine outlives the service.
func (s *Service) Close() {
	s.closing.Do(func() {
		if s.stopWork != nil {
			s.stopWork()
		}
		close(s.quit)
		if closer, ok := s.engine.(interface{ Close() }); ok {
			closer.Close()
		}
		<-s.done
		s.work.Wait()
	})
}

func (s *Service) run() {
	defer close(s.done)
	// Reuse the process watchers; no IPC or extra goroutine is needed to check exits.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.quit:
			return
		case c := <-s.cmds:
			s.handle(c)
		case r := <-s.results:
			s.finish(r)
		case <-ticker.C:
			s.supervise()
		case <-s.thumbProgress():
			// A preview landed on disk; the picker only picks it up on a
			// snapshot, so publish one.
			s.publish()
		}
	}
}

// supervise replays a committed assignment when its current owned child exits.
func (s *Service) supervise() {
	watcher, ok := s.engine.(interface{ OwnedExited(string, uint64) bool })
	if !ok {
		return
	}
	for _, connector := range s.store.Connectors() {
		rt := s.store.Runtime(connector)
		if rt.State == StateStarting || rt.State == StateError ||
			(rt.Engine != EngineTerminal && rt.Engine != EngineGSlapper) ||
			!watcher.OwnedExited(connector, s.store.gen[connector]) {
			continue
		}
		// ponytail: one automatic restart per selection avoids crash loops;
		// sustained recovery would need a backoff policy. Apply explicitly retries.
		if s.restarted[connector] {
			s.store.noteRuntimeErr(connector, errors.New("wallpaper: process exited again after automatic restart; apply to retry"))
			s.publish()
			continue
		}
		s.restarted[connector] = true
		s.dispatch(s.store.Reconnect(connector))
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
		if c.Token == AllOutputs {
			clear(s.restarted)
		} else {
			delete(s.restarted, c.Token)
		}
		jobs := s.store.Apply(c.Token, c.Path, c.Kind)
		for i := range jobs {
			jobs[i].Effect = c.Effect
			jobs[i].Theme = c.Theme
		}
		s.dispatch(jobs)
		return
	case OpPause, OpResume:
		s.setPaused(c.Token, c.Op == OpPause)
	case OpRestore:
		s.restore(c.Token)
	case OpConnect:
		delete(s.restarted, c.Token)
		s.dispatch(s.store.Reconnect(c.Token))
		return
	case OpDisconnect:
		delete(s.restarted, c.Token)
		generation := s.store.Disconnect(c.Token)
		if s.engine != nil {
			s.engine.AdvanceGeneration(c.Token, generation)
		}
		s.publish()
		if s.engine != nil {
			_ = s.engine.Restore(c.Token, "")
		}
		return
	case OpRefresh:
		s.lib = Scan(s.roots)
		s.refreshCoverage()
		s.enqueueThumbs()
	case OpRefreshTerminalCatalog:
		if s.engine != nil {
			s.caps = s.engine.RefreshTerminalCatalog()
		}
	}
	s.publish()
}

// dispatch runs each job on its own goroutine and publishes the starting
// state at once, so the picker shows work in flight rather than nothing.
func (s *Service) dispatch(jobs []Job) {
	if s.engine != nil {
		for _, job := range jobs {
			s.engine.AdvanceGeneration(job.Connector, job.Gen)
		}
	}
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
		before, hadBefore := s.store.Assignment(r.job.Connector)
		assignmentChanged := false
		if s.store.Fail(r.job, r.err) {
			after, hasAfter := s.store.Assignment(r.job.Connector)
			assignmentChanged = hadBefore != hasAfter || (hadBefore && before != after)
			if assignmentChanged {
				// A failed replacement may have restored an apply that finished
				// after this job was queued; keep that actual assignment on disk.
				s.persist()
				s.refreshCoverage()
			}
		}
		s.publish()
		// An adopted assignment may be the only available theme seed, even
		// when rollback leaves its value unchanged. notifySeed deduplicates it.
		s.notifySeed(s.store.SeedPath())
		return
	}
	if !s.store.Commit(r.job, r.preview, s.caps.EngineFor(r.job.Kind)) {
		// A stale generation: a newer apply already owns this output, so the
		// work is discarded rather than committed over it.
		return
	}
	if a, ok := s.store.Assignment(r.job.Connector); ok && a.DesiredPlayback == StatePaused {
		// Some engines replace their process when an assignment changes. Restore
		// the persisted playback request before publishing the successful apply.
		if err := s.engine.SetPaused(r.job.Connector, true); err != nil {
			s.store.noteRuntimeErr(r.job.Connector, err)
		}
	}
	s.persist()
	s.refreshCoverage()
	s.publish()
	s.notifySeed(s.store.SeedPath())
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
	if seed == "" {
		return
	}
	s.mu.Lock()
	if s.notifiedSeed == seed {
		s.mu.Unlock()
		return
	}
	s.notifiedSeed = seed
	hook := s.cfgHook
	s.mu.Unlock()
	if hook != nil {
		hook("wallpaper", seed)
	}
}

func (s *Service) setPaused(token string, paused bool) {
	targets := []string{token}
	if token == AllOutputs {
		targets = s.store.Connectors()
	}
	for _, connector := range targets {
		a, ok := s.store.Assignment(connector)
		if !ok || (a.Kind != KindVideo && a.Kind != KindEffect) ||
			s.store.Runtime(connector).State == StateStatic {
			// Pause is for pipelines that play; an image, or a player restored
			// to its still, has nothing to hold.
			continue
		}
		if err := s.engine.SetPaused(connector, paused); err != nil {
			s.store.noteRuntimeErr(connector, err)
			continue
		}
		s.store.SetPlayback(connector, paused)
	}
	s.persist()
}

func (s *Service) restore(token string) {
	targets := []string{token}
	if token == AllOutputs {
		targets = s.store.Connectors()
	}
	for _, connector := range targets {
		generation := s.store.Invalidate(connector)
		s.engine.AdvanceGeneration(connector, generation)
		a, _ := s.store.Assignment(connector)
		if err := s.engine.Restore(connector, stillFor(a)); err != nil {
			s.store.noteRuntimeErr(connector, err)
			continue
		}
		s.store.SetRestored(connector, s.caps.Static())
	}
	s.persist()
}

// stillFor is the image Restore hands to the static fallback: the image
// itself, or the saved still for a video or effect. Empty leaves the output
// blank, which is the design's one intentional exception to gSlapper-first (D16).
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
	if s.thumbs != nil {
		snap.ThumbsDone, snap.ThumbsTotal = s.thumbs.Counts()
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
