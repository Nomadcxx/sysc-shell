package plugin

import (
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const inboundViolations = 3

// Inbound is the token bucket that decides whether a plugin line is worth
// decoding. Exhausted tokens drop the line before JSON parse.
type Inbound struct {
	now       func() time.Time
	burst     float64
	perSecond float64
	tokens    float64
	last      time.Time
	failed    int
}

// NewInbound starts with a full burst.
func NewInbound(now func() time.Time, lim v1.Limits) *Inbound {
	if now == nil {
		now = time.Now
	}
	burst := float64(lim.UpdateBurst)
	if burst <= 0 {
		burst = float64(v1.DefaultLimits.UpdateBurst)
	}
	rate := float64(lim.UpdatesPerSecond)
	if rate <= 0 {
		rate = float64(v1.DefaultLimits.UpdatesPerSecond)
	}
	return &Inbound{now: now, burst: burst, perSecond: rate, tokens: burst, last: now()}
}

func (i *Inbound) refill() {
	t := i.now()
	elapsed := t.Sub(i.last).Seconds()
	i.last = t
	if elapsed <= 0 {
		return
	}
	i.tokens += elapsed * i.perSecond
	if i.tokens > i.burst {
		i.tokens = i.burst
	}
}

// Allow consumes one token. A false result means the line must be dropped.
func (i *Inbound) Allow() bool {
	i.refill()
	if i.tokens < 1 {
		i.failed++
		return false
	}
	i.tokens--
	return true
}

// Accept returns raw only when a token was available, so the caller never
// decodes a discarded line.
func (i *Inbound) Accept(raw []byte) ([]byte, bool) {
	if !i.Allow() {
		return nil, false
	}
	return raw, true
}

// Degraded reports whether the plugin has exhausted its inbound budget enough
// times to be suppressed.
func (i *Inbound) Degraded() bool { return i.failed >= inboundViolations }

// Queue holds newest-wins view work and a bounded FIFO of control messages.
type Queue struct {
	views   map[string]v1.Message
	control []v1.Message
}

const maxControl = 32

func NewQueue() *Queue { return &Queue{views: make(map[string]v1.Message)} }

func (q *Queue) PushView(id string, m v1.Message) { q.views[id] = m }

func (q *Queue) PushControl(m v1.Message) {
	if len(q.control) >= maxControl {
		return
	}
	q.control = append(q.control, m)
}

func (q *Queue) TakeViews() map[string]v1.Message {
	out := q.views
	q.views = make(map[string]v1.Message)
	return out
}

func (q *Queue) TakeControl() []v1.Message {
	out := q.control
	q.control = nil
	return out
}
