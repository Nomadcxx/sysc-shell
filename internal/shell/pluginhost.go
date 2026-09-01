package shell

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
	ID       string
	Plugin   string
	Entry    string
	Instance string
	Output   string
	Kind     v1.ViewKind
	Revision uint64
	Root     *ui.Node
	Failed   bool
	Label    string
	Width    int
	Height   int
}

type pluginHost struct {
	r    *Registry
	opts PluginHostOptions
	ctx  context.Context
	stop context.CancelFunc
	prep *plugin.Preparer

	mu      sync.Mutex
	slots   map[string]*pluginSlot
	views   map[string]*hostedView
	nextID  uint64
	inputs  []v1.InputEvent
	closed  []string
	panel   *hostedView
	catalog plugin.Catalog
}

func pluginMeasure(s string, _ bool) (int, int) { return len(s) * 8, 16 }

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

	want := map[string]bool{}
	for _, id := range enabled {
		want[id] = true
		if err := h.ensure(id, cat); err != nil {
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
	h.syncBars()
	return nil
}

func (h *pluginHost) ensure(id string, cat plugin.Catalog) error {
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
	if err := rt.Start(h.ctx); err != nil {
		return err
	}
	stateDir := h.opts.StateDir
	if stateDir == "" {
		stateDir = plugin.StateRoot()
	}
	store, err := plugin.OpenStore(stateDir, id)
	if err != nil {
		rt.Stop()
		return err
	}
	m := rt.Manifest()
	slot := &pluginSlot{rt: rt}
	slot.disp = plugin.NewDispatcher(plugin.CallEnv{
		PluginID:       id,
		Granted:        grantedCaps(rt),
		DeclaredPanels: m.Panels,
		Store:          store,
		OpenPanel:      func(_ context.Context, p v1.PanelParams) (v1.PanelResult, error) { return h.openPanel(id, p) },
		ClosePanel:     func(_ context.Context, p v1.PanelParams) error { return h.closePanel(id, p) },
		Notify:         h.opts.Notify,
	})
	h.mu.Lock()
	h.slots[id] = slot
	h.mu.Unlock()
	go h.pumpRuntime(slot)
	h.pushSettings(id, rt)
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
	pluginVals := h.r.cfg.Plugins.Settings[id]
	h.r.mu.Unlock()
	_ = rt.Send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: pluginVals})
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
		h.prep.Submit(plugin.Job{
			ViewID: m.ViewID, Plugin: v.Plugin, View: v.Kind,
			Revision: m.Revision, Root: m.Root,
			Bounds: ui.Rect{W: v.Width, H: v.Height},
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
	cfg := h.r.cfg
	var desired []hostedView
	for _, bar := range h.r.bars {
		conn := bar.connector()
		policy := cfg.ForConnector(conn)
		for _, item := range allItems(policy) {
			if item.ID != "plugin" {
				continue
			}
			desired = append(desired, hostedView{
				Plugin: item.Plugin, Entry: item.Entry, Instance: item.Instance,
				Output: conn, Kind: v1.ViewBar, Width: 120, Height: 32,
			})
		}
	}
	h.r.mu.Unlock()

	h.mu.Lock()
	have := map[string]*hostedView{}
	for _, v := range h.views {
		if v.Kind == v1.ViewBar {
			have[barKey(v.Plugin, v.Instance, v.Output)] = v
		}
	}
	h.mu.Unlock()

	want := map[string]hostedView{}
	for _, d := range desired {
		want[barKey(d.Plugin, d.Instance, d.Output)] = d
		if _, ok := have[barKey(d.Plugin, d.Instance, d.Output)]; !ok {
			h.openView(d)
		}
	}
	for key, v := range have {
		if _, ok := want[key]; !ok {
			h.closeView(v.ID)
		}
	}
}

func barKey(pluginID, instance, output string) string {
	return pluginID + "/" + instance + "/" + output
}

func (h *pluginHost) openView(spec hostedView) {
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
		Instance: spec.Instance, Output: spec.Output,
		Width: spec.Width, Height: spec.Height,
	})
	h.r.mu.Lock()
	inst := h.r.cfg.Plugins.Instances[spec.Instance]
	h.r.mu.Unlock()
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
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range h.views {
		if v.Kind != v1.ViewBar || v.Output != output {
			continue
		}
		out[v.Instance] = pluginFrame{
			Root: v.Root, Revision: v.Revision, Failed: v.Failed, Label: v.Label,
		}
	}
	// Enabled but not yet viewed placements still need a starting frame so
	// the widget does not stay at the unset revision forever.
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
	global := h.r.outputGlobalLocked(p.Output)
	trig := Trigger{}
	if bar, ok := h.r.bars[global]; ok {
		policy := h.r.cfg.ForConnector(bar.connector())
		trig = Trigger{BarEdge: policy.Edge, BarZone: policy.Height}
	}
	h.r.mu.Unlock()
	if global == 0 {
		return v1.PanelResult{}, errors.New("no output for plugin panel")
	}
	if err := h.r.OpenPanel(PanelPlugin, global, trig); err != nil {
		return v1.PanelResult{}, err
	}

	view := hostedView{
		Plugin: pluginID, Entry: p.Entry, Instance: p.Instance,
		Output: p.Output, Kind: v1.ViewPanel,
		Width: spec.Width, Height: spec.Height,
	}
	h.openView(view)
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
	return v1.PanelResult{ViewID: id}, nil
}

func (h *pluginHost) dropPanelViews() {
	h.mu.Lock()
	var drop []string
	for _, v := range h.views {
		if v.Kind == v1.ViewPanel {
			drop = append(drop, v.ID)
		}
	}
	h.mu.Unlock()
	for _, id := range drop {
		h.closeView(id)
	}
}

func (h *pluginHost) closePanel(pluginID string, p v1.PanelParams) error {
	h.mu.Lock()
	var drop []string
	for _, v := range h.views {
		if v.Kind == v1.ViewPanel && v.Plugin == pluginID && (p.Entry == "" || v.Entry == p.Entry) {
			drop = append(drop, v.ID)
		}
	}
	h.mu.Unlock()
	for _, id := range drop {
		h.closeView(id)
	}
	h.r.ClosePanel(PanelPlugin)
	return nil
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

func (h *pluginHost) panelTree() *ui.Node {
	h.mu.Lock()
	v := h.panel
	h.mu.Unlock()
	if v == nil {
		return pluginPanelError("starting", false)
	}
	if v.Failed || v.Root == nil {
		return pluginPanelError(v.Label, true)
	}
	return v.Root
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
	var ev v1.InputEvent
	if ok {
		ev = v1.InputEvent{
			ViewID: hit.ViewID, Revision: v.Revision, Node: hit.Node,
			Event: event, Button: button, Text: text, Output: v.Output,
		}
		h.inputs = append(h.inputs, ev)
	}
	h.mu.Unlock()
	if !ok || slot == nil {
		return false
	}
	_ = slot.rt.Send(&ev)
	return true
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
	h.r.mu.Unlock()
	if err := h.r.writeConfig(cfg); err != nil {
		return err
	}
	return h.syncEnabled()
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

func (h *pluginHost) rescan() error { return h.syncEnabled() }

func (h *pluginHost) applySetting(pluginID, key string, value any) error {
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
	h.r.mu.Lock()
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
		h.r.mu.Unlock()
		return err
	}
	cfg.Plugins.Settings[pluginID] = cur
	h.r.cfg = cfg
	h.r.mu.Unlock()
	if err := h.r.writeConfig(cfg); err != nil {
		return err
	}
	if slot != nil {
		_ = slot.rt.Send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: cur})
	}
	return nil
}
