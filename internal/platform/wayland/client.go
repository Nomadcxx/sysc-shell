package wayland

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/fractionalscale"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/viewporter"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

// namespace identifies the proof's layer surface to the compositor.
const namespace = "sysc-shell:proof"

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
// rather than an interface because the proof has exactly one implementation.
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
		state:   newSurfaceState(),
		sched:   render.NewScheduler(),
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

	compositor     *client.Compositor
	shm            *client.Shm
	seat           *client.Seat
	layerShell     *layershell.ZwlrLayerShellV1
	scaleMgr       *fractionalscale.WpFractionalScaleManagerV1
	viewporter     *viewporter.WpViewporter
	output         *client.Output
	outputProxies  []outputProxy
	selectedGlobal uint32
	connector      string
	pointer        *client.Pointer

	surface  *client.Surface
	layer    *layershell.ZwlrLayerSurfaceV1
	scale    *fractionalscale.WpFractionalScaleV1
	viewport *viewporter.WpViewport

	state *surfaceState
	sched *render.Scheduler

	current  *generation
	retiring []*generation
	genID    int

	frameCallback *client.Callback
	pointerInside bool

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
		if out, ok := o.rs.removeGlobal(e.Name); ok && o.output != nil && out.global == o.selectedGlobal {
			o.closed = true
			o.sched.Close()
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

	// Bind every output so wl_output.name can be matched against --output.
	for _, entry := range o.rs.outputs {
		proxy := client.NewOutput(ctx)
		if err := o.registry.Bind(entry.global, "wl_output", entry.version, proxy); err != nil {
			return fmt.Errorf("wayland: bind wl_output %d: %w", entry.global, err)
		}
		global := entry.global
		proxy.SetNameHandler(func(e client.OutputNameEvent) { o.rs.setOutputName(global, e.Name) })
		o.outputProxies = append(o.outputProxies, outputProxy{global: global, proxy: proxy})
	}
	o.cleanup.push("output", o.releaseOutputs)
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
			o.pointerInside = e.Surface == o.surface
			if o.pointerInside {
				o.deliver(Event{Kind: EventPointerEnter, X: int(e.SurfaceX), Y: int(e.SurfaceY)})
			}
		})
		pointer.SetLeaveHandler(func(client.PointerLeaveEvent) {
			o.pointerInside = false
			o.deliver(Event{Kind: EventPointerLeave})
		})
		pointer.SetMotionHandler(func(e client.PointerMotionEvent) {
			if o.pointerInside {
				o.deliver(Event{Kind: EventPointerMotion, X: int(e.SurfaceX), Y: int(e.SurfaceY)})
			}
		})
		pointer.SetButtonHandler(func(e client.PointerButtonEvent) {
			if !o.pointerInside {
				return
			}
			kind := EventPointerRelease
			if e.State == uint32(client.PointerButtonStatePressed) {
				kind = EventPointerPress
			}
			o.deliver(Event{Kind: kind, Button: e.Button})
		})
	case !hasPointer && o.pointer != nil:
		o.fail(o.pointer.Release())
		o.pointer = nil
		o.pointerInside = false
	}
}

// deliver hands a pointer event to the application and invalidates when the
// application reports that its state changed.
func (o *owner) deliver(e Event) {
	if o.cb.Handle(e) {
		o.sched.Invalidate()
	}
}

// createSurface builds the layer surface and its fractional-scale and viewport
// objects, then performs the fixed initial sequence: set properties, commit
// once without a buffer, and wait for the first configure.
func (o *owner) createSurface() error {
	if err := o.chooseOutput(); err != nil {
		return err
	}

	surface, err := o.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("wayland: create surface: %w", err)
	}
	o.surface = surface
	o.cleanup.push("surface", surface.Destroy)

	layer, err := o.layerShell.GetLayerSurface(surface, o.output,
		uint32(layershell.ZwlrLayerShellV1LayerTop), namespace)
	if err != nil {
		return fmt.Errorf("wayland: get layer surface: %w", err)
	}
	o.layer = layer
	o.cleanup.push("layer-surface", layer.Destroy)

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
	layer.SetConfigureHandler(o.onConfigure)
	layer.SetClosedHandler(func(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
		o.closed = true
		o.sched.Close()
	})

	scale, err := o.scaleMgr.GetFractionalScale(surface)
	if err != nil {
		return fmt.Errorf("wayland: get fractional scale: %w", err)
	}
	o.scale = scale
	o.cleanup.push("fractional-scale", scale.Destroy)
	scale.SetPreferredScaleHandler(o.onPreferredScale)

	viewport, err := o.viewporter.GetViewport(surface)
	if err != nil {
		return fmt.Errorf("wayland: get viewport: %w", err)
	}
	o.viewport = viewport
	o.cleanup.push("viewport", viewport.Destroy)

	// One commit with no buffer attached, then the compositor configures.
	if err := surface.Commit(); err != nil {
		return fmt.Errorf("wayland: initial commit: %w", err)
	}
	return nil
}

// onConfigure acknowledges every configure before any buffer is attached.
func (o *owner) onConfigure(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
	if err := o.layer.AckConfigure(e.Serial); err != nil {
		o.fail(fmt.Errorf("wayland: ack configure: %w", err))
		return
	}
	changed := o.state.configure(int(e.Width), int(e.Height))
	o.state.acknowledge()
	if changed || o.current == nil {
		o.fail(o.reconfigure())
	}
}

// onPreferredScale handles wp_fractional_scale_v1.preferred_scale. It is not
// ordered against configure and may arrive alone when the output scale changes
// at an unchanged logical size, which still retires the physical buffers.
func (o *owner) onPreferredScale(e fractionalscale.WpFractionalScaleV1PreferredScaleEvent) {
	if !o.state.preferredScale(ui.Scale120(e.Scale)) {
		return
	}
	if o.state.eligible() {
		o.fail(o.reconfigure())
	}
}

// reconfigure retires the current buffer generation and allocates a new one at
// the current physical size.
func (o *owner) reconfigure() error {
	width, height, err := o.state.bufferSize()
	if err != nil {
		return err
	}

	// The viewport destination is the logical configure size. The buffer scale
	// stays at its default of 1, and no source rectangle is set.
	if err := o.viewport.SetDestination(int32(o.state.logicalWidth), int32(o.state.logicalHeight)); err != nil {
		return fmt.Errorf("wayland: set viewport destination: %w", err)
	}
	if err := o.cb.Configure(o.state.logicalWidth, o.state.logicalHeight, int(o.state.scale120)); err != nil {
		return err
	}

	// The outstanding frame callback belongs to a retired generation.
	if err := o.dropFrameCallback(); err != nil {
		return err
	}
	if o.current != nil {
		o.retiring = append(o.retiring, o.current)
		o.current = nil
	}
	o.sweepRetired()

	o.genID++
	gen, err := newGeneration(o.shm, o.genID, width, height)
	if err != nil {
		return err
	}
	for slot := range gen.slots {
		slot, gen := slot, gen
		gen.slots[slot].SetReleaseHandler(func(client.BufferReleaseEvent) {
			o.onBufferRelease(gen, slot)
		})
	}
	o.current = gen
	o.sched.Configure(int(width), int(height))
	return nil
}

// onBufferRelease frees a slot. A frame callback never implies a release: Niri
// delivers wl_callback.done while the submitted buffer is still held.
func (o *owner) onBufferRelease(gen *generation, slot int) {
	o.fail(gen.retire.released())
	if gen == o.current {
		o.fail(o.sched.Release(slot))
	}
	o.sweepRetired()
}

// sweepRetired unmaps every retired generation whose buffers have all been
// released.
func (o *owner) sweepRetired() {
	kept := o.retiring[:0]
	for _, gen := range o.retiring {
		if gen.retire.freeable() {
			o.fail(gen.destroy())
			continue
		}
		kept = append(kept, gen)
	}
	o.retiring = kept
}

func (o *owner) dropFrameCallback() error {
	if o.frameCallback != nil {
		err := o.frameCallback.Destroy()
		o.frameCallback = nil
		return err
	}
	return nil
}

// renderJob paints one slot and submits it.
func (o *owner) renderJob(job render.Job) error {
	gen := o.current
	if gen == nil {
		return errors.New("wayland: render requested with no buffer generation")
	}

	if err := o.cb.Render(gen.pixels(job.Slot), int(gen.width), int(gen.height), int(gen.stride)); err != nil {
		return err
	}
	if err := o.surface.Attach(gen.slots[job.Slot], 0, 0); err != nil {
		return fmt.Errorf("wayland: attach: %w", err)
	}
	// Damage is submitted in buffer pixels, never in surface units.
	if err := o.surface.DamageBuffer(0, 0, gen.width, gen.height); err != nil {
		return fmt.Errorf("wayland: damage: %w", err)
	}

	callback, err := o.surface.Frame()
	if err != nil {
		return fmt.Errorf("wayland: frame: %w", err)
	}
	o.frameCallback = callback
	callback.SetDoneHandler(func(client.CallbackDoneEvent) {
		if callback != o.frameCallback {
			return // a callback from a retired generation
		}
		o.frameCallback = nil
		o.fail(o.sched.Frame())
	})

	if err := o.surface.Commit(); err != nil {
		return fmt.Errorf("wayland: commit: %w", err)
	}
	gen.retire.attached()
	return o.sched.Submitted(job.Slot)
}

// loop drives the owner goroutine: render when the scheduler offers work, then
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

		if decision, job := o.sched.Next(); decision == render.DecisionRender {
			if err := o.renderJob(job); err != nil {
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
			o.sched.Invalidate()
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

// shutdown destroys everything child-to-parent, flushes the destructor
// requests, and reports every cleanup failure.
func (o *owner) shutdown() error {
	var errs []error
	if o.current != nil {
		o.current.retire.destroy()
		o.retiring = append(o.retiring, o.current)
		o.current = nil
	}
	for _, gen := range o.retiring {
		gen.retire.destroy()
		if err := gen.destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	o.retiring = nil

	if err := o.dropFrameCallback(); err != nil {
		errs = append(errs, err)
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
