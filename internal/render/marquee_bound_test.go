package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipMarqueeTextBoundsRunes(t *testing.T) {
	in := strings.Repeat("界", maxMarqueeRunes*4)
	got := clipMarqueeText(in)
	if n := utf8.RuneCountInString(got); n != maxMarqueeRunes {
		t.Fatalf("clipMarqueeText kept %d runes, want %d", n, maxMarqueeRunes)
	}
	if !utf8.ValidString(got) || !strings.HasPrefix(in, got) {
		t.Fatal("clipMarqueeText split a rune or changed the prefix")
	}
	if short := "now playing"; clipMarqueeText(short) != short {
		t.Fatal("clipMarqueeText changed a short string")
	}
}
