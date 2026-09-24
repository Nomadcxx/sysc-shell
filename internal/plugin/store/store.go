package store

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Source is one enabled catalog source.
type Source struct {
	Name string
	URL  string
}

// Options wires the store to the shell.
type Options struct {
	// CacheDir holds one git clone per source name.
	CacheDir  string
	Installer *Installer
	Git       Git
	// Sources returns the enabled sources. It is read at every refresh, so a
	// configuration change is picked up without restarting the store.
	Sources func() []Source
	// Local returns plugin id to directory for every usable user-root copy.
	Local func() map[string]string
}

type Status string

const (
	StatusAvailable       Status = "available"
	StatusInstalled       Status = "installed"
	StatusUpdateAvailable Status = "update available"
	StatusHeldBack        Status = "held back"
	StatusIncompatible    Status = "incompatible"
	StatusShadowed        Status = "shadowed"
	StatusLocalOnly       Status = "local only"
)

// Listing is one catalog row resolved against this machine. It carries every
// decoded field; the manager reads listings and never raw catalog JSON.
type Listing struct {
	Source        string
	CatalogCommit string
	Entry         Entry
	Resolution    Resolution
	Status        Status
	// Installed is the managed copy's record, when there is one.
	Installed *Record
	// LocalDir is a user-root copy with the same id, when there is one.
	LocalDir string
	// Err is the last failed operation on this plugin, until the next one.
	Err error
}

// SourceState is one source's last fetch. Err with a zero FetchedAt means the
// source was never read; with a non-zero one, its listings are stale.
type SourceState struct {
	Name      string
	URL       string
	Commit    string
	FetchedAt time.Time
	Plugins   int
	Rejected  []RowError
	Err       error
}

// State is an immutable snapshot for the manager and IPC.
type State struct {
	Sources  []SourceState
	Listings []Listing
	// Busy names the operation in flight, "" when idle.
	Busy string
}

const queueDepth = 16

type op struct {
	name string
	id   string
	run  func(ctx context.Context) error
	done chan error
}

// Store runs every git, network and disk operation on one goroutine. Callers
// enqueue and read snapshots; nothing they call blocks on I/O.
type Store struct {
	opts Options
	ops  chan op

	mu       sync.Mutex
	sources  []SourceState
	catalogs map[string]Catalog
	errs     map[string]error
	state    State
}

func New(opts Options) *Store {
	return &Store{opts: opts, ops: make(chan op, queueDepth), catalogs: map[string]Catalog{}, errs: map[string]error{}}
}

// Run is the worker. It returns when ctx is done.
func (s *Store) Run(ctx context.Context) {
	if err := s.opts.Installer.CleanStaging(); err != nil {
		slog.Warn("plugin store: cannot clear staging", "err", err)
	}
	s.rebuild()
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-s.ops:
			s.setBusy(o.name)
			err := o.run(ctx)
			s.mu.Lock()
			if o.id != "" {
				s.errs[o.id] = err
			}
			s.mu.Unlock()
			s.rebuild()
			s.setBusy("")
			o.done <- err
		}
	}
}

// State returns the latest snapshot. Its slices are not shared with the worker.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return State{
		Sources:  slices.Clone(s.state.Sources),
		Listings: slices.Clone(s.state.Listings),
		Busy:     s.state.Busy,
	}
}

func (s *Store) Refresh() (<-chan error, error) {
	return s.enqueue(op{name: "refresh", run: s.refresh})
}

// Install queues installing id from source. The command itself is the consent:
// the manager shows its consent sheet before calling this.
func (s *Store) Install(source, id string) (<-chan error, error) {
	if _, err := s.find(source, id); err != nil {
		return nil, err
	}
	return s.enqueue(op{name: "install " + id, id: id, run: func(ctx context.Context) error {
		l, err := s.find(source, id)
		if err != nil {
			return err
		}
		return s.install(ctx, l)
	}})
}

// Update queues updating id from the source it was installed from. An update
// that changes capabilities or required commands is refused until confirmed.
func (s *Store) Update(id string, confirmed bool) (<-chan error, error) {
	l, err := s.updatable(id)
	if err != nil {
		return nil, err
	}
	rel := l.Resolution.Release
	if !confirmed && (!sameSet(l.Installed.Capabilities, rel.Capabilities) || !sameSet(l.Installed.Requires, rel.Requires.Commands)) {
		return nil, fail(KindConsent, nil, "%s %s asks for capabilities %v and commands %v; installed %s has %v and %v",
			id, rel.Version, rel.Capabilities, rel.Requires.Commands, l.Installed.Version, l.Installed.Capabilities, l.Installed.Requires)
	}
	return s.enqueue(op{name: "update " + id, id: id, run: func(ctx context.Context) error {
		l, err := s.updatable(id)
		if err != nil {
			return err
		}
		return s.install(ctx, l)
	}})
}

func (s *Store) Rollback(id string) (<-chan error, error) {
	return s.enqueue(op{name: "rollback " + id, id: id, run: func(context.Context) error {
		return s.opts.Installer.Rollback(id)
	}})
}

func (s *Store) Remove(id string) (<-chan error, error) {
	return s.enqueue(op{name: "remove " + id, id: id, run: func(context.Context) error {
		return s.opts.Installer.Remove(id)
	}})
}

func (s *Store) enqueue(o op) (<-chan error, error) {
	o.done = make(chan error, 1)
	select {
	case s.ops <- o:
		return o.done, nil
	default:
		return nil, fail(KindBusy, nil, "%d operations already queued", queueDepth)
	}
}

func (s *Store) find(source, id string) (Listing, error) {
	for _, l := range s.State().Listings {
		if l.Source == source && l.Entry.ID == id {
			return l, nil
		}
	}
	return Listing{}, fail(KindNotListed, nil, "%s in source %q", id, source)
}

func (s *Store) updatable(id string) (Listing, error) {
	for _, l := range s.State().Listings {
		if l.Entry.ID == id && l.Installed != nil && l.Installed.Source == l.Source {
			if l.Status != StatusUpdateAvailable && l.Status != StatusShadowed {
				return Listing{}, fail(KindNotListed, nil, "no update for %s", id)
			}
			if l.Resolution.Release == nil || !Newer(l.Resolution.Release.Version, l.Installed.Version) {
				return Listing{}, fail(KindNotListed, nil, "no update for %s", id)
			}
			return l, nil
		}
	}
	return Listing{}, fail(KindNotListed, nil, "%s is not installed from an enabled source", id)
}

func (s *Store) install(ctx context.Context, l Listing) error {
	if l.Resolution.Release == nil {
		return fail(KindNoAsset, nil, "%s needs protocol %d.%d", l.Entry.ID, l.Resolution.Needs.Major, l.Resolution.Needs.Minor)
	}
	return s.opts.Installer.Install(ctx, Plan{
		Source: l.Source, CatalogCommit: l.CatalogCommit, ID: l.Entry.ID, Release: *l.Resolution.Release,
	})
}

// refresh reads every enabled source. A source that fails keeps its last good
// catalog, marked stale, so a network outage never empties the manager.
func (s *Store) refresh(ctx context.Context) error {
	s.mu.Lock()
	prev := map[string]SourceState{}
	for _, st := range s.sources {
		prev[st.Name] = st
	}
	prevCats := s.catalogs
	s.mu.Unlock()

	var next []SourceState
	cats := map[string]Catalog{}
	var errs []error
	for _, src := range s.opts.Sources() {
		st := SourceState{Name: src.Name, URL: src.URL}
		if old, ok := prev[src.Name]; ok && old.URL == src.URL {
			st = old
			cats[src.Name] = prevCats[src.Name]
		}
		body, commit, err := s.opts.Git.Catalog(ctx, filepath.Join(s.opts.CacheDir, src.Name), src.URL)
		var cat Catalog
		if err == nil {
			cat, err = Decode(body)
		}
		if err != nil {
			st.Err = err
			errs = append(errs, err)
		} else {
			st.Err = nil
			st.Commit, st.FetchedAt, st.Plugins, st.Rejected = commit, time.Now(), len(cat.Entries), cat.Rejected
			cats[src.Name] = cat
		}
		next = append(next, st)
	}
	s.mu.Lock()
	s.sources, s.catalogs = next, cats
	s.mu.Unlock()
	return errors.Join(errs...)
}

// rebuild recomputes listings from the catalogs, the managed tree and the
// local copies. It runs on the worker only.
func (s *Store) rebuild() {
	installed, err := LoadInstalled(s.opts.Installer.Root)
	if err != nil {
		slog.Warn("plugin store: cannot read installed plugins", "err", err)
		installed = Installed{}
	}
	var local map[string]string
	if s.opts.Local != nil {
		local = s.opts.Local()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Sources = slices.Clone(s.sources)
	s.state.Listings = buildListings(s.sources, s.catalogs, installed, local, s.errs, s.opts.Installer.Arch)
}

func (s *Store) setBusy(what string) {
	s.mu.Lock()
	s.state.Busy = what
	s.mu.Unlock()
}

// buildListings resolves every catalog row, in source order then catalog order.
func buildListings(sources []SourceState, catalogs map[string]Catalog, installed Installed,
	local map[string]string, errs map[string]error, arch string) []Listing {

	var out []Listing
	for _, src := range sources {
		for _, e := range catalogs[src.Name].Entries {
			l := Listing{Source: src.Name, CatalogCommit: src.Commit, Entry: e, Resolution: Resolve(e, arch),
				LocalDir: local[e.ID], Err: errs[e.ID]}
			if rec, ok := installed[e.ID]; ok {
				l.Installed = &rec
			}
			l.Status = statusOf(l)
			out = append(out, l)
		}
	}
	return out
}

func statusOf(l Listing) Status {
	switch {
	case l.LocalDir != "" && l.Installed != nil:
		return StatusShadowed
	case l.LocalDir != "":
		return StatusLocalOnly
	case l.Installed != nil:
		if l.Installed.Source == l.Source && l.Resolution.Release != nil && Newer(l.Resolution.Release.Version, l.Installed.Version) {
			return StatusUpdateAvailable
		}
		return StatusInstalled
	case l.Resolution.Compat == Incompatible:
		return StatusIncompatible
	case l.Resolution.Compat == HeldBack:
		return StatusHeldBack
	}
	return StatusAvailable
}
