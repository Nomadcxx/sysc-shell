package wayland

import "testing"

func TestActivationRequestsQueueUntilOwnerTakesThem(t *testing.T) {
	t.Parallel()

	wake := &wakePipe{}
	req := ActivationTokenRequest{Serial: 91}
	wake.pushActivation(req)
	got := wake.takeActivation()
	if len(got) != 1 || got[0].Serial != req.Serial {
		t.Fatalf("activation requests = %+v, want serial %d", got, req.Serial)
	}
	if got := wake.takeActivation(); len(got) != 0 {
		t.Fatalf("requests remained after take: %+v", got)
	}
}
