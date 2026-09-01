package integration

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestPluginUpdateGateDropsAFlood(t *testing.T) {
	now := time.Unix(0, 0)
	in := plugin.NewInbound(func() time.Time { return now }, v1.DefaultLimits)
	for i := 0; i < v1.DefaultLimits.UpdateBurst; i++ {
		if !in.Allow() {
			t.Fatalf("burst %d refused", i)
		}
	}
	raw := []byte(`{"type":"view.snapshot","view_id":"v1","revision":1}`)
	if _, ok := in.Accept(raw); ok {
		t.Fatal("flood was decoded")
	}
	for i := 0; i < 3; i++ {
		in.Allow()
	}
	if !in.Degraded() {
		t.Fatal("flood did not degrade")
	}
}

func TestPluginUpdateGatePatchLossAsksOnce(t *testing.T) {
	vt := &plugin.ViewTree{View: v1.ViewPanel}
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindText, Key: "time", Text: "00:00", Tabular: true},
	}}
	if err := vt.ApplySnapshot(1, root); err != nil {
		t.Fatal(err)
	}
	first, err := vt.ApplyPatch(&v1.ViewPatch{Base: 0, Revision: 2, Replacements: []v1.Replacement{
		{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "x"}},
	}})
	if err == nil || !first {
		t.Fatalf("first: err=%v resync=%v", err, first)
	}
	second, _ := vt.ApplyPatch(&v1.ViewPatch{Base: 0, Revision: 3, Replacements: []v1.Replacement{
		{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "y"}},
	}})
	if second {
		t.Fatal("lost patches requested a second resync")
	}
}

func TestPluginUpdateGateDepthAndNodeLimits(t *testing.T) {
	ok := chain(v1.MaxDepth)
	if err := v1.Validate(ok, v1.ViewPanel); err != nil {
		t.Fatalf("depth %d rejected: %v", v1.MaxDepth, err)
	}
	if err := v1.Validate(chain(v1.MaxDepth+1), v1.ViewPanel); err == nil {
		t.Fatal("depth 17 accepted")
	}
	if err := v1.Validate(sized(v1.MaxNodes), v1.ViewPanel); err != nil {
		t.Fatalf("1024 nodes rejected: %v", err)
	}
	if err := v1.Validate(sized(v1.MaxNodes+1), v1.ViewPanel); err == nil {
		t.Fatal("1025 nodes accepted")
	}
}

func TestPluginUpdateGateTwoOutputsShareOneProcess(t *testing.T) {
	reg := bindGate(t, "ok", true)
	if _, err := reg.NewHost(1, "DP-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.NewHost(2, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	waitViews(t, reg, "DP-1", 1)
	waitViews(t, reg, "HDMI-1", 1)
	if pid := reg.PluginPID("org.sysc.timer"); pid == 0 {
		t.Fatal("no plugin process")
	}
}

func chain(depth int) *v1.Node {
	root := &v1.Node{Kind: v1.KindColumn}
	n := root
	for i := 1; i < depth; i++ {
		child := &v1.Node{Kind: v1.KindColumn}
		n.Children = []*v1.Node{child}
		n = child
	}
	n.Kind = v1.KindText
	n.Text = "leaf"
	return root
}

func sized(count int) *v1.Node {
	root := &v1.Node{Kind: v1.KindColumn}
	remaining := count - 1
	for remaining > 0 {
		group := &v1.Node{Kind: v1.KindColumn}
		root.Children = append(root.Children, group)
		remaining--
		for n := 0; n < v1.MaxChildren && remaining > 0; n++ {
			group.Children = append(group.Children, &v1.Node{Kind: v1.KindText, Text: "x"})
			remaining--
		}
	}
	return root
}
