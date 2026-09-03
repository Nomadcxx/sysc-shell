package wallpaper

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProcess struct {
	mu      sync.Mutex
	stopped bool
	exit    chan struct{}
	err     error
}

func newFakeProcess() *fakeProcess { return &fakeProcess{exit: make(chan struct{})} }

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
}

func newEngineHarness(t *testing.T) *engineHarness {
	t.Helper()
	h := &engineHarness{
		t:                  t,
		replies:            map[string]string{},
		spawnCreatesSocket: true,
	}
	dir := t.TempDir()
	h.mediaDir = t.TempDir()
	h.eng = &gslapperEngine{
		dir:       dir,
		caps:      Capabilities{GSlapper: true, Static: engineAwww},
		readyWait: 300 * time.Millisecond,
		poll:      5 * time.Millisecond,
		owned:     map[string]Process{},
		fallbacks: map[string]Process{},
		spawn: func(argv []string) (Process, error) {
			h.mu.Lock()
			h.spawned = append(h.spawned, slices.Clone(argv))
			proc := newFakeProcess()
			h.procs = append(h.procs, proc)
			create := h.spawnCreatesSocket
			h.mu.Unlock()
			if create && argv[0] == "gslapper" {
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
			return "OK", nil
		},
	}
	return h
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

func TestEngineChangeOnLiveSocket(t *testing.T) {
	h := newEngineHarness(t)
	// An instance is already up and answering.
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	h.replies["query"] = "STATUS: playing image /w/old.png"

	job := Job{Connector: "DP-1", Gen: 1, Path: h.media("new.png"), Kind: KindImage}
	if _, err := h.eng.Apply(job, defaultSettings()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if argvs := h.argvs(); len(argvs) != 0 {
		t.Fatalf("a live socket must take the change path, spawned %v", argvs)
	}
}

func TestEngineAutoStopErrorRelaunchesOnce(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
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
	h.replies["query"] = "STATUS: playing video /w/b.mp4"
	h.replies["stop"] = "OK"

	if err := h.eng.Restore("DP-1", "/c/still.jpg"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(h.socket("DP-1")); !os.IsNotExist(err) {
		t.Fatal("restore must wait for the owned socket to go away")
	}
	argvs := h.argvs()
	if len(argvs) != 1 || argvs[0][0] != engineAwww {
		t.Fatalf("restore must hand the still to the static fallback, got %v", argvs)
	}
	if !slices.Contains(argvs[0], "/c/still.jpg") {
		t.Fatalf("fallback argv missing the still: %v", argvs[0])
	}
}

func TestEngineRestoreWithNoStillLeavesEmpty(t *testing.T) {
	h := newEngineHarness(t)
	if err := os.WriteFile(h.socket("DP-1"), nil, 0o600); err != nil {
		t.Fatalf("seed socket: %v", err)
	}
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
	h.eng.caps = Capabilities{GSlapper: false, Static: engineSwaybg}

	image := Job{Connector: "DP-1", Gen: 1, Path: h.media("a.png"), Kind: KindImage}
	if _, err := h.eng.Apply(image, defaultSettings()); err != nil {
		t.Fatalf("a still must still apply without gSlapper: %v", err)
	}
	argvs := h.argvs()
	if len(argvs) != 1 || argvs[0][0] != engineSwaybg {
		t.Fatalf("got %v, want a swaybg argv", argvs)
	}

	video := Job{Connector: "DP-3", Gen: 1, Path: h.media("b.mp4"), Kind: KindVideo}
	if _, err := h.eng.Apply(video, defaultSettings()); err == nil {
		t.Fatal("a video without gSlapper must fail rather than silently do nothing")
	}
}
