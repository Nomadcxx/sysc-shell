package wayland

import (
	"errors"
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

type fakeActivationToken struct {
	serial    uint32
	seat      *client.Seat
	surface   *client.Surface
	done      func(string)
	commitErr error
	committed bool
	destroyed bool
}

func (t *fakeActivationToken) SetSerial(serial uint32, seat *client.Seat) error {
	t.serial, t.seat = serial, seat
	return nil
}

func (t *fakeActivationToken) SetSurface(surface *client.Surface) error {
	t.surface = surface
	return nil
}

func (t *fakeActivationToken) SetDoneHandler(done func(string)) { t.done = done }
func (t *fakeActivationToken) Commit() error {
	t.committed = true
	return t.commitErr
}
func (t *fakeActivationToken) Destroy() error { t.destroyed = true; return nil }

type fakeActivationTokenSource struct {
	token *fakeActivationToken
	err   error
}

func (s fakeActivationTokenSource) NewToken() (activationToken, error) {
	return s.token, s.err
}

func TestActivationTokenRequestReturnsTokenAndReleasesProxy(t *testing.T) {
	t.Parallel()

	seat := &client.Seat{}
	surface := &client.Surface{}
	token := &fakeActivationToken{}
	reply := make(chan string, 1)
	o := &owner{
		seat:                   seat,
		activationTokens:       fakeActivationTokenSource{token: token},
		activationInputSerial:  91,
		activationInputSurface: surface,
	}
	o.requestActivationToken(ActivationTokenRequest{Serial: 91, Done: reply})

	if token.serial != 91 || token.seat != seat || token.surface != surface || !token.committed {
		t.Fatalf("token request = serial %d, seat %p, surface %p, committed %v", token.serial, token.seat, token.surface, token.committed)
	}
	if len(o.pendingActivationTokens) != 1 {
		t.Fatalf("pending tokens = %d, want 1", len(o.pendingActivationTokens))
	}
	token.done("activation-token")
	if got := <-reply; got != "activation-token" {
		t.Fatalf("token = %q, want activation-token", got)
	}
	if !token.destroyed || len(o.pendingActivationTokens) != 0 {
		t.Fatalf("token proxy destroyed=%v, pending=%d", token.destroyed, len(o.pendingActivationTokens))
	}
}

func TestActivationTokenRequestFallsBackWhenUnavailable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		owner  *owner
		serial uint32
	}{
		{name: "no manager", owner: &owner{seat: &client.Seat{}}, serial: 91},
		{name: "no input serial", owner: &owner{seat: &client.Seat{}, activationTokens: fakeActivationTokenSource{token: &fakeActivationToken{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := make(chan string, 1)
			tc.owner.requestActivationToken(ActivationTokenRequest{Serial: tc.serial, Done: reply})
			if got := <-reply; got != "" {
				t.Fatalf("token = %q, want empty fallback", got)
			}
		})
	}
}

func TestActivationTokenCommitFailureFallsBack(t *testing.T) {
	t.Parallel()

	token := &fakeActivationToken{commitErr: errors.New("write failed")}
	owner := &owner{seat: &client.Seat{}, activationTokens: fakeActivationTokenSource{token: token}}
	reply := make(chan string, 1)
	owner.requestActivationToken(ActivationTokenRequest{Serial: 91, Done: reply})
	if got := <-reply; got != "" {
		t.Fatalf("token = %q, want empty fallback", got)
	}
	if !token.destroyed || len(owner.pendingActivationTokens) != 0 {
		t.Fatalf("failed token proxy destroyed=%v, pending=%d", token.destroyed, len(owner.pendingActivationTokens))
	}
}
