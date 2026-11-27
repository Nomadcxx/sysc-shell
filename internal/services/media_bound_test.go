package services

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipMediaTextKeepsAValidPrefix(t *testing.T) {
	cases := []string{
		strings.Repeat("a", maxMediaTextBytes*2),
		strings.Repeat("界", maxMediaTextBytes),
		"short title",
	}
	for _, in := range cases {
		got := clipMediaText(in)
		if len(got) > maxMediaTextBytes {
			t.Fatalf("clipMediaText left %d bytes, want at most %d", len(got), maxMediaTextBytes)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("clipMediaText split a UTF-8 sequence: %q", got)
		}
		if !strings.HasPrefix(in, got) {
			t.Fatalf("clipMediaText did not return a prefix")
		}
	}
}
