package plugin

import (
	"context"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestFloatingSurfaceCallsRequireCapabilityAndValidateBeforeOpening(t *testing.T) {
	called := 0
	env := CallEnv{
		Granted: []Capability{CapFloatingSurfaces},
		OpenSurface: func(_ context.Context, p v1.SurfaceOpenParams) (v1.SurfaceResult, error) {
			called++
			return v1.SurfaceResult{ViewID: "v1"}, nil
		},
	}
	valid := v1.SurfaceOpenParams{Key: "note-key", Title: "Plan", Output: "DP-1", X: 20, Y: 30, Width: 360, Height: 480}
	opened := NewDispatcher(env).Handle(context.Background(), &v1.HostCall{ID: "open", Call: v1.CallSurfaceOpen, Params: jsonOf(t, valid)})
	if !opened.OK || called != 1 {
		t.Fatalf("open = %+v, callback calls %d", opened, called)
	}
	for name, params := range map[string]v1.SurfaceOpenParams{
		"path key":          {Key: "../note", Width: 360, Height: 480},
		"small width":       {Key: "note", Width: 100, Height: 480},
		"large height":      {Key: "note", Width: 360, Height: 3000},
		"negative position": {Key: "note", X: -1, Width: 360, Height: 480},
	} {
		t.Run(name, func(t *testing.T) {
			reply := NewDispatcher(env).Handle(context.Background(), &v1.HostCall{ID: "bad", Call: v1.CallSurfaceOpen, Params: jsonOf(t, params)})
			if reply.OK {
				t.Fatalf("accepted invalid surface: %+v", params)
			}
		})
	}
	if called != 1 {
		t.Fatalf("invalid parameters reached the surface host %d times", called-1)
	}
	denied := NewDispatcher(CallEnv{}).Handle(context.Background(), &v1.HostCall{ID: "denied", Call: v1.CallSurfaceOpen, Params: jsonOf(t, valid)})
	if denied.OK {
		t.Fatal("surface open worked without its capability")
	}
}
