package wayland

import (
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/xdgactivation"
	"github.com/Nomadcxx/sysc-wayland/client"
)

// ActivationTokenRequest asks the Wayland owner to create a token for the
// input serial that launched an application. An empty reply means the
// compositor cannot provide a token; the caller can still start the app.
type ActivationTokenRequest struct {
	Serial uint32
	Done   chan<- string
}

func (r ActivationTokenRequest) answer(token string) {
	if r.Done == nil {
		return
	}
	select {
	case r.Done <- token:
	default:
	}
}

type activationToken interface {
	SetSerial(uint32, *client.Seat) error
	SetSurface(*client.Surface) error
	SetDoneHandler(func(string))
	Commit() error
	Destroy() error
}

type activationTokenSource interface {
	NewToken() (activationToken, error)
}

type xdgActivationTokenSource struct {
	manager *xdgactivation.XdgActivationV1
}

func (s xdgActivationTokenSource) NewToken() (activationToken, error) {
	token, err := s.manager.GetActivationToken()
	if err != nil {
		return nil, err
	}
	return xdgActivationToken{proxy: token}, nil
}

type xdgActivationToken struct {
	proxy *xdgactivation.XdgActivationTokenV1
}

func (t xdgActivationToken) SetSerial(serial uint32, seat *client.Seat) error {
	return t.proxy.SetSerial(serial, seat)
}

func (t xdgActivationToken) SetSurface(surface *client.Surface) error {
	return t.proxy.SetSurface(surface)
}

func (t xdgActivationToken) SetDoneHandler(done func(string)) {
	t.proxy.SetDoneHandler(func(e xdgactivation.XdgActivationTokenV1DoneEvent) {
		if done != nil {
			done(e.Token)
		}
	})
}

func (t xdgActivationToken) Commit() error  { return t.proxy.Commit() }
func (t xdgActivationToken) Destroy() error { return t.proxy.Destroy() }

func (o *owner) requestActivationToken(req ActivationTokenRequest) {
	if req.Serial == 0 || o.seat == nil || o.activationTokens == nil {
		req.answer("")
		return
	}
	token, err := o.activationTokens.NewToken()
	if err != nil || token == nil {
		req.answer("")
		return
	}
	token.SetDoneHandler(func(value string) {
		delete(o.pendingActivationTokens, token)
		_ = token.Destroy()
		req.answer(value)
	})
	if err := token.SetSerial(req.Serial, o.seat); err != nil {
		_ = token.Destroy()
		req.answer("")
		return
	}
	if o.activationInputSerial == req.Serial && o.activationInputSurface != nil && !o.activationInputSurface.IsZombie() {
		if err := token.SetSurface(o.activationInputSurface); err != nil {
			_ = token.Destroy()
			req.answer("")
			return
		}
	}
	if o.pendingActivationTokens == nil {
		o.pendingActivationTokens = make(map[activationToken]ActivationTokenRequest)
	}
	o.pendingActivationTokens[token] = req
	if err := token.Commit(); err != nil {
		delete(o.pendingActivationTokens, token)
		_ = token.Destroy()
		req.answer("")
	}
}

func (o *owner) recordActivationInput(serial uint32, surface *client.Surface) {
	if serial == 0 || surface == nil {
		return
	}
	o.activationInputSerial = serial
	o.activationInputSurface = surface
}

func (o *owner) destroyPendingActivationTokens() {
	for token, req := range o.pendingActivationTokens {
		delete(o.pendingActivationTokens, token)
		_ = token.Destroy()
		req.answer("")
	}
}
