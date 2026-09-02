package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	"github.com/Nomadcxx/sysc-shell/plugins/reference/recorder"
)

func TestPluginRecorderGateRecordArgsNotifyAndDisable(t *testing.T) {
	h := startRecorder(t, "hang")
	h.open("bar-a", v1.ViewBar, "DP-1")
	h.open("bar-b", v1.ViewBar, "HDMI-1")
	h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Record") })
	if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{
		"directory": h.dir, "video_source": "focused", "frame_rate": 30.0,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
		t.Fatal(err)
	}
	h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
	h.waitView("bar-b", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
	args := h.waitArgs(func(a []string) bool { return hasArg(a, "-w", "DP-1") && hasArg(a, "-f", "30") })
	if hasShell(args) {
		t.Fatalf("args look like a shell line: %v", args)
	}
	pid := h.recorderPID()
	if pid <= 0 {
		t.Fatal("recording left no backend pid")
	}

	if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
		t.Fatal(err)
	}
	h.waitView("bar-a", func(n *v1.Node) bool {
		return strings.Contains(treeText(n), "Record") && !strings.Contains(treeText(n), "Recording")
	})
	h.waitNotify(func(p v1.NotifyParams) bool { return strings.Contains(p.Summary, "saved") })
	if alive(pid) {
		t.Fatalf("backend pid %d still running after stop", pid)
	}

	if err := h.send(&v1.HostShutdown{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not stop the plugin")
	}
}

func TestPluginRecorderGateCrashHungZeroFloodAndRejectedConfig(t *testing.T) {
	t.Run("crash", func(t *testing.T) {
		h := startRecorder(t, "crash")
		h.open("bar-a", v1.ViewBar, "DP-1")
		if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{"directory": h.dir}}); err != nil {
			t.Fatal(err)
		}
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "failed") })
		h.waitNotify(func(p v1.NotifyParams) bool { return p.Body != "" })
	})
	t.Run("zero", func(t *testing.T) {
		h := startRecorder(t, "zero")
		h.open("bar-a", v1.ViewBar, "DP-1")
		if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{"directory": h.dir}}); err != nil {
			t.Fatal(err)
		}
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "failed") })
	})
	t.Run("hung-stop", func(t *testing.T) {
		h := startRecorder(t, "ignore-int")
		h.open("bar-a", v1.ViewBar, "DP-1")
		if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{"directory": h.dir}}); err != nil {
			t.Fatal(err)
		}
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
		pid := h.recorderPID()
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool {
			return strings.Contains(treeText(n), "failed") || strings.Contains(treeText(n), "Record")
		})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && alive(pid) {
			time.Sleep(20 * time.Millisecond)
		}
		if alive(pid) {
			t.Fatalf("hung backend pid %d survived SIGKILL", pid)
		}
	})
	t.Run("flood", func(t *testing.T) {
		h := startRecorder(t, "flood")
		h.open("bar-a", v1.ViewBar, "DP-1")
		if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{"directory": h.dir}}); err != nil {
			t.Fatal(err)
		}
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitView("bar-a", func(n *v1.Node) bool {
			return strings.Contains(treeText(n), "Record") && !strings.Contains(treeText(n), "Recording")
		})
	})
	t.Run("rejected-config", func(t *testing.T) {
		h := startRecorder(t, "hang")
		h.open("bar-a", v1.ViewBar, "DP-1")
		if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{
			"directory": h.dir, "frame_rate": 999.0,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
			t.Fatal(err)
		}
		h.waitArgs(func(a []string) bool { return hasArg(a, "-f", "60") })
	})
}

func TestPluginRecorderGateAdoptionAndAmbiguous(t *testing.T) {
	h := startRecorder(t, "hang")
	h.open("bar-a", v1.ViewBar, "DP-1")
	if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{"directory": h.dir}}); err != nil {
		t.Fatal(err)
	}
	if err := h.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
		t.Fatal(err)
	}
	h.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
	own := h.waitOwn(func(o recorder.Ownership) bool { return o.PID > 0 })
	if !alive(own.PID) {
		t.Fatal("backend died before adoption")
	}
	_ = h.cmd.Process.Kill()
	select {
	case <-h.done:
	case <-time.After(2 * time.Second):
		t.Fatal("killed plugin did not exit")
	}
	if !alive(own.PID) {
		t.Fatal("killing the plugin reaped the backend")
	}

	h2 := startRecorderWithState(t, "hang", h.binDir, own)
	h2.open("bar-a", v1.ViewBar, "DP-1")
	h2.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Recording") })
	if err := h2.send(&v1.InputEvent{ViewID: "bar-a", Node: "record", Event: v1.EventActivate, Output: "DP-1"}); err != nil {
		t.Fatal(err)
	}
	h2.waitView("bar-a", func(n *v1.Node) bool {
		return strings.Contains(treeText(n), "Record") && !strings.Contains(treeText(n), "Recording")
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && alive(own.PID) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(own.PID) {
		t.Fatalf("adopted pid %d still running after stop", own.PID)
	}

	dupArgs := own.Args
	p1 := startRawFake(t, h.binDir, dupArgs)
	p2 := startRawFake(t, h.binDir, dupArgs)
	t.Cleanup(func() {
		_ = p1.Process.Kill()
		_ = p2.Process.Kill()
	})
	h3 := startRecorderWithState(t, "hang", h.binDir, own)
	h3.open("bar-a", v1.ViewBar, "DP-1")
	h3.waitView("bar-a", func(n *v1.Node) bool { return strings.Contains(treeText(n), "failed") })
}

func TestPluginRecorderGateMissingDependencyAndHostDisable(t *testing.T) {
	_ = builtRecorder(t)
	origPATH := os.Getenv("PATH")
	root := repoRoot(t)
	pluginDir := filepath.Join(t.TempDir(), "org.sysc.screen-recorder")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "plugins/reference/recorder/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "bin", "sysc-plugin-screen-recorder"), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Bar.Left = nil
	cfg.Bar.Center = []config.Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}
	cfg.Bar.Right = []config.Item{{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "rec-1"}}
	cfg.Plugins.Enabled = []string{"org.sysc.screen-recorder"}
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(shell.PluginHostOptions{
		Roots:    []plugin.Root{{Path: filepath.Dir(pluginDir), Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.NewHost(1, "DP-1"); err != nil {
		t.Fatal(err)
	}
	if got := reg.PluginPID("org.sysc.screen-recorder"); got != 0 {
		t.Fatalf("missing dependency started pid %d", got)
	}

	binDir := t.TempDir()
	installFakeGSR(t, binDir)
	pluginDir2 := filepath.Join(t.TempDir(), "org.sysc.screen-recorder")
	if err := installBuiltRecorder(t, pluginDir2); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPATH)
	cfg2 := config.Default()
	cfg2.Accessibility.ReducedMotion = true
	cfg2.Bar.Left = nil
	cfg2.Bar.Center = []config.Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}
	cfg2.Bar.Right = []config.Item{{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "rec-1"}}
	cfg2.Plugins.Enabled = []string{"org.sysc.screen-recorder"}
	cfg2.Plugins.Settings = map[string]map[string]any{
		"org.sysc.screen-recorder": {"directory": t.TempDir()},
	}
	reg2 := shell.NewRegistry(cfg2)
	t.Cleanup(reg2.Close)
	if err := reg2.BindPlugins(shell.PluginHostOptions{
		Roots:    []plugin.Root{{Path: filepath.Dir(pluginDir2), Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg2.NewHost(1, "DP-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg2.NewHost(2, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	waitViews(t, reg2, "DP-1", 1)
	waitViews(t, reg2, "HDMI-1", 1)
	pid := reg2.PluginPID("org.sysc.screen-recorder")
	if pid == 0 {
		t.Fatal("no plugin process")
	}
	if err := reg2.SetPluginEnabled("org.sysc.screen-recorder", false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reg2.PluginPID("org.sysc.screen-recorder") == 0 && reg2.PluginBarViews("DP-1") == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("disable left pid=%d views=%d", reg2.PluginPID("org.sysc.screen-recorder"), reg2.PluginBarViews("DP-1"))
}

type recorderHost struct {
	t      *testing.T
	dir    string
	binDir string
	cmd    *exec.Cmd
	enc    *v1.Encoder
	mu     sync.Mutex
	roots  map[string]*v1.Node
	notify []v1.NotifyParams
	args   []string
	own    recorder.Ownership
	seed   *recorder.Ownership
	wake   chan struct{}
	done   chan struct{}
}

func startRecorder(t *testing.T, behavior string) *recorderHost {
	t.Helper()
	binDir := t.TempDir()
	installFakeGSR(t, binDir)
	return startRecorderWithState(t, behavior, binDir, recorder.Ownership{})
}

func startRecorderWithState(t *testing.T, behavior, binDir string, seed recorder.Ownership) *recorderHost {
	t.Helper()
	pluginDir := filepath.Join(t.TempDir(), "org.sysc.screen-recorder")
	if err := installBuiltRecorder(t, pluginDir); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.json")
	pidPath := filepath.Join(dir, "pid")
	cmd := exec.Command(filepath.Join(pluginDir, "bin", "sysc-plugin-screen-recorder"))
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SYSC_FAKE_RECORDER=1",
		"SYSC_FAKE_BEHAVIOR="+behavior,
		"SYSC_FAKE_ARGV="+argvPath,
		"SYSC_FAKE_PID="+pidPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("plugin stderr:\n%s", stderr.String())
		}
	})
	seedCopy := seed
	h := &recorderHost{
		t: t, dir: dir, binDir: binDir, cmd: cmd,
		enc: v1.NewEncoder(stdin), roots: map[string]*v1.Node{}, wake: make(chan struct{}, 8), done: make(chan struct{}),
		seed: &seedCopy,
	}
	if seed.PID == 0 {
		h.seed = nil
	}
	dec := v1.NewDecoder(stdout, v1.ToHost)
	go func() {
		_ = cmd.Wait()
		close(h.done)
	}()
	t.Cleanup(func() {
		_ = h.send(&v1.HostShutdown{})
		select {
		case <-h.done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-h.done
		}
		_ = stdin.Close()
	})
	go func() {
		for {
			msg, err := dec.Decode()
			if err != nil {
				return
			}
			switch m := msg.(type) {
			case *v1.HostCall:
				reply := v1.HostReply{ID: m.ID, OK: true}
				switch m.Call {
				case v1.CallNotify:
					var p v1.NotifyParams
					_ = json.Unmarshal(m.Params, &p)
					h.mu.Lock()
					h.notify = append(h.notify, p)
					h.mu.Unlock()
					raw, _ := json.Marshal(v1.NotifyResult{ID: 1})
					reply.Result = raw
				case v1.CallOutputContext:
					raw, _ := json.Marshal(v1.OutputContextResult{Output: "DP-1", Generation: 1})
					reply.Result = raw
				case v1.CallStateGet:
					res := v1.StateGetResult{Found: false}
					if h.seed != nil && h.seed.PID != 0 {
						raw, _ := json.Marshal(*h.seed)
						res = v1.StateGetResult{Found: true, Value: raw}
					}
					raw, _ := json.Marshal(res)
					reply.Result = raw
				case v1.CallStateSet:
					var p v1.StateSetParams
					_ = json.Unmarshal(m.Params, &p)
					if p.Key == "ownership" {
						var own recorder.Ownership
						_ = json.Unmarshal(p.Value, &own)
						h.mu.Lock()
						h.own = own
						h.mu.Unlock()
					}
				}
				h.mu.Lock()
				if raw, err := os.ReadFile(argvPath); err == nil {
					var args []string
					if json.Unmarshal(raw, &args) == nil {
						h.args = args
					}
				}
				h.mu.Unlock()
				_ = h.send(&reply)
				select {
				case h.wake <- struct{}{}:
				default:
				}
			case *v1.ViewSnapshot:
				h.mu.Lock()
				h.roots[m.ViewID] = m.Root
				h.mu.Unlock()
				select {
				case h.wake <- struct{}{}:
				default:
				}
			}
		}
	}()
	if err := h.send(&v1.HostHello{
		Supported:    []v1.Version{{Major: 1, Minor: 0}},
		Plugin:       v1.Identity{ID: "org.sysc.screen-recorder", Name: "Screen Recorder", Version: "1.0.0"},
		Capabilities: []string{"notifications", "settings", "state"},
		Limits:       v1.DefaultLimits,
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *recorderHost) send(m v1.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enc.Encode(m)
}

func (h *recorderHost) open(id string, kind v1.ViewKind, output string) {
	h.t.Helper()
	if err := h.send(&v1.ViewOpen{ViewID: id, View: kind, Entry: "bar", Output: output, Width: 120, Height: 32}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *recorderHost) waitView(id string, ok func(*v1.Node) bool) {
	h.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		root := h.roots[id]
		h.mu.Unlock()
		if root != nil && ok(root) {
			return
		}
		select {
		case <-h.wake:
		case <-time.After(40 * time.Millisecond):
		}
	}
	h.mu.Lock()
	root := h.roots[id]
	h.mu.Unlock()
	h.t.Fatalf("view %s never matched\n%s", id, dumpTree(root))
}

func (h *recorderHost) waitNotify(ok func(v1.NotifyParams) bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		notes := append([]v1.NotifyParams(nil), h.notify...)
		h.mu.Unlock()
		for _, p := range notes {
			if ok(p) {
				return
			}
		}
		select {
		case <-h.wake:
		case <-time.After(40 * time.Millisecond):
		}
	}
	h.t.Fatalf("notification never matched: %+v", h.notify)
}

func (h *recorderHost) waitArgs(ok func([]string) bool) []string {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		args := append([]string(nil), h.args...)
		h.mu.Unlock()
		if ok(args) {
			return args
		}
		select {
		case <-h.wake:
		case <-time.After(40 * time.Millisecond):
		}
	}
	h.t.Fatalf("args never matched: %v", h.args)
	return nil
}

func (h *recorderHost) waitOwn(ok func(recorder.Ownership) bool) recorder.Ownership {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		own := h.own
		h.mu.Unlock()
		if ok(own) {
			return own
		}
		select {
		case <-h.wake:
		case <-time.After(40 * time.Millisecond):
		}
	}
	h.t.Fatalf("ownership never matched: %+v", h.own)
	return recorder.Ownership{}
}

func (h *recorderHost) recorderPID() int {
	raw, err := os.ReadFile(filepath.Join(h.dir, "pid"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return n
}

var (
	recorderBinOnce sync.Once
	recorderBinPath string
	recorderBinErr  error
)

func builtRecorder(t *testing.T) string {
	t.Helper()
	recorderBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sysc-plugin-screen-recorder-")
		if err != nil {
			recorderBinErr = err
			return
		}
		recorderBinPath = filepath.Join(dir, "sysc-plugin-screen-recorder")
		cmd := exec.Command("go", "build", "-o", recorderBinPath, "./cmd/sysc-plugin-screen-recorder")
		cmd.Dir = repoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			recorderBinErr = err
			t.Logf("build recorder: %s", out)
		}
	})
	if recorderBinErr != nil {
		t.Fatal(recorderBinErr)
	}
	return recorderBinPath
}

func installBuiltRecorder(t *testing.T, pluginDir string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		return err
	}
	manifest, err := os.ReadFile(filepath.Join(repoRoot(t), "plugins/reference/recorder/manifest.json"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0o644); err != nil {
		return err
	}
	data, err := os.ReadFile(builtRecorder(t))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginDir, "bin", "sysc-plugin-screen-recorder"), data, 0o755)
}

func installFakeGSR(t *testing.T, binDir string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(binDir, "gpu-screen-recorder")
	if err := os.Symlink(self, dst); err != nil {
		t.Fatal(err)
	}
}

func startRawFake(t *testing.T, binDir string, args []string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(filepath.Join(binDir, "gpu-screen-recorder"), args...)
	cmd.Env = append(os.Environ(), "SYSC_FAKE_RECORDER=1", "SYSC_FAKE_BEHAVIOR=hang")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func hasArg(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasShell(args []string) bool {
	joined := strings.Join(args, " ")
	return strings.Contains(joined, ">>") || strings.Contains(joined, "&&")
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func runGateFakeRecorder() int {
	signal.Reset(syscall.SIGINT)
	if p := os.Getenv("SYSC_FAKE_PID"); p != "" {
		_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	if p := os.Getenv("SYSC_FAKE_ARGV"); p != "" {
		raw, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(p, raw, 0o644)
	}
	switch os.Getenv("SYSC_FAKE_BEHAVIOR") {
	case "crash":
		_, _ = os.Stdout.WriteString("ready\n")
		return 1
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 4096)
		for i := 0; i < 40; i++ {
			_, _ = os.Stderr.Write(chunk)
		}
	case "ignore-int":
		signal.Ignore(syscall.SIGINT)
	}
	if p := argValue("-o"); p != "" {
		body := []byte("mp4")
		if os.Getenv("SYSC_FAKE_BEHAVIOR") == "zero" {
			body = nil
		}
		_ = os.WriteFile(p, body, 0o644)
	}
	if os.Getenv("SYSC_FAKE_BEHAVIOR") == "ignore-int" {
		_, _ = os.Stdout.WriteString("ready\n")
		select {}
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGUSR1)
	_, _ = os.Stdout.WriteString("ready\n")
	for sig := range ch {
		if sig == syscall.SIGUSR1 {
			if dir := argValue("-ro"); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(filepath.Join(dir, "gsr.mp4"), []byte("mp4"), 0o644)
			}
			continue
		}
		return 0
	}
	return 0
}

func argValue(flag string) string {
	args := os.Args[1:]
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
