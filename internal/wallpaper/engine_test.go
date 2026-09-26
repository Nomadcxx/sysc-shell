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

// exitedProcess is a one-shot client that has already finished with err.
func exitedProcess(err error) *fakeProcess {
	p := &fakeProcess{exit: make(chan struct{}), err: err}
	close(p.exit)
	return p
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
	dir := t.TempDir()
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

func (h *engineHarness) ownSocket(connector string) {
	h.eng.owned[connector] = newFakeProcess()
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

func TestEngineStopOwnedRefusesForeignLiveSocket(t *testing.T) {
	h := newEngineHarness(t)
	l, err := net.Listen("unix", h.socket("DP-1"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	if err := h.eng.stopOwned("DP-1", h.socket("DP-1")); err == nil || !strings.Contains(err.Error(), "not ours to stop") {
		t.Fatalf("stopOwned over a live foreign socket: got %v, want a refusal", err)
	}
	if _, err := os.Lstat(h.socket("DP-1")); err != nil {
		t.Fatalf("live foreign socket was removed: %v", err)
	}
}

func TestRemoveSocketIfSameRefusesReplacedSocket(t *testing.T) {
	dir := t.TempDir()
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
	err := h.eng.waitForQuery(watchProcess(exitedProcess(nil)), h.socket("DP-1"))
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
