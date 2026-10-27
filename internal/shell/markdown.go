package shell

import (
	"regexp"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

const maxMarkdownNodes = 400

var (
	markdownImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	markdownLink  = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	markdownBold  = regexp.MustCompile(`\*\*[^*]+\*\*|__[^_]+__`)
	markdownHTML  = regexp.MustCompile(`(?s)<!--.*?-->|</?[A-Za-z][^>]*>`)
)

func markdownNodes(src string, width int, m theme.Metrics, measure ui.MeasureText) []*ui.Node {
	truncated := int64(len(src)) > catalog.MaxReadmeBytes
	if truncated {
		src = src[:int(catalog.MaxReadmeBytes)]
	}
	src = strings.ToValidUTF8(src, "�")
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(src, "\n")
	width = max(width, 0)
	blockGap := max(m.CardGap/2, 0)
	measureWidth := func(role theme.TextRole) func(string) int {
		attrs := ui.TextAttrs{Role: role}
		return func(text string) int {
			w, _ := measure(text, attrs)
			return w
		}
	}
	bodyMeasure := measureWidth(theme.RoleBody)
	monoMeasure := measureWidth(theme.RoleMono)

	nodes := make([]*ui.Node, 0, min(len(lines), 64))
	nodeCount := 0
	add := func(node *ui.Node) bool {
		count := markdownTreeSize(node)
		if nodeCount+count > maxMarkdownNodes-1 { // keep one node for the truncation notice
			return false
		}
		nodes = append(nodes, node)
		nodeCount += count
		return true
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if role, text, ok := markdownHeading(line); ok {
			text = markdownInline(text)
			if text != "" && !add(&ui.Node{Kind: ui.KindText, Text: text, TextRole: role}) {
				truncated = true
				break
			}
			i++
			continue
		}
		if markdownRule(line) {
			if !add(&ui.Node{Kind: ui.KindSeparator}) {
				truncated = true
				break
			}
			i++
			continue
		}
		if fence, ok := markdownFence(line); ok {
			budget := maxMarkdownNodes - 1 - nodeCount
			node, next, clipped := markdownCodeBlock(lines, i+1, fence, width, m, budget, monoMeasure)
			if node != nil && !add(node) {
				truncated = true
				break
			}
			i = next
			truncated = truncated || clipped
			if clipped {
				break
			}
			continue
		}
		if item, ok := markdownListItem(line); ok {
			budget := maxMarkdownNodes - 1 - nodeCount
			node, clipped := markdownListNode(item, width, blockGap, budget, bodyMeasure)
			if node != nil && !add(node) {
				truncated = true
				break
			}
			i++
			if clipped {
				truncated = true
				break
			}
			continue
		}
		if markdownTableStart(lines, i) {
			next, clipped := markdownTable(lines, i, width, blockGap,
				maxMarkdownNodes-1-nodeCount, add)
			i = next
			if clipped {
				truncated = true
				break
			}
			continue
		}

		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" &&
			!markdownBlockStart(lines[i]) && !markdownTableStart(lines, i) {
			i++
		}
		if i == start { // malformed or unsupported syntax: make progress as plain text
			i++
		}
		paragraph := markdownInline(strings.Join(lines[start:i], " "))
		if paragraph == "" {
			continue
		}
		budget := maxMarkdownNodes - 1 - nodeCount
		node, clipped := markdownParagraph(paragraph, width, budget, bodyMeasure)
		if node != nil && !add(node) {
			truncated = true
			break
		}
		if clipped {
			truncated = true
			break
		}
	}

	if truncated {
		nodes = append(nodes, &ui.Node{Kind: ui.KindText, Text: "README truncated", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	return nodes
}

func markdownInline(src string) string {
	src = markdownImage.ReplaceAllString(src, "")
	src = markdownLink.ReplaceAllString(src, "$1 ($2)")
	src = markdownBold.ReplaceAllStringFunc(src, func(mark string) string {
		return mark[2 : len(mark)-2]
	})
	src = markdownHTML.ReplaceAllString(src, "")
	return strings.TrimSpace(src)
}

func markdownHeading(line string) (theme.TextRole, string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	count := 0
	for count < len(trimmed) && trimmed[count] == '#' {
		count++
	}
	if count == 0 || count > 6 || count == len(trimmed) || trimmed[count] != ' ' && trimmed[count] != '\t' {
		return theme.RoleBody, "", false
	}
	role := theme.RoleTitle
	if count == 1 {
		role = theme.RoleHeadline
	}
	return role, strings.TrimSpace(trimmed[count:]), true
}

func markdownRule(line string) bool {
	compact := strings.ReplaceAll(strings.TrimSpace(line), " ", "")
	compact = strings.ReplaceAll(compact, "\t", "")
	if len(compact) < 3 {
		return false
	}
	mark := compact[0]
	if mark != '-' && mark != '*' && mark != '_' {
		return false
	}
	for i := 1; i < len(compact); i++ {
		if compact[i] != mark {
			return false
		}
	}
	return true
}

func markdownFence(line string) (fence, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '`' && trimmed[0] != '~' {
		return fence{}, false
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == trimmed[0] {
		count++
	}
	if count < 3 {
		return fence{}, false
	}
	return fence{mark: trimmed[0], size: count}, true
}

type fence struct {
	mark byte
	size int
}

func markdownFenceClose(line string, opening fence) bool {
	trimmed := strings.TrimSpace(line)
	count := 0
	for count < len(trimmed) && trimmed[count] == opening.mark {
		count++
	}
	return count >= opening.size && strings.TrimSpace(trimmed[count:]) == ""
}

func markdownCodeBlock(lines []string, start int, opening fence, width int, m theme.Metrics, budget int, measure func(string) int) (*ui.Node, int, bool) {
	pad := max(m.CardPadding, 0)
	if budget < 2 {
		return nil, len(lines), true
	}
	lineBudget := budget - 2 // capsule and column
	contentWidth := max(width-2*pad, 0)
	children := make([]*ui.Node, 0, min(lineBudget, 32))
	truncated := false
	i := start
	for ; i < len(lines); i++ {
		if markdownFenceClose(lines[i], opening) {
			i++
			break
		}
		chunks, clipped := markdownCodeLine(lines[i], contentWidth, lineBudget-len(children), measure)
		for _, chunk := range chunks {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: chunk, TextRole: theme.RoleMono, Tone: ui.ToneSubtle})
		}
		if clipped {
			truncated = true
			break
		}
	}
	column := &ui.Node{Kind: ui.KindColumn, Width: contentWidth, Gap: max(m.CardGap/2, 0), Children: children}
	return &ui.Node{Kind: ui.KindCapsule, Width: width, Fill: ui.FillContainerHigh, Padding: pad, Children: []*ui.Node{column}}, i, truncated
}

func markdownCodeLine(line string, width, maxLines int, measure func(string) int) ([]string, bool) {
	if maxLines <= 0 {
		return nil, line != ""
	}
	runes := []rune(line)
	if len(runes) == 0 {
		return []string{""}, false
	}
	width = max(width, 1)
	if measure(line) <= width {
		return []string{line}, false
	}
	lines := make([]string, 0, min(len(runes), maxLines))
	for start := 0; start < len(runes); {
		if len(lines) == maxLines {
			return lines, true
		}
		end := min(start+max(width/8, 1), len(runes))
		for end > start+1 && measure(string(runes[start:end])) > width {
			end--
		}
		lines = append(lines, string(runes[start:end]))
		start = end
	}
	return lines, false
}

func markdownListItem(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && (trimmed[1] == ' ' || trimmed[1] == '\t') {
		return strings.TrimSpace(trimmed[2:]), true
	}
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(trimmed) && (trimmed[i] == '.' || trimmed[i] == ')') && (trimmed[i+1] == ' ' || trimmed[i+1] == '\t') {
		return strings.TrimSpace(trimmed[i+2:]), true
	}
	return "", false
}

func markdownListNode(text string, width, gap, budget int, measure func(string) int) (*ui.Node, bool) {
	if budget < 4 {
		return nil, true
	}
	text = markdownInline(text)
	if text == "" {
		return nil, false
	}
	textWidth := max(width-20, 0)
	lines, clipped := markdownWrap(text, textWidth, budget-3, measure)
	if len(lines) == 0 {
		return nil, true
	}
	textNodes := make([]*ui.Node, len(lines))
	for i, line := range lines {
		textNodes[i] = &ui.Node{Kind: ui.KindText, Text: line}
	}
	return &ui.Node{Kind: ui.KindRow, Width: width, Gap: gap, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "•", TextRole: theme.RoleLabel},
		{Kind: ui.KindColumn, Width: textWidth, Children: textNodes},
	}}, clipped
}

func markdownTableRow(line string) bool { return strings.Contains(line, "|") }

// markdownTableRowStyled reports whether line is shaped like a row of the
// table whose header line carries the given outer-pipe style. Rows of one
// table share that style, so a line with a pipe that does not match it is
// prose following the table and must not be absorbed as a row.
func markdownTableRowStyled(line string, lead, trail bool) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") {
		return false
	}
	return strings.HasPrefix(trimmed, "|") == lead && strings.HasSuffix(trimmed, "|") == trail
}

func markdownTableStart(lines []string, start int) bool {
	if start < 0 || start+1 >= len(lines) || !markdownTableRow(lines[start]) || !markdownTableRow(lines[start+1]) {
		return false
	}
	header := markdownTableCells(lines[start])
	delimiter := markdownTableCells(lines[start+1])
	return len(header) > 0 && len(delimiter) == len(header) && markdownTableRule(delimiter)
}

func markdownTable(lines []string, start, width, gap, budget int, add func(*ui.Node) bool) (int, bool) {
	first := markdownTableCells(lines[start])
	if markdownTableRule(first) {
		return start + 1, false
	}
	columns := len(first)
	if columns == 0 {
		return start + 1, false
	}
	maxColumns := max((budget-1)/2, 0)
	clipped := columns > maxColumns
	columns = min(columns, maxColumns)
	if columns == 0 {
		return start, true
	}
	cellGap := max(gap/2, 0)
	cellWidth := max((width-cellGap*(columns-1))/columns, 1)
	lead := strings.HasPrefix(strings.TrimSpace(lines[start]), "|")
	trail := strings.HasSuffix(strings.TrimSpace(lines[start]), "|")
	i := start
	for i < len(lines) && (i == start+1 || markdownTableRowStyled(lines[i], lead, trail)) {
		cells := markdownTableCells(lines[i])
		i++
		if markdownTableRule(cells) {
			continue
		}
		row := &ui.Node{Kind: ui.KindRow, Width: width, Gap: cellGap,
			Children: make([]*ui.Node, columns)}
		for col := 0; col < columns; col++ {
			cell := ""
			if col < len(cells) {
				cell = markdownInline(cells[col])
			}
			row.Children[col] = &ui.Node{Kind: ui.KindColumn, Width: cellWidth, Children: []*ui.Node{
				{Kind: ui.KindText, Text: cell, MaxWidth: cellWidth},
			}}
		}
		if !add(row) {
			return i - 1, true
		}
		if clipped {
			return i, true
		}
		budget -= 1 + 2*columns
		maxColumns = max((budget-1)/2, 0)
		if maxColumns < columns && i < len(lines) && (i == start+1 || markdownTableRowStyled(lines[i], lead, trail)) {
			return i, true
		}
	}
	return i, false
}

func markdownTableCells(line string) []string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func markdownTableRule(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		cell = strings.TrimPrefix(cell, ":")
		cell = strings.TrimSuffix(cell, ":")
		if cell == "" || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func markdownParagraph(text string, width, budget int, measure func(string) int) (*ui.Node, bool) {
	if budget < 2 {
		return nil, true
	}
	lines, clipped := markdownWrap(text, width, budget-1, measure)
	if len(lines) == 0 {
		return nil, clipped
	}
	children := make([]*ui.Node, len(lines))
	for i, line := range lines {
		children[i] = &ui.Node{Kind: ui.KindText, Text: line}
	}
	return &ui.Node{Kind: ui.KindColumn, Width: width, Children: children}, clipped
}

func markdownWrap(text string, width, maxLines int, measure func(string) int) ([]string, bool) {
	if maxLines <= 0 {
		return nil, text != ""
	}
	lines := wrapLines(text, max(width, 1), measure, maxLines+1)
	if len(lines) > maxLines {
		return lines[:maxLines], true
	}
	return lines, false
}

func markdownBlockStart(line string) bool {
	if _, _, ok := markdownHeading(line); ok || markdownRule(line) {
		return true
	}
	if _, ok := markdownFence(line); ok {
		return true
	}
	_, ok := markdownListItem(line)
	return ok
}

func markdownTreeSize(n *ui.Node) int {
	if n == nil {
		return 0
	}
	count := 1
	for _, child := range n.Children {
		count += markdownTreeSize(child)
	}
	return count
}
