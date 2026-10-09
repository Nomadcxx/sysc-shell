// Gamma control is the only place that holds zwlr_gamma_control_v1 objects.
// The policy in internal/services decides the colour temperature; this file
// applies it to every bound output on the Wayland goroutine, so create, set
// and destroy order matches the order requests arrived in.
package wayland

import (
	"fmt"
	"log/slog"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/gamma"
)

// GammaState reports what the compositor said about one output's gamma.
type GammaState uint8

const (
	// GammaReady means the compositor accepted a gamma control for the output and,
	// when a temperature is active, the ramp was applied.
	GammaReady GammaState = iota
	// GammaFailed means the compositor revoked control, usually because
	// another client holds it. The control object is destroyed and never
	// retried on its own.
	GammaFailed
	// GammaUnsupported means the compositor never advertised
	// zwlr-gamma-control-manager-v1.
	GammaUnsupported
	// GammaRemoved clears the per-output state when the output goes away.
	GammaRemoved
)

// GammaRequest is one colour temperature for every bound output. Neutral asks
// the compositor to restore its own tables and is what an off night light
// sends.
type GammaRequest struct {
	Kelvin  int
	Neutral bool
	// Retry is set only for a user or configuration action. It lets the owner
	// retry a control the compositor previously revoked without a timer loop.
	Retry bool
}

// GammaEvent reports one output's gamma state back to the policy loop. Global
// is zero for GammaUnsupported, which is about the compositor, not an output.
type GammaEvent struct {
	Global    uint32
	Connector string
	State     GammaState
	Size      uint32
}

type gammaControl interface {
	SetGamma(fd int) error
	Destroy() error
	SetGammaSizeHandler(gamma.ZwlrGammaControlV1GammaSizeHandlerFunc)
	SetFailedHandler(gamma.ZwlrGammaControlV1FailedHandlerFunc)
}

// gammaOutput is the live control for one bound wl_output.
type gammaOutput struct {
	host   *OutputHost
	ctl    gammaControl
	size   uint32
	failed bool
	ready  bool
}

// attachGamma creates the control for a freshly bound output. Bar-disabled
// outputs go through here too: the night light is not a bar feature.
func (o *owner) attachGamma(h *OutputHost) {
	if o.gammaMgr == nil && o.gammaFactory == nil {
		return
	}
	if o.gammaOut == nil {
		o.gammaOut = make(map[uint32]*gammaOutput)
	}
	o.gammaOut[h.global] = &gammaOutput{host: h}
	o.gammaCreate(h.global)
}

// gammaCreate asks the manager for a control and registers its handlers. On
// success the size handler applies the current request, so a request that
// arrived before gamma_size did is not lost.
func (o *owner) gammaCreate(global uint32) {
	out := o.gammaOut[global]
	if out == nil || (o.gammaMgr == nil && o.gammaFactory == nil) || out.failed {
		return
	}
	var ctl gammaControl
	var err error
	if o.gammaFactory != nil {
		ctl, err = o.gammaFactory(out.host.proxy)
	} else {
		ctl, err = o.gammaMgr.GetGammaControl(out.host.proxy)
	}
	if err != nil {
		if ctl != nil {
			_ = ctl.Destroy()
		}
		o.gammaFail(global, out, nil, err)
		return
	}
	out.ctl = ctl
	out.size = 0
	ctl.SetGammaSizeHandler(func(e gamma.ZwlrGammaControlV1GammaSizeEvent) {
		if out.ctl != ctl || out.failed {
			return
		}
		if e.Size == 0 || e.Size > uint32(gamma.MaxRampEntries) {
			o.gammaFail(global, out, ctl, fmt.Errorf("invalid gamma size %d", e.Size))
			return
		}
		out.size = e.Size
		if o.gammaCurrent == nil || o.gammaCurrent.Neutral {
			o.gammaReady(global, out)
			if o.gammaCurrent != nil && o.gammaCurrent.Neutral {
				o.destroyGammaControl(out)
			}
			return
		}
		o.applyGammaTo(global)
	})
	ctl.SetFailedHandler(func(gamma.ZwlrGammaControlV1FailedEvent) {
		if out.ctl == ctl {
			o.gammaFail(global, out, ctl, nil)
		}
	})
}

// applyGamma is the owner-side entry point for one request.
func (o *owner) applyGamma(req GammaRequest) {
	if req.Retry {
		for global, out := range o.gammaOut {
			if out.failed {
				out.failed = false
				o.gammaCreate(global)
			}
		}
	}
	if o.gammaMgr == nil && o.gammaFactory == nil {
		o.emitGamma(GammaEvent{State: GammaUnsupported})
		return
	}
	o.gammaCurrent = &req
	if req.Neutral {
		// A control without gamma_size has not changed the output yet. Keep it
		// until its size arrives so the shell can learn that gamma is supported.
		for _, out := range o.gammaOut {
			if out.size != 0 {
				o.destroyGammaControl(out)
			}
		}
		return
	}
	for global := range o.gammaOut {
		o.applyGammaTo(global)
	}
}

// applyGammaTo sets the current temperature on one output. A missing control
// is re-created here, which bounds failure retries to actual requests rather
// than any timer.
func (o *owner) applyGammaTo(global uint32) {
	out := o.gammaOut[global]
	if out == nil || out.failed || o.gammaCurrent == nil || o.gammaCurrent.Neutral {
		return
	}
	if out.ctl == nil {
		o.gammaCreate(global)
		if out.ctl == nil {
			return
		}
	}
	if out.ctl == nil || out.failed || out.size == 0 {
		return // gamma_size has not arrived; its handler re-applies
	}
	r, g, b := gammaRamps(o.gammaCurrent.Kelvin, out.size)
	f, err := gamma.RampFile(int(out.size), r, g, b)
	if err != nil {
		o.gammaFail(global, out, out.ctl, err)
		return
	}
	if err := out.ctl.SetGamma(int(f.Fd())); err != nil {
		_ = f.Close()
		o.gammaFail(global, out, out.ctl, err)
		return
	}
	if err := f.Close(); err != nil {
		slog.Warn("gamma ramp file close failed", "connector", out.host.connector, "error", err)
	}
	o.gammaReady(global, out)
}

func (o *owner) gammaReady(global uint32, out *gammaOutput) {
	if out.ready {
		return
	}
	out.ready = true
	o.emitGamma(GammaEvent{Global: global, Connector: out.host.connector, State: GammaReady, Size: out.size})
}

func (o *owner) gammaFail(global uint32, out *gammaOutput, ctl gammaControl, cause error) {
	if cause != nil {
		slog.Warn("gamma control failed", "connector", out.host.connector, "error", cause)
	}
	if ctl != nil {
		if err := ctl.Destroy(); err != nil {
			slog.Warn("gamma control destroy failed", "connector", out.host.connector, "error", err)
		}
	}
	if out.ctl == ctl {
		out.ctl = nil
	}
	out.size = 0
	out.ready = false
	out.failed = true
	o.emitGamma(GammaEvent{Global: global, Connector: out.host.connector, State: GammaFailed})
}

func (o *owner) destroyGammaControl(out *gammaOutput) {
	if out.ctl != nil {
		if err := out.ctl.Destroy(); err != nil {
			slog.Warn("gamma control destroy failed", "connector", out.host.connector, "error", err)
		}
		out.ctl = nil
	}
	out.size = 0
}

func (o *owner) destroyGammaAll() {
	for _, out := range o.gammaOut {
		o.destroyGammaControl(out)
	}
}

// destroyGamma removes one output's control, used before its wl_output proxy
// is released.
func (o *owner) destroyGamma(global uint32) {
	out := o.gammaOut[global]
	if out == nil {
		return
	}
	delete(o.gammaOut, global)
	o.destroyGammaControl(out)
	o.emitGamma(GammaEvent{Global: global, Connector: out.host.connector, State: GammaRemoved})
}

// emitGamma hands an event to the policy loop without ever blocking the
// dispatch goroutine, mirroring emitIdle.
func (o *owner) emitGamma(ev GammaEvent) {
	if o.cb.GammaEvents == nil {
		return
	}
	select {
	case o.cb.GammaEvents <- ev:
	default:
		o.gammaEvQueue = append(o.gammaEvQueue, ev)
		if o.wake != nil {
			o.wake.signal()
		}
	}
}

// deliverGammaEvents flushes the queue after requests have been applied.
func (o *owner) deliverGammaEvents() {
	for len(o.gammaEvQueue) > 0 && o.cb.GammaEvents != nil {
		select {
		case o.cb.GammaEvents <- o.gammaEvQueue[0]:
			o.gammaEvQueue = o.gammaEvQueue[1:]
		default:
			if o.wake != nil {
				o.wake.signal()
			}
			return
		}
	}
}

// kelvinRGB approximates a black-body colour, returning 8-bit channel values.
// It is the Tanner Helland approximation, the same one wlsunset uses, so a
// handoff between the two looks identical.
func kelvinRGB(kelvin int) (int, int, int) {
	if kelvin < 1000 {
		kelvin = 1000
	}
	t := float64(kelvin) / 100
	var r, g, b float64
	if t <= 66 {
		r = 255
		g = 99.4708025861*math.Log(t) - 161.1195681661
	} else {
		r = 329.698727446 * math.Pow(t-60, -0.1332047592)
		g = 288.1221695283 * math.Pow(t-60, -0.0755148492)
	}
	switch {
	case t >= 66:
		b = 255
	case t <= 19:
		b = 0
	default:
		b = 138.5177312231*math.Log(t-10) - 305.0447927307
	}
	return clamp8(r), clamp8(g), clamp8(b)
}

func clamp8(v float64) int {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	default:
		return int(v + 0.5)
	}
}

// gammaRamps scales the identity ramp by each channel's share of white, so
// every ramp is monotonic from black to that channel and no channel is ever
// inverted. size is the compositor's gamma_size.
func gammaRamps(kelvin int, size uint32) ([]uint16, []uint16, []uint16) {
	r, g, b := kelvinRGB(kelvin)
	rRamp := make([]uint16, size)
	gRamp := make([]uint16, size)
	bRamp := make([]uint16, size)
	if size == 0 {
		return rRamp, gRamp, bRamp
	}
	if size == 1 {
		// ponytail: a one-entry ramp is degenerate; scale 8-bit to 16-bit.
		rRamp[0], gRamp[0], bRamp[0] = uint16(r)*257, uint16(g)*257, uint16(b)*257
		return rRamp, gRamp, bRamp
	}
	den := uint64(size-1) * 255
	for i := range rRamp {
		step := uint64(i) * 65535
		rRamp[i] = uint16(uint64(r) * step / den)
		gRamp[i] = uint16(uint64(g) * step / den)
		bRamp[i] = uint16(uint64(b) * step / den)
	}
	return rRamp, gRamp, bRamp
}
