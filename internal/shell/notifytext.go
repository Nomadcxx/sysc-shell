package shell

import (
	"strings"
	"unicode/utf8"
)

// Notification bodies carry the freedesktop markup subset: <b>, <i>, <u>,
// <a href>, <img> and line breaks. The body is another process's text, so the
// parser is bounded everywhere it could grow and never renders a tag it does
// not know.
const (
	// MaxRuns caps the styled spans one body produces.
	MaxRuns = 256
	// MaxMarkupDepth caps nesting. Past it the body is treated as plain text.
	MaxMarkupDepth = 16
	// MaxLinkBytes caps one link target.
	MaxLinkBytes = 2 << 10
)

// Run is one styled span of body text. A run never spans a line break: the
// card builder turns each break into a new line of nodes.
type Run struct {
	Text      string
	Bold      bool
	Italic    bool
	Underline bool
	// Link is the href of the enclosing anchor, empty when there is none or
	// when link support is off.
	Link  bool
	Href  string
	Break bool
}

// style is the active markup state while parsing.
type style struct {
	bold      int
	italic    int
	underline int
	href      string
}

// ParseBody turns a notification body into bounded styled runs.
//
// Markup the spec does not define, or markup that does not close, makes the
// whole body plain text rather than a guess: a half-applied style is a worse
// lie than no style. Links become runs only when the shell is allowed to open
// them; otherwise the anchor text survives unstyled.
func ParseBody(body string, allowLinks bool) []Run {
	if body == "" {
		return nil
	}
	runs, ok := parseMarkup(body, allowLinks)
	if !ok {
		return plainRuns(body)
	}
	return runs
}

func parseMarkup(body string, allowLinks bool) ([]Run, bool) {
	var runs []Run
	var stack []style
	current := style{}
	var text strings.Builder

	flush := func() bool {
		if text.Len() == 0 {
			return true
		}
		if len(runs) >= MaxRuns {
			return false
		}
		runs = append(runs, Run{
			Text: text.String(), Bold: current.bold > 0, Italic: current.italic > 0,
			Underline: current.underline > 0 || current.href != "",
			Link:      current.href != "", Href: current.href,
		})
		text.Reset()
		return true
	}

	for i := 0; i < len(body); {
		switch body[i] {
		case '<':
			end := strings.IndexByte(body[i:], '>')
			if end < 0 {
				return nil, false
			}
			tag := body[i+1 : i+end]
			i += end + 1
			if !flush() {
				return nil, false
			}
			closing := strings.HasPrefix(tag, "/")
			name, attributes := splitTag(strings.TrimPrefix(tag, "/"))
			switch name {
			case "b", "i", "u", "a":
				if closing {
					if len(stack) == 0 {
						return nil, false
					}
					current = stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					continue
				}
				if len(stack) >= MaxMarkupDepth {
					return nil, false
				}
				stack = append(stack, current)
				switch name {
				case "b":
					current.bold++
				case "i":
					current.italic++
				case "u":
					current.underline++
				case "a":
					href := attributeValue(attributes, "href")
					if allowLinks && href != "" && len(href) <= MaxLinkBytes && utf8.ValidString(href) {
						current.href = href
					}
				}
			case "br":
				// <br> and <br/> are breaks, never a nesting level.
				if closing {
					return nil, false
				}
				if len(runs) >= MaxRuns {
					return nil, false
				}
				runs = append(runs, Run{Break: true})
			case "img":
				// The spec allows <img>; the shell shows the alt text rather
				// than fetching anything a notification names.
				if closing {
					return nil, false
				}
				text.WriteString(attributeValue(attributes, "alt"))
			default:
				return nil, false
			}
		case '&':
			entity, width, ok := decodeEntity(body[i:])
			if !ok {
				return nil, false
			}
			text.WriteString(entity)
			i += width
		case '\n':
			if !flush() {
				return nil, false
			}
			if len(runs) >= MaxRuns {
				return nil, false
			}
			runs = append(runs, Run{Break: true})
			i++
		default:
			text.WriteByte(body[i])
			i++
		}
	}
	if len(stack) != 0 {
		return nil, false
	}
	if !flush() {
		return nil, false
	}
	return runs, true
}

// plainRuns is the fallback: the body shown exactly as it arrived, split only
// on real line breaks.
func plainRuns(body string) []Run {
	var runs []Run
	for i, line := range strings.Split(body, "\n") {
		if i > 0 {
			if len(runs) >= MaxRuns {
				return runs
			}
			runs = append(runs, Run{Break: true})
		}
		if line == "" {
			continue
		}
		if len(runs) >= MaxRuns {
			return runs
		}
		runs = append(runs, Run{Text: line})
	}
	return runs
}

func splitTag(tag string) (string, string) {
	tag = strings.TrimSuffix(strings.TrimSpace(tag), "/")
	if index := strings.IndexAny(tag, " \t"); index >= 0 {
		return strings.ToLower(tag[:index]), tag[index+1:]
	}
	return strings.ToLower(tag), ""
}

// attributeValue reads one quoted attribute. An unquoted or malformed value
// reports empty, which the caller treats as absent.
func attributeValue(attributes, name string) string {
	rest := attributes
	for {
		index := strings.Index(strings.ToLower(rest), name+"=")
		if index < 0 {
			return ""
		}
		rest = rest[index+len(name)+1:]
		if rest == "" {
			return ""
		}
		quote := rest[0]
		if quote != '"' && quote != '\'' {
			continue
		}
		end := strings.IndexByte(rest[1:], quote)
		if end < 0 {
			return ""
		}
		value, ok := unescapeEntities(rest[1 : 1+end])
		if !ok {
			return ""
		}
		return value
	}
}

func unescapeEntities(value string) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '&' {
			out.WriteByte(value[i])
			i++
			continue
		}
		entity, width, ok := decodeEntity(value[i:])
		if !ok {
			return "", false
		}
		out.WriteString(entity)
		i += width
	}
	return out.String(), true
}

// decodeEntity reads one XML entity, reporting how many bytes it consumed.
func decodeEntity(input string) (string, int, bool) {
	for entity, decoded := range map[string]string{
		"&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": `"`, "&apos;": "'",
	} {
		if strings.HasPrefix(input, entity) {
			return decoded, len(entity), true
		}
	}
	return "", 0, false
}
