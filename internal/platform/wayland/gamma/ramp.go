package gamma

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// ErrGammaUnavailable reports that the compositor revoked gamma control for
// an object. Clients call it failed for this reason when another program
// already holds the output's tables.
var ErrGammaUnavailable = errors.New("gamma: compositor revoked gamma control")

// MaxRampEntries bounds tables from a compositor. If a real output needs more,
// verify the protocol and memory cost before raising the cap.
const MaxRampEntries = 65536

// RampFile builds the memfd zwlr_gamma_control_v1.set_gamma wants: three
// ramps of size uint16 values in host byte order, red first. The fd is
// sealed, which is what the protocol requires, and the caller closes it once
// the request returns.
func RampFile(size int, r, g, b []uint16) (*os.File, error) {
	if size <= 0 {
		return nil, fmt.Errorf("gamma: ramp size %d is not positive", size)
	}
	if size > MaxRampEntries {
		return nil, fmt.Errorf("gamma: ramp size %d is above the %d entry ceiling", size, MaxRampEntries)
	}
	if len(r) != size || len(g) != size || len(b) != size {
		return nil, fmt.Errorf("gamma: ramps are %d/%d/%d entries, want %d", len(r), len(g), len(b), size)
	}

	buf := make([]byte, size*6)
	for i := 0; i < size; i++ {
		binary.NativeEndian.PutUint16(buf[i*2:], r[i])
		binary.NativeEndian.PutUint16(buf[(size+i)*2:], g[i])
		binary.NativeEndian.PutUint16(buf[(2*size+i)*2:], b[i])
	}

	fd, err := unix.MemfdCreate("sysc-shell-gamma", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, fmt.Errorf("gamma: memfd: %w", err)
	}
	f := os.NewFile(uintptr(fd), "sysc-shell-gamma")
	cleanup := func() { _ = f.Close() }
	if err := unix.Ftruncate(fd, int64(len(buf))); err != nil {
		cleanup()
		return nil, fmt.Errorf("gamma: size ramp: %w", err)
	}
	if n, err := f.WriteAt(buf, 0); err != nil {
		cleanup()
		return nil, fmt.Errorf("gamma: write ramp: %w", err)
	} else if n != len(buf) {
		cleanup()
		return nil, io.ErrShortWrite
	}
	seals := unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE | unix.F_SEAL_SEAL
	if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, seals); err != nil {
		cleanup()
		return nil, fmt.Errorf("gamma: seal ramp: %w", err)
	}
	return f, nil
}
