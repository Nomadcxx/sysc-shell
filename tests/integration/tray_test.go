// Package integration drives the shell through its exported wiring seams with
// a fake tray service and a fake compositor. Nothing here talks to Wayland or
// to sysc-tray: the service is a recorder behind the command sender, and the
// compositor is this file calling the host callbacks the registry hands back.
package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

const (
	barWidth   = 1920
	barSurface = 44
	scaleUnit  = 120
	// Evdev button codes, as wl_pointer reports them.
	buttonLeft   = 0x110
	buttonRight  = 0x111
	buttonMiddle = 0x112
)

// fakeService records the commands the shell sends and can fail on demand.
// Request IDs count from one, matching the client's own correlation.
type fakeService struct {
	mu     sync.Mutex
	sent   []tray.Command
	ids    []uint64
	failOn map[tray.CommandKind]error
	next   uint64
}

func newFakeService() *fakeService {
	return &fakeService{failOn: map[tray.CommandKind]error{}}
}

func (f *fakeService) Send(command tray.Command) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failOn[command.Kind]; err != nil {
		return 0, err
	}
	f.next++
	f.sent = append(f.sent, command)
	f.ids = append(f.ids, f.next)
	return f.next, nil
}

func (f *fakeService) commands() []tray.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tray.Command(nil), f.sent...)
}

func (f *fakeService) lastID() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ids) == 0 {
		return 0
	}
	return f.ids[len(f.ids)-1]
}

func (f *fakeService) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent, f.ids = nil, nil
}

// kinds reports the command kinds seen, in order, for readable assertions.
func (f *fakeService) kinds() []tray.CommandKind {
	commands := f.commands()
	out := make([]tray.CommandKind, 0, len(commands))
	for _, command := range commands {
		out = append(out, command.Kind)
	}
	return out
}

func (f *fakeService) count(kind tray.CommandKind) int {
	n := 0
	for _, seen := range f.kinds() {
		if seen == kind {
			n++
		}
	}
	return n
}

// fakeCompositor drains the two channels the Wayland owner drains and keeps
// the surface set the compositor would hold. Without it the registry blocks
// on a full channel exactly as it would against a wedged owner.
type fakeCompositor struct {
	mu    sync.Mutex
	open  map[string]uint32           // surface ID to output global
	specs map[string]*wayland.AuxSpec // the spec each open surface was created from
	opens []string
	stop  chan struct{}
	done  chan struct{}
}

func newFakeCompositor(t *testing.T, reg *shell.Registry) *fakeCompositor {
	t.Helper()
	c := &fakeCompositor{
		open: map[string]uint32{}, specs: map[string]*wayland.AuxSpec{},
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go func() {
		defer close(c.done)
		for {
			select {
			case <-c.stop:
				return
			case <-reg.Invalidations():
			case <-reg.Tooltips():
			case request := <-reg.AuxRequests():
				c.apply(request)
			}
		}
	}()
	t.Cleanup(func() { close(c.stop); <-c.done })
	return c
}

func (c *fakeCompositor) apply(request wayland.AuxRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case request.Open != nil:
		c.open[request.Open.ID] = request.Output
		c.specs[request.Open.ID] = request.Open
		c.opens = append(c.opens, request.Open.ID)
	case request.Update != nil:
	default:
		delete(c.open, request.ID)
		delete(c.specs, request.ID)
	}
}

// outputOf reports the output a surface is open on. Polling absorbs the hop
// through the aux channel without a fixed sleep.
func (c *fakeCompositor) outputOf(surfaceID string) (uint32, bool) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		output, ok := c.open[surfaceID]
		c.mu.Unlock()
		if ok || time.Now().After(deadline) {
			return output, ok
		}
		time.Sleep(time.Millisecond)
	}
}

func (c *fakeCompositor) gone(surfaceID string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		_, ok := c.open[surfaceID]
		c.mu.Unlock()
		if !ok || time.Now().After(deadline) {
			return !ok
		}
		time.Sleep(time.Millisecond)
	}
}

// callbacksFor is what a compositor holds after mapping a surface: the hooks
// it drives for configure, paint, and input.
func (c *fakeCompositor) callbacksFor(surfaceID string) (wayland.HostCallbacks, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	spec, ok := c.specs[surfaceID]
	if !ok {
		return wayland.HostCallbacks{}, false
	}
	return spec.Callbacks, true
}

func (c *fakeCompositor) openCount(surfaceID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, id := range c.opens {
		if id == surfaceID {
			n++
		}
	}
	return n
}

// waitOpenCount waits for a surface to have been opened want times. Aux
// requests cross a channel, so a count read the instant a command lands can
// be one short of the truth.
func (c *fakeCompositor) waitOpenCount(surfaceID string, want int) int {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := c.openCount(surfaceID); got >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(time.Millisecond)
	}
}

// barOnly is a configuration with no built-in widgets, so the only thing on
// a bar is the tray. It keeps the sweep below honest: every hit it finds is
// a tray target.
func barOnly() config.Config {
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = nil, nil, nil
	cfg.Outputs = nil
	return cfg
}

type harness struct {
	reg        *shell.Registry
	service    *fakeService
	compositor *fakeCompositor
	hosts      map[uint32]wayland.HostCallbacks
}

func newHarness(t *testing.T, cfg config.Config, outputs map[uint32]string) *harness {
	t.Helper()
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	compositor := newFakeCompositor(t, reg)
	service := newFakeService()
	reg.BindTray(service)

	hosts := make(map[uint32]wayland.HostCallbacks, len(outputs))
	for global, connector := range outputs {
		callbacks, err := reg.NewHost(global, connector)
		if err != nil {
			t.Fatalf("NewHost(%d, %q): %v", global, connector, err)
		}
		if err := callbacks.Configure(barWidth, barSurface, scaleUnit); err != nil {
			t.Fatalf("configure %q: %v", connector, err)
		}
		hosts[global] = callbacks
	}
	return &harness{reg: reg, service: service, compositor: compositor, hosts: hosts}
}

// relayout forces the deferred arrangement the owner would do at paint time.
// The bar re-arranges while rendering, because that goroutine owns the fonts.
func (h *harness) relayout(t *testing.T, global uint32) {
	t.Helper()
	stride := barWidth * 4
	pixels := make([]byte, stride*barSurface)
	if err := h.hosts[global].Render(pixels, barWidth, barSurface, stride); err != nil {
		t.Fatalf("render on %d: %v", global, err)
	}
}

// click presses and releases at one point, the gesture the bar accepts.
func (h *harness) click(global uint32, x, y int, button uint32) {
	handle := h.hosts[global].Handle
	handle(wayland.Event{Kind: wayland.EventPointerEnter, X: float64(x), Y: float64(y)})
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: float64(x), Y: float64(y),
		Button: button, Serial: 42})
	handle(wayland.Event{Kind: wayland.EventPointerRelease, X: float64(x), Y: float64(y),
		Button: button, Serial: 43})
}

func (h *harness) scroll(global uint32, x, y int, discrete int32) {
	handle := h.hosts[global].Handle
	handle(wayland.Event{Kind: wayland.EventPointerEnter, X: float64(x), Y: float64(y)})
	handle(wayland.Event{Kind: wayland.EventPointerAxis, X: float64(x), Y: float64(y),
		AxisDiscrete: discrete})
}

// sweep clicks right to left across the bar and reports the first x that made
// the shell act. The exact tray geometry is the bar's business; the test only
// needs a point that hits it, so it looks for one rather than predicting it.
func (h *harness) sweep(t *testing.T, global uint32, button uint32, acted func() bool) int {
	t.Helper()
	h.relayout(t, global)
	for x := barWidth - 1; x >= 0; x-- {
		h.click(global, x, barSurface/2, button)
		if acted() {
			return x
		}
	}
	t.Fatalf("no point on output %d produced an action", global)
	return -1
}

func snapshot(generation uint64, items ...tray.Item) trayclient.Message {
	return trayclient.Message{
		Generation: generation, Kind: trayclient.KindSnapshot, Sequence: 1,
		Snapshot: tray.Snapshot{Items: items},
	}
}

func item(owner, path, id, title string, generation uint64) tray.Item {
	return tray.Item{
		Key:      tray.ItemKey{Owner: owner, ObjectPath: path, Generation: generation},
		ID:       id,
		Title:    title,
		Category: tray.CategoryApplicationStatus,
		Status:   tray.StatusActive,
		Icon:     tray.Icon{Name: id},
	}
}

func flatMenu(revision uint32, labels ...string) tray.Menu {
	children := make([]tray.MenuNode, 0, len(labels))
	for i, label := range labels {
		children = append(children, tray.MenuNode{
			ID: int32(i + 1), Label: label, Enabled: true, Visible: true,
		})
	}
	return tray.Menu{Revision: revision, Root: tray.MenuNode{ID: 0, Visible: true, Children: children}}
}

// Two outputs each project the same items, and a click carries the key and
// the output global of the bar that was clicked.
func TestTrayProjectsOnBothOutputsAndActivates(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1", 8: "DP-2"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))

	for _, global := range []uint32{7, 8} {
		h.service.reset()
		h.sweep(t, global, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })
		commands := h.service.commands()
		if commands[0].Item != mail.Key {
			t.Fatalf("output %d activated %+v, want %+v", global, commands[0].Item, mail.Key)
		}
		if commands[0].Output != global {
			t.Fatalf("activate carried output %d, want %d", commands[0].Output, global)
		}
	}
}

// The middle button secondary-activates and a wheel scrolls, both against the
// same item the left button activates.
func TestTraySecondaryActivationAndScroll(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))

	x := h.sweep(t, 7, buttonMiddle, func() bool {
		return h.service.count(tray.CommandSecondaryActivate) > 0
	})
	h.service.reset()
	h.scroll(7, x, barSurface/2, -1)
	commands := h.service.commands()
	if len(commands) != 1 || commands[0].Kind != tray.CommandScroll {
		t.Fatalf("scroll produced %v", h.service.kinds())
	}
	if commands[0].Orientation != tray.ScrollVertical || commands[0].Delta != -120 {
		t.Fatalf("scroll command = %+v, want one vertical step", commands[0])
	}
}

// The right button opens the item's menu: the service is told, and one menu
// surface appears on the clicked output.
func TestTrayRightClickOpensOneMenuSurface(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1", 8: "DP-2"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open", "Quit")}})

	h.sweep(t, 8, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	output, ok := h.compositor.outputOf("tray-menu")
	if !ok {
		t.Fatalf("no menu surface after a right click; commands = %v", h.service.kinds())
	}
	if output != 8 {
		t.Fatalf("menu opened on output %d, want the clicked one", output)
	}
	if got := h.compositor.waitOpenCount("tray-menu", 2); got != 1 {
		t.Fatalf("menu surface opened %d times, want exactly one", got)
	}
}

// A menu the service has not published yet waits: the surface appears only
// when the menu arrives, and only for the request that asked for it.
func TestTrayMenuOpensWhenTheServicePublishesIt(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))

	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if _, ok := h.compositor.outputOf("tray-menu"); ok {
		t.Fatal("a menu surface appeared before the service published a menu")
	}
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	if _, ok := h.compositor.outputOf("tray-menu"); !ok {
		t.Fatal("the pending menu did not open when its menu arrived")
	}
}

// A refused menu.open opens nothing: the failure is terminal for that click.
func TestTrayMenuFailureOpensNothing(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	h.service.failOn[tray.CommandMenuOpen] = trayclient.ErrBusy
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})

	h.relayout(t, 7)
	for x := barWidth - 1; x >= 0; x-- {
		h.click(7, x, barSurface/2, buttonRight)
	}
	if _, ok := h.compositor.outputOf("tray-menu"); ok {
		t.Fatal("a refused menu.open still opened a surface")
	}
}

// A failed reply for the pending request drops it: the menu never opens, even
// if the service later publishes one for that item.
func TestTrayFailedReplyDropsThePendingMenu(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })

	h.reg.ApplyTray(trayclient.Message{
		Generation: 1, Kind: trayclient.KindReply, RequestID: h.service.lastID(),
		Reply: tray.Reply{Error: &tray.ProtocolError{Code: tray.ErrorUnavailable}},
	})
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	if _, ok := h.compositor.outputOf("tray-menu"); ok {
		t.Fatal("a menu opened after its request had already failed")
	}
}

// A reply for a request the shell never made, or already consumed, changes
// nothing. Replaying the same reply must not act twice.
func TestTrayStaleReplyIsInert(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.sweep(t, 7, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })
	before := len(h.service.commands())

	stale := trayclient.Message{
		Generation: 1, Kind: trayclient.KindReply, RequestID: 9999,
		Reply: tray.Reply{Error: &tray.ProtocolError{Code: tray.ErrorStaleItem}},
	}
	h.reg.ApplyTray(stale)
	h.reg.ApplyTray(stale)
	if got := len(h.service.commands()); got != before {
		t.Fatalf("a stale reply produced %d extra commands", got-before)
	}
}

// A reconnect republishes every item under a fresh generation. Commands for
// the previous generation's key never leave the shell.
func TestTrayReconnectRetiresTheOldGeneration(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	first := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, first))
	h.sweep(t, 7, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })

	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindDisconnected})
	second := item("org.mail", "/item/1", "mail", "Mail", 2)
	h.reg.ApplyTray(snapshot(2, second))

	h.service.reset()
	h.sweep(t, 7, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })
	for _, command := range h.service.commands() {
		if command.Item.Generation != 2 {
			t.Fatalf("command carried generation %d after a reconnect", command.Item.Generation)
		}
	}
}

// Losing the service closes every tray surface it owned.
func TestTrayServiceLossClosesTheMenu(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if _, ok := h.compositor.outputOf("tray-menu"); !ok {
		t.Fatal("no menu to lose")
	}

	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindDisconnected})
	if !h.compositor.gone("tray-menu") {
		t.Fatal("the menu surface survived losing the service")
	}
}

// Losing only the item that owns the menu closes that menu and leaves the
// other item alone.
func TestTrayItemLossClosesOnlyItsMenu(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	chat := item("org.chat", "/item/1", "chat", "Chat", 1)
	h.reg.ApplyTray(snapshot(1, mail, chat))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	h.sweep(t, 7, buttonRight, func() bool {
		for _, command := range h.service.commands() {
			if command.Kind == tray.CommandMenuOpen && command.Item == mail.Key {
				return true
			}
		}
		return false
	})
	if _, ok := h.compositor.outputOf("tray-menu"); !ok {
		t.Fatal("no menu to lose")
	}

	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemRemoved,
		Removed: tray.ItemRemoved{Key: mail.Key}})
	if !h.compositor.gone("tray-menu") {
		t.Fatal("the menu survived losing the item that owned it")
	}

	h.service.reset()
	h.sweep(t, 7, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })
	if h.service.commands()[0].Item != chat.Key {
		t.Fatal("the surviving item stopped responding")
	}
}

// An unrelated root replacing the chain closes the menu. A panel is the
// unrelated root here because it is the other chain owner the shell has.
func TestTrayRootReplacementClosesTheMenu(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if _, ok := h.compositor.outputOf("tray-menu"); !ok {
		t.Fatal("no menu to replace")
	}

	if err := h.reg.OpenPanel(shell.PanelClock, 7, shell.Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	if !h.compositor.gone("tray-menu") {
		t.Fatal("the menu survived an unrelated root taking the chain")
	}
}

// A root replacement between a menu.open and the menu arriving drops the
// pending request: the gesture that asked for it is over.
func TestTrayRootReplacementDropsThePendingMenu(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })

	if err := h.reg.OpenPanel(shell.PanelClock, 7, shell.Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	if _, ok := h.compositor.outputOf("tray-menu"); ok {
		t.Fatal("a pending menu opened over the root that replaced it")
	}
}

// A menu whose siblings repeat an ID is malformed. The shell shows an empty
// menu rather than guessing, and a selection against it sends nothing.
func TestTrayMalformedSiblingsSelectNothing(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	malformed := tray.Menu{Revision: 1, Root: tray.MenuNode{ID: 0, Visible: true,
		Children: []tray.MenuNode{
			{ID: 4, Label: "One", Enabled: true, Visible: true},
			{ID: 4, Label: "Two", Enabled: true, Visible: true},
		}}}
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: malformed}})
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if _, ok := h.compositor.outputOf("tray-menu"); !ok {
		t.Fatal("a malformed menu opened no surface at all")
	}

	h.service.reset()
	stride := 280 * 4
	pixels := make([]byte, stride*200)
	spec, ok := h.compositor.callbacksFor("tray-menu")
	if !ok || spec.Configure == nil || spec.Render == nil || spec.Handle == nil {
		t.Fatal("the menu surface has no callbacks")
	}
	if err := spec.Configure(280, 200, scaleUnit); err != nil {
		t.Fatal(err)
	}
	if err := spec.Render(pixels, 280, 200, stride); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 200; y += 4 {
		spec.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: 20, Y: float64(y)})
		spec.Handle(wayland.Event{Kind: wayland.EventPointerPress, X: 20, Y: float64(y), Serial: 5})
		spec.Handle(wayland.Event{Kind: wayland.EventPointerRelease, X: 20, Y: float64(y), Serial: 6})
	}
	if h.service.count(tray.CommandMenuSelect) != 0 {
		t.Fatalf("a malformed menu selected an entry: %v", h.service.kinds())
	}
}

// Two items publishing the same stable token cannot own a preference between
// them: both stay on the bar even though the token is marked hidden.
func TestTrayPreferenceCollisionIgnoresThePreference(t *testing.T) {
	cfg := barOnly()
	cfg.Tray.Hidden = []string{"id:shared"}
	h := newHarness(t, cfg, map[uint32]string{7: "DP-1"})
	first := item("org.a", "/item/1", "shared", "First", 1)
	second := item("org.b", "/item/1", "shared", "Second", 1)
	h.reg.ApplyTray(snapshot(1, first, second))

	h.relayout(t, 7)
	seen := map[tray.ItemKey]bool{}
	for x := barWidth - 1; x >= 0; x-- {
		h.click(7, x, barSurface/2, buttonLeft)
	}
	for _, command := range h.service.commands() {
		if command.Kind == tray.CommandActivate {
			seen[command.Item] = true
		}
	}
	if !seen[first.Key] || !seen[second.Key] {
		t.Fatalf("a colliding token hid an item: reachable = %d of 2", len(seen))
	}
}

// Unplugging an output releases its tray surfaces and leaves the other output
// working. Replugging gives a fresh global with no duplicate bar.
func TestTrayOutputHotplug(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1", 8: "DP-2"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	h.sweep(t, 8, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if output, ok := h.compositor.outputOf("tray-menu"); !ok || output != 8 {
		t.Fatal("the menu is not on the output that will be unplugged")
	}

	h.reg.DropHost(8)
	if !h.compositor.gone("tray-menu") {
		t.Fatal("unplugging an output left its menu surface behind")
	}

	h.service.reset()
	h.sweep(t, 7, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })

	callbacks, err := h.reg.NewHost(9, "DP-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := callbacks.Configure(barWidth, barSurface, scaleUnit); err != nil {
		t.Fatal(err)
	}
	h.hosts[9] = callbacks
	h.service.reset()
	h.sweep(t, 9, buttonLeft, func() bool { return h.service.count(tray.CommandActivate) > 0 })
	if h.service.commands()[0].Output != 9 {
		t.Fatal("the replugged output activated under the old global")
	}
}

// A compositor-side close of the menu releases the shell's root without the
// shell asking the compositor to close it again.
func TestTrayCompositorCloseReleasesTheRoot(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })

	h.reg.DropAux(7, "tray-menu")
	// Reopening proves the root was released: a chain still held by the old
	// menu would refuse the second open.
	h.service.reset()
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if got := h.compositor.waitOpenCount("tray-menu", 2); got != 2 {
		t.Fatalf("menu opened %d times, want a clean reopen", got)
	}
}

// Shutting down closes every tray surface. Nothing is left mapped and the
// call does not block on a channel no owner is draining any more.
func TestTrayCloseReleasesEverySurface(t *testing.T) {
	h := newHarness(t, barOnly(), map[uint32]string{7: "DP-1"})
	mail := item("org.mail", "/item/1", "mail", "Mail", 1)
	h.reg.ApplyTray(snapshot(1, mail))
	h.reg.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: mail.Key, Menu: flatMenu(1, "Open")}})
	h.sweep(t, 7, buttonRight, func() bool { return h.service.count(tray.CommandMenuOpen) > 0 })
	if _, ok := h.compositor.outputOf("tray-menu"); !ok {
		t.Fatal("no menu to close")
	}

	done := make(chan struct{})
	go func() { h.reg.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked at shutdown")
	}
	if h.service.count(tray.CommandMenuClose) == 0 {
		t.Fatal("shutdown did not release the service's menu state")
	}
}
