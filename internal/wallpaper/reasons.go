package wallpaper

// Reason is one cause for holding an output paused. An output's pause state
// is the set of its active reasons: it plays while the set is empty.
type Reason uint8

const (
	// ReasonManual is the user's own pause. It is the only reason that is
	// persisted and the only one the user toggles directly.
	ReasonManual Reason = iota
	ReasonLock
	ReasonLockBackground
	// ReasonOccluded means a window covers the output's working area.
	// "Covered" was taken by the foreign-surface snapshot field.
	ReasonOccluded
	ReasonScreensOff
	ReasonBattery
	ReasonLowBattery

	reasonCount
)

// Reasons is the set of active reasons for one output.
type Reasons uint16

func (r Reason) bit() Reasons { return 1 << r }

func (r Reasons) Has(reason Reason) bool        { return r&reason.bit() != 0 }
func (r Reasons) With(reason Reason) Reasons    { return r | reason.bit() }
func (r Reasons) Without(reason Reason) Reasons { return r &^ reason.bit() }
func (r Reasons) Empty() bool                   { return r == 0 }

// Auto drops the manual reason, leaving only the automatic ones.
func (r Reasons) Auto() Reasons { return r.Without(ReasonManual) }

// primaryOrder decides which reason a status chip names when several are
// active: the session-wide causes first, then per-output ones.
var primaryOrder = [reasonCount]Reason{
	ReasonLock,
	ReasonLockBackground,
	ReasonScreensOff,
	ReasonLowBattery,
	ReasonBattery,
	ReasonOccluded,
	ReasonManual,
}

// Primary returns the reason that best names the set, or false when empty.
func (r Reasons) Primary() (Reason, bool) {
	for _, reason := range primaryOrder {
		if r.Has(reason) {
			return reason, true
		}
	}
	return 0, false
}

// String returns the stable token for a reason. Panels build user-facing
// text from these; keep the tokens stable.
func (r Reason) String() string {
	switch r {
	case ReasonManual:
		return "manual"
	case ReasonLock:
		return "lock"
	case ReasonLockBackground:
		return "lock-background"
	case ReasonOccluded:
		return "covered"
	case ReasonScreensOff:
		return "screens-off"
	case ReasonBattery:
		return "battery"
	case ReasonLowBattery:
		return "low-battery"
	}
	return "unknown"
}

// String names the primary reason, or "" when the set is empty.
func (r Reasons) String() string {
	if reason, ok := r.Primary(); ok {
		return reason.String()
	}
	return ""
}

// reasonBits lists every reason bit, which drivers use to diff two sets.
var reasonBits = []Reason{
	ReasonManual,
	ReasonLock,
	ReasonLockBackground,
	ReasonOccluded,
	ReasonScreensOff,
	ReasonBattery,
	ReasonLowBattery,
}

// EachReason calls fn for every reason.
func EachReason(fn func(reason Reason)) {
	for _, reason := range reasonBits {
		fn(reason)
	}
}
