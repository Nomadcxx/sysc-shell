package wayland

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func withKeymap(t *testing.T, rh *repeatHarness, name string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	k, err := newKeymapResolver(raw, "testdata/Compose.test")
	if err != nil {
		t.Fatal(err)
	}
	rh.o.keymap = k
}

func TestOwnerTypesThroughTheKeymap(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-de.xkb")
	rh.o.setModifiers(0, 0, 0, 0)
	rh.o.deliverKey(5, evY, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "z" {
		t.Fatalf("de Y = %q", got.Text)
	}
}

// Review focus 5.
func TestRepeatResolvesTextAtDeliveryTime(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-de.xkb")
	rh.o.setModifiers(0, 0, 0, 0)
	rh.o.deliverKey(5, evA, uint32(client.KeyboardKeyStatePressed))
	rh.o.setModifiers(maskShift, 0, 0, 0)
	rh.advance(600 * time.Millisecond)
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "A" {
		t.Fatalf("repeat after Shift = %q, want A", got.Text)
	}
}

func TestUnsupportedKeymapFormatKeepsTheFallback(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	rh.o.loadKeymap(0 /* no_keymap */, -1, 0)
	if rh.o.keymap != nil {
		t.Fatal("no_keymap installed a resolver")
	}
	rh.o.deliverKey(5, evA, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "a" {
		t.Fatalf("fallback a = %q", got.Text)
	}
}

func TestLeavingFocusCancelsAnOpenCompose(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-us-intl.xkb")
	host, unit := rh.o.keyFocus.host, rh.o.keyFocus.unit
	rh.o.deliverKey(5, evApostrophe, uint32(client.KeyboardKeyStatePressed))
	rh.o.leaveKeyboard()
	rh.o.enterKeyboard(host, unit)
	rh.o.deliverKey(6, evE, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "e" {
		t.Fatalf("after refocus e = %q, want plain e", got.Text)
	}
}

// The keymap arrives as a NUL-terminated file descriptor the owner maps.
func TestKeymapLoadsFromTheCompositorFD(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	raw, err := os.ReadFile("testdata/keymap-de.xkb")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "keymap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(raw, 0)); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	rh.o.loadKeymap(uint32(client.KeyboardKeymapFormatXkbV1), fd, uint32(len(raw)+1))
	if rh.o.keymap == nil {
		t.Fatal("xkb_v1 keymap was not installed")
	}
	rh.o.deliverKey(5, evY, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "z" {
		t.Fatalf("de Y after loading = %q", got.Text)
	}
}

// Releasing the dead key before the next press is how everyone types; the
// release must not feed compose and cancel the sequence.
func TestDeadKeyReleaseKeepsTheSequence(t *testing.T) {
	rh := newRepeatHarness(t, 25, 600)
	withKeymap(t, rh, "keymap-us-intl.xkb")
	rh.o.deliverKey(5, evApostrophe, uint32(client.KeyboardKeyStatePressed))
	rh.o.deliverKey(6, evApostrophe, uint32(client.KeyboardKeyStateReleased))
	rh.o.deliverKey(7, evE, uint32(client.KeyboardKeyStatePressed))
	if got := (*rh.seen)[len(*rh.seen)-1]; got.Text != "é" {
		t.Fatalf("dead_acute, release, e = %q, want é", got.Text)
	}
}
