package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	case "crash-after-hello":
		return 4
	case "ignore-shutdown":
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
			if mode == "bad-view" && m.View == v1.ViewBar {
				_ = out.Encode(&v1.ViewSnapshot{ViewID: m.ViewID, Revision: 1,
					Root: &v1.Node{Kind: v1.KindText, Text: "hello"}})
				break
			}
			_ = out.Encode(helperSnapshot(m.ViewID, m.View))
			if mode == "call-panel" && m.View == v1.ViewBar {
				params, _ := json.Marshal(v1.PanelParams{Entry: "panel", Output: m.Output, Instance: m.Instance})
				_ = out.Encode(&v1.HostCall{ID: "c1", Call: v1.CallPanelOpen, Params: params})
			}
			if mode == "crash-after-open" {
				return 4
			}
		case *v1.HostReply:
			_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: string(m.Result)})
		case *v1.InputEvent:
			_ = out.Encode(&v1.PluginStatus{State: v1.StatusOK, Message: m.Node})
		}
	}
}

func helperSnapshot(viewID string, view v1.ViewKind) *v1.ViewSnapshot {
	var root *v1.Node
	switch view {
	case v1.ViewBar:
		root = &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{
			{Kind: v1.KindButton, ID: "go", Text: "hello", Name: "Start", Role: "button",
				Events: []v1.EventKind{v1.EventActivate, v1.EventPointer}},
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
