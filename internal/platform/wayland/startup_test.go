package wayland

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestRunDrainsInvalidationsDuringConnect(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "wayland-test"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}

	// Startup services can fill the registry's queue before the compositor
	// reports its capabilities. That callback must still be able to publish.
	invalidations := make(chan Invalidation, 8)
	for i := 0; i < cap(invalidations); i++ {
		invalidations <- Invalidation{Global: 1}
	}
	finished := make(chan error, 1)
	go func() {
		finished <- Run(context.Background(), config.Default(), Callbacks{
			Invalidations: invalidations,
			NewHost: func(uint32, string) (HostCallbacks, error) {
				return HostCallbacks{}, nil
			},
			PrepareConfig: func(config.Config, []HostIdentity) (PreparedConfig, error) {
				return PreparedConfig{}, nil
			},
		})
	}()
	connection, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// End the unfinished handshake and require Run to clean up its reader.
		_ = connection.Close()
		select {
		case err := <-finished:
			if err == nil {
				t.Error("Run returned nil after the compositor disconnected")
			}
		case <-time.After(2 * time.Second):
			t.Error("Run did not stop after the compositor disconnected")
		}
	}()

	// The server deliberately sends no reply: the queue must drain while
	// connect is waiting for its first roundtrip, before the owner loop runs.
	select {
	case invalidations <- Invalidation{Global: 1}:
	case <-time.After(2 * time.Second):
		t.Fatal("startup repaint blocked on a full queue before the owner loop")
	}
}
