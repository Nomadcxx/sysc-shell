package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const diagnosticLimit = 4096

// Diagnostic is the bounded, source-redacted form of a configuration error.
// Excerpt is copied from the parsed revision and contains no source values.
type Diagnostic struct {
	File    string
	Field   string
	Message string
	Excerpt string
	Line    int
	Column  int
	cause   error
}

func (d *Diagnostic) Error() string {
	if d == nil {
		return "config: invalid configuration"
	}
	file := safeDiagnosticText(d.File, 512)
	field := safeFieldPath(d.Field)
	message := safeDiagnosticText(d.Message, 2048)
	if message == "" {
		message = "invalid configuration"
	}

	var prefix strings.Builder
	prefix.WriteString("config:")
	if d.Line > 0 && d.Column > 0 {
		if file == "" {
			file = "<config>"
		}
		fmt.Fprintf(&prefix, " %s:%d:%d:", file, d.Line, d.Column)
	} else if file != "" && file != "<config>" {
		fmt.Fprintf(&prefix, " %s:", file)
	}
	if field != "" {
		fmt.Fprintf(&prefix, " %s:", field)
	}
	prefix.WriteByte(' ')
	suffix := ""
	if d.Line > 0 && d.Column > 0 && d.Excerpt != "" {
		suffix = "\n" + safeExcerptText(d.Excerpt)
	}
	message = truncateUTF8(message, diagnosticLimit-prefix.Len()-len(suffix))
	return prefix.String() + message + suffix
}

func (d *Diagnostic) Unwrap() error {
	if d == nil {
		return nil
	}
	return d.cause
}

type pathError struct {
	field   string
	message string
}

func (e *pathError) Error() string {
	if e == nil {
		return "config: invalid value"
	}
	if e.field == "" {
		return "config: " + e.message
	}
	return "config: " + e.field + ": " + e.message
}

func newPathError(field, format string, args ...any) *pathError {
	safeArgs := make([]any, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case string:
			safeArgs[i] = "[redacted]"
		case time.Duration:
			safeArgs[i] = v
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
			safeArgs[i] = v
		default:
			safeArgs[i] = "[redacted]"
		}
	}
	return &pathError{
		field:   safeFieldPath(field),
		message: safeDiagnosticText(fmt.Sprintf(format, safeArgs...), 2048),
	}
}

type parseIssue struct {
	cause  error
	offset int64
}

func (e *parseIssue) Error() string { return e.cause.Error() }
func (e *parseIssue) Unwrap() error { return e.cause }

type extraJSONValueError struct{}

func (extraJSONValueError) Error() string { return "more than one JSON value" }

// diagnosticAt binds a decoder or validation error to the exact bytes that
// were read. Callers pass a one-based byte offset, or zero when it is unknown.
func diagnosticAt(file string, source []byte, offset int64, cause error) error {
	if cause == nil {
		cause = errors.New("invalid configuration")
	}
	if issue, ok := cause.(*parseIssue); ok {
		offset, cause = issue.offset, issue.cause
	}

	d := &Diagnostic{File: safeDiagnosticText(file, 512), cause: cause}
	var pe *pathError
	if errors.As(cause, &pe) {
		d.Field, d.Message = pe.field, pe.message
	} else {
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(cause, &typeErr):
			d.Field = safeFieldPath(typeErr.Field)
			d.Message = "wrong value type"
		case errors.Is(cause, io.EOF), errors.Is(cause, io.ErrUnexpectedEOF):
			d.Message = "unexpected end of JSON"
		case isUnknownField(cause):
			d.Message = "unknown field"
		case isExtraJSONValue(cause):
			d.Message = "more than one JSON value"
		default:
			d.Message = "invalid JSON"
		}
	}
	if offset > 0 {
		d.Line, d.Column, d.Excerpt = diagnosticLocation(source, offset)
	}
	return d
}

func readDiagnostic(file string, cause error) error {
	message := readErrorMessage(cause)
	return &Diagnostic{File: safeDiagnosticText(file, 512), Message: message, cause: cause}
}

func isUnknownField(err error) bool {
	return strings.HasPrefix(err.Error(), "json: unknown field ")
}

func isExtraJSONValue(err error) bool {
	var extra extraJSONValueError
	return errors.As(err, &extra)
}

func decoderOffset(err error) int64 {
	var issue *parseIssue
	if errors.As(err, &issue) {
		return issue.offset
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return syntax.Offset
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return typeErr.Offset
	}
	return 0
}

func diagnosticLocation(source []byte, offset int64) (line, column int, excerpt string) {
	index := int64(0)
	if offset > 0 {
		index = offset - 1
	}
	if index > int64(len(source)) {
		index = int64(len(source))
	}
	if index < 0 {
		index = 0
	}

	line, column = 1, 1
	lineStart := 0
	for i := 0; i < int(index); {
		switch source[i] {
		case '\r':
			line++
			column = 1
			if i+1 < len(source) && source[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			lineStart = i
		case '\n':
			line++
			column = 1
			i++
			lineStart = i
		default:
			_, size := utf8.DecodeRune(source[i:])
			if size == 0 {
				size = 1
			}
			i += size
			column++
		}
	}

	lineEnd := lineStart
	for lineEnd < len(source) && source[lineEnd] != '\n' && source[lineEnd] != '\r' {
		lineEnd++
	}
	caretByte := int(index) - lineStart
	if caretByte < 0 {
		caretByte = 0
	}
	if caretByte > lineEnd-lineStart {
		caretByte = lineEnd - lineStart
	}
	excerpt = formatExcerpt(source, lineStart, lineEnd, caretByte, line)
	return line, column, excerpt
}

func formatExcerpt(source []byte, start, end, caretByte, line int) string {
	inString, escaped := stringState(source[:start])
	raw := source[start:end]
	expanded, caretColumn := redactAndExpand(raw, caretByte, inString, escaped)
	visible, caretColumn := cropExcerpt(expanded, caretColumn)
	margin := 4
	if n := len(strconv.Itoa(line)); n > margin {
		margin = n
	}
	return fmt.Sprintf("%*d | %s\n%*s | %s^", margin, line, string(visible), margin, "", strings.Repeat(" ", caretColumn))
}

func stringState(source []byte) (inString, escaped bool) {
	for i := 0; i < len(source); i++ {
		b := source[i]
		if inString {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
		} else if b == '"' {
			inString = true
		}
	}
	return inString, escaped
}

func redactAndExpand(source []byte, caretByte int, inString, escaped bool) ([]rune, int) {
	out := make([]rune, 0, len(source))
	displayColumn, caretColumn := 0, -1
	for i := 0; i < len(source); {
		if i >= caretByte && caretColumn < 0 {
			caretColumn = displayColumn
		}
		r, size := utf8.DecodeRune(source[i:])
		if size == 0 {
			size = 1
		}
		if inString {
			switch {
			case escaped:
				out = append(out, '*')
				displayColumn++
				escaped = false
			case r == '\\':
				out = append(out, '*')
				displayColumn++
				escaped = true
			case r == '"':
				out = append(out, '"')
				displayColumn++
				inString = false
			default:
				out = append(out, '*')
				displayColumn++
			}
		} else {
			switch {
			case r == '"':
				out = append(out, '"')
				displayColumn++
				inString = true
			case r == '\t':
				spaces := 4 - displayColumn%4
				for range spaces {
					out = append(out, ' ')
				}
				displayColumn += spaces
			case unicode.IsSpace(r):
				out = append(out, r)
				displayColumn++
			case strings.ContainsRune("{}[]:,", r):
				out = append(out, r)
				displayColumn++
			default:
				out = append(out, '*')
				displayColumn++
			}
		}
		i += size
	}
	if caretColumn < 0 {
		caretColumn = displayColumn
	}
	return out, caretColumn
}

func cropExcerpt(line []rune, caret int) ([]rune, int) {
	const maxColumns = 240
	if len(line) <= maxColumns {
		return line, caret
	}
	start := caret - (maxColumns-1)/2
	if start < 0 {
		start = 0
	}
	if start > len(line)-maxColumns {
		start = len(line) - maxColumns
	}
	end := start + maxColumns
	left := start > 0
	right := end < len(line)
	if left && right {
		end--
	} else if left {
		start++
	} else if right {
		end--
	}
	visible := make([]rune, 0, maxColumns)
	if left {
		visible = append(visible, '…')
	}
	visible = append(visible, line[start:end]...)
	if right {
		visible = append(visible, '…')
	}
	newCaret := caret - start
	if left {
		newCaret++
	}
	if newCaret < 0 {
		newCaret = 0
	}
	if newCaret > len(visible) {
		newCaret = len(visible)
	}
	return visible, newCaret
}

func safeFieldPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, ".")
	known := configFieldSegments()
	for i, part := range parts {
		nameEnd := strings.IndexByte(part, '[')
		name, suffix := part, ""
		if nameEnd >= 0 {
			name, suffix = part[:nameEnd], part[nameEnd:]
			if !validArraySuffix(suffix) {
				parts[i] = "[redacted]"
				continue
			}
		}
		if _, ok := known[name]; !ok {
			parts[i] = "[redacted]"
			continue
		}
		parts[i] = name + suffix
	}
	return safeDiagnosticText(strings.Join(parts, "."), 512)
}

func validArraySuffix(suffix string) bool {
	for len(suffix) > 0 {
		if suffix[0] != '[' {
			return false
		}
		end := strings.IndexByte(suffix, ']')
		if end < 2 {
			return false
		}
		for _, r := range suffix[1:end] {
			if r < '0' || r > '9' {
				return false
			}
		}
		suffix = suffix[end+1:]
	}
	return true
}

func configFieldSegments() map[string]struct{} {
	fields := make(map[string]struct{})
	seen := make(map[reflect.Type]bool)
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
				name := strings.SplitN(tag, ",", 2)[0]
				if name != "" {
					fields[name] = struct{}{}
				}
			}
			walk(field.Type)
		}
	}
	walk(reflect.TypeOf(wireConfig{}))
	for name := range knownItems {
		fields[name] = struct{}{}
	}
	return fields
}

func safeDiagnosticText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return truncateUTF8(b.String(), limit)
}

func safeExcerptText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r == '\n' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return truncateUTF8(b.String(), 1200)
}

func truncateUTF8(value string, limit int) string {
	if limit < 0 || len(value) <= limit {
		return value
	}
	end := 0
	for _, r := range value {
		if end+utf8.RuneLen(r) > limit {
			break
		}
		end += utf8.RuneLen(r)
	}
	return value[:end]
}

func parseIssueFor(cause error, offset int64) error {
	return &parseIssue{cause: cause, offset: offset}
}

func offsetAfterFirstValue(source []byte, firstEnd int64) int64 {
	i := firstEnd
	if i < 0 {
		i = 0
	}
	for i < int64(len(source)) {
		r, size := utf8.DecodeRune(source[i:])
		if size == 0 {
			break
		}
		if !unicode.IsSpace(r) {
			return i + 1
		}
		i += int64(size)
	}
	return 0
}

func readErrorMessage(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "configuration file is missing"
	}
	return "configuration file could not be read"
}
