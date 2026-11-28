package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// toastClicks records what a pluginToastOwner was handed.
type toastClicks struct {
	mu   sync.Mutex
	seen []v1.NotifyActionEvent
}

func (c *toastClicks) owner(ev v1.NotifyActionEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, ev)
}

func (c *toastClicks) events() []v1.NotifyActionEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]v1.NotifyActionEvent(nil), c.seen...)
}

func bindPluginToasts(t *testing.T) (*Registry, *pluginToastRecorder) {
	t.Helper()
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	keepInvalidationsDrained(t, r)
	rec := &pluginToastRecorder{}
	r.BindNotifications(rec)
	return r, rec
}

// postPluginToast posts one toast for owner and answers it with the service
// notification id, the way the client's reply reaches applyNotify.
func postPluginToast(t *testing.T, r *Registry, owner pluginToastOwner, id uint32) {
	t.Helper()
	res, err := r.PluginNotify(context.Background(), owner, v1.NotifyParams{
		Summary: "Renews soon", Actions: []v1.NotifyAction{{Key: "keep:x", Label: "Keep"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindReply,
		RequestID: uint64(res.ID), Reply: protocol.Reply{OK: true, ID: id}})
}

func actionCommands(cmds []protocol.Command) []protocol.Command {
	var out []protocol.Command
	for _, c := range cmds {
		if c.Kind == protocol.CommandAction {
			out = append(out, c)
		}
	}
	return out
}

func TestPluginNotifyPassesActions(t *testing.T) {
	r, rec := bindPluginToasts(t)
	if _, err := r.PluginNotify(context.Background(), nil, v1.NotifyParams{
		Summary: "Renews soon", Actions: []v1.NotifyAction{{Key: "keep:x", Label: "Keep"}},
	}); err != nil {
		t.Fatal(err)
	}
	got := rec.commands()
	if len(got) != 1 || got[0].Producer == nil {
		t.Fatalf("commands = %+v", got)
	}
	if want := []protocol.Action{{Key: "keep:x", Label: "Keep"}}; fmt.Sprint(got[0].Producer.Actions) != fmt.Sprint(want) {
		t.Fatalf("producer actions = %+v, want %+v", got[0].Producer.Actions, want)
	}
}

func TestPluginActionRoutesToOwner(t *testing.T) {
	r, rec := bindPluginToasts(t)
	var clicks, other toastClicks
	postPluginToast(t, r, clicks.owner, 42)
	postPluginToast(t, r, other.owner, 43)

	r.mu.Lock()
	r.toasts.invoke(42, "keep:x")
	r.mu.Unlock()
	if got := clicks.events(); len(got) != 1 || got[0] != (v1.NotifyActionEvent{ID: 42, Key: "keep:x"}) {
		t.Fatalf("owner got %+v", got)
	}
	if got := other.events(); len(got) != 0 {
		t.Fatalf("another plugin got %+v", got)
	}
	if got := actionCommands(rec.sentCommands()); len(got) != 1 || got[0].ID != 42 || got[0].ActionKey != "keep:x" {
		t.Fatalf("service commands = %+v", got)
	}

	// The drawer's card routes the same way, body click included.
	h := &PanelHost{id: PanelNotifications}
	r.mu.Lock()
	h.activateNotify(r, &ui.Node{Action: "notify:43:action:keep:x"})
	h.activateNotify(r, &ui.Node{Action: "notify:43:default"})
	r.mu.Unlock()
	if got := other.events(); len(got) != 2 || got[0] != (v1.NotifyActionEvent{ID: 43, Key: "keep:x"}) || got[1] != (v1.NotifyActionEvent{ID: 43, Key: "default"}) {
		t.Fatalf("drawer clicks = %+v", got)
	}
	if got := actionCommands(rec.sentCommands()); len(got) != 3 {
		t.Fatalf("service commands = %+v", got)
	}
}

func TestForeignNotificationNeverRoutes(t *testing.T) {
	r, rec := bindPluginToasts(t)
	var clicks toastClicks
	postPluginToast(t, r, clicks.owner, 42)

	r.mu.Lock()
	r.toasts.invoke(7, "keep:x")
	r.mu.Unlock()
	if got := clicks.events(); len(got) != 0 {
		t.Fatalf("a click on notification 7 reached a plugin: %+v", got)
	}
	if got := actionCommands(rec.sentCommands()); len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("service commands = %+v", got)
	}

	// A reply the tracker never asked for claims nothing.
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindReply,
		RequestID: 999, Reply: protocol.Reply{OK: true, ID: 7}})
	r.mu.Lock()
	r.toasts.invoke(7, "keep:x")
	r.mu.Unlock()
	if got := clicks.events(); len(got) != 0 {
		t.Fatalf("an unrequested reply made 7 routable: %+v", got)
	}
}

func TestOwnedMapCleanedOnClose(t *testing.T) {
	r, _ := bindPluginToasts(t)
	var clicks toastClicks
	postPluginToast(t, r, clicks.owner, 42)
	postPluginToast(t, r, clicks.owner, 43)

	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDelta,
		Delta: protocol.Delta{Kind: protocol.DeltaClosed, ID: 42, CloseReason: protocol.CloseDismissed}})
	// A reconnect snapshot without 43 retires it too.
	r.applyNotify(notifyclient.Message{Generation: 2, Kind: notifyclient.KindSnapshot})
	r.mu.Lock()
	r.toasts.invoke(42, "keep:x")
	r.toasts.invoke(43, "keep:x")
	r.mu.Unlock()
	if got := clicks.events(); len(got) != 0 {
		t.Fatalf("closed notifications still routed: %+v", got)
	}

	// The map holds at most maxPluginToasts; the oldest goes first.
	for id := uint32(100); id < 100+maxPluginToasts+1; id++ {
		postPluginToast(t, r, clicks.owner, id)
	}
	r.mu.Lock()
	r.toasts.invoke(100, "keep:x")
	r.toasts.invoke(100+maxPluginToasts, "keep:x")
	r.mu.Unlock()
	if got := clicks.events(); len(got) != 1 || got[0].ID != 100+maxPluginToasts {
		t.Fatalf("after the cap, clicks = %+v", got)
	}
}

// bindToastTestPlugin starts a shell-script plugin at minor that appends every
// notify.action line it reads to out.
func bindToastTestPlugin(t *testing.T, minor int, out string) *Registry {
	t.Helper()
	const id = "org.sysc.toaster"
	root := t.TempDir()
	dir := filepath.Join(root, id)
	bin := filepath.Join(dir, "bin", "sysc-plugin-toaster")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	program := fmt.Sprintf(`#!/bin/sh
IFS= read -r hello || exit 2
printf '%%s\n' '{"type":"plugin.hello","protocol":{"major":1,"minor":%d},"plugin":{"id":"%s","name":"Toaster","version":"1.0.0"},"capabilities":["notifications"]}'
while IFS= read -r line; do
  case "$line" in
    *'"type":"notify.action"'*) printf '%%s\n' "$line" >> '%s' ;;
    *'"type":"host.shutdown"'*) exit 0 ;;
  esac
done
`, minor, id, out)
	if err := os.WriteFile(bin, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"schema": 1, "id": "%s", "name": "Toaster", "version": "1.0.0",
  "protocol": {"major": 1, "minor": %d}, "exec": "bin/sysc-plugin-toaster", "capabilities": ["notifications"]}`, id, minor)
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Plugins.Enabled = []string{id}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	keepInvalidationsDrained(t, reg)
	if err := reg.BindPlugins(PluginHostOptions{Roots: []plugin.Root{{Path: root, Source: plugin.SourceUser}}, StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	waitPluginState(t, reg.plugins, id, plugin.StateRunning)
	return reg
}

func readToastLog(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestPluginToastOwnerDeliversToSession(t *testing.T) {
	out := filepath.Join(t.TempDir(), "actions")
	reg := bindToastTestPlugin(t, v1.NotifyActionMinor, out)
	slot := pluginSlotForTest(t, reg.plugins, "org.sysc.toaster")
	owner := reg.plugins.toastOwner("org.sysc.toaster", slot.rt)
	owner(v1.NotifyActionEvent{ID: 42, Key: "keep:x"})
	waitFor(t, func() bool { return strings.Contains(readToastLog(out), `"key":"keep:x"`) })
}

func TestOwnerBelowMinor29GetsNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "actions")
	reg := bindToastTestPlugin(t, v1.NotifyActionMinor-1, out)
	slot := pluginSlotForTest(t, reg.plugins, "org.sysc.toaster")
	owner := reg.plugins.toastOwner("org.sysc.toaster", slot.rt)
	owner(v1.NotifyActionEvent{ID: 42, Key: "keep:x"})
	time.Sleep(100 * time.Millisecond)
	if got := readToastLog(out); got != "" {
		t.Fatalf("a minor-28 plugin was sent %q", got)
	}
}

func TestStoppedPluginDropsSilently(t *testing.T) {
	out := filepath.Join(t.TempDir(), "actions")
	reg := bindToastTestPlugin(t, v1.NotifyActionMinor, out)
	slot := pluginSlotForTest(t, reg.plugins, "org.sysc.toaster")
	owner := reg.plugins.toastOwner("org.sysc.toaster", slot.rt)
	reg.plugins.stopPlugin("org.sysc.toaster")
	owner(v1.NotifyActionEvent{ID: 42, Key: "keep:x"})
	time.Sleep(100 * time.Millisecond)
	if got := readToastLog(out); got != "" {
		t.Fatalf("a stopped plugin was sent %q", got)
	}
}

// A click on a toast an earlier session posted does not reach the session
// that replaced it in the same runtime.
func TestRestartedSessionDropsClick(t *testing.T) {
	out := filepath.Join(t.TempDir(), "actions")
	reg := bindToastTestPlugin(t, v1.NotifyActionMinor, out)
	slot := pluginSlotForTest(t, reg.plugins, "org.sysc.toaster")
	owner := reg.plugins.toastOwner("org.sysc.toaster", slot.rt)
	reg.plugins.mu.Lock()
	slot.session++
	reg.plugins.mu.Unlock()
	owner(v1.NotifyActionEvent{ID: 42, Key: "keep:x"})
	time.Sleep(100 * time.Millisecond)
	if got := readToastLog(out); got != "" {
		t.Fatalf("a later session was sent %q", got)
	}
}
