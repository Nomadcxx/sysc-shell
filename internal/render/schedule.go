package render

import "fmt"

// Decision is the transition the scheduler offers the platform layer.
type Decision uint8

const (
	DecisionWait Decision = iota
	DecisionRender
)

// slotCount is the number of shared-memory buffer slots the proof keeps.
const slotCount = 2

// Job describes one render the platform layer should perform.
type Job struct {
	Slot          int
	Width, Height int
	// Generation identifies the buffer generation the slot belongs to. A
	// configure starts a new generation.
	Generation int
}

type slot struct {
	// busy is true from submission until wl_buffer.release. It is scoped to
	// the current generation: a configure allocates fresh buffers, which frees
	// every slot regardless of what the compositor still holds.
	busy bool
}

// Scheduler owns the pure draw-scheduling state: two buffer slots, one
// coalesced dirty flag, and one frame callback. It knows nothing about
// Wayland, and the Wayland owner goroutine is its only caller.
//
// Frame completion never implies a free buffer. Niri 26.04 delivers
// wl_callback.done for a commit while that commit's buffer is still held, and
// wl_buffer.release only after the next buffer is attached and committed.
type Scheduler struct {
	slots        [slotCount]slot
	generation   int
	width        int
	height       int
	dirty        bool
	framePending bool
	closed       bool
	// last is the slot most recently submitted; the next render prefers the
	// other one.
	last int
}

func NewScheduler() *Scheduler {
	return &Scheduler{last: slotCount - 1}
}

// Invalidate marks the surface dirty. Repeated calls coalesce into one redraw.
func (s *Scheduler) Invalidate() {
	if s.closed {
		return
	}
	s.dirty = true
}

// Configure records a new surface size and starts a new buffer generation.
// Any undelivered render is discarded, and the caller is responsible for
// destroying the outstanding frame callback so no stale done event arrives.
func (s *Scheduler) Configure(width, height int) {
	if s.closed {
		return
	}
	s.width, s.height = width, height
	s.generation++
	s.framePending = false
	s.dirty = true

	// The new generation allocates two fresh buffers, so every slot is free.
	// Buffers the compositor still holds belong to the retired generation and
	// are tracked by its owner; their releases never reach the scheduler.
	for i := range s.slots {
		s.slots[i].busy = false
	}
}

// Close stops all further work.
func (s *Scheduler) Close() {
	s.closed = true
	s.dirty = false
	s.framePending = false
}

// Frame records wl_callback.done. It does not free a buffer.
func (s *Scheduler) Frame() error {
	if s.closed {
		return nil
	}
	if !s.framePending {
		return fmt.Errorf("render: frame done with no frame pending")
	}
	s.framePending = false
	return nil
}

// Release records wl_buffer.release, returning the slot's storage to the pool
// at the current generation.
func (s *Scheduler) Release(index int) error {
	if index < 0 || index >= slotCount {
		return fmt.Errorf("render: release of slot %d, which does not exist", index)
	}
	if !s.slots[index].busy {
		return fmt.Errorf("render: release of slot %d, which is not busy", index)
	}
	s.slots[index].busy = false
	return nil
}

// Submitted records that the slot's buffer was attached and committed and a
// frame callback was requested.
func (s *Scheduler) Submitted(index int) error {
	if index < 0 || index >= slotCount {
		return fmt.Errorf("render: submission of slot %d, which does not exist", index)
	}
	if s.slots[index].busy {
		return fmt.Errorf("render: submission of slot %d, which is still busy", index)
	}
	s.slots[index].busy = true
	s.framePending = true
	s.dirty = false
	s.last = index
	return nil
}

// Next reports whether the platform layer should render now, and into which
// slot. It offers work only when the surface is dirty, no frame callback is
// outstanding, and a slot of the current generation is free.
func (s *Scheduler) Next() (Decision, Job) {
	// generation stays zero until the first configure. An application
	// invalidation can arrive before that, and there is no buffer to draw into
	// until a configure has allocated one.
	if s.closed || s.generation == 0 || !s.dirty || s.framePending {
		return DecisionWait, Job{}
	}
	for i := 1; i <= slotCount; i++ {
		index := (s.last + i) % slotCount
		if s.slots[index].busy {
			continue
		}
		return DecisionRender, Job{
			Slot:       index,
			Width:      s.width,
			Height:     s.height,
			Generation: s.generation,
		}
	}
	return DecisionWait, Job{}
}
