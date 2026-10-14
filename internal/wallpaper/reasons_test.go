package wallpaper

import "testing"

func TestReasonsBitsetSetAlgebra(t *testing.T) {
	var rs Reasons
	if !rs.Empty() || rs.Has(ReasonLock) {
		t.Fatalf("zero Reasons: empty=%v hasLock=%v", rs.Empty(), rs.Has(ReasonLock))
	}
	rs = rs.With(ReasonManual).With(ReasonLock)
	if !rs.Has(ReasonManual) || !rs.Has(ReasonLock) || rs.Has(ReasonBattery) {
		t.Fatalf("With lost bits: %b", rs)
	}
	if auto := rs.Auto(); auto.Has(ReasonManual) || !auto.Has(ReasonLock) || auto.Empty() {
		t.Fatalf("Auto must drop only manual: %b", auto)
	}
	if rs2 := rs.Without(ReasonManual); rs2.Has(ReasonManual) || !rs2.Has(ReasonLock) {
		t.Fatalf("Without manual: %b", rs2)
	}
}

func TestReasonsPrimaryPriority(t *testing.T) {
	rs := Reasons(0).With(ReasonOccluded).With(ReasonBattery).With(ReasonScreensOff)
	if got, ok := rs.Primary(); !ok || got != ReasonScreensOff {
		t.Fatalf("Primary = %v ok=%v, want screens-off over battery/covered", got, ok)
	}
	rs = rs.With(ReasonLockBackground)
	if got, _ := rs.Primary(); got != ReasonLockBackground {
		t.Fatalf("Primary = %v, want lock-background over screens-off", got)
	}
	rs = rs.With(ReasonLock)
	if got, _ := rs.Primary(); got != ReasonLock {
		t.Fatalf("Primary = %v, want lock", got)
	}
	if got, _ := Reasons(0).With(ReasonManual).Primary(); got != ReasonManual {
		t.Fatalf("Primary = %v, want manual as last resort", got)
	}
	if _, ok := Reasons(0).Primary(); ok {
		t.Fatal("empty Reasons must have no primary")
	}
}

func TestReasonStringTokens(t *testing.T) {
	want := map[Reason]string{
		ReasonManual:         "manual",
		ReasonLock:           "lock",
		ReasonLockBackground: "lock-background",
		ReasonOccluded:       "covered",
		ReasonScreensOff:     "screens-off",
		ReasonBattery:        "battery",
		ReasonLowBattery:     "low-battery",
	}
	for r, s := range want {
		if got := r.String(); got != s {
			t.Errorf("Reason(%d).String() = %q, want %q", r, got, s)
		}
	}
	if got := Reasons(0).String(); got != "" {
		t.Errorf("empty Reasons.String() = %q, want \"\"", got)
	}
	rs := Reasons(0).With(ReasonBattery).With(ReasonLowBattery)
	if got := rs.String(); got != "low-battery" {
		t.Errorf("Reasons.String() = %q, want primary token low-battery", got)
	}
}
