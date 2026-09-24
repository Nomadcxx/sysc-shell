package shell

import (
	"context"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Exercise the real input handler, which owns Registry.mu during activation.
func TestPluginFailureButtonsReturn(t *testing.T) {
	for _, action := range []string{"plugin-close", "plugin-disable", "plugin-retry"} {
		t.Run(action, func(t *testing.T) {
			reg := NewRegistry(config.Default())
			reg.plugins = &pluginHost{r: reg, ctx: context.Background()}
			host := &PanelHost{id: PanelPlugin, root: pluginPanelError("failed", true)}
			host.focus = ui.Focusables(host.root)
			for i, n := range host.focus {
				if n.Action == action {
					host.roving.Count = 3
					host.roving.Set(i)
				}
			}
			done := make(chan struct{})
			go func() { host.handle(reg)(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEnter}); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("failure button deadlocked the shell input handler")
			}
		})
	}
}

func TestPluginRetryRecreatesViews(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	before := reg.plugins.barViewIDs("DP-1")[0]
	if err := reg.plugins.retry("org.sysc.timer"); err != nil {
		t.Fatal(err)
	}
	after := reg.plugins.barViewIDs("DP-1")
	if len(after) != 1 || after[0] == before {
		t.Fatalf("retry kept stale process view: %v", after)
	}
	waitPluginText(t, reg.bars[1], "hello")
}

func TestPluginRetryInputDoesNotWaitForProcess(t *testing.T) {
	reg := bindTestPlugin(t, "ignore-shutdown")
	reg.plugins.panel = &hostedView{Plugin: "org.sysc.timer"}
	host := &PanelHost{id: PanelPlugin, root: pluginPanelError("failed", true)}
	host.focus = ui.Focusables(host.root)
	host.roving.Count = len(host.focus)
	host.roving.Set(1)
	done := make(chan struct{})
	go func() { host.handle(reg)(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEnter}); close(done) }()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		<-done
		t.Fatal("Retry blocks desktop input while waiting for plugin shutdown")
	}
}
