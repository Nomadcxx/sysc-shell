package shell

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// PluginHostOptions is how the process attaches discovery, state, and notify.
type PluginHostOptions struct {
	Roots    []plugin.Root
	StateDir string
	Notify   func(context.Context, v1.NotifyParams) (v1.NotifyResult, error)
}

type pluginSlot struct {
	rt   *plugin.Runtime
	disp *plugin.Dispatcher
}

type hostedView struct {
	ID         string
	Plugin     string
	Entry      string
	Instance   string
	Output     string
	Generation uint32
	Kind       v1.ViewKind
	Revision   uint64
	Root       *ui.Node
	tree       *plugin.ViewTree
	Failed     bool
	Label      string
	Width      int
	Height     int
}

type pluginHost struct {
	r    *Registry
	opts PluginHostOptions
	ctx  context.Context
	stop context.CancelFunc
	prep *plugin.Preparer

	mu           sync.Mutex
	slots        map[string]*pluginSlot
	views        map[string]*hostedView
	nextID       uint64
	inputs       []v1.InputEvent
	textOut      plugin.TextOut
	flushPending bool
	closed       []string
	panel        *hostedView
	catalog      plugin.Catalog
}

func pluginMeasure(s string, _ bool) (int, int) { return len(s) * 8, 16 }

// pluginBarViewWidth is the prepare slot for a plugin bar tree. Camera+Record+Stop
// with the error-fill Record chip is ~128px under pluginMeasure; 120 failed and
// the bar showed "!" (no clicks).
const pluginBarViewWidth = 240
const pluginBarViewHeight = 32

var hostPluginCaps = []plugin.Capability{
	plugin.CapNotifications, plugin.CapPanels, plugin.CapSettings, plugin.CapState,
}

// BindPlugins discovers enabled plugins and starts one runtime for each.
// Tests call it; production calls it once after NewRegistry.
func (r *Registry) BindPlugins(opts PluginHostOptions) error {
	ctx, stop := context.WithCancel(context.Background())
	h := &pluginHost{
		r:     r,
		opts:  opts,
		ctx:   ctx,
		stop:  stop,
		prep:  plugin.NewPreparer(2, pluginMeasure),
		slots: make(map[string]*pluginSlot),
		views: make(map[string]*hostedView),
	}
	r.mu.Lock()
	old := r.plugins
	r.plugins = h
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
	go h.pumpResults()
	return h.syncEnabled()
}

func (h *pluginHost) Close() {
	if h == nil {
		return
	}
	h.stop()
	h.mu.Lock()
	slots := h.slots
	h.slots = nil
	h.views = make(map[string]*hostedView)
	h.mu.Unlock()
	for _, s := range slots {
		s.rt.Stop()
	}
	h.prep.Close()
}

func (h *pluginHost) syncEnabled() error {
	cat, err := plugin.Discover(h.opts.Roots...)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.catalog = cat
	h.mu.Unlock()

	h.r.mu.Lock()
	enabled := append([]string(nil), h.r.cfg.Plugins.Enabled...)
	h.r.mu.Unlock()
	return h.finishSyncEnabled(enabled, cat, false)
}

// syncEnabledLocked assumes Registry.mu is held (panel handle / enableLocked).
func (h *pluginHost) syncEnabledLocked() error {
	cat, err := plugin.Discover(h.opts.Roots...)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.catalog = cat
	h.mu.Unlock()
	enabled := append([]string(nil), h.r.cfg.Plugins.Enabled...)
	return h.finishSyncEnabled(enabled, cat, true)
}

func (h *pluginHost) finishSyncEnabled(enabled []string, cat plugin.Catalog, registryHeld bool) error {
	want := map[string]bool{}
	for _, id := range enabled {
		want[id] = true
		if err := h.ensure(id, cat, registryHeld); err != nil {
			// A plugin that cannot start still occupies its bar slot as a
			// placeholder; the rest of the bar must stay up.
			continue
		}
	}
	h.mu.Lock()
	var drop []string
	for id := range h.slots {
		if !want[id] {
			drop = append(drop, id)
		}
	}
	h.mu.Unlock()
	for _, id := range drop {
		h.stopPlugin(id)
	}
	if registryHeld {
		h.syncBarsLocked()
	} else {
		h.syncBars()
	}
	return nil
}

func (h *pluginHost) ensure(id string, cat plugin.Catalog, registryHeld bool) error {
	h.mu.Lock()
	if h.slots[id] != nil {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	c, ok := cat.Lookup(id)
	if !ok {
		return fmt.Errorf("plugin %s is not installed", id)
	}
	rt := plugin.NewRuntime(c, plugin.RuntimeOptions{
		Supported: hostPluginCaps,
		Limits:    v1.DefaultLimits,
	})
	stateDir := h.opts.StateDir
	if stateDir == "" {
		stateDir = plugin.StateRoot()
	}
	store, err := plugin.OpenStore(stateDir, id)
	if err != nil {
		return err
	}
	if err := rt.Start(h.ctx); err != nil {
		return err
	}
	m := rt.Manifest()
	disp := plugin.NewDispatcher(plugin.CallEnv{
		PluginID:       id,
		Granted:        grantedCaps(rt),
		DeclaredPanels: m.Panels,
		Store:          store,
		OpenPanel:      func(_ context.Context, p v1.PanelParams) (v1.PanelResult, error) { return h.openPanel(id, p) },
		ClosePanel:     func(_ context.Context, p v1.PanelParams) error { return h.closePanel(id, p) },
		Notify:         h.opts.Notify,
		OutputContext: func(_ context.Context, p v1.OutputContextParams) (v1.OutputContextResult, error) {
			return h.outputContext(p)
		},
	})
	rt.SetCalls(disp)
	slot := &pluginSlot{rt: rt, disp: disp}
	h.mu.Lock()
	h.slots[id] = slot
	h.mu.Unlock()
	go h.pumpRuntime(slot)
	if registryHeld {
		h.pushSettingsLocked(id, rt)
	} else {
		h.pushSettings(id, rt)
	}
	return nil
}

func grantedCaps(rt *plugin.Runtime) []plugin.Capability {
	var out []plugin.Capability
	for _, c := range hostPluginCaps {
		if rt.Allows(c) {
			out = append(out, c)
		}
	}
	return out
}

func (h *pluginHost) pushSettings(id string, rt *plugin.Runtime) {
	h.r.mu.Lock()
	h.pushSettingsLocked(id, rt)
	h.r.mu.Unlock()
}

func (h *pluginHost) pushSettingsLocked(id string, rt *plugin.Runtime) {
	_ = rt.Send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: h.r.cfg.Plugins.Settings[id]})
}

func (h *pluginHost) stopPlugin(id string) {
	h.mu.Lock()
	slot := h.slots[id]
	delete(h.slots, id)
	var drop []string
	for vid, v := range h.views {
		if v.Plugin == id {
			drop = append(drop, vid)
		}
	}
	h.mu.Unlock()
	for _, vid := range drop {
		h.closeView(vid)
	}
	if slot != nil {
		slot.rt.Stop()
	}
}

func (h *pluginHost) pumpRuntime(slot *pluginSlot) {
	for {
		select {
		case <-h.ctx.Done():
			return
		case msg, ok := <-slot.rt.Messages():
			if !ok {
				return
			}
			h.onMessage(slot, msg)
		}
	}
}

func (h *pluginHost) pumpResults() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case res, ok := <-h.prep.Results():
			if !ok {
				return
			}
			h.applyResult(res)
		}
	}
}

func (h *pluginHost) onMessage(slot *pluginSlot, msg v1.Message) {
	switch m := msg.(type) {
	case *v1.ViewSnapshot:
		h.mu.Lock()
		v, ok := h.views[m.ViewID]
		h.mu.Unlock()
		if !ok {
			return
		}
		h.mu.Lock()
		if v.tree == nil {
			v.tree = &plugin.ViewTree{View: v.Kind}
		}
		_ = v.tree.ApplySnapshot(m.Revision, m.Root)
		h.mu.Unlock()
		h.prep.Submit(plugin.Job{
			ViewID: m.ViewID, Plugin: v.Plugin, View: v.Kind,
			Revision: m.Revision, Root: m.Root,
			Bounds: ui.Rect{W: v.Width, H: v.Height},
		})
	case *v1.ViewPatch:
		h.mu.Lock()
		v, ok := h.views[m.ViewID]
		slot := (*pluginSlot)(nil)
		if ok {
			slot = h.slots[v.Plugin]
		}
		h.mu.Unlock()
		if !ok || slot == nil {
			return
		}
		h.mu.Lock()
		if v.tree == nil {
			h.mu.Unlock()
			_ = slot.rt.Send(&v1.ViewResync{ViewID: m.ViewID})
			return
		}
		resync, err := v.tree.ApplyPatch(m)
		root, rev := v.tree.Root, v.tree.Revision
		kind := v.Kind
		pluginID := v.Plugin
		w, ht := v.Width, v.Height
		h.mu.Unlock()
		if resync {
			_ = slot.rt.Send(&v1.ViewResync{ViewID: m.ViewID})
			return
		}
		if err != nil {
			return
		}
		h.prep.Submit(plugin.Job{
			ViewID: m.ViewID, Plugin: pluginID, View: kind,
			Revision: rev, Root: root,
			Bounds: ui.Rect{W: w, H: ht},
		})
	case *v1.HostCall:
		reply := slot.disp.Handle(h.ctx, m)
		_ = slot.rt.Send(&reply)
	}
}

func (h *pluginHost) applyResult(res plugin.Result) {
	h.mu.Lock()
	v, ok := h.views[res.ViewID]
	if !ok {
		h.mu.Unlock()
		return
	}
	v.Revision = res.Revision
	if res.Err != nil {
		v.Failed = true
		v.Root = nil
		v.Label = res.Err.Error()
	} else {
		stampPluginActions(res.Root, res.ViewID)
		v.Failed = false
		v.Root = res.Root
		v.Label = ""
	}
	kind := v.Kind
	h.mu.Unlock()
	if kind == v1.ViewPanel {
		h.refreshPanel()
		return
	}
	h.r.refreshPluginBars()
}

func (h *pluginHost) syncBars() {
	h.r.mu.Lock()
	desired := h.desiredBarViewsLocked()
	h.r.mu.Unlock()
	h.reconcileBarViews(desired, false)
}

// syncBarsLocked assumes Registry.mu is held.
func (h *pluginHost) syncBarsLocked() {
	h.reconcileBarViews(h.desiredBarViewsLocked(), true)
}

func (h *pluginHost) desiredBarViewsLocked() []hostedView {
	cfg := h.r.cfg
	var desired []hostedView
	for global, bar := range h.r.bars {
		conn := bar.connector()
		policy := cfg.ForConnector(conn)
		for _, item := range allItems(policy) {
			if item.ID != "plugin" {
				continue
			}
			desired = append(desired, hostedView{
				Plugin: item.Plugin, Entry: item.Entry, Instance: item.Instance,
				Output: conn, Generation: global, Kind: v1.ViewBar, Width: pluginBarViewWidth, Height: pluginBarViewHeight,
			})
		}
	}
	return desired
}

func (h *pluginHost) reconcileBarViews(desired []hostedView, registryHeld bool) {
	h.mu.Lock()
	haveBar := map[string]*hostedView{}
	haveTip := map[string]*hostedView{}
	for _, v := range h.views {
		key := barKey(v.Plugin, v.Instance, v.Output)
		switch v.Kind {
		case v1.ViewBar:
			haveBar[key] = v
		case v1.ViewTooltip:
			haveTip[key] = v
		}
	}
	h.mu.Unlock()

	want := map[string]hostedView{}
	for _, d := range desired {
		key := barKey(d.Plugin, d.Instance, d.Output)
		want[key] = d
		if _, ok := haveBar[key]; !ok {
			h.openView(d, registryHeld)
		}
		if _, ok := haveTip[key]; !ok {
			tip := d
			tip.Kind = v1.ViewTooltip
			tip.Width, tip.Height = 280, 200
			h.openView(tip, registryHeld)
		}
	}
	for key, v := range haveBar {
		if _, ok := want[key]; !ok {
			h.closeView(v.ID)
		}
	}
	for key, v := range haveTip {
		if _, ok := want[key]; !ok {
			h.closeView(v.ID)
		}
	}
}

func barKey(pluginID, instance, output string) string {
	return pluginID + "/" + instance + "/" + output
}

func (h *pluginHost) openView(spec hostedView, registryHeld bool) {
	h.mu.Lock()
	slot := h.slots[spec.Plugin]
	n := 0
	for _, v := range h.views {
		if v.Plugin == spec.Plugin {
			n++
		}
	}
	if slot == nil || n >= v1.DefaultLimits.MaxViews {
		h.mu.Unlock()
		return
	}
	h.nextID++
	spec.ID = fmt.Sprintf("v%d", h.nextID)
	copied := spec
	h.views[spec.ID] = &copied
	h.mu.Unlock()

	_ = slot.rt.Send(&v1.ViewOpen{
		ViewID: spec.ID, View: spec.Kind, Entry: spec.Entry,
		Instance: spec.Instance, Output: spec.Output, Generation: spec.Generation,
		Width: spec.Width, Height: spec.Height,
	})
	var inst map[string]any
	if registryHeld {
		inst = h.r.cfg.Plugins.Instances[spec.Instance]
	} else {
		h.r.mu.Lock()
		inst = h.r.cfg.Plugins.Instances[spec.Instance]
		h.r.mu.Unlock()
	}
	if inst != nil {
		_ = slot.rt.Send(&v1.SettingsChanged{Scope: v1.ScopeInstance, Instance: spec.Instance, Values: inst})
	}
}

func (h *pluginHost) closeView(id string) {
	h.mu.Lock()
	v, ok := h.views[id]
	if !ok {
		h.mu.Unlock()
		return
	}
	slot := h.slots[v.Plugin]
	delete(h.views, id)
	h.closed = append(h.closed, id)
	if h.panel != nil && h.panel.ID == id {
		h.panel = nil
	}
	h.mu.Unlock()
	if slot != nil {
		_ = slot.rt.Send(&v1.ViewClose{ViewID: id})
	}
}

func (h *pluginHost) frames(output string) map[string]pluginFrame {
	out := map[string]pluginFrame{}
	tips := map[string]*hostedView{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range h.views {
		if v.Output != output {
			continue
		}
		switch v.Kind {
		case v1.ViewBar:
			out[v.Instance] = pluginFrame{
				Root: v.Root, Revision: v.Revision, Failed: v.Failed, Label: v.Label,
				ViewID: v.ID,
			}
		case v1.ViewTooltip:
			tips[v.Instance] = v
		}
	}
	for inst, frame := range out {
		if tip := tips[inst]; tip != nil && !tip.Failed {
			frame.Tooltip = tip.Root
			out[inst] = frame
		}
	}
	return out
}

func (h *pluginHost) failPlugin(id, reason string) {
	h.mu.Lock()
	for _, v := range h.views {
		if v.Plugin == id {
			v.Failed = true
			v.Root = nil
			v.Label = reason
			v.Revision++
		}
	}
	h.mu.Unlock()
	h.r.refreshPluginBars()
	h.refreshPanel()
	h.r.dwell.leave()
}

func (h *pluginHost) openPanel(pluginID string, p v1.PanelParams) (v1.PanelResult, error) {
	h.mu.Lock()
	slot := h.slots[pluginID]
	var spec plugin.Panel
	found := false
	if slot != nil {
		for _, panel := range slot.rt.Manifest().Panels {
			if panel.ID == p.Entry {
				spec = panel
				found = true
				break
			}
		}
	}
	h.mu.Unlock()
	if slot == nil {
		return v1.PanelResult{}, errors.New("plugin is not running")
	}
	if !found {
		return v1.PanelResult{}, fmt.Errorf("panel %q is not declared", p.Entry)
	}

	h.r.mu.Lock()
	conn, global, err := h.resolveOutputLocked(v1.OutputContextParams{Output: p.Output, Generation: p.Generation})
	trig := Trigger{}
	if err == nil {
		if bar, ok := h.r.bars[global]; ok {
			policy := h.r.cfg.ForConnector(bar.connector())
			trig = Trigger{BarEdge: policy.Edge, BarZone: exclusiveBarZone(bar)}
		}
	}
	h.r.mu.Unlock()
	if err != nil {
		return v1.PanelResult{}, err
	}
	p.Output = conn

	// Toggle close: re-validate under r.mu then h.mu. If ownership moved,
	// errPanelNotOwned falls through to the open/replace path.
	if err := h.closePanelOwned(pluginID, p, global, true); err == nil {
		return v1.PanelResult{}, nil
	} else if !errors.Is(err, errPanelNotOwned) {
		return v1.PanelResult{}, err
	}

	// Size must be on h.panel before OpenPanel: spawnPanelLocked reads
	// panelSize() for the aux surface. Opening first kept the 320×280
	// fallback, so include_settings TextFields failed layout (sysc-139).
	h.mu.Lock()
	h.panel = &hostedView{
		Plugin: pluginID, Entry: p.Entry, Instance: p.Instance,
		Output: p.Output, Generation: global, Kind: v1.ViewPanel,
		Width: spec.Width, Height: spec.Height,
	}
	h.mu.Unlock()

	h.r.mu.Lock()
	_, open := h.r.panels.Output(PanelPlugin)
	h.r.mu.Unlock()
	if open {
		h.r.ClosePanel(PanelPlugin)
	}
	if err := h.r.OpenPanel(PanelPlugin, global, trig); err != nil {
		h.mu.Lock()
		h.panel = nil
		h.mu.Unlock()
		return v1.PanelResult{}, err
	}

	view := hostedView{
		Plugin: pluginID, Entry: p.Entry, Instance: p.Instance,
		Output: p.Output, Generation: global, Kind: v1.ViewPanel,
		Width: spec.Width, Height: spec.Height,
	}
	h.openView(view, false)
	h.mu.Lock()
	// The view we just opened is the newest panel for this plugin.
	var opened *hostedView
	for _, v := range h.views {
		if v.Kind == v1.ViewPanel && v.Plugin == pluginID && v.Entry == p.Entry {
			opened = v
		}
	}
	h.panel = opened
	id := ""
	if opened != nil {
		id = opened.ID
	}
	h.mu.Unlock()
	h.refreshPanel()
	return v1.PanelResult{ViewID: id}, nil
}

// errPanelNotOwned means this plugin+entry does not own PanelPlugin on the
// requested output, so openPanel should open/replace instead of toggling closed.
var errPanelNotOwned = errors.New("plugin panel not owned")

// closePanel sync-drops this plugin's panel views then closes PanelPlugin.
func (h *pluginHost) closePanel(pluginID string, p v1.PanelParams) error {
	return h.closePanelOwned(pluginID, p, 0, false)
}

// closePanelOwned drops matching panel views and closes PanelPlugin. When
// requireOutput is set, ownership is checked under r.mu then h.mu and the close
// runs in that same section; a mismatch returns errPanelNotOwned.
func (h *pluginHost) closePanelOwned(pluginID string, p v1.PanelParams, global uint32, requireOutput bool) error {
	h.r.mu.Lock()
	where, open := h.r.panels.Output(PanelPlugin)
	owns := open && h.r.roots.owns(panelRoot(PanelPlugin))
	if requireOutput {
		owns = owns && where == global
	}
	h.mu.Lock()
	same := h.panel != nil && h.panel.Plugin == pluginID && (p.Entry == "" || h.panel.Entry == p.Entry)
	if requireOutput && (!same || !owns) {
		h.mu.Unlock()
		h.r.mu.Unlock()
		return errPanelNotOwned
	}
	var drop []string
	for _, v := range h.views {
		if v.Kind == v1.ViewPanel && v.Plugin == pluginID && (p.Entry == "" || v.Entry == p.Entry) {
			drop = append(drop, v.ID)
		}
	}
	h.mu.Unlock()
	h.r.closePanelLocked(PanelPlugin)
	h.r.mu.Unlock()
	for _, id := range drop {
		h.closeView(id)
	}
	return nil
}

// snapshotPanelViewIDs returns current panel view IDs. Called with Registry.mu
// held from panel close cleanup so a later drop cannot race a reopen.
func (h *pluginHost) snapshotPanelViewIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var ids []string
	for _, v := range h.views {
		if v.Kind == v1.ViewPanel {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

// dropPanelViews closes only the snapshotted panel view IDs from close time.
// Under r.mu then h.mu it also refuses to drop the live panel view when
// PanelPlugin is already open again, so a bad snapshot cannot wipe a reopen.
func (h *pluginHost) dropPanelViews(ids []string) {
	h.r.mu.Lock()
	live := ""
	if _, open := h.r.panels.Output(PanelPlugin); open && h.r.roots.owns(panelRoot(PanelPlugin)) {
		h.mu.Lock()
		if h.panel != nil {
			live = h.panel.ID
		}
		h.mu.Unlock()
	}
	h.r.mu.Unlock()
	for _, id := range ids {
		if id != "" && id == live {
			continue
		}
		h.closeView(id)
	}
}

func (h *pluginHost) refreshPanel() {
	h.r.mu.Lock()
	host := h.r.panelHosts[PanelPlugin]
	h.r.mu.Unlock()
	if host == nil {
		return
	}
	h.r.mu.Lock()
	h.r.rebuildPanel(host)
	h.r.mu.Unlock()
	h.r.publishSurface(host.output, panelSurfaceID(PanelPlugin))
}

func (h *pluginHost) panelTree(host *PanelHost) *ui.Node {
	h.mu.Lock()
	v := h.panel
	if v == nil {
		h.mu.Unlock()
		return pluginPanelError("starting", false)
	}
	failed, label, root := v.Failed, v.Label, v.Root
	pluginID, entry := v.Plugin, v.Entry
	include := false
	var schema []plugin.Setting
	if slot := h.slots[pluginID]; slot != nil {
		m := slot.rt.Manifest()
		schema = m.Settings
		for _, panel := range m.Panels {
			if panel.ID == entry {
				include = panel.IncludeSettings
				break
			}
		}
	}
	h.mu.Unlock()
	if failed || root == nil {
		return pluginPanelError(label, true)
	}
	if !include {
		return root
	}
	settings := pluginPanelSettings(h.r, host, pluginID, schema)
	head := root
	if root.Kind != ui.KindCapsule {
		head = monitorCard([]*ui.Node{root})
	}
	return &ui.Node{Kind: ui.KindScroll, Gap: monitorCardGap, Padding: monitorPanelPadding, Children: append([]*ui.Node{head}, settings...)}
}

func (h *pluginHost) panelSize() ui.Rect {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.panel != nil && h.panel.Width > 0 {
		return ui.Rect{W: h.panel.Width, H: h.panel.Height}
	}
	return ui.Rect{W: 320, H: 280}
}

func pluginPanelError(reason string, actions bool) *ui.Node {
	if reason == "" {
		reason = "failed"
	}
	rows := []*ui.Node{{Kind: ui.KindText, Text: reason, Tone: ui.ToneError}}
	if actions {
		rows = append(rows,
			&ui.Node{Kind: ui.KindButton, Text: "Close", Action: "plugin-close", Name: "Close", Role: "button", Focusable: true},
			&ui.Node{Kind: ui.KindButton, Text: "Retry", Action: "plugin-retry", Name: "Retry", Role: "button", Focusable: true},
			&ui.Node{Kind: ui.KindButton, Text: "Disable", Action: "plugin-disable", Name: "Disable", Role: "button", Focusable: true},
		)
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: rows}
}

func (h *pluginHost) deliver(hit pluginHit, event v1.EventKind, button v1.PointerButton, text string) bool {
	h.mu.Lock()
	v, ok := h.views[hit.ViewID]
	var slot *pluginSlot
	if ok {
		slot = h.slots[v.Plugin]
	}
	var toSend []v1.InputEvent
	if ok {
		ev := v1.InputEvent{
			ViewID: hit.ViewID, Revision: v.Revision, Node: hit.Node,
			Event: event, Button: button, Text: text, Output: v.Output, Generation: v.Generation,
		}
		toSend = h.textOut.Push(ev)
		h.inputs = append(h.inputs, toSend...)
		if event == v1.EventChange && len(toSend) == 0 {
			h.scheduleTextFlushLocked()
		}
	}
	h.mu.Unlock()
	if !ok || slot == nil {
		return false
	}
	for i := range toSend {
		_ = slot.rt.Send(&toSend[i])
	}
	return true
}

func (h *pluginHost) scheduleTextFlushLocked() {
	if h.flushPending {
		return
	}
	h.flushPending = true
	time.AfterFunc(time.Second/30, h.flushText)
}

func (h *pluginHost) flushText() {
	h.mu.Lock()
	h.flushPending = false
	pending := h.textOut.Flush()
	h.inputs = append(h.inputs, pending...)
	slots := make([]*pluginSlot, 0, len(pending))
	for _, ev := range pending {
		v := h.views[ev.ViewID]
		if v == nil {
			slots = append(slots, nil)
			continue
		}
		slots = append(slots, h.slots[v.Plugin])
	}
	h.mu.Unlock()
	for i, ev := range pending {
		if slots[i] == nil {
			continue
		}
		_ = slots[i].rt.Send(&ev)
	}
}

func (h *pluginHost) lastInputs() []v1.InputEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]v1.InputEvent, len(h.inputs))
	copy(out, h.inputs)
	return out
}

func (h *pluginHost) closedViews() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.closed))
	copy(out, h.closed)
	return out
}

func (h *pluginHost) pid(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.slots[id]; s != nil {
		return s.rt.Status().PID
	}
	return 0
}

func (h *pluginHost) barViewIDs(output string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var ids []string
	for _, v := range h.views {
		if v.Kind == v1.ViewBar && v.Output == output {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

func (h *pluginHost) retryOrDisable(action string) {
	h.mu.Lock()
	v := h.panel
	slot := (*pluginSlot)(nil)
	if v != nil {
		slot = h.slots[v.Plugin]
	}
	pluginID := ""
	if v != nil {
		pluginID = v.Plugin
	}
	h.mu.Unlock()
	switch action {
	case "plugin-close":
		h.r.ClosePanel(PanelPlugin)
	case "plugin-retry":
		if slot != nil {
			_ = slot.rt.Retry(h.ctx)
		}
	case "plugin-disable":
		if pluginID != "" {
			h.stopPlugin(pluginID)
		}
		h.r.ClosePanel(PanelPlugin)
	}
}

func (r *Registry) refreshPluginBars() {
	r.mu.Lock()
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()
	r.publish(changed)
}

func (r *Registry) outputGlobalLocked(connector string) uint32 {
	if connector != "" {
		for global, bar := range r.bars {
			if bar.connector() == connector {
				return global
			}
		}
	}
	if r.focused != "" {
		for global, bar := range r.bars {
			if bar.connector() == r.focused {
				return global
			}
		}
	}
	for global := range r.bars {
		return global
	}
	return 0
}

func (h *pluginHost) outputContext(p v1.OutputContextParams) (v1.OutputContextResult, error) {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	conn, gen, err := h.resolveOutputLocked(p)
	if err != nil {
		return v1.OutputContextResult{}, err
	}
	return v1.OutputContextResult{Output: conn, Generation: gen}, nil
}

func (h *pluginHost) resolveOutputLocked(p v1.OutputContextParams) (string, uint32, error) {
	if p.Output != "" {
		for global, bar := range h.r.bars {
			if bar.connector() == p.Output {
				if p.Generation != 0 && p.Generation != global {
					return "", 0, fmt.Errorf("output %s generation is stale", p.Output)
				}
				return p.Output, global, nil
			}
		}
		return "", 0, fmt.Errorf("output %s is not declared", p.Output)
	}
	global := h.r.outputGlobalLocked("")
	if global == 0 {
		return "", 0, errors.New("no output")
	}
	conn := h.r.bars[global].connector()
	if p.Generation != 0 && p.Generation != global {
		return "", 0, fmt.Errorf("output %s generation is stale", conn)
	}
	return conn, global, nil
}

func (r *Registry) handlePluginBar(action string, event wayland.Event) bool {
	hit, ok := parsePluginAction(action)
	if !ok || r.plugins == nil {
		return false
	}
	kind := v1.EventActivate
	var button v1.PointerButton
	switch event.Kind {
	case wayland.EventPointerPress:
		kind = v1.EventPointer
		button = pointerButton(event.Button)
		// Secondary/middle fire on release only. Press+release both used to
		// deliver, so a right-click opened the panel and the implicit grab's
		// release toggled it closed.
		if button != v1.ButtonPrimary {
			return false
		}
	case wayland.EventPointerRelease:
		if event.Button != 0 && event.Button != 272 {
			kind = v1.EventPointer
			button = pointerButton(event.Button)
		}
	}
	return r.plugins.deliver(hit, kind, button, "")
}

func (r *Registry) deliverPluginText(action, text string, kind v1.EventKind) bool {
	hit, ok := parsePluginAction(action)
	if !ok || r.plugins == nil {
		return false
	}
	return r.plugins.deliver(hit, kind, "", text)
}

func (r *Registry) PluginPID(id string) int {
	if r.plugins == nil {
		return 0
	}
	return r.plugins.pid(id)
}

func (r *Registry) SetPluginEnabled(id string, on bool) error {
	if r.plugins == nil {
		return errors.New("plugins not bound")
	}
	return r.plugins.enable(id, on)
}

func (r *Registry) PluginBarViews(output string) int {
	if r.plugins == nil {
		return 0
	}
	return len(r.plugins.barViewIDs(output))
}

func (h *pluginHost) discovered() plugin.Catalog {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.catalog
}

func (h *pluginHost) status(id string) plugin.Status {
	h.mu.Lock()
	slot := h.slots[id]
	h.mu.Unlock()
	if slot == nil {
		return plugin.Status{State: plugin.StateDisabled}
	}
	return slot.rt.Status()
}

func (h *pluginHost) enable(id string, on bool) error {
	h.r.mu.Lock()
	cfg := h.setEnabledConfigLocked(id, on)
	h.r.mu.Unlock()
	if err := h.r.writeConfig(cfg); err != nil {
		return err
	}
	return h.syncEnabled()
}

// enableLocked assumes Registry.mu is held (panel handle).
func (h *pluginHost) enableLocked(id string, on bool) error {
	cfg := h.setEnabledConfigLocked(id, on)
	if err := h.r.writeConfig(cfg); err != nil {
		return err
	}
	return h.syncEnabledLocked()
}

func (h *pluginHost) setEnabledConfigLocked(id string, on bool) config.Config {
	cfg := h.r.cfg
	cfg.Plugins = cfg.Plugins.Clone()
	next := make([]string, 0, len(cfg.Plugins.Enabled)+1)
	for _, have := range cfg.Plugins.Enabled {
		if have != id {
			next = append(next, have)
		}
	}
	if on {
		next = append(next, id)
	}
	cfg.Plugins.Enabled = next
	h.r.cfg = cfg
	return cfg
}

func (h *pluginHost) retry(id string) error {
	h.mu.Lock()
	slot := h.slots[id]
	h.mu.Unlock()
	if slot == nil {
		return h.enable(id, true)
	}
	return slot.rt.Retry(h.ctx)
}

func (h *pluginHost) retryLocked(id string) error {
	h.mu.Lock()
	slot := h.slots[id]
	h.mu.Unlock()
	if slot == nil {
		return h.enableLocked(id, true)
	}
	return slot.rt.Retry(h.ctx)
}

func (h *pluginHost) rescan() error { return h.syncEnabled() }

func (h *pluginHost) applySetting(pluginID, key string, value any) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.applySettingLocked(pluginID, key, value)
}

// applySettingLocked assumes Registry.mu is held (panel handle).
func (h *pluginHost) applySettingLocked(pluginID, key string, value any) error {
	h.mu.Lock()
	slot := h.slots[pluginID]
	var schema []plugin.Setting
	if slot != nil {
		schema = slot.rt.Manifest().Settings
	} else {
		if c, ok := h.catalog.Lookup(pluginID); ok {
			schema = c.Manifest.Settings
		}
	}
	h.mu.Unlock()
	cfg := h.r.cfg
	cfg.Plugins = cfg.Plugins.Clone()
	if cfg.Plugins.Settings == nil {
		cfg.Plugins.Settings = map[string]map[string]any{}
	}
	cur := map[string]any{}
	for k, v := range cfg.Plugins.Settings[pluginID] {
		cur[k] = v
	}
	cur[key] = value
	if err := plugin.CheckValues(schema, cur); err != nil {
		return err
	}
	cfg.Plugins.Settings[pluginID] = cur
	h.r.cfg = cfg
	if err := h.r.writeConfig(cfg); err != nil {
		return err
	}
	if slot != nil {
		_ = slot.rt.Send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: cur})
	}
	return nil
}
