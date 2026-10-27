package wallpaper

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/fakebin"
	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

type fakeProcess struct {
	mu      sync.Mutex
	stopped bool
	exit    chan struct{}
	err     error
}

type failedStopProcess struct {
	exit chan struct{}
	err  error
}

func (p *failedStopProcess) Wait() error {
	<-p.exit
	return nil
}

func (p *failedStopProcess) Stop() error { return p.err }

type stubbornProcess struct{ exit chan struct{} }

func (p *stubbornProcess) Wait() error {
	<-p.exit
	return nil
}

func (*stubbornProcess) Stop() error      { return nil }
func (*stubbornProcess) ForceStop() error { return nil }

func newFakeProcess() *fakeProcess { return &fakeProcess{exit: make(chan struct{})} }

// exitedProcess is a one-shot client that has already finished with err.
func exitedProcess(err error) *fakeProcess {
	p := &fakeProcess{exit: make(chan struct{}), err: err}
	close(p.exit)
	return p
}

func TestEngineRefreshTerminalCatalogReadsCurrentList(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog")
	binaryPath := filepath.Join(dir, "sysc-terminal")
	if err := fakebin.Write(binaryPath, "#!/bin/sh\nif [ \"$1\" != \"--list\" ]; then exit 2; fi\nexec /bin/cat \"$SYSC_TERMINAL_TEST_CATALOG\"\n", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SYSC_TERMINAL_TEST_CATALOG", catalogPath)
	writeCatalog := func(text string) {
		t.Helper()
		if err := os.WriteFile(catalogPath, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCatalog("effect fire 0\ntheme nord\n")
	engine := NewEngine(sockpath.Dir(t, "wc"), func(name string) bool { return name == "sysc-terminal" })
	t.Cleanup(engine.Close)

	if got := engine.Capabilities(); !got.Terminal || len(got.Catalog.Effects) != 1 || got.Catalog.Effects[0] != "fire" {
		t.Fatalf("initial capabilities = %+v, want fire", got)
	}
	writeCatalog("effect rain 0\ntheme dracula\n")
	got := engine.RefreshTerminalCatalog()
	if !got.Terminal || len(got.Catalog.Effects) != 1 || got.Catalog.Effects[0] != "rain" || got.Catalog.Themes[0] != "dracula" {
		t.Fatalf("refreshed capabilities = %+v, want rain and dracula", got)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	got = engine.RefreshTerminalCatalog()
	if !got.Terminal || len(got.Catalog.Effects) != 1 || got.Catalog.Effects[0] != "rain" || got.Catalog.Themes[0] != "dracula" {
		t.Fatalf("failed refresh lost the last good catalog: %+v", got)
	}
}

func (p *fakeProcess) Wait() error {
	<-p.exit
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *fakeProcess) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped {
		p.stopped = true
		select {
		case <-p.exit:
		default:
			close(p.exit)
		}
	}
	return nil
}

func (p *fakeProcess) wasStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

type hardStopProcess struct {
	*fakeProcess
	forceStopped bool
}

type startupSocketProcess struct {
	*fakeProcess
	listener *net.UnixListener
}

func (p *startupSocketProcess) Stop() error {
	_ = p.listener.Close()
	return p.fakeProcess.Stop()
}

func (p *hardStopProcess) ForceStop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.forceStopped = true
	p.stopped = true
	select {
	case <-p.exit:
	default:
		close(p.exit)
	}
	return nil
}

func (p *hardStopProcess) wasForceStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.forceStopped
}

// engineHarness wires a gSlapper engine to fakes: no exec, no real socket.
type engineHarness struct {
	t        *testing.T
	eng      *gslapperEngine
	mediaDir string

	mu      sync.Mutex
	spawned [][]string
	procs   []*fakeProcess
	// replies maps a command verb to what the fake engine answers.
	replies map[string]string
	// ready, when set, makes the socket answer query only after spawn.
	spawnCreatesSocket bool
	// daemonUp models awww-daemon: the awww client only succeeds once it is up.
	daemonUp bool
}

func newEngineHarness(t *testing.T) *engineHarness {
	t.Helper()
	h := &engineHarness{
		t:                  t,
		replies:            map[string]string{},
		spawnCreatesSocket: true,
	}
	dir := sockpath.Dir(t, "wc")
	h.mediaDir = t.TempDir()
	h.eng = &gslapperEngine{
		dir:       dir,
		caps:      Capabilities{GSlapper: true, Statics: []string{engineAwww}},
		readyWait: 300 * time.Millisecond,
		poll:      5 * time.Millisecond,
		owned:     map[string]Process{},
		fallbacks: map[string]Process{},
		spawn: func(argv []string) (Process, error) {
			h.mu.Lock()
			h.spawned = append(h.spawned, slices.Clone(argv))
			create := h.spawnCreatesSocket
			// awww is a client for awww-daemon: it exits at once, and only
			// succeeds while the daemon is up.
			switch {
			case argv[0] == engineAwwwDaemon:
				h.daemonUp = true
				proc := newFakeProcess()
				h.procs = append(h.procs, proc)
				h.mu.Unlock()
				return proc, nil
			case argv[0] == engineAwww:
				up := h.daemonUp
				h.mu.Unlock()
				if !up {
					return exitedProcess(errors.New("awww-daemon is not running")), nil
				}
				return exitedProcess(nil), nil
			}
			proc := newFakeProcess()
			h.procs = append(h.procs, proc)
			h.mu.Unlock()
			if create && (argv[0] == "gslapper" || argv[0] == "sysc-terminal") {
				if i := slices.Index(argv, "-I"); i >= 0 {
					_ = os.WriteFile(argv[i+1], nil, 0o600)
				}
			}
			return proc, nil
		},
		request: func(socket, command string, _ time.Duration) (string, error) {
			if _, err := os.Stat(socket); err != nil {
				return "", err
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			verb, _, _ := strings.Cut(command, " ")
			if verb == "stop" || verb == "quit" {
				_ = os.Remove(socket)
			}
			if reply, ok := h.replies[verb]; ok {
				if strings.HasPrefix(reply, "ERROR:") {
					return "", errors.New(reply)
				}
				return reply, nil
			}
			if verb == "query" && strings.Contains(socket, "sysc-terminal-") {
				return "STATUS: playing effect fire theme nord", nil
			}
			if verb == "query" {
				return "STATUS: playing image /w/test.png", nil
			}
			return "OK", nil
		},
	}
	return h
}

// withTerminal installs sysc-terminal with the catalog the tests apply from.
func (h *engineHarness) withTerminal() {
	h.eng.caps.Terminal = true
	h.eng.caps.Catalog = Catalog{Effects: []string{"fire", "rain"}, Themes: []string{"nord", "dracula"}}
}

func TestApplyEffectRefusesEffectOutsideCatalog(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire-text", Theme: "nord"}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), `does not offer effect "fire-text"`) {
		t.Fatalf("apply error = %v, want a refusal naming the effect", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("a refused effect launched %v", argvs)
	}
}

func TestEngineRoutesEffectsToSyscTerminal(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	if got := h.eng.Capabilities().EngineFor(KindEffect); got != EngineTerminal {
		t.Fatalf("effect engine = %q, want %q", got, EngineTerminal)
	}
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire", Theme: "nord"}, defaultSettings()); err != nil {
		t.Fatalf("apply effect: %v", err)
	}
	argvs := h.argvs()
	if len(argvs) != 1 || argvs[0][0] != "sysc-terminal" {
		t.Fatalf("effect driver launched %v, want sysc-terminal", argvs)
	}
}

// media creates a real file, because Apply refuses a path that is not there.
func (h *engineHarness) media(name string) string {
	h.t.Helper()
	path := filepath.Join(h.mediaDir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		h.t.Fatalf("seed media: %v", err)
	}
	return path
}

func (h *engineHarness) socket(connector string) string {
	return socketPath(h.eng.dir, connector)
}

func (h *engineHarness) ownSocket(connector string) {
	h.eng.owned[connector] = watchProcess(newFakeProcess())
}

func (h *engineHarness) argvs() [][]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]string(nil), h.spawned...)
}

func TestEngineLaunchesWhenNoSocket(t *testing.T) {
	h := newEngineHarness(t)
	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.mp4"), Kind: KindVideo}
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	argvs := h.argvs()
	if len(argvs) != 1 {
		t.Fatalf("spawned %d processes, want 1: %v", len(argvs), argvs)
	}
	if argvs[0][0] != "gslapper" {
		t.Fatalf("spawned %v", argvs[0])
	}
	if argAfter(argvs[0], "-I") != h.socket("DP-1") {
		t.Fatalf("launched on %q, want the owned socket", argAfter(argvs[0], "-I"))
	}
}

func TestApplyEffectDoesNotStatEmptyPath(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	job := Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire", Theme: "nord"}
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	argvs := h.argvs()
	if len(argvs) != 1 {
		t.Fatalf("spawned %d processes, want 1: %v", len(argvs), argvs)
	}
	if argvs[0][0] != "sysc-terminal" {
		t.Fatalf("spawned %v, want sysc-terminal", argvs[0])
	}
	if argAfter(argvs[0], "--effect") != "fire" || argAfter(argvs[0], "--theme") != "nord" || argAfter(argvs[0], "--output") != "DP-1" {
		t.Fatalf("argv %v missing effect launch flags", argvs[0])
	}
	if slices.Contains(argvs[0], "") {
		t.Fatalf("empty path leaked onto argv: %v", argvs[0])
	}
}

func TestApplyEffectStopsOwnedGSlapper(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("image: %v", err)
	}
	h.mu.Lock()
	gslapper := h.procs[0]
	h.mu.Unlock()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Kind: KindEffect, Effect: "rain", Theme: "dracula"}, defaultSettings()); err != nil {
		t.Fatalf("effect: %v", err)
	}
	if !gslapper.wasStopped() {
		t.Fatal("applying an effect left gSlapper running on the output")
	}
	argvs := h.argvs()
	if len(argvs) != 2 || argvs[1][0] != "sysc-terminal" {
		t.Fatalf("spawned %v, want gslapper then sysc-terminal", argvs)
	}
}

func TestApplyImageStopsTerminalEffectWithoutGSlapper(t *testing.T) {
	h := newEngineHarness(t)
	h.eng.caps.GSlapper = false
	h.withTerminal()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire", Theme: "nord"}, defaultSettings()); err != nil {
		t.Fatalf("effect: %v", err)
	}
	terminal := h.eng.ownedProcess("DP-1")
	if terminal == nil {
		t.Fatal("effect did not start a terminal process")
	}
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Kind: KindImage, Path: h.media("still.png")}, defaultSettings()); err != nil {
		t.Fatalf("image: %v", err)
	}
	if !processExited(terminal) {
		t.Fatal("applying a static image left Terminal Art running over it")
	}
}

func TestApplyEffectReadinessErrorNamesTerminalProcess(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	h.eng.spawn = func([]string) (Process, error) { return exitedProcess(nil), nil }
	_, err := h.eng.Apply(Job{Connector: "DP-1", Kind: KindEffect, Effect: "fire"}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "sysc-terminal exited before it was ready") {
		t.Fatalf("apply error = %v, want sysc-terminal readiness failure", err)
	}
}

func TestApplyEffectRejectsTerminalErrReadinessReply(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	h.eng.readyWait = 20 * time.Millisecond
	h.eng.poll = time.Millisecond
	h.replies["query"] = "ERR not ready"
	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire"}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "sysc-terminal did not answer") {
		t.Fatalf("apply error = %v, want a terminal readiness failure", err)
	}
	if h.eng.ownedProcess("DP-1") != nil {
		t.Fatal("a terminal process returning ERR was recorded as ready")
	}
	if _, err := os.Stat(terminalSocketPath(h.eng.dir, "DP-1")); !os.IsNotExist(err) {
		t.Fatalf("failed terminal socket = %v, want removed", err)
	}
}

func TestApplyEffectClearsDeadTerminalSocket(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	socket := terminalSocketPath(h.eng.dir, "DP-1")
	deadSocketFileFor(t, socket)
	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire"}, defaultSettings())
	if err != nil {
		t.Fatalf("apply over dead terminal socket: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 1 || argvs[0][0] != "sysc-terminal" {
		t.Fatalf("spawned %v, want sysc-terminal after stale socket cleanup", argvs)
	}
}

func TestApplyEffectLeavesPriorWallpaperWhenTerminalSocketIsLiveForeign(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("previous.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("apply prior image: %v", err)
	}
	prior := h.eng.ownedProcess("DP-1")
	socket := terminalSocketPath(h.eng.dir, "DP-1")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on foreign terminal socket: %v", err)
	}
	defer listener.Close()

	_, err = h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Kind: KindEffect, Effect: "fire"}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "socket is not owned by this shell") {
		t.Fatalf("effect apply = %v, want refusal of a live foreign socket", err)
	}
	if h.eng.ownedProcess("DP-1") != prior || processExited(prior) {
		t.Fatal("refusing the foreign socket stopped the prior wallpaper")
	}
	if argvs := h.argvs(); len(argvs) != 1 || argvs[0][0] != "gslapper" {
		t.Fatalf("spawned %v; refusal must happen before replacing the prior wallpaper", argvs)
	}
}

func TestApplyEffectRefusesForeignGSlapperBeforeStoppingStaticFallback(t *testing.T) {
	h := newEngineHarness(t)
	h.eng.caps = Capabilities{Statics: []string{engineSwaybg}}
	h.withTerminal()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("previous.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("apply prior image: %v", err)
	}
	prior := h.eng.fallbackProcess("DP-1")
	if prior == nil {
		t.Fatal("static fallback was not started")
	}
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed foreign gSlapper socket path: %v", err)
	}

	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Kind: KindEffect, Effect: "fire"}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "not ours to stop") {
		t.Fatalf("effect apply = %v, want refusal of foreign gSlapper socket", err)
	}
	if processExited(prior) || h.eng.fallbackProcess("DP-1") != prior {
		t.Fatal("refusing the foreign socket stopped the previous static wallpaper")
	}
}

func TestFailedSupersedingEffectRestoresLatestLiveAssignment(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	h.eng.readyWait = 20 * time.Millisecond
	h.eng.poll = time.Millisecond
	store := newTestStore()
	first := store.Apply("DP-1", "", KindEffect)[0]
	first.Effect, first.Theme = "fire", "nord"
	second := store.Apply("DP-1", "", KindEffect)[0]
	second.Effect, second.Theme = "rain", "dracula"
	if second.HasPrevious || second.PreviousState != StateStarting {
		t.Fatalf("test setup expects an uncommitted first job, got %+v", second)
	}

	spawn := h.eng.spawn
	terminalLaunches := 0
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == "sysc-terminal" {
			terminalLaunches++
			if terminalLaunches == 2 {
				h.mu.Lock()
				creates := h.spawnCreatesSocket
				h.spawnCreatesSocket = false
				h.mu.Unlock()
				proc, err := spawn(argv)
				h.mu.Lock()
				h.spawnCreatesSocket = creates
				h.mu.Unlock()
				return proc, err
			}
		}
		return spawn(argv)
	}
	if _, err := h.eng.Apply(first, defaultSettings()); err != nil {
		t.Fatalf("first effect: %v", err)
	}
	_, err := h.eng.Apply(second, defaultSettings())
	var restored *restoredApplyError
	if !errors.As(err, &restored) || restored.state != StatePlaying {
		t.Fatalf("second effect error = %v, want the first live effect restored", err)
	}
	if !store.Fail(second, err) {
		t.Fatal("current failed job was rejected")
	}
	assignment, ok := store.Assignment("DP-1")
	if !ok || assignment.Kind != KindEffect || assignment.Effect != "fire" || assignment.Theme != "nord" ||
		store.Runtime("DP-1").State != StatePlaying {
		t.Fatalf("store after failed superseding apply = assignment %+v, runtime %+v; want fire/nord playing", assignment, store.Runtime("DP-1"))
	}
	argvs := h.argvs()
	if len(argvs) != 3 || !slices.Contains(argvs[0], "fire") || !slices.Contains(argvs[1], "rain") || !slices.Contains(argvs[2], "fire") {
		t.Fatalf("launches = %v, want fire, failed rain, restored fire", argvs)
	}
}

func TestFailedEffectAfterUncommittedImageRestoresImage(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	h.eng.readyWait = 20 * time.Millisecond
	h.eng.poll = time.Millisecond
	store := newTestStore()
	first := store.Apply("DP-1", h.media("previous.png"), KindImage)[0]
	second := store.Apply("DP-1", "", KindEffect)[0]
	second.Effect, second.Theme = "fire", "nord"
	if second.HasPrevious || second.PreviousState != StateStarting {
		t.Fatalf("test setup expects an uncommitted image, got %+v", second)
	}

	spawn := h.eng.spawn
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == "sysc-terminal" {
			h.mu.Lock()
			creates := h.spawnCreatesSocket
			h.spawnCreatesSocket = false
			h.mu.Unlock()
			proc, err := spawn(argv)
			h.mu.Lock()
			h.spawnCreatesSocket = creates
			h.mu.Unlock()
			return proc, err
		}
		return spawn(argv)
	}
	if _, err := h.eng.Apply(first, defaultSettings()); err != nil {
		t.Fatalf("first image: %v", err)
	}
	_, err := h.eng.Apply(second, defaultSettings())
	var restored *restoredApplyError
	if !errors.As(err, &restored) || restored.state != StateStatic {
		t.Fatalf("effect error = %v, want prior image restored", err)
	}
	if !store.Fail(second, err) {
		t.Fatal("current failed job was rejected")
	}
	assignment, ok := store.Assignment("DP-1")
	if !ok || assignment.Kind != KindImage || assignment.Path != first.Path || store.Runtime("DP-1").State != StateStatic || store.SeedPath() != first.Path {
		t.Fatalf("store after failed effect = assignment %+v, runtime %+v; want prior image static", assignment, store.Runtime("DP-1"))
	}
	argvs := h.argvs()
	if len(argvs) != 3 || argvs[0][0] != "gslapper" || argvs[1][0] != "sysc-terminal" || argvs[2][0] != "gslapper" || !slices.Contains(argvs[2], first.Path) {
		t.Fatalf("launches = %v, want image, failed effect, restored image", argvs)
	}
}

func TestSuccessfulEffectAfterUncommittedImageKeepsPreviousStill(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	store := newTestStore()
	first := store.Apply("DP-1", h.media("previous.png"), KindImage)[0]
	second := store.Apply("DP-1", "", KindEffect)[0]
	second.Effect, second.Theme = "fire", "nord"
	if _, err := h.eng.Apply(first, defaultSettings()); err != nil {
		t.Fatalf("first image: %v", err)
	}
	preview, err := h.eng.Apply(second, defaultSettings())
	if err != nil {
		t.Fatalf("effect: %v", err)
	}
	if !store.Commit(second, preview, EngineTerminal) {
		t.Fatal("effect commit was rejected")
	}
	assignment, ok := store.Assignment("DP-1")
	if !ok || assignment.PreviewPath != first.Path {
		t.Fatalf("effect assignment = %+v, want prior image %q saved for Restore", assignment, first.Path)
	}
}

func TestFailedEffectLaunchRestoresPreviousImage(t *testing.T) {
	h := newEngineHarness(t)
	previous := h.media("previous.png")
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: previous, Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("image: %v", err)
	}
	h.withTerminal()
	spawn := h.eng.spawn
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == "sysc-terminal" {
			h.mu.Lock()
			creates := h.spawnCreatesSocket
			h.spawnCreatesSocket = false
			h.mu.Unlock()
			proc, err := spawn(argv)
			h.mu.Lock()
			h.spawnCreatesSocket = creates
			h.mu.Unlock()
			return proc, err
		}
		return spawn(argv)
	}
	_, err := h.eng.Apply(Job{
		Connector: "DP-1", Gen: 2, Kind: KindEffect, Effect: "fire",
		Previous: Assignment{Kind: KindImage, Path: previous}, HasPrevious: true,
		PreviousState: StateStatic, PreviousEngine: EngineGSlapper,
	}, defaultSettings())
	if err == nil {
		t.Fatal("terminal launch without a socket must fail")
	}
	var restored *restoredApplyError
	if !errors.As(err, &restored) || restored.state != StateStatic || restored.engine != EngineGSlapper {
		t.Fatalf("apply error = %v, want prior image runtime restored", err)
	}
	argvs := h.argvs()
	if len(argvs) != 3 || argvs[2][0] != "gslapper" || !slices.Contains(argvs[2], previous) {
		t.Fatalf("launches = %v, want gSlapper restored to %s after terminal failure", argvs, previous)
	}
}

func TestFailedWallpaperReplacementRestoresPriorImage(t *testing.T) {
	h := newEngineHarness(t)
	store := newTestStore()
	previous := h.media("previous.png")
	first := store.Apply("DP-1", previous, KindImage)[0]
	preview, err := h.eng.Apply(first, defaultSettings())
	if err != nil || !store.Commit(first, preview, EngineGSlapper) {
		t.Fatalf("apply prior image: err=%v", err)
	}

	next := store.Apply("DP-1", h.media("next.mp4"), KindVideo)[0]
	spawn := h.eng.spawn
	launches := 0
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == "gslapper" {
			launches++
			if launches == 1 {
				return nil, errors.New("launch failed")
			}
		}
		return spawn(argv)
	}
	_, err = h.eng.Apply(next, defaultSettings())
	var restored *restoredApplyError
	if !errors.As(err, &restored) || restored.state != StateStatic || restored.engine != EngineGSlapper {
		t.Fatalf("replacement error = %v, want prior image restored", err)
	}
	if !store.Fail(next, err) {
		t.Fatal("failed replacement was rejected")
	}
	assignment, ok := store.Assignment("DP-1")
	if !ok || assignment.Kind != KindImage || assignment.Path != previous || store.Runtime("DP-1").State != StateStatic {
		t.Fatalf("store after failed replacement = %+v runtime=%+v", assignment, store.Runtime("DP-1"))
	}
	if active := h.eng.active["DP-1"]; active.assignment.Path != previous || active.state != StateStatic {
		t.Fatalf("active wallpaper after failure = %+v, want prior image restored", active)
	}
}

func TestFailedEffectLaunchRestoresStaticFallbackImage(t *testing.T) {
	h := newEngineHarness(t)
	h.eng.caps = Capabilities{GSlapper: false, Statics: []string{engineSwaybg}}
	previous := h.media("previous.png")
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: previous, Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("fallback image: %v", err)
	}
	if h.eng.fallbackProcess("DP-1") == nil {
		t.Fatal("fallback image process was not recorded")
	}
	h.withTerminal()
	spawn := h.eng.spawn
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == "sysc-terminal" {
			h.mu.Lock()
			creates := h.spawnCreatesSocket
			h.spawnCreatesSocket = false
			h.mu.Unlock()
			proc, err := spawn(argv)
			h.mu.Lock()
			h.spawnCreatesSocket = creates
			h.mu.Unlock()
			return proc, err
		}
		return spawn(argv)
	}
	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Kind: KindEffect, Effect: "fire", Theme: "nord"}, defaultSettings())
	if err == nil {
		t.Fatal("terminal launch without a socket must fail")
	}
	var restored *restoredApplyError
	if !errors.As(err, &restored) || restored.state != StateStatic || restored.engine != engineSwaybg {
		t.Fatalf("effect error = %v, want the previous static image restored", err)
	}
	if h.eng.fallbackProcess("DP-1") == nil {
		t.Fatal("failed effect launch stopped the previous image without restarting the static fallback")
	}
	argvs := h.argvs()
	if len(argvs) != 3 || argvs[0][0] != engineSwaybg || argvs[1][0] != "sysc-terminal" ||
		argvs[2][0] != engineSwaybg || !slices.Contains(argvs[2], previous) {
		t.Fatalf("launches = %v, want fallback image, failed effect, restored fallback image", argvs)
	}
}

func TestFailedEffectLaunchRestoresPriorEffectWithoutStill(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	h.eng.caps.Catalog.BlurMax = 64
	previous := Assignment{Kind: KindEffect, Effect: "fire", Theme: "nord", Blur: 16}
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: previous.Effect, Theme: previous.Theme, Blur: previous.Blur}, defaultSettings()); err != nil {
		t.Fatalf("prior effect: %v", err)
	}

	spawn := h.eng.spawn
	terminalLaunches := 1
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == "sysc-terminal" {
			terminalLaunches++
			if terminalLaunches == 2 {
				h.mu.Lock()
				creates := h.spawnCreatesSocket
				h.spawnCreatesSocket = false
				h.mu.Unlock()
				proc, err := spawn(argv)
				h.mu.Lock()
				h.spawnCreatesSocket = creates
				h.mu.Unlock()
				return proc, err
			}
		}
		return spawn(argv)
	}
	_, err := h.eng.Apply(Job{
		Connector: "DP-1", Gen: 2, Kind: KindEffect, Effect: "rain", Theme: "dracula",
		Previous: previous, HasPrevious: true, PreviousState: StatePlaying, PreviousEngine: EngineTerminal,
	}, defaultSettings())
	if err == nil {
		t.Fatal("new terminal launch without a socket must fail")
	}
	var restored *restoredApplyError
	if !errors.As(err, &restored) || restored.state != StatePlaying || restored.engine != EngineTerminal {
		t.Fatalf("apply error = %v, want prior effect runtime restored", err)
	}
	argvs := h.argvs()
	if len(argvs) != 3 || !slices.Contains(argvs[2], "fire") || !slices.Contains(argvs[2], "nord") || !slices.Contains(argvs[2], "--blur") || !slices.Contains(argvs[2], "16") {
		t.Fatalf("launches = %v, want prior fire/nord effect relaunched", argvs)
	}
}

// deadSocketFileFor leaves a real socket file with no listener behind, the way
// a killed run does.
func deadSocketFileFor(t *testing.T, path string) {
	t.Helper()
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket missing: %v", err)
	}
}

func TestEngineUnlinksStaleSocketWithoutListener(t *testing.T) {
	h := newEngineHarness(t)
	deadSocketFileFor(t, h.socket("DP-1"))
	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatalf("apply over a stale socket: %v", err)
	}
	// Reaching spawn proves the dead file was cleared first: launch refuses
	// while the path exists.
	if argvs := h.argvs(); len(argvs) != 1 || argvs[0][0] != "gslapper" {
		t.Fatalf("spawned %v, want one gslapper", argvs)
	}
}

func TestEngineStillRefusesForeignLiveSocket(t *testing.T) {
	h := newEngineHarness(t)
	l, err := net.Listen("unix", h.socket("DP-1"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}
	if _, err := h.eng.Apply(job, defaultSettings()); err == nil || !strings.Contains(err.Error(), "not owned by this shell") {
		t.Fatalf("apply over a live foreign socket: got %v, want a refusal", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("spawned %v despite a live foreign socket", argvs)
	}
}

func TestEngineRestoreClearsStaleSocket(t *testing.T) {
	h := newEngineHarness(t)
	deadSocketFileFor(t, h.socket("DP-1"))
	if err := h.eng.Restore("DP-1", ""); err != nil {
		t.Fatalf("restore over a stale socket: %v", err)
	}
	if _, err := os.Lstat(h.socket("DP-1")); !os.IsNotExist(err) {
		t.Fatalf("stale socket survived restore: %v", err)
	}
}

func TestEngineRestoreRefusesForeignLiveSocketWithoutStoppedMarker(t *testing.T) {
	h := newEngineHarness(t)
	l, err := net.Listen("unix", h.socket("DP-1"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	err = h.eng.Restore("DP-1", "/c/still.jpg")
	if err == nil || !strings.Contains(err.Error(), "not ours to stop") {
		t.Fatalf("restore over a live foreign socket: got %v, want a refusal", err)
	}
	var stopped stoppedRestoreFailure
	if errors.As(err, &stopped) {
		t.Fatalf("restore error = %T, must not mark an unowned live socket stopped", err)
	}
	if _, err := os.Lstat(h.socket("DP-1")); err != nil {
		t.Fatalf("live foreign socket was removed: %v", err)
	}
}

func TestEngineRestoreDoesNotMarkProcessStopFailure(t *testing.T) {
	h := newEngineHarness(t)
	socket := h.socket("DP-1")
	deadSocketFileFor(t, socket)
	info, err := os.Lstat(socket)
	if err != nil {
		t.Fatalf("stat owned socket: %v", err)
	}
	proc := &failedStopProcess{exit: make(chan struct{}), err: errors.New("stop refused")}
	owned := watchProcess(proc)
	h.eng.owned["DP-1"] = owned
	h.eng.socketFiles = map[string]os.FileInfo{"DP-1": info}
	h.eng.request = func(string, string, time.Duration) (string, error) {
		return "", errors.New("IPC stop failed")
	}
	defer close(proc.exit)

	err = h.eng.Restore("DP-1", "/c/still.jpg")
	if err == nil || !strings.Contains(err.Error(), "stop process on DP-1: stop refused") {
		t.Fatalf("restore error = %v, want process stop failure", err)
	}
	var stopped stoppedRestoreFailure
	if errors.As(err, &stopped) {
		t.Fatalf("restore error = %T, must not mark a process that did not stop", err)
	}
	if _, err := os.Lstat(socket); err != nil {
		t.Fatalf("socket was removed after failed process stop: %v", err)
	}
}

func TestEngineRestoreDoesNotAssumeMissingSocketMeansProcessStopped(t *testing.T) {
	h := newEngineHarness(t)
	socket := h.socket("DP-1")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	info, err := os.Lstat(socket)
	if err != nil {
		t.Fatalf("stat owned socket: %v", err)
	}
	proc := &stubbornProcess{exit: make(chan struct{})}
	owned := watchProcess(proc)
	h.eng.owned["DP-1"] = owned
	h.eng.socketFiles = map[string]os.FileInfo{"DP-1": info}
	h.eng.request = func(path, command string, _ time.Duration) (string, error) {
		if command == "stop" {
			return "OK", os.Remove(path)
		}
		return "OK", nil
	}
	defer close(proc.exit)

	err = h.eng.Restore("DP-1", "/c/still.jpg")
	if err == nil || !strings.Contains(err.Error(), "process on DP-1 did not exit") {
		t.Fatalf("restore error = %v, want an unconfirmed process-stop error", err)
	}
	var stopped stoppedRestoreFailure
	if errors.As(err, &stopped) {
		t.Fatalf("restore error = %T, must not mark an unconfirmed stop", err)
	}
	if h.eng.ownedProcess("DP-1") != owned {
		t.Fatal("unconfirmed process was removed from engine ownership")
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Fatalf("stop fixture socket = %v, want absent", err)
	}
}

func TestRemoveSocketIfSameRefusesReplacedSocket(t *testing.T) {
	dir := sockpath.Dir(t, "wc")
	path := filepath.Join(dir, "gslapper-DP-1.sock")
	deadSocketFileFor(t, path)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	// Bind the replacement while the probed file still exists, then rename it
	// over: the inode cannot be a reuse of the one just unlinked.
	replacement := filepath.Join(dir, "replacement.sock")
	l, err := net.Listen("unix", replacement)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
	err = removeSocketIfSame(path, info)
	if err == nil || !strings.Contains(err.Error(), "changed while it was stopped") {
		t.Fatalf("removeSocketIfSame over a replaced socket: got %v, want a refusal", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("live replacement socket was removed: %v", err)
	}
}

func TestRemoveSocketIfSameRefusesReplacementAfterProbe(t *testing.T) {
	dir := sockpath.Dir(t, "wc")
	path := filepath.Join(dir, "gslapper-DP-1.sock")
	deadSocketFileFor(t, path)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat dead socket: %v", err)
	}

	replacement := filepath.Join(dir, "replacement.sock")
	listener, err := net.Listen("unix", replacement)
	if err != nil {
		t.Fatalf("listen replacement: %v", err)
	}
	defer listener.Close()

	err = removeSocketIfSameWithPeerProbe(path, info, func(path string) (int, error) {
		if err := os.Remove(path); err != nil {
			return 0, err
		}
		if err := os.Rename(replacement, path); err != nil {
			return 0, err
		}
		return 0, syscall.ECONNREFUSED
	})
	if err == nil || !strings.Contains(err.Error(), "changed while it was stopped") {
		t.Fatalf("removeSocketIfSame after replacement during probe: got %v, want a refusal", err)
	}
	if _, err := socketPeerPID(path); err != nil {
		t.Fatalf("live replacement socket was removed: %v", err)
	}
}

func TestRemoveSocketIfSameRefusesLiveSocket(t *testing.T) {
	path := filepath.Join(sockpath.Dir(t, "wc"), "gslapper-DP-1.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}

	err = removeSocketIfSame(path, info)
	if err == nil || !strings.Contains(err.Error(), "still live") {
		t.Fatalf("removeSocketIfSame on a live socket: got %v, want a refusal", err)
	}
	if _, err := socketPeerPID(path); err != nil {
		t.Fatalf("live socket stopped answering after cleanup refusal: %v", err)
	}
}

func TestEngineChildExitBeforeReady(t *testing.T) {
	h := newEngineHarness(t)
	h.mu.Lock()
	h.spawnCreatesSocket = false // the socket never appears, so query never succeeds
	h.mu.Unlock()

	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.mp4"), Kind: KindVideo}
	done := make(chan error, 1)
	go func() { _, err := h.eng.Apply(job, defaultSettings()); done <- err }()

	// The child exits on its own before it ever answers.
	deadline := time.After(time.Second)
	for {
		h.mu.Lock()
		procs := append([]*fakeProcess(nil), h.procs...)
		h.mu.Unlock()
		if len(procs) > 0 {
			procs[0].mu.Lock()
			close(procs[0].exit)
			procs[0].mu.Unlock()
			break
		}
		select {
		case <-deadline:
			t.Fatal("nothing was spawned")
		case <-time.After(2 * time.Millisecond):
		}
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a child that exits before it is ready must fail the apply")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("apply did not give up on a dead child")
	}
	if h.eng.ownedProcess("DP-1") != nil {
		t.Fatal("a failed launch must not stay registered as owned")
	}
}

func TestEngineRemovesSocketAfterFailedReady(t *testing.T) {
	h := newEngineHarness(t)
	h.replies["query"] = "ERROR: not ready"

	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}, defaultSettings())
	if err == nil {
		t.Fatal("a gSlapper that never answers must fail")
	}
	if _, statErr := os.Stat(h.socket("DP-1")); !os.IsNotExist(statErr) {
		t.Fatalf("failed launch left socket behind: %v", statErr)
	}
}

func TestEngineChangeOnLiveSocket(t *testing.T) {
	h := newEngineHarness(t)
	set := defaultSettings()
	set.Fade = true
	// An instance is already up and answering.
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.replies["query"] = "STATUS: playing image /w/old.png"

	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("new.png"), Kind: KindImage}
	if _, err := h.eng.Apply(job, set); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("a live socket must take the change path, spawned %v", argvs)
	}
}

func TestEngineSerializesSameOutputChanges(t *testing.T) {
	h := newEngineHarness(t)
	set := defaultSettings()
	set.Fade = true
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	started := make(chan string, 2)
	release := make(chan struct{})
	h.eng.request = func(socket, command string, _ time.Duration) (string, error) {
		if _, err := os.Stat(socket); err != nil {
			return "", err
		}
		verb, _, _ := strings.Cut(command, " ")
		if verb == "query" {
			return "STATUS: playing image /w/old.png", nil
		}
		if verb == "change" {
			started <- command
			<-release
		}
		return "OK", nil
	}

	first := make(chan error, 1)
	go func() {
		_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("first.png"), Kind: KindImage}, set)
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first change did not reach the engine")
	}

	second := make(chan error, 1)
	go func() {
		_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Path: h.media("second.png"), Kind: KindImage}, set)
		second <- err
	}()
	select {
	case command := <-started:
		close(release)
		<-first
		<-second
		t.Fatalf("same-output change started concurrently: %q", command)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second apply: %v", err)
	}
}

func TestEngineSkipsApplyOlderThanAdvancedGeneration(t *testing.T) {
	h := newEngineHarness(t)
	h.eng.AdvanceGeneration("DP-1", 2)

	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("stale.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("stale apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("stale apply spawned work: %v", argvs)
	}

	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 2, Path: h.media("current.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("current apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 1 {
		t.Fatalf("current apply spawned %d processes, want one: %v", len(argvs), argvs)
	}
}

func TestEngineKeepsOwnedProcessOnChangeErrorReply(t *testing.T) {
	h := newEngineHarness(t)
	set := defaultSettings()
	set.Fade = true
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.eng.request = func(socket, command string, _ time.Duration) (string, error) {
		if _, err := os.Stat(socket); err != nil {
			return "", err
		}
		verb, _, _ := strings.Cut(command, " ")
		if verb == "query" {
			return "STATUS: playing image /w/old.png", nil
		}
		if verb == "change" {
			return "ERROR: no such file", nil
		}
		return "OK", nil
	}

	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("new.png"), Kind: KindImage}, set)
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("change error = %v, want the wire error", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("an unrelated change error must not relaunch, spawned %v", argvs)
	}
}

func TestEngineLeavesForeignLiveSocketAlone(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.replies["query"] = "STATUS: playing image /w/foreign.png"

	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("new.png"), Kind: KindImage}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("foreign socket apply error = %v, want ownership error", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("foreign socket must not be relaunched, spawned %v", argvs)
	}
}

func TestEngineLeavesForeignErrorSocketAlone(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.eng.request = func(socket, command string, _ time.Duration) (string, error) {
		if _, err := os.Stat(socket); err != nil {
			return "", err
		}
		if verb, _, _ := strings.Cut(command, " "); verb == "query" {
			return "ERROR: no pipeline", nil
		}
		return "OK", nil
	}

	_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("new.png"), Kind: KindImage}, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("foreign error socket apply error = %v, want ownership error", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("foreign error socket must not be relaunched, spawned %v", argvs)
	}
}

func TestEngineDoesNotRemoveReplacedSocket(t *testing.T) {
	h := newEngineHarness(t)
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("old.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	socket := h.socket("DP-1")
	// Create the replacement while the original still exists, then rename it
	// over: the inode cannot be a reuse of the one just unlinked, so the
	// identity check stays meaningful on filesystems that recycle inodes.
	replacement := socket + ".replacement"
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, socket); err != nil {
		t.Fatalf("replace old socket: %v", err)
	}
	called := false
	h.eng.request = func(string, string, time.Duration) (string, error) {
		called = true
		return "OK", nil
	}
	if err := h.eng.Restore("DP-1", ""); err == nil {
		t.Fatal("restore must report a replaced socket")
	}
	if called {
		t.Fatal("restore sent IPC to a socket that replaced the owned one")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
}

func TestEngineTreatsQueryErrorAsNotReady(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	firstQuery := true
	h.eng.request = func(socket, command string, _ time.Duration) (string, error) {
		verb, _, _ := strings.Cut(command, " ")
		if verb == "query" {
			if firstQuery {
				firstQuery = false
				return "ERROR: no pipeline", nil
			}
			return "STATUS: playing image /w/old.png", nil
		}
		if verb == "stop" {
			_ = os.Remove(socket)
		}
		return "OK", nil
	}

	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("new.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 1 {
		t.Fatalf("a query error must restart the owned process, spawned %v", argvs)
	}
}

func TestEngineCloseStopsOwnedProcess(t *testing.T) {
	h := newEngineHarness(t)
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	h.mu.Lock()
	proc := h.procs[0]
	h.mu.Unlock()

	closer, ok := any(h.eng).(interface{ Close() })
	if !ok {
		t.Fatal("engine does not expose shutdown")
	}
	closer.Close()

	if !proc.wasStopped() {
		t.Fatal("engine close did not stop the owned process")
	}
	if h.eng.ownedProcess("DP-1") != nil {
		t.Fatal("engine close left an owned process registered")
	}
}

func TestEngineCloseRemovesTerminalSocket(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire"}, defaultSettings()); err != nil {
		t.Fatalf("apply effect: %v", err)
	}
	socket := terminalSocketPath(h.eng.dir, "DP-1")
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("terminal socket before close: %v", err)
	}

	h.eng.Close()
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("terminal socket after close: %v, want removed", err)
	}
}

func TestEngineCloseRemovesTerminalSocketDuringLaunch(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	h.eng.readyWait = time.Hour
	socket := terminalSocketPath(h.eng.dir, "DP-1")
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	h.eng.request = func(_, _ string, _ time.Duration) (string, error) {
		close(queryStarted)
		<-releaseQuery
		return "STATUS: playing effect fire theme nord", nil
	}
	spawn := h.eng.spawn
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] != "sysc-terminal" {
			return spawn(argv)
		}
		i := slices.Index(argv, "-I")
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: argv[i+1], Net: "unix"})
		if err != nil {
			return nil, err
		}
		listener.SetUnlinkOnClose(false)
		proc := &startupSocketProcess{fakeProcess: newFakeProcess(), listener: listener}
		h.mu.Lock()
		h.spawned = append(h.spawned, slices.Clone(argv))
		h.procs = append(h.procs, proc.fakeProcess)
		h.mu.Unlock()
		return proc, nil
	}
	result := make(chan error, 1)
	go func() {
		_, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire"}, defaultSettings())
		result <- err
	}()
	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal launch never reached readiness query")
	}
	h.eng.Close()
	_, statErr := os.Stat(socket)
	close(releaseQuery)
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("apply did not stop after engine close")
	}
	if !os.IsNotExist(statErr) {
		t.Fatalf("terminal socket after closing during launch: %v, want removed", statErr)
	}
}

func TestEngineCloseDoesNotWaitOnSocketIPC(t *testing.T) {
	h := newEngineHarness(t)
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	blocked := make(chan struct{})
	h.eng.request = func(string, string, time.Duration) (string, error) {
		<-blocked
		return "", errors.New("request should not run during close")
	}

	done := make(chan struct{})
	go func() {
		h.eng.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		close(blocked)
		t.Fatal("engine close waited on socket IPC")
	}
}

func TestEngineStopsOwnedGSlapperWithHardStop(t *testing.T) {
	h := newEngineHarness(t)
	proc := &hardStopProcess{fakeProcess: newFakeProcess()}
	h.eng.owned["DP-1"] = watchProcess(proc)

	if err := h.eng.stopOwned("DP-1", h.socket("DP-1")); err != nil {
		t.Fatalf("stop owned: %v", err)
	}
	if !proc.wasForceStopped() {
		t.Fatal("owned gSlapper teardown must use the hard stop path")
	}
}

func TestEngineStopsExitedFallbackWithoutSignallingIt(t *testing.T) {
	h := newEngineHarness(t)
	proc := exitedProcess(nil)
	tracked := watchProcess(proc)
	if !waitProcess(tracked, time.Second) {
		t.Fatal("exited fallback was not reaped")
	}
	h.eng.fallbacks["DP-1"] = tracked

	if err := h.eng.stopFallback("DP-1"); err != nil {
		t.Fatalf("stop fallback: %v", err)
	}
	if proc.wasStopped() {
		t.Fatal("an exited fallback must not receive another stop signal")
	}
}

func TestEngineAutoStopErrorRelaunchesOnce(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.replies["query"] = "STATUS: playing image /w/old.png"
	h.replies["change"] = "ERROR: cannot update path (use --auto-stop for video changes)"

	// hidden=auto-stop takes the change path, which is what fails here.
	set := defaultSettings()
	set.Hidden = HiddenAutoStop
	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("new.mp4"), Kind: KindVideo}
	if _, err := h.eng.Apply(job, set); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 1 {
		t.Fatalf("the auto-stop error must relaunch exactly once, spawned %v", argvs)
	}
}

func TestEngineVideoSwapRestartsWithoutAutoStop(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.replies["query"] = "STATUS: playing video /w/old.mp4"

	// hidden=none cannot change a video path, so the engine must not even try.
	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("new.mp4"), Kind: KindVideo}
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 1 {
		t.Fatalf("a video swap at hidden=none must relaunch, spawned %v", argvs)
	}
}

func TestEngineRestoreStopsThenFallsBack(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.replies["query"] = "STATUS: playing video /w/b.mp4"
	h.replies["stop"] = "OK"

	if err := h.eng.Restore("DP-1", "/c/still.jpg"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(h.socket("DP-1")); !os.IsNotExist(err) {
		t.Fatal("restore must wait for the owned socket to go away")
	}
	var img []string
	for _, argv := range h.argvs() {
		if argv[0] == engineAwww && slices.Contains(argv, "img") {
			img = argv
		}
	}
	if img == nil {
		t.Fatalf("restore must hand the still to the static fallback, got %v", h.argvs())
	}
	if !slices.Contains(img, "/c/still.jpg") {
		t.Fatalf("fallback argv missing the still: %v", img)
	}
	// The daemon has to be up before the client can hand anything over (D16).
	if !slices.ContainsFunc(h.argvs(), func(a []string) bool { return a[0] == engineAwwwDaemon }) {
		t.Fatal("awww-daemon was never started")
	}
}

func TestEngineRestoreMarksFallbackFailureAfterStopping(t *testing.T) {
	h := newEngineHarness(t)
	h.eng.caps = Capabilities{GSlapper: true}
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.replies["stop"] = "OK"
	h.eng.setActive("DP-1", Assignment{Kind: KindEffect, Effect: "fire", Theme: "nord"}, StatePlaying, EngineTerminal)

	err := h.eng.Restore("DP-1", "/c/still.jpg")
	if !errors.Is(err, ErrNoStaticEngine) {
		t.Fatalf("restore error = %v, want missing static engine", err)
	}
	var stopped stoppedRestoreFailure
	if !errors.As(err, &stopped) {
		t.Fatalf("restore error = %T, want marker that the animated engine stopped", err)
	}
	if _, err := os.Stat(h.socket("DP-1")); !os.IsNotExist(err) {
		t.Fatalf("owned engine socket still exists after restore failure: %v", err)
	}
	if _, ok := h.eng.active["DP-1"]; ok {
		t.Fatal("restore failure retained the stopped effect as active")
	}
}

func TestEngineRestoreClearsActiveWhenStoppingFallbackFails(t *testing.T) {
	h := newEngineHarness(t)
	socket := h.socket("DP-1")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	info, err := os.Lstat(socket)
	if err != nil {
		t.Fatalf("stat owned socket: %v", err)
	}
	h.eng.owned["DP-1"] = watchProcess(exitedProcess(nil))
	h.eng.socketFiles = map[string]os.FileInfo{"DP-1": info}
	fallback := &failedStopProcess{exit: make(chan struct{}), err: errors.New("fallback stop refused")}
	h.eng.fallbacks["DP-1"] = watchProcess(fallback)
	h.eng.setActive("DP-1", Assignment{Kind: KindEffect, Effect: "fire", Theme: "nord"}, StatePlaying, EngineTerminal)
	defer close(fallback.exit)

	err = h.eng.Restore("DP-1", "/c/still.jpg")
	if err == nil || !strings.Contains(err.Error(), "fallback stop refused") {
		t.Fatalf("restore error = %v, want fallback stop failure", err)
	}
	var stopped stoppedRestoreFailure
	if !errors.As(err, &stopped) {
		t.Fatalf("restore error = %T, want marker that the animated engine stopped", err)
	}
	if _, ok := h.eng.active["DP-1"]; ok {
		t.Fatal("fallback stop failure retained the stopped effect as active")
	}
}

func TestEngineRestoreMarksSocketCleanupFailureAfterStopping(t *testing.T) {
	h := newEngineHarness(t)
	socket := h.socket("DP-1")
	deadSocketFileFor(t, socket)
	info, err := os.Lstat(socket)
	if err != nil {
		t.Fatalf("stat owned socket: %v", err)
	}
	proc := watchProcess(newFakeProcess())
	h.eng.owned["DP-1"] = proc
	h.eng.socketFiles = map[string]os.FileInfo{"DP-1": info}
	h.eng.setActive("DP-1", Assignment{Kind: KindEffect, Effect: "fire", Theme: "nord"}, StatePlaying, EngineTerminal)

	var replacement net.Listener
	h.eng.request = func(path, command string, _ time.Duration) (string, error) {
		if command != "stop" {
			return "OK", nil
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
		replacement, err = net.Listen("unix", path)
		if err != nil {
			return "", err
		}
		return "OK", proc.Stop()
	}
	defer func() {
		if replacement != nil {
			_ = replacement.Close()
		}
	}()

	err = h.eng.Restore("DP-1", "/c/still.jpg")
	if err == nil || !strings.Contains(err.Error(), "changed while it was stopped") {
		t.Fatalf("restore error = %v, want socket cleanup failure", err)
	}
	var stopped stoppedRestoreFailure
	if !errors.As(err, &stopped) {
		t.Fatalf("restore error = %T, want marker that the owned engine stopped", err)
	}
	if _, ok := h.eng.active["DP-1"]; ok {
		t.Fatal("socket cleanup failure retained the stopped effect as active")
	}
	failedApply := errors.New("next apply failed")
	job := h.eng.actualPrevious(Job{Connector: "DP-1", Gen: 2, Path: h.media("next.mp4"), Kind: KindVideo})
	if job.HasPrevious {
		t.Fatalf("next apply inherited stale active assignment: %+v", job.Previous)
	}
	if got := h.eng.restorePreviousAfterApplyFailure(job, defaultSettings(), failedApply); !errors.Is(got, failedApply) {
		t.Fatalf("failed apply rollback error = %v, want original error", got)
	}
	if _, err := os.Lstat(socket); err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
}

func TestEngineRestoreWithNoStillLeavesEmpty(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.ownSocket("DP-1")
	h.replies["stop"] = "OK"

	if err := h.eng.Restore("DP-1", ""); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("no still means an empty desktop, not a spawn: %v", argvs)
	}
}

func TestEngineNeverBuildsAKillArgv(t *testing.T) {
	h := newEngineHarness(t)
	h.replies["stop"] = "OK"
	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.mp4"), Kind: KindVideo}
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	_ = h.eng.Restore("DP-1", "/c/still.jpg")

	for _, argv := range h.argvs() {
		for _, arg := range argv {
			for _, banned := range []string{"pkill", "killall", "kill"} {
				if arg == banned {
					t.Fatalf("argv %v matches processes by name; stop only what we own (D17)", argv)
				}
			}
		}
	}
}

func TestEngineStaticFallbackWithoutGSlapper(t *testing.T) {
	h := newEngineHarness(t)
	h.eng.caps = Capabilities{GSlapper: false, Statics: []string{engineSwaybg}}

	image := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}
	if _, err := h.eng.Apply(image, defaultSettings()); err != nil {
		t.Fatalf("a still must still apply without gSlapper: %v", err)
	}
	argvs := h.argvs()
	if len(argvs) != 1 || argvs[0][0] != engineSwaybg {
		t.Fatalf("got %v, want a swaybg argv", argvs)
	}
	// swaybg owns the surface, so unlike awww its process is held onto.
	if h.eng.fallbackProcess("DP-1") == nil {
		t.Error("a persistent fallback must be recorded so we can stop the one we started")
	}

	video := Job{Connector: "DP-3", Gen: 1, Path: h.media("b.mp4"), Kind: KindVideo}
	if _, err := h.eng.Apply(video, defaultSettings()); err == nil {
		t.Fatal("a video without gSlapper must fail rather than silently do nothing")
	}
}

func TestEngineCleansFailedAwwwDaemon(t *testing.T) {
	h := newEngineHarness(t)
	baseSpawn := h.eng.spawn
	h.eng.spawn = func(argv []string) (Process, error) {
		if argv[0] == engineAwwwDaemon {
			return exitedProcess(errors.New("daemon exited")), nil
		}
		return baseSpawn(argv)
	}

	if err := h.eng.ensureAwwwDaemon(); err == nil {
		t.Fatal("a daemon that exits before readiness must fail")
	}
	if h.eng.awww != nil {
		t.Fatal("failed awww startup left a daemon handle behind")
	}
}

// Cancellation must end readiness even if the process ignores shutdown.
func TestEngineReadinessCancellation(t *testing.T) {
	h := newEngineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.eng.ctx = ctx
	h.eng.readyWait = time.Second
	cancel()
	started := time.Now()
	err := h.eng.waitForQuery(watchProcess(exitedProcess(nil)), h.socket("DP-1"), "gslapper")
	if !errors.Is(err, errEngineClosed) {
		t.Fatalf("readiness = %v", err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("cancelled readiness waited")
	}
}

func TestEngineStillWithoutFadeRestartsForEveryApply(t *testing.T) {
	h := newEngineHarness(t)
	for i, name := range []string{"a.png", "b.png", "a.png"} {
		if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: uint64(i + 1), Path: h.media(name), Kind: KindImage}, defaultSettings()); err != nil {
			t.Fatal(err)
		}
	}
	defer h.eng.Close()
	if got := len(h.argvs()); got != 3 {
		t.Fatalf("launched %d processes; non-fading stills require a fresh render on each apply", got)
	}
}

func TestStillForExtractsVideoStillOnDemand(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "clip.mp4")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(srcDir, "gone.mp4")

	var called int
	e := &gslapperEngine{extract: func(_ context.Context, _, dst string) error {
		called++
		return os.WriteFile(dst, []byte("fake jpeg"), 0o644)
	}}
	want := CachedStillPath(src)
	if want == "" {
		t.Fatal("no cache path")
	}
	if got := e.stillFor(Job{Kind: KindVideo, Path: src}); got != want || called != 1 {
		t.Fatalf("first stillFor = %q (extract calls %d)", got, called)
	}
	if got := e.stillFor(Job{Kind: KindVideo, Path: src}); got != want || called != 1 {
		t.Fatalf("cached stillFor re-extracted (calls %d)", called)
	}

	e.extract = func(context.Context, string, string) error { return errors.New("no ffmpeg") }
	if got := e.stillFor(Job{Kind: KindVideo, Path: missing}); got != "" {
		t.Fatalf("failed extractor must degrade to no seed, got %q", got)
	}
}

func TestOwnedExitedRejectsStaleGenerationAndStoppedOwnership(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	defer h.eng.Close()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire", Theme: "nord"}, defaultSettings()); err != nil {
		t.Fatal(err)
	}
	if h.eng.OwnedExited("DP-1", 1) {
		t.Fatal("live process reported exited")
	}
	h.mu.Lock()
	proc := h.procs[0]
	h.mu.Unlock()
	_ = proc.Stop()
	if !waitProcess(h.eng.ownedProcess("DP-1"), time.Second) {
		t.Fatal("child was not reaped")
	}
	if !h.eng.OwnedExited("DP-1", 1) {
		t.Fatal("current exited process was missed")
	}
	h.eng.AdvanceGeneration("DP-1", 2)
	if h.eng.OwnedExited("DP-1", 1) {
		t.Fatal("stale generation reported exited")
	}
	if err := h.eng.Restore("DP-1", ""); err != nil {
		t.Fatal(err)
	}
	if h.eng.OwnedExited("DP-1", 2) {
		t.Fatal("restored output reported exited")
	}
}

func TestTerminalBlurLaunchAndCompatibility(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	job := Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire", Theme: "nord", Blur: 16}
	if _, err := h.eng.Apply(job, defaultSettings()); err == nil {
		t.Fatal("old engine accepted blur")
	}
	if len(h.argvs()) != 0 {
		t.Fatal("unsupported blur replaced wallpaper")
	}
	h.eng.caps.Catalog.BlurMax = 64
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatal(err)
	}
	args := h.argvs()[0]
	found := false
	for i, v := range args {
		if v == "--blur" && i+1 < len(args) && args[i+1] == "16" {
			found = true
		}
	}
	if !found {
		t.Fatalf("launch omitted blur: %v", args)
	}
	job.Blur = 65
	if _, err := h.eng.Apply(job, defaultSettings()); err == nil {
		t.Fatal("out-of-range blur accepted")
	}
	if len(h.argvs()) != 1 {
		t.Fatal("invalid blur restarted wallpaper")
	}
}

func TestEngineSetFPSUnsupportedIsCachedPerProcess(t *testing.T) {
	h := newEngineHarness(t)
	h.withTerminal()
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Kind: KindEffect, Effect: "fire", Theme: "nord"}, defaultSettings()); err != nil {
		t.Fatalf("apply effect: %v", err)
	}
	h.replies["fps"] = "ERR unknown command"
	calls := 0
	base := h.eng.request
	h.eng.request = func(socket, command string, d time.Duration) (string, error) {
		if strings.HasPrefix(command, "fps ") {
			calls++
		}
		return base(socket, command, d)
	}

	if err := h.eng.SetFPS("DP-1", 10); !errors.Is(err, ErrFPSUnsupported) {
		t.Fatalf("SetFPS on an old sysc-terminal = %v, want ErrFPSUnsupported", err)
	}
	if err := h.eng.SetFPS("DP-1", 20); !errors.Is(err, ErrFPSUnsupported) {
		t.Fatalf("cached SetFPS = %v, want ErrFPSUnsupported", err)
	}
	if calls != 1 {
		t.Fatalf("fps requests reached the process %d times, want 1 (cached per process)", calls)
	}
	proc := h.eng.ownedProcess("DP-1")
	if proc == nil {
		t.Fatal("effect process is not owned")
	}
	h.eng.mu.Lock()
	pid := h.eng.fpsUnsupported["DP-1"]
	h.eng.mu.Unlock()
	if pid != processPID(proc) {
		t.Fatalf("cached unsupported pid = %d, want %d", pid, processPID(proc))
	}
}

func TestEngineSetFPSRefusesNonTerminalEngines(t *testing.T) {
	h := newEngineHarness(t)
	path := h.media("clip.mp4")
	if _, err := h.eng.Apply(Job{Connector: "DP-1", Gen: 1, Path: path, Kind: KindVideo}, defaultSettings()); err != nil {
		t.Fatalf("apply video: %v", err)
	}
	requests := 0
	base := h.eng.request
	h.eng.request = func(socket, command string, d time.Duration) (string, error) {
		if strings.HasPrefix(command, "fps ") {
			requests++
		}
		return base(socket, command, d)
	}
	if err := h.eng.SetFPS("DP-1", 10); !errors.Is(err, ErrFPSUnsupported) {
		t.Fatalf("SetFPS on gslapper = %v, want ErrFPSUnsupported", err)
	}
	if requests != 0 {
		t.Fatalf("gslapper received %d fps requests, want none", requests)
	}
}

// The helpers under the engine know nothing about the caller, so they report
// what they found without a domain prefix. The engine layer adds wallpaper:
// exactly once, so a user never reads it twice.
func TestSocketHelpersCarryNoDomainPrefix(t *testing.T) {
	dir := sockpath.Dir(t, "wc")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	symlink := filepath.Join(dir, "gslapper-DP-1.sock")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := socketFileIfOwned(symlink, nil); err == nil {
		t.Fatal("socketFileIfOwned over a symlink: got no error, want a refusal")
	} else if strings.HasPrefix(err.Error(), "wallpaper:") {
		t.Fatalf("socketFileIfOwned returned %q; the caller owns the domain prefix", err)
	}

	dead := filepath.Join(dir, "dead.sock")
	deadSocketFileFor(t, dead)
	info, err := os.Lstat(dead)
	if err != nil {
		t.Fatalf("lstat dead socket: %v", err)
	}
	replacement := filepath.Join(dir, "replacement.sock")
	listener, err := net.Listen("unix", replacement)
	if err != nil {
		t.Fatalf("listen replacement: %v", err)
	}
	defer listener.Close()
	if err := os.Rename(replacement, dead); err != nil {
		t.Fatalf("rename replacement: %v", err)
	}
	if err := removeSocketIfSame(dead, info); err == nil {
		t.Fatal("removeSocketIfSame over a replaced socket: got no error, want a refusal")
	} else if strings.HasPrefix(err.Error(), "wallpaper:") {
		t.Fatalf("removeSocketIfSame returned %q; the caller owns the domain prefix", err)
	}
}

// Every failure the engine reports to the user carries the wallpaper: prefix
// once. A second one is a doubled message, not a clearer one.
func TestEngineErrorsCarryOneWallpaperPrefix(t *testing.T) {
	t.Run("symlinked socket", func(t *testing.T) {
		h := newEngineHarness(t)
		// The spawned engine leaves a symlink where its socket belongs: the
		// ownership check refuses it and the refusal reaches the user.
		h.eng.spawn = func(argv []string) (Process, error) {
			i := slices.Index(argv, "-I")
			if i < 0 {
				t.Fatalf("launch args %v carry no socket path", argv)
			}
			seed := filepath.Join(h.mediaDir, "socket-target")
			if err := os.WriteFile(seed, []byte("x"), 0o600); err != nil {
				t.Fatalf("seed target: %v", err)
			}
			if err := os.Symlink(seed, argv[i+1]); err != nil {
				t.Fatalf("symlink socket: %v", err)
			}
			return newFakeProcess(), nil
		}
		job := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}
		_, err := h.eng.Apply(job, defaultSettings())
		if err == nil {
			t.Fatal("apply over a symlinked socket: got no error, want a refusal")
		}
		if got := strings.Count(err.Error(), "wallpaper:"); got != 1 {
			t.Fatalf("apply over a symlinked socket: got %q with %d wallpaper prefixes, want exactly one", err, got)
		}
	})

	t.Run("unverifiable owned socket", func(t *testing.T) {
		h := newEngineHarness(t)
		// The process is gone and its socket identity was never recorded, so
		// the socket cannot be verified and is left alone.
		deadSocketFileFor(t, h.socket("DP-1"))
		h.eng.request = func(socket, command string, _ time.Duration) (string, error) { return "OK", nil }
		h.eng.owned["DP-1"] = watchProcess(exitedProcess(nil))
		err := h.eng.stopOwned("DP-1", h.socket("DP-1"))
		if err == nil {
			t.Fatal("stopOwned over an unverified socket: got no error, want a refusal")
		}
		if got := strings.Count(err.Error(), "wallpaper:"); got != 1 {
			t.Fatalf("stopOwned over an unverified socket: got %q with %d wallpaper prefixes, want exactly one", err, got)
		}
	})
}
