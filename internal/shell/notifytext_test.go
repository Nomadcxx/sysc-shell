package shell

import (
	"strings"
	"testing"
)

func TestNotifyTextAppliesTheSupportedSubset(t *testing.T) {
	runs := ParseBody("plain <b>bold</b> <i>italic</i> <u>under</u>", true)
	got := map[string]Run{}
	for _, run := range runs {
		got[strings.TrimSpace(run.Text)] = run
	}
	if !got["bold"].Bold || got["bold"].Italic {
		t.Fatalf("bold run = %+v", got["bold"])
	}
	if !got["italic"].Italic {
		t.Fatalf("italic run = %+v", got["italic"])
	}
	if !got["under"].Underline {
		t.Fatalf("underline run = %+v", got["under"])
	}
	if got["plain"].Bold || got["plain"].Italic || got["plain"].Underline {
		t.Fatalf("plain run = %+v", got["plain"])
	}
}

func TestNotifyTextNestsStylesAndClosesThemInOrder(t *testing.T) {
	runs := ParseBody("<b>bold <i>both</i> bold</b>", true)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}
	if !runs[1].Bold || !runs[1].Italic {
		t.Fatalf("nested run = %+v, want both styles", runs[1])
	}
	if runs[2].Italic {
		t.Fatal("italic outlived its closing tag")
	}
	if !runs[2].Bold {
		t.Fatal("bold ended early")
	}
}

func TestNotifyTextTurnsBreaksIntoLines(t *testing.T) {
	for _, body := range []string{"one\ntwo", "one<br>two", "one<br/>two"} {
		runs := ParseBody(body, true)
		if len(runs) != 3 || !runs[1].Break {
			t.Fatalf("%q produced %+v", body, runs)
		}
		if runs[0].Text != "one" || runs[2].Text != "two" {
			t.Fatalf("%q split into %q / %q", body, runs[0].Text, runs[2].Text)
		}
	}
}

func TestNotifyTextDecodesEntities(t *testing.T) {
	runs := ParseBody("a &amp; b &lt;c&gt; &quot;d&quot; &apos;e&apos;", true)
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].Text != `a & b <c> "d" 'e'` {
		t.Fatalf("decoded %q", runs[0].Text)
	}
}

func TestNotifyTextCarriesLinksOnlyWhenAllowed(t *testing.T) {
	body := `see <a href="https://example.test/x">the page</a>`
	allowed := ParseBody(body, true)
	var link Run
	for _, run := range allowed {
		if run.Link {
			link = run
		}
	}
	if link.Href != "https://example.test/x" || link.Text != "the page" {
		t.Fatalf("link run = %+v", link)
	}
	if !link.Underline {
		t.Fatal("a link should read as one")
	}

	// With the opener capability off, the anchor text survives without
	// becoming something the shell offers to open.
	refused := ParseBody(body, false)
	for _, run := range refused {
		if run.Link || run.Href != "" {
			t.Fatalf("a link survived with links disabled: %+v", run)
		}
	}
	if !strings.Contains(joined(refused), "the page") {
		t.Fatalf("anchor text was lost: %q", joined(refused))
	}
}

func TestNotifyTextRefusesOversizedLinkTargets(t *testing.T) {
	body := `<a href="` + strings.Repeat("x", MaxLinkBytes+1) + `">text</a>`
	for _, run := range ParseBody(body, true) {
		if run.Link {
			t.Fatal("an oversized link target was accepted")
		}
	}
}

func TestNotifyTextShowsImageAltRatherThanFetching(t *testing.T) {
	runs := ParseBody(`before <img src="https://example.test/a.png" alt="a picture"> after`, true)
	if text := joined(runs); !strings.Contains(text, "a picture") {
		t.Fatalf("alt text missing from %q", text)
	}
	if text := joined(runs); strings.Contains(text, "example.test") {
		t.Fatalf("an image source leaked into the body: %q", text)
	}
}

func TestNotifyTextFallsBackToPlainOnInvalidMarkup(t *testing.T) {
	for name, body := range map[string]string{
		"unknown tag":    "a <blink>b</blink> c",
		"unclosed":       "a <b>b",
		"stray close":    "a </b> b",
		"unterminated":   "a <b c",
		"unknown entity": "a &nope; b",
		"too deep":       strings.Repeat("<b>", MaxMarkupDepth+1) + "x" + strings.Repeat("</b>", MaxMarkupDepth+1),
	} {
		t.Run(name, func(t *testing.T) {
			runs := ParseBody(body, true)
			if len(runs) == 0 {
				t.Fatal("invalid markup produced no text at all")
			}
			for _, run := range runs {
				if run.Bold || run.Italic || run.Underline || run.Link {
					t.Fatalf("invalid markup produced styled runs: %+v", runs)
				}
			}
			if got := joined(runs); !strings.Contains(got, "b") && !strings.Contains(got, "x") {
				t.Fatalf("fallback lost the text: %q", got)
			}
		})
	}
}

func TestNotifyTextStaysWithinItsRunBound(t *testing.T) {
	runs := ParseBody(strings.Repeat("<b>x</b>\n", MaxRuns*2), true)
	if len(runs) > MaxRuns {
		t.Fatalf("produced %d runs, want at most %d", len(runs), MaxRuns)
	}
	if len(runs) == 0 {
		t.Fatal("a long body produced nothing")
	}
}

func TestNotifyTextHandlesAnEmptyBody(t *testing.T) {
	if runs := ParseBody("", true); len(runs) != 0 {
		t.Fatalf("empty body produced %+v", runs)
	}
}

func joined(runs []Run) string {
	var out strings.Builder
	for _, run := range runs {
		if run.Break {
			out.WriteByte('\n')
			continue
		}
		out.WriteString(run.Text)
	}
	return out.String()
}
