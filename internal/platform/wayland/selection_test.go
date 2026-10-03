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

func TestSelectionOfferForTextAndImage(t *testing.T) {
	t.Parallel()
	mimes, payload := SelectionRequest{Copy: "hi"}.offer()
	if len(mimes) != len(textMimes) || mimes[0] != textMimes[0] || string(payload) != "hi" {
		t.Fatalf("text offer = %v %q", mimes, payload)
	}
	mimes, payload = SelectionRequest{Mime: "image/png", Data: []byte{1, 2}}.offer()
	if len(mimes) != 1 || mimes[0] != "image/png" || len(payload) != 2 {
		t.Fatalf("image offer = %v %v", mimes, payload)
	}
}

// A copy whose sender waits on Done must hear back even when the seat has no
// data device, or the selector it belongs to never closes.
func TestSelectionCopyWithoutADeviceStillAnswersDone(t *testing.T) {
	t.Parallel()
	o := &owner{}
	done := make(chan error, 1)
	o.handleSelection(SelectionRequest{Mime: "image/png", Data: []byte{1}, Done: done}, nil)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Done reported success with no data device")
		}
	default:
		t.Fatal("Done was never answered")
	}
}
