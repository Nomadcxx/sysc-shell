package wayland

import (
	"errors"
	"fmt"
	"math"

	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

// slotCount is the number of buffers a generation holds.
const slotCount = 2

// generation owns one memfd, its mapping, its pool, and its buffers together.
// A pool may be destroyed while its buffers live, but storage the compositor
// still reads must never be written or unmapped, so the whole generation is
// retired as a unit.
type generation struct {
	id     int
	fd     int
	data   []byte
	pool   *client.ShmPool
	slots  [slotCount]*client.Buffer
	width  int32
	height int32
	stride int32
	retire retirement
}

var errAllocate = errors.New("wayland: cannot allocate buffers")

// newGeneration creates the memfd, maps it, and creates one buffer per slot.
func newGeneration(shm *client.Shm, id int, width, height int32) (*generation, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: size is %dx%d", errAllocate, width, height)
	}
	stride := int64(width) * 4
	slotSize := stride * int64(height)
	total := slotSize * slotCount
	if stride > math.MaxInt32 || total > math.MaxInt32 {
		return nil, fmt.Errorf("%w: %dx%d needs %d bytes", errAllocate, width, height, total)
	}

	fd, err := unix.MemfdCreate("sysc-shell", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("%w: memfd_create: %w", errAllocate, err)
	}
	g := &generation{id: id, fd: fd, width: width, height: height, stride: int32(stride)}

	if err := unix.Ftruncate(fd, total); err != nil {
		g.closeFD()
		return nil, fmt.Errorf("%w: ftruncate: %w", errAllocate, err)
	}
	data, err := unix.Mmap(fd, 0, int(total), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		g.closeFD()
		return nil, fmt.Errorf("%w: mmap: %w", errAllocate, err)
	}
	g.data = data

	pool, err := shm.CreatePool(fd, int32(total))
	if err != nil {
		_ = g.destroy()
		return nil, fmt.Errorf("%w: create pool: %w", errAllocate, err)
	}
	g.pool = pool

	for slot := range g.slots {
		buffer, err := pool.CreateBuffer(int32(int64(slot)*slotSize), width, height, int32(stride), formatARGB8888)
		if err != nil {
			_ = g.destroy()
			return nil, fmt.Errorf("%w: create buffer %d: %w", errAllocate, slot, err)
		}
		g.slots[slot] = buffer
	}
	return g, nil
}

// pixels returns the mapped bytes backing one slot.
func (g *generation) pixels(slot int) []byte {
	size := int(g.stride) * int(g.height)
	start := slot * size
	return g.data[start : start+size]
}

func (g *generation) closeFD() {
	if g.fd >= 0 {
		_ = unix.Close(g.fd)
		g.fd = -1
	}
}

// destroy releases the generation child-to-parent: buffers, pool, mapping,
// then the descriptor. It must only run once the generation is freeable.
func (g *generation) destroy() error {
	var errs []error
	for slot, buffer := range g.slots {
		if buffer != nil {
			if err := buffer.Destroy(); err != nil {
				errs = append(errs, fmt.Errorf("destroy buffer %d: %w", slot, err))
			}
			g.slots[slot] = nil
		}
	}
	if g.pool != nil {
		if err := g.pool.Destroy(); err != nil {
			errs = append(errs, fmt.Errorf("destroy pool: %w", err))
		}
		g.pool = nil
	}
	if g.data != nil {
		if err := unix.Munmap(g.data); err != nil {
			errs = append(errs, fmt.Errorf("munmap: %w", err))
		}
		g.data = nil
	}
	g.closeFD()
	return errors.Join(errs...)
}
