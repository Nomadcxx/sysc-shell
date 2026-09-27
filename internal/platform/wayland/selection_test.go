package wayland

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPickTextMimePrefersUTF8(t *testing.T) {
	if got, ok := pickTextMime([]string{"image/png", "STRING", "text/plain;charset=utf-8"}); !ok || got != "text/plain;charset=utf-8" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := pickTextMime([]string{"image/png"}); ok {
		t.Fatal("picked a text mime from an image-only offer")
	}
}

func TestReadPasteCapsAndTimesOut(t *testing.T) {
	r, w, _ := os.Pipe()
	go func() { w.WriteString(strings.Repeat("x", 100)); w.Close() }()
	got, err := readPaste(r, 10, time.Second)
	if err != nil || got != strings.Repeat("x", 10) {
		t.Fatalf("capped read = %q, %v", got, err)
	}
	r2, w2, _ := os.Pipe()
	defer w2.Close()
	if _, err := readPaste(r2, 10, 20*time.Millisecond); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("stalled writer: err = %v, want deadline exceeded", err)
	}
}

func TestSanitizePasteDropsNUL(t *testing.T) {
	if got := sanitizePaste("a\x00b\nc"); got != "ab\nc" {
		t.Fatalf("got %q", got)
	}
}

// A paste that returns after focus moved is dropped, not typed elsewhere.
func TestPasteForAnUnfocusedSurfaceIsDropped(t *testing.T) {
	rh := newRepeatHarness(t, 0, 0)
	host, unit := rh.o.keyFocus.host, rh.o.keyFocus.unit
	rh.o.leaveKeyboard()
	rh.o.deliverPaste(pasteResult{unit: unit, text: "hi"})
	for _, e := range *rh.seen {
		if e.Kind == EventPaste {
			t.Fatal("paste delivered to a surface without keyboard focus")
		}
	}
	rh.o.enterKeyboard(host, unit)
	rh.o.deliverPaste(pasteResult{unit: unit, text: "hi"})
	got := (*rh.seen)[len(*rh.seen)-1]
	if got.Kind != EventPaste || got.Paste != "hi" {
		t.Fatalf("focused surface got %+v, want the paste", got)
	}
}
