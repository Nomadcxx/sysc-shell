package displays

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// A stateful stand-in for niri's IPC socket. It exists so the preview, the
// recovery and the service can be exercised end to end: the shell sends real
// requests, the fake really mutates its outputs, and a read back disagrees when
// a command was refused. Scripting replies would not prove anything about the
// apply and restore paths, which are exactly what must not be wrong.
// fataler is the one thing the fake needs from a test, so the same fake can run
// inside the test binary's own helper modes where there is no *testing.T.
type fataler interface {
	Fatalf(format string, args ...any)
}

type fakeNiri struct {
	t    fataler
	mu   sync.Mutex
	log  string
	outs map[string]*fakeOut
	// requests counts every request the fake has seen, so a test can assert
	// the shell actually reloaded the configuration it claimed to keep.
	reloads   int
	queryErr  bool
	reloadErr bool
	failAfter int
	applied   int
}

type fakeOut struct {
	name    string
	modes   []niriMode
	current int
	scale   float64
	tx      string
	x, y    int32
	vrr     bool
	// vrrOnDemand records what was asked for, so a read back matches what
	// niri would report: an on-demand request that is not refreshing is off.
	vrrOnDemand bool
	// vrrSupported is false by default, as on the laptop panel: niri then
	// accepts a VRR request and reports it off.
	vrrSupported bool
}

type niriMode struct {
	w, h uint16
	hz   uint32
}

func (f *fakeNiri) outputsJSON() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	outs := map[string]any{}
	for name, o := range f.outs {
		modes := make([]any, 0, len(o.modes))
		for i, m := range o.modes {
			modes = append(modes, map[string]any{
				"width": m.w, "height": m.h, "refresh_rate": m.hz, "is_preferred": i == 0,
			})
		}
		outs[name] = map[string]any{
			"name": o.name, "make": "Test", "model": "Screen", "serial": "0",
			"modes": modes, "current_mode": o.current, "is_custom_mode": false,
			"vrr_supported": o.vrrSupported, "vrr_enabled": o.vrr && o.vrrSupported,
			"logical": map[string]any{
				"x": o.x, "y": o.y, "width": o.modes[o.current].w, "height": o.modes[o.current].h,
				"scale": o.scale, "transform": o.tx,
			},
		}
	}
	body, err := json.Marshal(map[string]any{"Ok": map[string]any{"Outputs": outs}})
	if err != nil {
		f.t.Fatalf("marshal outputs: %v", err)
	}
	return body
}

// apply mutates one output and refuses nothing, because the refusal paths are
// the ones the decision table handles, not the ones a happy-path fake can
// reproduce honestly.
func (f *fakeNiri) apply(conn net.Conn, output string, action json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAfter > 0 && f.applied >= f.failAfter {
		f.queryErr = true
		reply(conn, []byte(`{"Err":"injected output failure"}`))
		return
	}
	o, ok := f.outs[output]
	if !ok {
		f.say("missing %s", output)
		reply(conn, []byte(`{"Ok":{"OutputConfigChanged":"OutputWasMissing"}}`))
		return
	}
	applyAction(o, action)
	f.applied++
	f.say("%s %s", output, compact(action))
	reply(conn, []byte(`{"Ok":{"OutputConfigChanged":"Applied"}}`))
}

func applyAction(o *fakeOut, action json.RawMessage) {
	// Every OutputAction variant is externally tagged, and the ones that matter
	// here are all nested one level: {"Scale":{"scale":{...}}}.
	for _, kind := range []string{"Mode", "CustomMode", "Scale", "Transform", "Position", "Vrr", "On", "Off"} {
		raw, ok := cut(object(action), kind)
		if !ok {
			continue
		}
		switch kind {
		case "On":
			o.current = 0
		case "Off":
			o.current = 0
		case "Mode":
			if w, h, hz, specific := specificMode(object(raw)); specific {
				for i, m := range o.modes {
					if m.w == w && m.h == h && m.hz == hz {
						o.current = i
					}
				}
			} else {
				o.current = 0
			}
		case "Scale":
			if f, specific := specificScale(object(raw)); specific {
				o.scale = f
			} else {
				o.scale = 1
			}
		case "Transform":
			_ = json.Unmarshal(object(raw)["transform"], &o.tx)
		case "Position":
			if x, y, specific := specificPosition(object(raw)); specific {
				o.x, o.y = x, y
			}
		case "Vrr":
			enabled, onDemand := vrr(object(raw))
			o.vrr, o.vrrOnDemand = enabled, onDemand
		}
		return
	}
}

// logScales records one "query <name>=<scale>" line per output.
func (f *fakeNiri) logScales() {
	f.mu.Lock()
	names := make([]string, 0, len(f.outs))
	for name := range f.outs {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%v", name, f.outs[name].scale))
	}
	f.mu.Unlock()
	if f.log == "" {
		return
	}
	f.say("query %s", strings.Join(parts, " "))
}

// wireScales reads the scale each output had when the compositor last answered
// a query, which is the only state an out-of-process test can honestly see.
func wireScales(t *testing.T, log string) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, line := range strings.Split(readFile(t, log), "\n") {
		rest, ok := strings.CutPrefix(line, "query ")
		if !ok {
			continue
		}
		for _, field := range strings.Fields(rest) {
			name, value, ok := strings.Cut(field, "=")
			if ok {
				got[name] = value
			}
		}
	}
	return got
}

func (f *fakeNiri) say(format string, args ...any) {
	if f.log == "" {
		return
	}
	fh, err := os.OpenFile(f.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer fh.Close()
	fmt.Fprintf(fh, format+"\n", args...)
}

func (f *fakeNiri) handle(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == `"Outputs"`:
			f.mu.Lock()
			failed := f.queryErr
			f.mu.Unlock()
			if failed {
				reply(conn, []byte(`{"Err":"injected query failure"}`))
				continue
			}
			// Every query records the state it answered, so a test in another
			// process can observe the fake's real state through the wire.
			f.logScales()
			reply(conn, f.outputsJSON())
		case strings.HasPrefix(line, `{"Output":`):
			var req struct {
				Output struct {
					Output string          `json:"output"`
					Action json.RawMessage `json:"action"`
				} `json:"Output"`
			}
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				reply(conn, []byte(`{"Err":"bad request"}`))
				continue
			}
			f.apply(conn, req.Output.Output, req.Output.Action)
		default:
			f.mu.Lock()
			if strings.Contains(line, "LoadConfigFile") {
				if f.reloadErr {
					f.mu.Unlock()
					reply(conn, []byte(`{"Err":"injected reload failure"}`))
					continue
				}
				f.reloads++
				f.say("reload %s", line)
			}
			f.mu.Unlock()
			reply(conn, []byte(`{"Ok":"Handled"}`))
		}
	}
}

func reply(conn net.Conn, body []byte) { conn.Write(append(body, '\n')) }

func (f *fakeNiri) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

// newFakeNiri builds one output with a single advertised mode, which is the
// smallest thing a preview can change and verify.
func newFakeNiri(t fataler, log string) *fakeNiri {
	return &fakeNiri{t: t, log: log, outs: map[string]*fakeOut{
		"DP-1": {name: "DP-1", modes: []niriMode{{1920, 1080, 60030}}, current: 0, scale: 1, tx: "Normal"},
	}}
}

func (f *fakeNiri) start(t *testing.T) string {
	t.Helper()
	return f.startAt(t, shortSocket(t, "niri"))
}

func (f *fakeNiri) startAt(t *testing.T, path string) string {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on %s: %v", path, err)
	}
	t.Cleanup(func() { ln.Close() })
	go f.serve(ln)
	return path
}

// startProcess runs the same fake in its own process, so it outlives the shell
// the test is about to kill.
func (f *fakeNiri) startProcess(t *testing.T, dir string) *os.Process {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		testModeEnv+"="+fakeNiriMode,
		fakeSocketEnv+"="+filepath.Join(dir, "niri.sock"),
		fakeLogEnv+"="+filepath.Join(dir, "niri.log"),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake niri: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	socket := filepath.Join(dir, "niri.sock")
	waitFor(t, "the fake niri socket", 10*time.Second, func() bool {
		_, err := os.Stat(socket)
		return err == nil
	})
	return cmd.Process
}

// scale reports what the fake currently believes, for a test's assertions.
func (f *fakeNiri) scale(name string) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outs[name].scale
}

func (f *fakeNiri) reloadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloads
}

func object(raw json.RawMessage) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	return m
}

func cut(m map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	v, ok := m[key]
	return v, ok
}

func compact(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// specificMode decodes {"Mode":{"mode":{"Specific":{"width":..,"height":..,"refresh":60.03}}}}
// or {"Mode":{"mode":"Automatic"}}, reporting whether a concrete mode was named.
func specificMode(m map[string]json.RawMessage) (w, h uint16, hz uint32, ok bool) {
	raw, present := m["mode"]
	if !present {
		return 0, 0, 0, false
	}
	inner := object(raw)
	fields, ok := cut(inner, "Specific")
	if !ok {
		return 0, 0, 0, false
	}
	cm := object(fields)
	w = uint16(number(cm, "width"))
	h = uint16(number(cm, "height"))
	// niri takes refresh in hertz and reports millihertz, so the comparison has
	// to round rather than truncate: 60.03 hertz is 60030 millihertz.
	hz = uint32(math.Round(number(cm, "refresh") * 1000))
	return w, h, hz, true
}

func specificScale(m map[string]json.RawMessage) (float64, bool) {
	raw, ok := m["scale"]
	if !ok {
		return 0, false
	}
	// The value under Specific is a bare number, not an object.
	fields, ok := cut(object(raw), "Specific")
	if !ok {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(fields, &f); err != nil {
		return 0, false
	}
	return f, true
}

func specificString(m map[string]json.RawMessage) (string, bool) {
	for _, key := range []string{"transform", "mode", "value"} {
		raw, ok := m[key]
		if !ok {
			continue
		}
		if s, ok := cut(object(raw), "Specific"); ok {
			var out string
			_ = json.Unmarshal(s, &out)
			return out, true
		}
	}
	return "", false
}

func specificPosition(m map[string]json.RawMessage) (x, y int32, ok bool) {
	raw, ok := m["position"]
	if !ok {
		return 0, 0, false
	}
	fields, ok := cut(object(raw), "Specific")
	if !ok {
		return 0, 0, false
	}
	cp := object(fields)
	return int32(number(cp, "x")), int32(number(cp, "y")), true
}

func vrr(m map[string]json.RawMessage) (enabled, onDemand bool) {
	raw, ok := m["vrr"]
	if !ok {
		return false, false
	}
	v := object(raw)
	return boolOf(v, "vrr"), boolOf(v, "on_demand")
}

func number(m map[string]json.RawMessage, key string) float64 {
	if key == "" {
		for _, v := range m {
			var f float64
			_ = json.Unmarshal(v, &f)
			return f
		}
		return 0
	}
	var f float64
	_ = json.Unmarshal(m[key], &f)
	return f
}

func boolOf(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

func shortSocket(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "d")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func waitFor(t *testing.T, what string, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", limit, what)
}
