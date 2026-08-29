package wayland

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

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
	Kind   EventKind
	X, Y   int
	Button uint32
}

// Options configures the layer surface.
type Options struct {
	// Output is the wl_output.name connector to place the surface on. Empty
	// selects the first output the compositor advertised.
	Output string
	// Height is the logical height and exclusive zone of the surface.
	Height int
}

// Callbacks are the concrete hooks the application supplies. This is a struct
// rather than an interface because there is one implementation.
type Callbacks struct {
	// Configure reports the logical size from zwlr_layer_surface_v1.configure
	// and the scale as a numerator over 120; 120 means scale 1.0.
	Configure func(logicalWidth, logicalHeight, scale120 int) error
	// Render fills the physical buffer. Width and height are buffer pixels.
	Render func(pixels []byte, width, height, stride int) error
	// Handle consumes a pointer event and reports whether state changed.
	Handle func(Event) bool
	// Invalidations is owned by the caller. Run only receives from it and
	// never closes it.
	Invalidations <-chan struct{}
}

func (c Callbacks) validate() error {
	switch {
	case c.Configure == nil:
		return errors.New("wayland: Callbacks.Configure is nil")
	case c.Render == nil:
		return errors.New("wayland: Callbacks.Render is nil")
	case c.Handle == nil:
		return errors.New("wayland: Callbacks.Handle is nil")
	}
	return nil
}

// Run owns the Wayland connection and every proxy created from it until the
// context is cancelled or the compositor closes the surface. No other goroutine
// may call a Wayland proxy.
func Run(ctx context.Context, options Options, callbacks Callbacks) (err error) {
	if err := callbacks.validate(); err != nil {
		return err
	}
	if options.Height <= 0 {
		return fmt.Errorf("wayland: height %d is not positive", options.Height)
	}

	o := &owner{
		options: options,
		cb:      callbacks,
		hosts:   newHostSet(),
	}
	defer func() { err = errors.Join(err, o.shutdown()) }()

	if err := o.connect(); err != nil {
		return err
	}
	if err := o.createSurface(); err != nil {
		return err
	}
	return o.loop(ctx)
}

type owner struct {
	options Options
	cb      Callbacks

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

	// hosts holds every bound wl_output, keyed by registry global name. Only
	// the selected host carries a surface until multi-output creation lands.
	hosts    *hostSet
	selected *OutputHost
	// focus is the host whose surface the pointer is currently on, replacing
	// the single boolean the proof used. Nil means the pointer is on no bar.
	focus *OutputHost

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

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		o.rs.addGlobal(e.Name, e.Interface, e.Version)
	})
	registry.SetGlobalRemoveHandler(func(e client.RegistryGlobalRemoveEvent) {
		if _, ok := o.rs.removeGlobal(e.Name); ok && o.selected != nil && o.selected.global == e.Name {
			o.closed = true
			o.selected.sched.Close()
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

	// Bind every output and give each one a host, so wl_output.name can be
	// matched against --output. Host identity is the registry global name.
	for _, entry := range o.rs.outputs {
		proxy := client.NewOutput(ctx)
		if err := o.registry.Bind(entry.global, "wl_output", entry.version, proxy); err != nil {
			return fmt.Errorf("wayland: bind wl_output %d: %w", entry.global, err)
		}
		h, _ := o.hosts.add(entry.global, proxy)
		h.cleanup.push("output", proxy.Release)
		host := h
		proxy.SetNameHandler(func(e client.OutputNameEvent) {
			host.connector = e.Name
			o.rs.setOutputName(host.global, e.Name)
		})
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
			h, ok := o.hostBySurface(e.Surface)
			if !ok {
				return
			}
			o.focus = h
			o.deliver(h, Event{Kind: EventPointerEnter, X: int(e.SurfaceX), Y: int(e.SurfaceY)})
		})
		pointer.SetLeaveHandler(func(e client.PointerLeaveEvent) {
			h, ok := o.hostBySurface(e.Surface)
			if !ok || o.focus != h {
				return
			}
			o.focus = nil
			o.deliver(h, Event{Kind: EventPointerLeave})
		})
		pointer.SetMotionHandler(func(e client.PointerMotionEvent) {
			if o.focus == nil {
				return
			}
			o.deliver(o.focus, Event{Kind: EventPointerMotion, X: int(e.SurfaceX), Y: int(e.SurfaceY)})
		})
		pointer.SetButtonHandler(func(e client.PointerButtonEvent) {
			if o.focus == nil {
				return
			}
			kind := EventPointerRelease
			if e.State == uint32(client.PointerButtonStatePressed) {
				kind = EventPointerPress
			}
			o.deliver(o.focus, Event{Kind: kind, Button: e.Button})
		})
	case !hasPointer && o.pointer != nil:
		if o.focus != nil {
			o.deliver(o.focus, Event{Kind: EventPointerLeave})
			o.focus = nil
		}
		o.fail(o.pointer.Release())
		o.pointer = nil
	}
}

// hostBySurface resolves a wl_surface to the host that owns it.
func (o *owner) hostBySurface(surface *client.Surface) (*OutputHost, bool) {
	if surface == nil {
		return nil, false
	}
	for _, h := range o.hosts.each() {
		if h.surface == surface {
			return h, true
		}
	}
	return nil, false
}

// deliver hands a pointer event to one bar and invalidates it when the
// application reports that its state changed.
func (o *owner) deliver(h *OutputHost, e Event) {
	if h == nil || !h.alive || h.app.Handle == nil {
		return
	}
	if h.app.Handle(e) {
		h.sched.Invalidate()
	}
}

// createSurface builds the layer surface and its fractional-scale and viewport
// objects for the selected host, then performs the fixed initial sequence: set
// properties, commit once without a buffer, and wait for the first configure.
func (o *owner) createSurface() error {
	if err := o.chooseOutput(); err != nil {
		return err
	}
	h := o.selected
	h.app = HostCallbacks{
		Configure: o.cb.Configure,
		Render:    o.cb.Render,
		Handle:    o.cb.Handle,
	}
	h.state = hostCreating

	surface, err := o.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("wayland: create surface: %w", err)
	}
	h.surface = surface
	h.cleanup.push("surface", surface.Destroy)

	layer, err := o.layerShell.GetLayerSurface(surface, h.proxy,
		uint32(layershell.ZwlrLayerShellV1LayerTop), namespace)
	if err != nil {
		return fmt.Errorf("wayland: get layer surface: %w", err)
	}
	h.layer = layer
	h.cleanup.push("layer-surface", layer.Destroy)

	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
		layershell.ZwlrLayerSurfaceV1AnchorLeft |
		layershell.ZwlrLayerSurfaceV1AnchorRight)
	height := o.options.Height
	if height > math.MaxInt32 {
		return fmt.Errorf("wayland: height %d overflows", height)
	}
	// Width 0 asks the compositor for the anchored width.
	if err := layer.SetSize(0, uint32(height)); err != nil {
		return err
	}
	if err := layer.SetAnchor(anchor); err != nil {
		return err
	}
	if err := layer.SetExclusiveZone(int32(height)); err != nil {
		return err
	}
	if err := layer.SetKeyboardInteractivity(
		uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone)); err != nil {
		return err
	}
	layer.SetConfigureHandler(func(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
		if h.alive {
			o.onConfigure(h, e)
		}
	})
	layer.SetClosedHandler(func(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
		if !h.alive {
			return
		}
		o.closed = true
		h.sched.Close()
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
		return fmt.Errorf("wayland: initial commit: %w", err)
	}
	return nil
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

// invalidate marks every live bar dirty.
func (o *owner) invalidate() {
	for _, h := range o.hosts.each() {
		if h.alive && h.surface != nil {
			h.sched.Invalidate()
		}
	}
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
	wake.bridge(runCtx, o.cb.Invalidations)

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
			o.invalidate()
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
	if o.focus == h {
		o.focus = nil
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
	for _, h := range o.hosts.each() {
		if err := o.teardownHost(h); err != nil {
			errs = append(errs, err)
		}
	}
	o.selected = nil

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
