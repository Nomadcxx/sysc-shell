package wayland

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/fractionalscale"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/viewporter"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

// namespace identifies the shell's layer surfaces to the compositor.
const namespace = "sysc-shell:bar"

// EventKind names the pointer events the platform forwards.
type EventKind uint8

const (
	EventPointerMotion EventKind = iota
	EventPointerPress
	EventPointerRelease
	EventPointerLeave
	EventPointerEnter
)

// Event is one pointer event in logical surface coordinates, which match the
// viewport destination and therefore the layout's hit-test bounds.
type Event struct {
	Kind EventKind
	// X and Y are logical surface coordinates. They stay float64 because
	// wl_pointer reports sub-pixel precision; the consumer floors them at the
	// hit-test boundary rather than losing it here.
	X, Y   float64
	Button uint32
	// Serial is the input serial. Recorded now, first consumed at Milestone 4.
	Serial uint32
}

// Invalidation requests a redraw of one bar, named by its wl_registry global.
// A zero Global invalidates every bar; the compositor never assigns 0 as a
// global name, so it is free to use as the broadcast sentinel.
type Invalidation struct{ Global uint32 }

// PreparedConfig holds replacement callbacks built before a reload changes
// live state. Commit publishes their matching shell models after the Wayland
// owner has applied every host policy.
type PreparedConfig struct {
	Hosts  map[uint32]HostCallbacks
	Commit func()
	// Rollback undoes what preparing acquired. The owner can still reject a
	// candidate after the application prepared it, and a prepared bar may hold
	// service leases; without this they would leak a running goroutine.
	Rollback func()
}

// HostIdentity carries the registry global that owns a bar and the connector
// attribute used to join configuration and Niri state.
type HostIdentity struct {
	Global    uint32
	Connector string
}

// Callbacks are the concrete hooks the application supplies. This is a struct
// rather than an interface because there is one implementation.
type Callbacks struct {
	// NewHost supplies the per-bar hooks when a bar is created for a connector.
	NewHost func(global uint32, connector string) (HostCallbacks, error)
	// PrepareConfig builds replacement bars for every enabled connector without
	// changing live shell state.
	PrepareConfig func(config.Config, []HostIdentity) (PreparedConfig, error)
	// DropHost releases per-bar resources after a bar is destroyed.
	DropHost func(global uint32)
	// Invalidations is owned by the caller. Run only receives from it and
	// never closes it.
	Invalidations <-chan Invalidation
	// Reloads carries reload requests, normally from SIGHUP. The owner reads
	// and validates the file itself, so no other goroutine parses
	// configuration while a bar is mapped.
	Reloads <-chan struct{}
	// Tooltips asks the owner to show or hide a tooltip. It is owned by the
	// caller; Run only receives from it and never closes it.
	Tooltips <-chan TooltipRequest
	// ConfigPath is the file a reload re-reads. Empty disables reloading.
	ConfigPath string
}

func (c Callbacks) validate() error {
	if c.NewHost == nil {
		return errors.New("wayland: Callbacks.NewHost is nil")
	}
	if c.PrepareConfig == nil {
		return errors.New("wayland: Callbacks.PrepareConfig is nil")
	}
	return nil
}

func (c HostCallbacks) validate(connector string) error {
	switch {
	case c.Configure == nil:
		return fmt.Errorf("wayland: HostCallbacks.Configure is nil for %s", connector)
	case c.Render == nil:
		return fmt.Errorf("wayland: HostCallbacks.Render is nil for %s", connector)
	case c.Handle == nil:
		return fmt.Errorf("wayland: HostCallbacks.Handle is nil for %s", connector)
	}
	return nil
}

// Run owns the Wayland connection and every proxy created from it until the
// context is cancelled or the compositor closes the surface. No other goroutine
// may call a Wayland proxy.
func Run(ctx context.Context, cfg config.Config, callbacks Callbacks) (err error) {
	if err := callbacks.validate(); err != nil {
		return err
	}
	if cfg.Bar.Gap < 0 {
		return fmt.Errorf("wayland: gap %d is negative", cfg.Bar.Gap)
	}
	if body := cfg.Bar.Height - 2*cfg.Bar.Gap; body <= 0 {
		return fmt.Errorf("wayland: height %d with gap %d leaves a body of %d",
			cfg.Bar.Height, cfg.Bar.Gap, body)
	}

	o := &owner{
		cb:    callbacks,
		hosts: newHostSet(),
		cfg:   &cfg,
	}
	defer func() { err = errors.Join(err, o.shutdown()) }()

	// Bars are created by the readiness transition, not here: an output is
	// only ready once it has reported both a done and a connector name.
	if err := o.connect(); err != nil {
		return err
	}
	return o.loop(ctx)
}

type owner struct {
	cb Callbacks

	display  *client.Display
	registry *client.Registry
	rs       *registryState

	compositor *client.Compositor
	shm        *client.Shm
	seat       *client.Seat
	layerShell *layershell.ZwlrLayerShellV1
	scaleMgr   *fractionalscale.WpFractionalScaleManagerV1
	viewporter *viewporter.WpViewporter
	pointer    *client.Pointer

	// hosts holds every bound wl_output, keyed by registry global name. Every
	// ready host carries its own bar.
	hosts *hostSet
	// focus identifies the bar the pointer is on and the latest logical
	// coordinates, replacing the single boolean the proof used.
	focus pointerFocus
	// cfg is the live configuration. It is replaced only after a candidate has
	// resolved for every connected output.
	cfg *config.Config

	tooltip         *tooltipSurface
	tooltipRenderer *render.TextRenderer

	cleanup cleanupStack
	fatal   error
	closed  bool
}

// fail records the first error raised inside an event handler. Handlers cannot
// return errors, so the loop checks this after every dispatch.
func (o *owner) fail(err error) {
	if err != nil && o.fatal == nil {
		o.fatal = err
	}
}

func (o *owner) connect() error {
	display, err := client.Connect("")
	if err != nil {
		return fmt.Errorf("wayland: connect: %w", err)
	}
	o.display = display

	// Installed before any other request so protocol errors carry context.
	display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		o.fail(fmt.Errorf("wayland: protocol error %d on %T: %s", e.Code, e.ObjectId, e.Message))
	})

	registry, err := display.GetRegistry()
	if err != nil {
		return fmt.Errorf("wayland: get registry: %w", err)
	}
	o.registry = registry
	o.rs = newRegistryState()

	// Outputs are bound in the handler itself, so an output present at startup
	// and one hotplugged later travel exactly the same path.
	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		version, used := o.rs.addGlobal(e.Name, e.Interface, e.Version)
		if !used || e.Interface != "wl_output" {
			return
		}
		o.bindOutput(e.Name, version)
	})
	registry.SetGlobalRemoveHandler(func(e client.RegistryGlobalRemoveEvent) {
		o.rs.removeGlobal(e.Name)
		if h, ok := o.hosts.remove(e.Name); ok {
			o.fail(o.teardownHost(h))
			if o.cb.DropHost != nil {
				o.cb.DropHost(h.global)
			}
		}
	})

	// The first roundtrip delivers the globals.
	if err := display.Roundtrip(); err != nil {
		return fmt.Errorf("wayland: first roundtrip: %w", err)
	}
	if missing := o.rs.missingRequired(); len(missing) > 0 {
		return fmt.Errorf("wayland: the compositor does not offer %v", missing)
	}
	if err := o.bindGlobals(); err != nil {
		return err
	}
	// The second roundtrip delivers wl_output.name and the wl_shm formats.
	if err := display.Roundtrip(); err != nil {
		return fmt.Errorf("wayland: second roundtrip: %w", err)
	}
	if o.fatal != nil {
		return o.fatal
	}
	if !o.rs.hasFormat(formatARGB8888) {
		return errors.New("wayland: the compositor does not advertise the ARGB8888 shm format")
	}
	return nil
}

func (o *owner) bindGlobals() error {
	ctx := o.display.Context()

	o.compositor = client.NewCompositor(ctx)
	o.shm = client.NewShm(ctx)
	o.seat = client.NewSeat(ctx)
	o.layerShell = layershell.NewZwlrLayerShellV1(ctx)
	o.scaleMgr = fractionalscale.NewWpFractionalScaleManagerV1(ctx)
	o.viewporter = viewporter.NewWpViewporter(ctx)

	singletons := []struct {
		iface string
		proxy client.Proxy
	}{
		{"wl_compositor", o.compositor},
		{"wl_shm", o.shm},
		{"wl_seat", o.seat},
		{"zwlr_layer_shell_v1", o.layerShell},
		{"wp_fractional_scale_manager_v1", o.scaleMgr},
		{"wp_viewporter", o.viewporter},
	}
	for _, s := range singletons {
		entry := o.rs.singletons[s.iface]
		if err := o.registry.Bind(entry.global, s.iface, entry.version, s.proxy); err != nil {
			return fmt.Errorf("wayland: bind %s: %w", s.iface, err)
		}
	}
	o.cleanup.push("globals", o.destroyGlobals)

	o.shm.SetFormatHandler(func(e client.ShmFormatEvent) { o.rs.addFormat(e.Format) })
	o.seat.SetCapabilitiesHandler(o.onSeatCapabilities)

	// Outputs advertised before this point were recorded by the registry
	// handler but could not bind, because binding needs the display context
	// that only exists once the globals are bound. Bind them now; outputs
	// announced later bind directly in the handler.
	for _, entry := range o.rs.outputs {
		o.bindOutput(entry.global, entry.version)
	}
	return nil
}

// bindOutput binds one wl_output and creates its host. A bind failure is host
// scoped: the shell keeps running with its remaining outputs.
func (o *owner) bindOutput(global, version uint32) {
	if o.display == nil || o.registry == nil {
		return // globals are not bound yet; bindGlobals will revisit this one
	}
	h, created := o.hosts.add(global, nil)
	if !created {
		return
	}
	proxy := client.NewOutput(o.display.Context())
	if err := o.registry.Bind(global, "wl_output", version, proxy); err != nil {
		o.hosts.remove(global)
		return
	}
	h.proxy = proxy
	h.cleanup.push("output", proxy.Release)
	o.attachOutputHandlers(h)
}

// hostBecameReady creates the bar for a host that just gained done and a name.
func (o *owner) hostBecameReady(h *OutputHost) error {
	if o.cfg != nil {
		h.policy = o.cfg.ForConnector(h.connector)
		h.opaqueBackground = o.cfg.Theme.BackgroundOpaque()
	}
	if !h.policy.Enabled {
		h.state = hostIdle
		return nil
	}
	app, err := o.cb.NewHost(h.global, h.connector)
	if err != nil {
		return err
	}
	if err := app.validate(h.connector); err != nil {
		if o.cb.DropHost != nil {
			o.cb.DropHost(h.global)
		}
		return err
	}
	h.app = app
	if err := o.createBar(h); err != nil {
		if o.cb.DropHost != nil {
			o.cb.DropHost(h.global)
		}
		h.app = HostCallbacks{}
		return err
	}
	return nil
}

func (o *owner) destroyGlobals() error {
	var errs []error
	if o.viewporter != nil {
		errs = append(errs, o.viewporter.Destroy())
	}
	if o.scaleMgr != nil {
		errs = append(errs, o.scaleMgr.Destroy())
	}
	if o.layerShell != nil {
		errs = append(errs, o.layerShell.Destroy())
	}
	if o.pointer != nil {
		errs = append(errs, o.pointer.Release())
		o.pointer = nil
	}
	return errors.Join(errs...)
}

func (o *owner) onSeatCapabilities(e client.SeatCapabilitiesEvent) {
	hasPointer := e.Capabilities&uint32(client.SeatCapabilityPointer) != 0
	switch {
	case hasPointer && o.pointer == nil:
		pointer, err := o.seat.GetPointer()
		if err != nil {
			o.fail(fmt.Errorf("wayland: get pointer: %w", err))
			return
		}
		o.pointer = pointer
		pointer.SetEnterHandler(func(e client.PointerEnterEvent) {
			if h, ok := o.hostBySurface(e.Surface); ok {
				o.enterSurface(h, e.SurfaceX, e.SurfaceY, e.Serial)
			}
		})
		pointer.SetLeaveHandler(func(e client.PointerLeaveEvent) {
			if h, ok := o.hostBySurface(e.Surface); ok {
				o.leaveSurface(h)
			}
		})
		pointer.SetMotionHandler(func(e client.PointerMotionEvent) {
			if o.focus.host == nil {
				return
			}
			o.focus.x, o.focus.y = e.SurfaceX, e.SurfaceY
			o.deliver(o.focus.host, Event{
				Kind: EventPointerMotion, X: e.SurfaceX, Y: e.SurfaceY,
			})
		})
		pointer.SetButtonHandler(func(e client.PointerButtonEvent) {
			if o.focus.host == nil {
				return
			}
			kind := EventPointerRelease
			if e.State == uint32(client.PointerButtonStatePressed) {
				kind = EventPointerPress
			}
			// A button event carries no coordinates, so the focus position is
			// what the press acts on.
			o.deliver(o.focus.host, Event{
				Kind: kind, Button: e.Button, Serial: e.Serial,
				X: o.focus.x, Y: o.focus.y,
			})
		})
	case !hasPointer && o.pointer != nil:
		// Capability loss must reset focus, not merely release the proxy.
		o.clearFocus()
		o.fail(o.pointer.Release())
		o.pointer = nil
	}
}

// createBar builds one host's layer surface and its fractional-scale and
// viewport objects, then performs the fixed initial sequence: set properties,
// commit once without a buffer, and wait for the first configure.
func (o *owner) createBar(h *OutputHost) error {
	switch h.state {
	case hostReady, hostIdle, hostClosed:
	default:
		return nil // already creating, configuring or mapped
	}

	h.state = hostCreating

	surface, err := o.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("wayland: create surface for %s: %w", h.connector, err)
	}
	h.surface = surface
	h.cleanup.push("surface", surface.Destroy)

	layer, err := o.layerShell.GetLayerSurface(surface, h.proxy,
		uint32(layershell.ZwlrLayerShellV1LayerTop), namespace)
	if err != nil {
		return fmt.Errorf("wayland: get layer surface for %s: %w", h.connector, err)
	}
	h.layer = layer
	h.cleanup.push("layer-surface", layer.Destroy)

	if err := o.applyGeometryRequests(h); err != nil {
		return err
	}
	layer.SetConfigureHandler(func(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
		if h.alive {
			o.onConfigure(h, e)
		}
	})
	layer.SetClosedHandler(func(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
		if h.alive {
			o.fail(o.onLayerClosed(h))
		}
	})

	scale, err := o.scaleMgr.GetFractionalScale(surface)
	if err != nil {
		return fmt.Errorf("wayland: get fractional scale: %w", err)
	}
	h.scale = scale
	h.cleanup.push("fractional-scale", scale.Destroy)
	scale.SetPreferredScaleHandler(func(e fractionalscale.WpFractionalScaleV1PreferredScaleEvent) {
		if h.alive {
			o.onPreferredScale(h, e)
		}
	})

	viewport, err := o.viewporter.GetViewport(surface)
	if err != nil {
		return fmt.Errorf("wayland: get viewport: %w", err)
	}
	h.viewport = viewport
	h.cleanup.push("viewport", viewport.Destroy)

	// One commit with no buffer attached, then the compositor configures.
	if err := surface.Commit(); err != nil {
		return fmt.Errorf("wayland: initial commit for %s: %w", h.connector, err)
	}
	return nil
}

// applyGeometryRequests sets size, anchor, exclusive zone and keyboard policy.
// The surface height equals the exclusive zone, and the gap lives inside the
// surface with a zero layer margin, so the screen edge stays clickable.
func (o *owner) applyGeometryRequests(h *OutputHost) error {
	height := h.surfaceHeight()
	if height <= 0 || height > math.MaxInt32 {
		return fmt.Errorf("wayland: surface height %d is unusable", height)
	}
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
		layershell.ZwlrLayerSurfaceV1AnchorLeft |
		layershell.ZwlrLayerSurfaceV1AnchorRight)
	// Width 0 asks the compositor for the anchored width.
	if err := h.layer.SetSize(0, uint32(height)); err != nil {
		return err
	}
	if err := h.layer.SetAnchor(anchor); err != nil {
		return err
	}
	if err := h.layer.SetMargin(0, 0, 0, 0); err != nil {
		return err
	}
	if err := h.layer.SetExclusiveZone(int32(height)); err != nil {
		return err
	}
	return h.layer.SetKeyboardInteractivity(
		uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone))
}

// onLayerClosed destroys the role and rebuilds it if the budget permits. The
// wl_output survives, so the host keeps its metadata and its identity.
//
// The budget exists because closed has two very different causes: a transient
// compositor reset should self-heal, while a persistent refusal must not become
// a create/destroy livelock.
func (o *owner) onLayerClosed(h *OutputHost) error {
	if err := o.teardownSurface(h); err != nil {
		return err
	}
	h.state = hostClosed
	now := time.Now()
	if !h.mayRecreate(now) {
		fmt.Fprintf(os.Stderr,
			"sysc-shell: bar on %s closed %d times; leaving it down\n", h.connector, h.closeAttempts)
		return nil
	}
	h.recordCloseAttempt()
	return o.createBar(h)
}

// onConfigure acknowledges every configure before any buffer is attached.
func (o *owner) onConfigure(h *OutputHost, e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
	if err := h.layer.AckConfigure(e.Serial); err != nil {
		o.fail(fmt.Errorf("wayland: ack configure: %w", err))
		return
	}
	changed := h.ss.configure(int(e.Width), int(e.Height))
	h.ss.acknowledge()
	h.state = hostConfiguring
	if changed || h.current == nil {
		o.fail(o.reconfigure(h))
	}
}

// onPreferredScale handles wp_fractional_scale_v1.preferred_scale. It is not
// ordered against configure and may arrive alone when the output scale changes
// at an unchanged logical size, which still retires the physical buffers.
//
// The event arrives on this host's own fractional-scale object, so a scale
// change reallocates this host's buffers and touches no other host.
func (o *owner) onPreferredScale(h *OutputHost, e fractionalscale.WpFractionalScaleV1PreferredScaleEvent) {
	if !h.ss.preferredScale(ui.Scale120(e.Scale)) {
		return
	}
	if h.ss.eligible() {
		o.fail(o.reconfigure(h))
	}
}

// reconfigure retires the host's buffer generation and allocates a new one at
// its current physical size.
func (o *owner) reconfigure(h *OutputHost) error {
	width, height, err := h.bufferSize()
	if err != nil {
		return err
	}

	// The viewport destination is the logical configure size. The buffer scale
	// stays at its default of 1, and no source rectangle is set.
	if err := h.viewport.SetDestination(int32(h.ss.logicalWidth), int32(h.ss.logicalHeight)); err != nil {
		return fmt.Errorf("wayland: set viewport destination: %w", err)
	}

	if err := o.applyHostRegions(h, h.policy, h.opaqueBackground); err != nil {
		return fmt.Errorf("wayland: set regions: %w", err)
	}

	if err := h.app.Configure(h.ss.logicalWidth, h.ss.logicalHeight, int(h.ss.scale120)); err != nil {
		return err
	}

	// The outstanding frame callback belongs to a retired generation.
	if err := h.dropFrameCallback(); err != nil {
		return err
	}
	if h.current != nil {
		h.retiring = append(h.retiring, h.current)
		h.current = nil
	}
	o.sweepRetired(h)

	h.genID++
	gen, err := newGeneration(o.shm, h.genID, width, height)
	if err != nil {
		return err
	}
	for slot := range gen.slots {
		slot, gen := slot, gen
		gen.slots[slot].SetReleaseHandler(func(client.BufferReleaseEvent) {
			o.onBufferRelease(h, gen, slot)
		})
	}
	h.current = gen
	h.sched.Configure(int(width), int(height))
	h.state = hostMapped
	h.mappedSince = time.Now()
	return nil
}

// onBufferRelease frees a slot. A frame callback never implies a release: Niri
// delivers wl_callback.done while the submitted buffer is still held.
//
// Release accounting stays active after a host stops being alive, because a
// retired generation must observe its releases to become unmappable.
func (o *owner) onBufferRelease(h *OutputHost, gen *generation, slot int) {
	o.fail(gen.retire.released())
	if gen == h.current {
		o.fail(h.sched.Release(slot))
	}
	o.sweepRetired(h)
}

// sweepRetired unmaps every retired generation whose buffers have all been
// released.
func (o *owner) sweepRetired(h *OutputHost) {
	kept := h.retiring[:0]
	for _, gen := range h.retiring {
		if gen.retire.freeable() {
			o.fail(gen.destroy())
			continue
		}
		kept = append(kept, gen)
	}
	h.retiring = kept
}

// renderJob paints one slot of one host and submits it.
func (o *owner) renderJob(h *OutputHost, job render.Job) error {
	gen := h.current
	if gen == nil {
		return errors.New("wayland: render requested with no buffer generation")
	}

	if err := h.app.Render(gen.pixels(job.Slot), int(gen.width), int(gen.height), int(gen.stride)); err != nil {
		return err
	}
	if err := h.surface.Attach(gen.slots[job.Slot], 0, 0); err != nil {
		return fmt.Errorf("wayland: attach: %w", err)
	}
	// Damage is submitted in buffer pixels, never in surface units.
	if err := h.surface.DamageBuffer(0, 0, gen.width, gen.height); err != nil {
		return fmt.Errorf("wayland: damage: %w", err)
	}

	callback, err := h.surface.Frame()
	if err != nil {
		return fmt.Errorf("wayland: frame: %w", err)
	}
	h.frameCallback = callback
	callback.SetDoneHandler(func(client.CallbackDoneEvent) {
		if !h.alive || callback != h.frameCallback {
			return // a callback from a retired generation or a dead host
		}
		h.frameCallback = nil
		o.fail(h.sched.Frame())
	})

	if err := h.surface.Commit(); err != nil {
		return fmt.Errorf("wayland: commit: %w", err)
	}
	gen.retire.attached()
	return h.sched.Submitted(job.Slot)
}

// nextJob asks each live host for work in arrival order.
func (o *owner) nextJob() (*OutputHost, render.Decision, render.Job) {
	for _, h := range o.hosts.each() {
		if !h.alive || h.surface == nil {
			continue
		}
		if d, job := h.sched.Next(); d == render.DecisionRender {
			return h, d, job
		}
	}
	return nil, render.DecisionWait, render.Job{}
}

// invalidate marks hosts dirty. A zero global invalidates every bar, so one
// output's state change never redraws another output's bar. Two bars sharing a
// connector during reconnect overlap stay separately addressable.
func (o *owner) invalidate(inv Invalidation) {
	for _, h := range o.hosts.each() {
		if !h.alive {
			continue
		}
		if inv.Global == 0 || h.global == inv.Global {
			h.sched.Invalidate()
		}
	}
}

// reloadConfig re-reads and applies the configuration file. A failure at any
// stage leaves the previous configuration live and applies nothing.
func (o *owner) reloadConfig() error {
	if o.cb.ConfigPath == "" {
		return nil
	}
	cfg, err := config.Load(o.cb.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sysc-shell: reload rejected: %v\n", err)
		return nil
	}
	prepared, err := o.prepareConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sysc-shell: reload rejected: %v\n", err)
		return nil
	}
	if err := o.applyPreparedConfig(prepared); err != nil {
		return fmt.Errorf("wayland: apply reloaded config: %w", err)
	}
	if err := o.hideTooltip(); err != nil {
		return err
	}
	return nil
}

type preparedHostConfig struct {
	host             *OutputHost
	policy           config.Bar
	opaqueBackground bool
	app              HostCallbacks
}

type preparedOwnerConfig struct {
	cfg    config.Config
	hosts  []preparedHostConfig
	commit func()
}

// abandon releases what a prepared candidate acquired, for an owner-side
// failure after the application already prepared it.
func abandon(prepared PreparedConfig, err error) (preparedOwnerConfig, error) {
	prepared.Rollback()
	return preparedOwnerConfig{}, err
}

// prepareConfig resolves and builds every ready host before changing live
// owner or shell state.
func (o *owner) prepareConfig(cfg config.Config) (preparedOwnerConfig, error) {
	live := o.hosts.each()
	ready := make([]*OutputHost, 0, len(live))
	connectors := make([]string, 0, len(live))
	for _, h := range live {
		if !h.alive || !h.ready() {
			continue
		}
		ready = append(ready, h)
		connectors = append(connectors, h.connector)
	}
	policies, err := config.Resolve(cfg, connectors)
	if err != nil {
		return preparedOwnerConfig{}, err
	}

	enabled := make([]HostIdentity, 0, len(ready))
	for i, h := range ready {
		if policies[i].Enabled {
			enabled = append(enabled, HostIdentity{Global: h.global, Connector: h.connector})
		}
	}
	prepared, err := o.cb.PrepareConfig(cfg, enabled)
	if err != nil {
		return preparedOwnerConfig{}, err
	}
	if prepared.Commit == nil {
		return preparedOwnerConfig{}, errors.New("wayland: prepared config has no commit function")
	}
	if prepared.Rollback == nil {
		return preparedOwnerConfig{}, errors.New("wayland: prepared config has no rollback function")
	}

	updates := make([]preparedHostConfig, 0, len(ready))
	for i, h := range ready {
		update := preparedHostConfig{
			host:             h,
			policy:           policies[i],
			opaqueBackground: cfg.Theme.BackgroundOpaque(),
		}
		if update.policy.Enabled {
			app, ok := prepared.Hosts[h.global]
			if !ok {
				return abandon(prepared,
					fmt.Errorf("wayland: prepared config omitted %s", h.connector))
			}
			if err := app.validate(h.connector); err != nil {
				return abandon(prepared, err)
			}
			if h.state == hostMapped {
				if err := app.Configure(h.ss.logicalWidth, h.ss.logicalHeight, int(h.ss.scale120)); err != nil {
					return abandon(prepared, fmt.Errorf(
						"wayland: configure prepared replacement for %s: %w", h.connector, err))
				}
			}
			update.app = app
		}
		updates = append(updates, update)
	}
	return preparedOwnerConfig{cfg: cfg, hosts: updates, commit: prepared.Commit}, nil
}

// applyConfig resolves every live host's policy before committing, then applies
// the result in one pass.
//
// Resolving first is what keeps the outputs consistent: a candidate valid as a
// document but unresolvable for one connected output is rejected whole, so the
// shell can never end up with half its bars on new policy and half on old.
func (o *owner) applyConfig(cfg config.Config) error {
	prepared, err := o.prepareConfig(cfg)
	if err != nil {
		return err
	}
	return o.applyPreparedConfig(prepared)
}

func (o *owner) applyPreparedConfig(prepared preparedOwnerConfig) error {
	for _, update := range prepared.hosts {
		h, bar := update.host, update.policy
		h.policy = bar
		h.opaqueBackground = update.opaqueBackground
		switch {
		case !bar.Enabled:
			if h.surface != nil {
				if err := o.teardownSurface(h); err != nil {
					return err
				}
			}
			h.state = hostIdle
			h.app = HostCallbacks{}
		case bar.Enabled && h.surface == nil && h.ready():
			h.app = update.app
			if err := o.createBar(h); err != nil {
				return err
			}
		case bar.Enabled && h.surface != nil:
			h.app = update.app
			// Geometry and anchor changes are ordinary layer-surface requests
			// followed by a configure, which is cheaper and more correct than
			// destroying and rebuilding the role.
			if err := o.applyGeometryRequests(h); err != nil {
				return err
			}
			if refreshRegionsOnReload(h) {
				if err := o.applyHostRegions(h, bar, update.opaqueBackground); err != nil {
					return fmt.Errorf("wayland: refresh regions for %s: %w", h.connector, err)
				}
			}
			if err := h.surface.Commit(); err != nil {
				return err
			}
			if h.state == hostMapped {
				h.sched.Invalidate()
			}
		}
	}
	o.cfg = &prepared.cfg
	prepared.commit()
	return nil
}

func refreshRegionsOnReload(h *OutputHost) bool { return h.state == hostMapped }

// teardownSurface releases everything scoped to one bar and leaves the host and
// its wl_output intact, so a closed bar can be rebuilt without a registry cycle.
func (o *owner) teardownSurface(h *OutputHost) error {
	var errs []error

	h.sched.Close()
	if err := h.dropFrameCallback(); err != nil {
		errs = append(errs, err)
	}
	if h.current != nil {
		h.current.retire.destroy()
		h.retiring = append(h.retiring, h.current)
		h.current = nil
	}
	for _, gen := range h.retiring {
		gen.retire.destroy()
		if err := gen.destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	h.retiring = nil

	// clearFocus delivers a leave, so pressed-node state does not survive into
	// a recreated surface.
	if o.focus.host == h {
		o.clearFocus()
	}
	// Unwind viewport, fractional-scale, layer surface and wl_surface; the
	// output step stays so the host keeps its wl_output.
	if _, err := h.cleanup.unwindTo("output"); err != nil {
		errs = append(errs, err)
	}
	h.surface, h.layer, h.scale, h.viewport = nil, nil, nil, nil
	h.ss = newSurfaceState()
	h.sched = render.NewScheduler()
	return errors.Join(errs...)
}

// loop drives the owner goroutine: render when a scheduler offers work, then
// wait on the Wayland socket and the wake pipe.
func (o *owner) loop(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	wake, err := newWakePipe()
	if err != nil {
		return err
	}
	defer wake.close()
	wake.bridge(runCtx, o.cb.Invalidations, o.cb.Reloads, o.cb.Tooltips)

	for {
		if o.fatal != nil {
			return o.fatal
		}
		if o.closed || runCtx.Err() != nil {
			return nil
		}

		if h, decision, job := o.nextJob(); decision == render.DecisionRender {
			if err := o.renderJob(h, job); err != nil {
				return err
			}
			continue
		}

		waylandReady, wakeReady, err := o.poll(wake.read, -1)
		if err != nil {
			return err
		}
		if wakeReady {
			wake.drain()
			if wake.takeReload() {
				if err := o.reloadConfig(); err != nil {
					return err
				}
			}
			if req, ok := wake.takeTooltip(); ok {
				o.handleTooltip(req)
			}
			for _, inv := range wake.take() {
				o.invalidate(inv)
			}
		}
		if !waylandReady {
			continue
		}
		if err := o.dispatchAll(wake.read); err != nil {
			return err
		}
	}
}

// dispatchAll handles one message per readiness, then drains with zero-timeout
// polls. Dispatch blocks when no message is pending, so it is only called after
// poll reports the socket readable.
func (o *owner) dispatchAll(wakeFD int) error {
	if err := o.display.Context().Dispatch(); err != nil {
		return err
	}
	for o.fatal == nil && !o.closed {
		ready, _, err := o.poll(wakeFD, 0)
		if err != nil {
			return err
		}
		if !ready {
			return nil
		}
		if err := o.display.Context().Dispatch(); err != nil {
			return err
		}
	}
	return nil
}

// poll waits on the Wayland socket and the wake pipe. The Wayland descriptor is
// used only inside the ControlFD callback and never retained.
func (o *owner) poll(wakeFD, timeout int) (waylandReady, wakeReady bool, err error) {
	controlErr := o.display.Context().ControlFD(func(fd int) error {
		fds := []unix.PollFd{
			{Fd: int32(fd), Events: unix.POLLIN},
			{Fd: int32(wakeFD), Events: unix.POLLIN},
		}
		for {
			n, pollErr := unix.Poll(fds, timeout)
			if errors.Is(pollErr, unix.EINTR) {
				continue
			}
			if pollErr != nil {
				return pollErr
			}
			if n == 0 {
				return nil
			}
			const readable = unix.POLLIN | unix.POLLHUP | unix.POLLERR
			waylandReady = fds[0].Revents&readable != 0
			wakeReady = fds[1].Revents&readable != 0
			return nil
		}
	})
	if controlErr != nil {
		return false, false, fmt.Errorf("wayland: poll: %w", controlErr)
	}
	return waylandReady, wakeReady, nil
}

// teardownHost releases everything scoped to one host, child-to-parent, and
// reports every failure. Liveness clears first so events already queued in the
// dispatch stream are ignored.
func (o *owner) teardownHost(h *OutputHost) error {
	h.alive = false
	var errs []error

	if o.tooltip != nil && o.tooltip.host == h {
		if err := o.hideTooltip(); err != nil {
			errs = append(errs, err)
		}
	}

	h.sched.Close()
	if h.current != nil {
		h.current.retire.destroy()
		h.retiring = append(h.retiring, h.current)
		h.current = nil
	}
	for _, gen := range h.retiring {
		gen.retire.destroy()
		if err := gen.destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	h.retiring = nil

	if err := h.dropFrameCallback(); err != nil {
		errs = append(errs, err)
	}
	if o.focus.host == h {
		o.clearFocus()
	}
	if _, err := h.cleanup.unwind(); err != nil {
		errs = append(errs, err)
	}
	h.surface, h.layer, h.scale, h.viewport = nil, nil, nil, nil
	return errors.Join(errs...)
}

// shutdown destroys every host child-to-parent, then the shared globals, then
// flushes the destructor requests once and closes the display.
func (o *owner) shutdown() error {
	var errs []error
	if err := o.hideTooltip(); err != nil {
		errs = append(errs, err)
	}
	for _, h := range o.hosts.each() {
		if err := o.teardownHost(h); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := o.cleanup.unwind(); err != nil {
		errs = append(errs, err)
	}
	if o.display != nil {
		if err := o.display.Roundtrip(); err != nil {
			errs = append(errs, fmt.Errorf("wayland: shutdown roundtrip: %w", err))
		}
		if err := o.display.Context().Close(); err != nil {
			errs = append(errs, fmt.Errorf("wayland: close display: %w", err))
		}
		o.display = nil
	}
	return errors.Join(errs...)
}
