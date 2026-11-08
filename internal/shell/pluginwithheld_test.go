package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

// A plugin that changed since it was approved has its calls refused, and the
// cat discards the refusal: its click did nothing. The shell toasts when such
// a refusal follows the user's own input to that plugin, once per copy.
func TestPluginWithheldToastFollowsAClick(t *testing.T) {
	t.Parallel()
	var got []string
	h := &pluginHost{opts: PluginHostOptions{WithheldToast: func(id, name string, c plugin.Capability) {
		got = append(got, id+"/"+name+"/"+string(c))
	}}}
	click := func(id string) {
		h.mu.Lock()
		h.markInputLocked(id)
		h.mu.Unlock()
	}
	h.withheldCall("org.sysc.cat", "Cat", "d1", plugin.CapPanels)
	if len(got) != 0 {
		t.Fatalf("toasted with no click: %v", got)
	}
	click("org.sysc.cat")
	h.withheldCall("org.sysc.cat", "Cat", "d1", plugin.CapPanels)
	if len(got) != 1 || got[0] != "org.sysc.cat/Cat/panels" {
		t.Fatalf("after a click: %v, want one panels toast", got)
	}
	click("org.sysc.cat")
	h.withheldCall("org.sysc.cat", "Cat", "d1", plugin.CapPanels)
	if len(got) != 1 {
		t.Fatalf("a second click on the same copy toasted again: %v", got)
	}
	h.withheldCall("org.sysc.timer", "Timer", "t1", plugin.CapPanels)
	if len(got) != 1 {
		t.Fatalf("another plugin with no click toasted: %v", got)
	}
	click("org.sysc.cat")
	h.withheldCall("org.sysc.cat", "Cat", "d2", plugin.CapPanels)
	if len(got) != 2 {
		t.Fatalf("a newly changed copy did not toast: %v", got)
	}
	h.mu.Lock()
	h.lastInput["org.sysc.cat"] = time.Now().Add(-2 * withheldClickWindow)
	h.mu.Unlock()
	h.withheldCall("org.sysc.cat", "Cat", "d3", plugin.CapPanels)
	if len(got) != 2 {
		t.Fatalf("a refusal long after the click toasted: %v", got)
	}
}

func TestPluginWithheldToastNamesTheRefusedAction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c    plugin.Capability
		body string
	}{
		{plugin.CapPanels, "Approve it again in Settings > Plugins to let it open its panel."},
		{plugin.CapFloatingSurfaces, "Approve it again in Settings > Plugins to let it open its panel."},
		{plugin.CapNotifications, "Approve it again in Settings > Plugins to let it post notifications."},
		{plugin.CapClipboardRead, "Approve it again in Settings > Plugins to let it use the clipboard."},
		{plugin.CapClipboardWrite, "Approve it again in Settings > Plugins to let it use the clipboard."},
		{plugin.CapOpenURL, "Approve it again in Settings > Plugins to let it open links."},
		{plugin.CapFiles, "Approve it again in Settings > Plugins to let it open files."},
		{plugin.CapLocation, "Approve it again in Settings > Plugins to let it read your location."},
		{plugin.CapScreenshotCapture, "Approve it again in Settings > Plugins to let it take screenshots."},
		{plugin.CapScreenshotRead, "Approve it again in Settings > Plugins to let it take screenshots."},
		{plugin.CapStateRead, "Approve it again in Settings > Plugins to turn its permissions back on."},
	} {
		r := NewRegistry(config.Default())
		rec := &pluginToastRecorder{}
		r.producerSender = rec
		r.PluginWithheldToast("org.sysc.cat", "Cat", tc.c)
		cmds := rec.commands()
		if len(cmds) != 1 || cmds[0].Kind != protocol.CommandProducerPublish || cmds[0].Producer == nil {
			t.Fatalf("%s: commands = %+v, want one producer publish", tc.c, cmds)
		}
		p := cmds[0].Producer
		if p.Key != "sysc-shell:plugin-withheld:org.sysc.cat" || p.Summary != "Cat changed since you approved it" || p.Body != tc.body {
			t.Errorf("%s: toast = key %q summary %q body %q, want body %q", tc.c, p.Key, p.Summary, p.Body, tc.body)
		}
	}
}

func TestPluginWithheldToastIsLocalized(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Session.Language = "es"
	r := NewRegistry(cfg)
	rec := &pluginToastRecorder{}
	r.producerSender = rec
	r.PluginWithheldToast("org.sysc.cat", "Gato", plugin.CapPanels)
	cmds := rec.commands()
	if len(cmds) != 1 {
		t.Fatalf("commands = %d", len(cmds))
	}
	if p := cmds[0].Producer; p.Summary == "Gato changed since you approved it" || p.Body == "Approve it again in Settings > Plugins to let it open its panel." {
		t.Fatalf("Spanish session got English: %q / %q", p.Summary, p.Body)
	}
}
