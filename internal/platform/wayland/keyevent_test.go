package wayland

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestKeyEventsCarryResolvedSymTextAndMods(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	rh.o.setModifiers(1<<0, 0, 0, 0) // Shift held
	rh.o.deliverKey(5, 30, uint32(client.KeyboardKeyStatePressed))
	got := (*rh.seen)[len(*rh.seen)-1]
	if got.Sym != 'A' || got.Text != "A" || !got.Mods.Has(ui.ModShift) {
		t.Fatalf("Shift+a = %+v", got)
	}
}

// A repeat is resolved when it fires, so a modifier change mid-hold shows.
func TestRepeatUsesModifiersHeldAtDelivery(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	rh.o.deliverKey(5, 30, uint32(client.KeyboardKeyStatePressed))
	rh.o.setModifiers(1<<0, 0, 0, 0)
	rh.advance(600 * time.Millisecond)
	got := (*rh.seen)[len(*rh.seen)-1]
	if got.Text != "A" {
		t.Fatalf("repeat after Shift = %q, want A", got.Text)
	}
}
