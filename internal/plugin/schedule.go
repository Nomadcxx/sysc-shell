package plugin

import "time"

const publishInterval = time.Second / 30

// Schedule gates view publication at 30 Hz so a plugin that patches every
// millisecond cannot drive a frame per patch.
type Schedule struct {
	now  func() time.Time
	last map[string]time.Time
}

func NewSchedule(now func() time.Time) *Schedule {
	if now == nil {
		now = time.Now
	}
	return &Schedule{now: now, last: make(map[string]time.Time)}
}

// Due reports whether view id may publish, and records the publish time if so.
func (s *Schedule) Due(id string) bool {
	t := s.now()
	if prev, ok := s.last[id]; ok && t.Sub(prev) < publishInterval {
		return false
	}
	s.last[id] = t
	return true
}

// NextDue reports how long view id has left of its interval, zero when it may
// publish now. It records nothing: only Due reserves a publish. A caller uses
// the duration to sleep exactly as long as the view is held back, rather than
// waking on the interval whether or not anything is waiting.
func (s *Schedule) NextDue(id string) time.Duration {
	prev, ok := s.last[id]
	if !ok {
		return 0
	}
	left := publishInterval - s.now().Sub(prev)
	if left < 0 {
		return 0
	}
	return left
}
