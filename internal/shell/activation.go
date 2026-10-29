package shell

import (
	"context"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func (r *Registry) ActivationTokens() <-chan wayland.ActivationTokenRequest {
	return r.activationTokens
}

func (r *Registry) requestActivationToken(ctx context.Context, serial uint32) string {
	if serial == 0 || r.activationTokens == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reply := make(chan string, 1)
	req := wayland.ActivationTokenRequest{Serial: serial, Done: reply}
	select {
	case r.activationTokens <- req:
	case <-ctx.Done():
		return ""
	case <-r.closed:
		return ""
	}
	select {
	case token := <-reply:
		return token
	case <-ctx.Done():
		return ""
	case <-r.closed:
		return ""
	}
}
