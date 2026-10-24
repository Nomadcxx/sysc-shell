package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// HelperFlag marks a re-execution of a test binary as a fake plugin.
const HelperFlag = "-sysc-plugin-helper"

// WriteHelperPlugin lays out a plugin directory whose entry point re-runs
// self with HelperFlag and mode. The caller must intercept that flag in
// TestMain and call HelperServe.
func WriteHelperPlugin(dir, self, mode, manifest string) (Manifest, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(manifest), 0o644); err != nil {
		return Manifest{}, err
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %q %s %s\n", self, HelperFlag, mode)
	path := filepath.Join(dir, "bin", "sysc-plugin-timer")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return Manifest{}, err
	}
	return LoadManifest(dir)
}

// HelperServe is the fake plugin used by tests. Each mode is one way a plugin
// can be wrong, so supervision and hosting are proven against a real process.
func HelperServe(args []string) int {
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	// crash-once:<marker> dies on the first view.open and serves normally
	// afterwards: the shape of a respawn a host must reopen its views against.
	crashOnce := ""
	if p, ok := strings.CutPrefix(mode, "crash-once:"); ok {
		crashOnce = p
		mode = "ok"
	}
	// shutdown-marker:<path> serves normally and writes path when asked to
	// stop: proof the host let it finish its work rather than killing it.
	shutdownMarker := ""
	if p, ok := strings.CutPrefix(mode, "shutdown-marker:"); ok {
		shutdownMarker = p
		mode = "ok"
	}
	if mode == "hold-stderr" {
		time.Sleep(time.Minute)
		return 0
	}
	// record-input:<path> appends each input event. A host that drops the
	// last buffered change never produces a line for it.
	recordInput := ""
	if p, ok := strings.CutPrefix(mode, "record-input:"); ok {
		recordInput = p
		mode = "ok"
	}
	// The spawn-child fixtures leave a descendant behind so a test can check
	// what a forced stop does to it. The pid lands in the named file; the mode
	// the plugin serves afterwards is the part before the colon.
	spawnChild := ""
	if p, ok := strings.CutPrefix(mode, "spawn-child:"); ok {
		spawnChild, mode = p, "ok"
	}
	if p, ok := strings.CutPrefix(mode, "spawn-child-slow-shutdown:"); ok {
		spawnChild, mode = p, "slow-shutdown"
	}
	if p, ok := strings.CutPrefix(mode, "spawn-child-silent:"); ok {
		spawnChild, mode = p, "silent"
	}
	// crash-with-child:<path> writes the pid of the descendant it leaves
	// behind, so the test that started this can clean up after itself.
	crashWithChild := ""
	if p, ok := strings.CutPrefix(mode, "crash-with-child:"); ok {
		crashWithChild = p
		mode = "crash-with-child"
	}
	// probe reports the boundary the process started inside, on stderr, then
	// serves normally: its resource ceilings, and the outcome of each
	// "read:<path>", "write:<path>", "connect:<socket>" (a leading @ for an
	// abstract one), "signal:<pid>" or "child-signal:<program>" named in
	// SYSC_PROBE (";"-separated).
	if mode == "probe" {
		probeBoundary(os.Getenv("SYSC_PROBE"))
		mode = "ok"
	}
	out := v1.NewEncoder(os.Stdout)
	in := v1.NewDecoder(os.Stdin, v1.ToPlugin)

	switch mode {
	case "exit-before-hello":
		return 3
	case "garbage":
		fmt.Fprintln(os.Stdout, "this is not a protocol message")
		return 0
	}

	msg, err := in.Decode()
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read host.hello: %v\n", err)
		return 1
	}
	hello, ok := msg.(*v1.HostHello)
	if !ok {
		fmt.Fprintf(os.Stderr, "helper: first message was %T\n", msg)
		return 1
	}

	protocol, _ := v1.NegotiateProtocol(hello.Supported)
	reply := &v1.PluginHello{
		Protocol:     protocol,
		Plugin:       hello.Plugin,
		Capabilities: hello.Capabilities,
	}
	if spawnChild != "" {
		// A descendant in the plugin's own group, the shape of a plugin that
		// shells out to a recorder, a poll loop or a CLI. It writes nothing
		// and reads nothing, so it stays alive until something kills it.
		child := exec.Command(os.Args[0], HelperFlag, "hold-stderr")
		child.Stdin, child.Stdout, child.Stderr = nil, nil, nil
		if err := child.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "helper: start descendant: %v\n", err)
			return 1
		}
		if err := os.WriteFile(spawnChild, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "helper: record descendant: %v\n", err)
			return 1
		}
	}
	switch mode {
	case "silent":
		time.Sleep(time.Minute)
		return 0
	case "wrong-identity":
		reply.Plugin.ID = "org.sysc.impostor"
	case "wrong-major":
		reply.Protocol.Major = 2
	case "extra-capability":
		reply.Capabilities = append(reply.Capabilities, "root")
	case "update-first":
		_ = out.Encode(&v1.ViewSnapshot{ViewID: "v1", Revision: 1,
			Root: &v1.Node{Kind: v1.KindText, Text: "too early"}})
	case "loud-stderr":
		for i := 0; i < 4096; i++ {
			fmt.Fprintf(os.Stderr, "line %06d %s\n", i, strings.Repeat("x", 24))
		}
	}

	if err := out.Encode(reply); err != nil {
		return 1
	}

	switch mode {
	case "malformed-after-hello":
		fmt.Fprintln(os.Stdout, `{"type":"view.snapshot","unexpected":true}`)
	case "crash-after-hello":
		return 4
	case "alloc-after-hello":
		// Reserve more address space than the launcher's ceiling allows.
		// Without the ceiling this succeeds: only one page is touched.
		b := make([]byte, 9<<30)
		b[0] = 1
		fmt.Fprintf(os.Stderr, "helper: allocated %d bytes\n", len(b))
	case "crash-with-child":
		// A descendant that inherits both pipes: the shape of a script entry
		// point that runs `helper &` and then execs the real binary. The
		// plugin itself is gone the moment this returns, but the pipes stay
		// open in the child.
		child := exec.Command(os.Args[0], HelperFlag, "hold-stderr")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			return 1
		}
		if crashWithChild != "" {
			// Append: a runtime that restarts leaves one descendant per
			// attempt, and a test wants to end all of them.
			f, err := os.OpenFile(crashWithChild, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				_, _ = fmt.Fprintf(f, "%d\n", child.Process.Pid)
				_ = f.Close()
			}
		}
		return 4
	case "ignore-shutdown":
		time.Sleep(time.Minute)
		return 0
	case "slow-shutdown":
		msg, err := in.Decode()
		if err != nil {
			return 1
		}
		if _, ok := msg.(*v1.HostShutdown); !ok {
			return 1
		}
		time.Sleep(2 * time.Second)
		return 0
	case "stderr-tail-on-shutdown":
		msg, err := in.Decode()
		if err != nil {
			return 1
		}
		if _, ok := msg.(*v1.HostShutdown); !ok {
			return 1
		}
		fmt.Fprintln(os.Stderr, "stderr prefix")
		_ = out.Encode(&v1.ViewSnapshot{ViewID: "stderr-ready", Revision: 1,
			Root: &v1.Node{Kind: v1.KindText, Text: "stderr ready"}})
		time.Sleep(200 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "stderr final marker")
		return 0
	case "stderr-inherited-child":
		// The descendant is started, and its pid reported, before the
		// shutdown request is read. The host's grace bounds the shutdown, so
		// anything this helper has to do before it answers has to happen
		// before the grace starts; otherwise a loaded machine misses the
		// window and the forced stop ends the group before the snapshot is
		// ever written.
		child := exec.Command(os.Args[0], HelperFlag, "hold-stderr")
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			return 1
		}
		_ = out.Encode(&v1.ViewSnapshot{ViewID: "stderr-child", Revision: 1,
			Root: &v1.Node{Kind: v1.KindText, Text: strconv.Itoa(child.Process.Pid)}})
		msg, err := in.Decode()
		if err != nil {
			return 1
		}
		if _, ok := msg.(*v1.HostShutdown); !ok {
			return 1
		}
		// Stay alive holding the pipe. A plugin that exits here would leave
		// the test racing the grace against the pipe copy; going deaf on
		// purpose guarantees the host has to bound the wait and stop the
		// group itself.
		time.Sleep(time.Minute)
		return 0
	case "flood":
		// More frames than any sane budget allows, written in one burst so
		// the host has to drop them rather than merely fall behind.
		for i := 0; i < 200; i++ {
			_ = out.Encode(&v1.ViewSnapshot{ViewID: "v1", Revision: uint64(i + 1),
				Root: &v1.Node{Kind: v1.KindText, Text: "flood"}})
		}
	}

	if mode == "call-flood" {
		// More calls than the host holds before SetCalls; each reply is
		// echoed back as "<id>:<error>" so a test can see which went unanswered.
		params, _ := json.Marshal(v1.NotifyParams{Summary: "flood"})
		for i := 0; i < maxEarlyCalls+8; i++ {
			_ = out.Encode(&v1.HostCall{ID: fmt.Sprintf("f%d", i), Call: v1.CallNotify, Params: params})
		}
	}

	if mode == "notify-then-snapshot" {
		params, _ := json.Marshal(v1.NotifyParams{Summary: "saved"})
		_ = out.Encode(&v1.HostCall{ID: "n1", Call: v1.CallNotify, Params: params})
		_ = out.Encode(helperSnapshot("late", v1.ViewBar))
	}

	for {
		msg, err := in.Decode()
		if err != nil {
			return 0
		}
		switch m := msg.(type) {
		case *v1.HostShutdown:
			if shutdownMarker != "" {
				_ = os.WriteFile(shutdownMarker, []byte("flushed"), 0o600)
			}
			return 0
		case *v1.ViewOpen:
			if crashOnce != "" {
				if _, err := os.Stat(crashOnce); errors.Is(err, os.ErrNotExist) {
					_ = os.WriteFile(crashOnce, nil, 0o600)
					return 4
				}
			}
			if mode == "bad-view" && m.View == v1.ViewBar {
				_ = out.Encode(&v1.ViewSnapshot{ViewID: m.ViewID, Revision: 1,
					Root: &v1.Node{Kind: v1.KindText, Text: "hello"}})
				break
			}
			if mode == "image-panel" && m.View == v1.ViewPanel {
				_ = out.Encode(&v1.ViewSnapshot{ViewID: m.ViewID, Revision: 1, Root: imagePanelRoot()})
				break
			}
			if mode == "progress-panel" && m.View == v1.ViewPanel {
				_ = out.Encode(&v1.ViewSnapshot{ViewID: m.ViewID, Revision: 1, Root: progressPanelRoot(0.2)})
				go func() {
					time.Sleep(150 * time.Millisecond)
					_ = out.Encode(&v1.ViewSnapshot{ViewID: m.ViewID, Revision: 2, Root: progressPanelRoot(0.8)})
				}()
				break
			}
			_ = out.Encode(helperSnapshot(m.ViewID, m.View))
			if (mode == "call-panel" || mode == "image-panel" || mode == "progress-panel") && m.View == v1.ViewBar {
				params, _ := json.Marshal(v1.PanelParams{Entry: "panel", Output: m.Output, Instance: m.Instance})
				_ = out.Encode(&v1.HostCall{ID: "c1", Call: v1.CallPanelOpen, Params: params})
			}
			if mode == "crash-after-open" {
				return 4
			}
		case *v1.HostReply:
			if mode == "call-flood" {
				_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: m.ID + ":" + m.Error})
				break
			}
			_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: string(m.Result)})
		case *v1.InputEvent:
			if recordInput != "" {
				f, err := os.OpenFile(recordInput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err == nil {
					_, _ = fmt.Fprintf(f, "%s\t%s\n", m.Event, m.Text)
					_ = f.Close()
				}
			}
			_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: m.Node})
			if mode == "panel-on-input" {
				params, _ := json.Marshal(v1.PanelParams{Entry: "panel", Output: m.Output})
				_ = out.Encode(&v1.HostCall{ID: "c1", Call: v1.CallPanelOpen, Params: params})
			}
		}
	}
}

// imagePanelRoot is the image-panel mode's panel: one image node naming the
// file the test planted, addressed through the environment so the separate
// plugin process can reach the test's temp dir.
func imagePanelRoot() *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindImage, Path: os.Getenv("SYSC_HELPER_IMAGE"), ImageSize: 96},
		{Kind: v1.KindText, Text: "hello"},
	}}
}

// progressPanelRoot is the progress-panel mode's panel: one animated meter at
// the given value. The mode publishes two revisions so a test can watch the
// host glide between them.
func progressPanelRoot(value float64) *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindProgress, Key: "battery", Value: value, Animate: true},
		{Kind: v1.KindText, Text: "hello"},
	}}
}

func helperSnapshot(viewID string, view v1.ViewKind) *v1.ViewSnapshot {
	var root *v1.Node
	switch view {
	case v1.ViewBar:
		root = &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{
			{Kind: v1.KindButton, ID: "go", Text: "hello", Name: "Start", Role: "button",
				Events: []v1.EventKind{v1.EventActivate, v1.EventPointer}},
		}}
	case v1.ViewTooltip:
		root = &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "hello"},
		}}
	default:
		root = &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "hello"},
			{Kind: v1.KindButton, ID: "act", Text: "Go", Name: "Go", Role: "button",
				Events: []v1.EventKind{v1.EventActivate}},
			{Kind: v1.KindTextInput, ID: "note", Name: "Note", Role: "textbox",
				Events: []v1.EventKind{v1.EventChange, v1.EventSubmit}},
		}}
	}
	return &v1.ViewSnapshot{ViewID: viewID, Revision: 1, Root: root}
}

func probeBoundary(spec string) {
	for _, r := range []struct {
		name string
		res  int
	}{{"as", syscall.RLIMIT_AS}, {"nofile", syscall.RLIMIT_NOFILE}, {"fsize", syscall.RLIMIT_FSIZE}} {
		var lim syscall.Rlimit
		if err := syscall.Getrlimit(r.res, &lim); err == nil {
			fmt.Fprintf(os.Stderr, "probe rlimit %s %d\n", r.name, lim.Max)
		}
	}
	for _, op := range strings.Split(spec, ";") {
		kind, path, ok := strings.Cut(op, ":")
		if !ok {
			continue
		}
		var err error
		switch kind {
		case "read":
			_, err = os.ReadFile(path)
		case "write":
			err = os.WriteFile(path, []byte("probe"), 0o600)
		case "connect":
			// net reads a leading @ as an abstract socket.
			var conn net.Conn
			if conn, err = net.Dial("unix", path); err == nil {
				conn.Close()
			}
		case "signal":
			var pid int
			if pid, err = strconv.Atoi(path); err == nil {
				err = syscall.Kill(pid, 0)
			}
		case "child-signal":
			// Start the program at path and signal it: a plugin may always
			// stop what it started.
			child := exec.Command(path, "5")
			if err = child.Start(); err == nil {
				err = child.Process.Signal(syscall.SIGTERM)
				_ = child.Wait()
			}
		}
		result := "ok"
		if err != nil {
			result = "denied"
			if !errors.Is(err, fs.ErrPermission) {
				result = "error " + err.Error()
			}
		}
		fmt.Fprintf(os.Stderr, "probe %s %s %s\n", kind, path, result)
	}
}
