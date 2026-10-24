package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func markdownTestMetrics() theme.Metrics {
	return theme.Metrics{CardPadding: 10, CardGap: 8, PanelPadding: 12}
}

func markdownTestMeasure(text string, _ ui.TextAttrs) (int, int) {
	width := 0
	for _, r := range text {
		if r >= 0x2e80 {
			width += 16
		} else {
			width += 8
		}
	}
	return width, 16
}

func markdownTexts(nodes []*ui.Node) []string {
	var out []string
	var visit func(*ui.Node)
	visit = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Text != "" {
			out = append(out, n.Text)
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, n := range nodes {
		visit(n)
	}
	return out
}

func markdownNodeCount(nodes []*ui.Node) int {
	count := 0
	var visit func(*ui.Node)
	visit = func(n *ui.Node) {
		if n == nil {
			return
		}
		count++
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, n := range nodes {
		visit(n)
	}
	return count
}

func TestMarkdownNodesHeadingParagraphAndList(t *testing.T) {
	heading := markdownNodes("# Store README", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(heading) != 1 || heading[0].Kind != ui.KindText || heading[0].Text != "Store README" ||
		heading[0].TextRole != theme.RoleHeadline || heading[0].Bold {
		t.Fatalf("heading = %+v", heading)
	}

	paragraph := markdownNodes("one two three four five", 80, markdownTestMetrics(), markdownTestMeasure)
	if len(paragraph) != 1 || paragraph[0].Kind != ui.KindColumn || len(paragraph[0].Children) < 2 {
		t.Fatalf("paragraph did not wrap into a column: %+v", paragraph)
	}
	if got := strings.Join(markdownTexts(paragraph), " "); got != "one two three four five" {
		t.Fatalf("wrapped paragraph = %q", got)
	}

	list := markdownNodes("- a list item", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(list) != 1 || list[0].Kind != ui.KindRow || len(list[0].Children) != 2 ||
		list[0].Children[0].Text != "•" || list[0].Children[1].Kind != ui.KindColumn {
		t.Fatalf("list item = %+v", list)
	}
}

func TestMarkdownNodesInlineSubset(t *testing.T) {
	nodes := markdownNodes("Text **bold** [label](https://example.test) and `code` ![image](https://img.test) <b>HTML</b>.", 800, markdownTestMetrics(), markdownTestMeasure)
	got := strings.Join(markdownTexts(nodes), " ")
	for _, want := range []string{"Text", "bold", "label (https://example.test)", "`code`", "HTML"} {
		if !strings.Contains(got, want) {
			t.Errorf("inline text %q missing from %q", want, got)
		}
	}
	for _, unwanted := range []string{"**", "![", "image", "<b>", "</b>"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unsupported inline markup %q remains in %q", unwanted, got)
		}
	}
}

func TestMarkdownNodesCodeTableAndRule(t *testing.T) {
	code := markdownNodes("```go\nfmt.Println(1)\n```", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(code) != 1 || code[0].Kind != ui.KindCapsule || len(code[0].Children) != 1 ||
		code[0].Children[0].Kind != ui.KindColumn || len(code[0].Children[0].Children) != 1 ||
		code[0].Children[0].Children[0].TextRole != theme.RoleMono {
		t.Fatalf("code block = %+v", code)
	}

	table := markdownNodes("| name | version |\n| --- | --- |\n| Timer | 1.0 |", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(table) != 2 {
		t.Fatalf("table rows = %d, want header and data rows", len(table))
	}
	for _, row := range table {
		if row.Kind != ui.KindRow || len(row.Children) != 2 || row.Children[0].Kind != ui.KindColumn ||
			row.Children[1].Kind != ui.KindColumn || row.Children[0].Width != row.Children[1].Width {
			t.Fatalf("table row cells are not fixed-width columns: %+v", row)
		}
	}

	rule := markdownNodes("---", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(rule) != 1 || rule[0].Kind != ui.KindSeparator {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestMarkdownNodesKeepsPipeProseAsParagraph(t *testing.T) {
	nodes := markdownNodes("Use left | right when describing a choice.", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(nodes) != 1 || nodes[0].Kind != ui.KindColumn {
		t.Fatalf("pipe prose = %+v, want one paragraph column", nodes)
	}
}

func TestMarkdownNodesWrapsWideGlyphsWithinWidth(t *testing.T) {
	measure := ui.MeasureText(markdownTestMeasure)
	nodes := markdownNodes("漢漢漢", 32, markdownTestMetrics(), measure)
	if len(nodes) != 1 || nodes[0].Kind != ui.KindColumn {
		t.Fatalf("wide-glyph paragraph = %+v, want one paragraph column", nodes)
	}
	assertMarkdownLinesFit(t, nodes[0], measure)
	if len(nodes[0].Children) < 2 {
		t.Fatalf("wide glyphs stayed on one line at 32px: %+v", nodes[0].Children)
	}

	list := markdownNodes("- 漢漢漢", 60, markdownTestMetrics(), measure)
	if len(list) != 1 || list[0].Kind != ui.KindRow || len(list[0].Children) != 2 {
		t.Fatalf("wide-glyph list = %+v, want a measured list row", list)
	}
	assertMarkdownLinesFit(t, list[0].Children[1], measure)

	code := markdownNodes("```\n漢漢漢\n```", 52, markdownTestMetrics(), measure)
	if len(code) != 1 || code[0].Kind != ui.KindCapsule || len(code[0].Children) != 1 {
		t.Fatalf("wide-glyph code = %+v, want a code capsule", code)
	}
	assertMarkdownLinesFit(t, code[0].Children[0], measure)
	if len(code[0].Children[0].Children) < 2 {
		t.Fatalf("wide glyphs stayed on one code line: %+v", code[0].Children[0].Children)
	}
}

func assertMarkdownLinesFit(t *testing.T, column *ui.Node, measure ui.MeasureText) {
	t.Helper()
	for _, line := range column.Children {
		if line.Kind != ui.KindText {
			t.Fatalf("line node = %+v, want text", line)
		}
		if width, _ := measure(line.Text, ui.TextAttrsOf(line)); width > column.Width {
			t.Errorf("line %q measures %dpx in %dpx", line.Text, width, column.Width)
		}
	}
}

func TestMarkdownNodesUnterminatedFenceAndGarbage(t *testing.T) {
	code := markdownNodes("```\nunfinished <tag> & **text", 400, markdownTestMetrics(), markdownTestMeasure)
	if len(code) != 1 || code[0].Kind != ui.KindCapsule || !strings.Contains(strings.Join(markdownTexts(code), " "), "unfinished") {
		t.Fatalf("unterminated fence = %+v", code)
	}
	_ = markdownNodes(string([]byte{0xff, '\n', '[', ']', '(', ')', '`', '`', '`'}), -1, markdownTestMetrics(), markdownTestMeasure)
}

func TestMarkdownNodesTruncateLargeTablesAndInput(t *testing.T) {
	var src strings.Builder
	src.WriteString("| a | b | c |\n| --- | --- | --- |\n")
	for i := 0; i < 300; i++ {
		src.WriteString("| a | b | c |\n")
	}
	table := markdownNodes(src.String(), 400, markdownTestMetrics(), markdownTestMeasure)
	if markdownNodeCount(table) > 400 || len(table) == 0 || table[len(table)-1].Text != "README truncated" {
		t.Fatalf("table truncation: nodes=%d, tail=%+v", markdownNodeCount(table), table[len(table)-1])
	}

	readmeBytes := int(catalog.MaxReadmeBytes)
	large := "```\n" + strings.Repeat("x", readmeBytes+10) + "\nTAIL"
	limitedWidth := 8*readmeBytes + 2*markdownTestMetrics().CardPadding + 1
	limited := markdownNodes(large, limitedWidth, markdownTestMetrics(), markdownTestMeasure)
	if len(limited) == 0 || limited[len(limited)-1].Text != "README truncated" ||
		strings.Contains(strings.Join(markdownTexts(limited), ""), "TAIL") || markdownNodeCount(limited) > 400 {
		t.Fatalf("input cap not applied: nodes=%d, tail=%+v", markdownNodeCount(limited), limited[len(limited)-1])
	}
}

func FuzzMarkdownNodes(f *testing.F) {
	for _, src := range []string{"", "# title", "**bold**", "```\ncode", "| a | b |\n| -- | -- |", "![x](u)<script>"} {
		f.Add(src, 640)
	}
	f.Fuzz(func(t *testing.T, src string, width int) {
		nodes := markdownNodes(src, width, markdownTestMetrics(), markdownTestMeasure)
		if len(nodes) > 0 && nodes[len(nodes)-1].Text == "README truncated" {
			if markdownNodeCount(nodes) > 400 {
				t.Fatalf("truncated output has %d nodes", markdownNodeCount(nodes))
			}
		}
		if markdownNodeCount(nodes) > 400 {
			t.Fatalf("output has %d nodes", markdownNodeCount(nodes))
		}
	})
}
