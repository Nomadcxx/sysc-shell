package emoji

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Review focus 4.
func TestEmojiTableIntegrity(t *testing.T) {
	all := All()
	if len(all) < 1500 || len(all) > 4000 {
		t.Fatalf("table has %d rows; Unicode 16 without skin tones is ~1,900", len(all))
	}
	seen := map[string]bool{}
	for _, e := range all {
		if !utf8.ValidString(e.Char) || e.Char == "" || e.Name == "" {
			t.Fatalf("bad row %+v", e)
		}
		for _, r := range e.Char {
			if r >= 0x1F3FB && r <= 0x1F3FF {
				t.Fatalf("skin-tone modifier in %q", e.Name)
			}
		}
		if seen[e.Char] {
			t.Fatalf("duplicate %q", e.Char)
		}
		seen[e.Char] = true
	}
}

func TestSearchRanksNameBeforeKeyword(t *testing.T) {
	got := Search("party", 5)
	if len(got) == 0 || got[0].Name != "party popper" {
		t.Fatalf("first result %+v, want party popper (name prefix beats keyword)", got)
	}
	names := make([]string, len(got))
	for i, e := range got {
		names[i] = e.Name
	}
	if !contains(names, "partying face") {
		t.Fatalf("partying face missing from %v", names)
	}
}

func TestSearchExactNameFirstAndCap(t *testing.T) {
	if got := Search("fire", 50); len(got) == 0 || got[0].Name != "fire" {
		t.Fatalf("exact name not first: %+v", got[:min(3, len(got))])
	}
	if got := Search("a", 50); len(got) != 50 {
		t.Fatalf("cap: %d", len(got))
	}
	if got := Search("", 50); len(got) != 50 || got[0].Char != All()[0].Char {
		t.Fatal("empty query lists the table head")
	}
	if got := Search("PARTY", 5); len(got) == 0 || got[0].Name != "party popper" {
		t.Fatal("case-insensitive")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
