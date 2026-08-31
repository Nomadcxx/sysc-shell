package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// helperFlag marks a re-execution of this test binary as a fake plugin rather
// than a test run.
const helperFlag = "-sysc-plugin-helper"

// TestMain intercepts the helper re-execution before the testing package parses
// flags, so a fake plugin is this same binary behaving differently rather than
// a second program to build and ship.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == helperFlag {
		os.Exit(helperMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// installHelper writes a plugin directory whose entry point runs this test
// binary in the given helper mode.
//
// The entry point is a short script inside the plugin directory: a real
// regular executable that satisfies the manifest's containment rule without
// copying tens of megabytes of test binary for every case. The host still
// starts it directly; the kernel, not a command shell the host chose, follows
// the interpreter line.
func installHelper(t *testing.T, mode string) Manifest {
	t.Helper()
	return installHelperWith(t, mode, timerManifest)
}

func installHelperWith(t *testing.T, mode, manifest string) Manifest {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %q %s %s\n", self, helperFlag, mode)
	path := filepath.Join(dir, "bin", "sysc-plugin-timer")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest for helper %q: %v", mode, err)
	}
	return m
}

// helperMain is the fake plugin. Each mode is one way a plugin can be wrong,
// so the supervisor's handling of it is proven against a real process, real
// pipes, and a real exit rather than a stub.
func helperMain(args []string) int {
	mode := ""
	if len(args) > 0 {
		mode = args[0]
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

	reply := &v1.PluginHello{
		Protocol:     v1.Version{Major: 1, Minor: 0},
		Plugin:       hello.Plugin,
		Capabilities: hello.Capabilities,
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
		// A view before the handshake is a protocol violation, not an early
		// optimisation.
		_ = out.Encode(&v1.ViewSnapshot{ViewID: "v1", Revision: 1,
			Root: &v1.Node{Kind: v1.KindText, Text: "too early"}})
	case "loud-stderr":
		// Twice the retained tail, in identifiable lines, so a test can prove
		// which end was kept.
		for i := 0; i < 4096; i++ {
			fmt.Fprintf(os.Stderr, "line %06d %s\n", i, strings.Repeat("x", 24))
		}
	}

	if err := out.Encode(reply); err != nil {
		return 1
	}

	switch mode {
	case "crash-after-hello":
		return 4
	case "ignore-shutdown":
		// Survive both host.shutdown and the closing of standard input, so the
		// supervisor has to fall back to killing the process.
		time.Sleep(time.Minute)
		return 0
	}

	for {
		msg, err := in.Decode()
		if err != nil {
			return 0
		}
		switch m := msg.(type) {
		case *v1.HostShutdown:
			return 0
		case *v1.ViewOpen:
			_ = out.Encode(&v1.ViewSnapshot{ViewID: m.ViewID, Revision: 1,
				Root: &v1.Node{Kind: v1.KindText, Text: "hello"}})
		case *v1.HostReply:
			_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: string(m.Result)})
		case *v1.InputEvent:
			_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: m.Node})
		}
	}
}

// jsonOf is a small helper for building raw payloads in tests.
func jsonOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// errDiscovery stands in for a rejection produced by the scan.
var errDiscovery = errTest("manifest.json is malformed")

type errTest string

func (e errTest) Error() string { return string(e) }
