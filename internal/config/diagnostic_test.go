package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiredMissingDoesNotSelectDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := LoadRequired(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reload missing file: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("startup missing file: %v", err)
	}
}

func TestSyntaxDiagnosticKeepsLocationButMasksSource(t *testing.T) {
	data := []byte("{\n  \"secret\": \"token-do-not-publish\",\n  \"bar\": @\n}\n")
	_, err := Parse(data)
	var d *Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("not a diagnostic: %T %v", err, err)
	}
	if d.File != "<config>" || d.Line != 3 || d.Column != 10 {
		t.Fatalf("position: %+v", d)
	}
	text := err.Error()
	if strings.Contains(text, "token-do-not-publish") || strings.Contains(text, "secret") {
		t.Fatalf("source disclosed: %q", text)
	}
	if !strings.Contains(text, "   3 |   \"***\": *\n     |          ^") {
		t.Fatalf("excerpt/caret: %q", text)
	}
	data[0] = 'x'
	if err.Error() != text {
		t.Fatal("diagnostic changed after its source buffer was mutated")
	}
}

func TestDiagnosticPositionsUseSourceRunesAndFirstValueOffset(t *testing.T) {
	tests := []struct {
		name   string
		source []byte
		want   string
	}{
		{name: "multibyte before syntax error", source: []byte("{\n  \"x\": \"é\", @\n}"), want: "2:13"},
		{name: "CRLF", source: []byte("{\r\n  @\r\n}"), want: "2:3"},
		{name: "tab display", source: []byte("{\n\t@\n}"), want: "2:2"},
		{name: "second document", source: []byte("{}\n {}"), want: "2:2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.source)
			var d *Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("not a diagnostic: %T %v", err, err)
			}
			if !strings.Contains(err.Error(), "<config>:"+tt.want+":") {
				t.Fatalf("location = %q, want %q", err, tt.want)
			}
			if tt.name == "tab display" && !strings.Contains(err.Error(), "     |     ^") {
				t.Fatalf("tab caret was not expanded for display: %q", err)
			}
		})
	}
}

func TestDiagnosticEOFAndMalformedStringsDoNotDiscloseSource(t *testing.T) {
	for _, tt := range []struct {
		source []byte
		line   int
		column int
	}{
		{source: []byte{}, line: 1, column: 1},
		{source: []byte("{\"bar\":"), line: 1, column: 8},
		{source: []byte("{\"secret\":\"token-do-not-publish}"), line: 1},
		{source: []byte("{\"secret\":\"token-\\\"-do-not-publish}"), line: 1},
	} {
		_, err := Parse(tt.source)
		var d *Diagnostic
		if !errors.As(err, &d) {
			t.Fatalf("source %q: not a diagnostic: %T %v", tt.source, err, err)
		}
		if strings.Contains(err.Error(), "token-do-not-publish") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("source disclosed for %q: %q", tt.source, err)
		}
		wantColumn := tt.column
		if wantColumn == 0 {
			wantColumn = len(tt.source) + 1
		}
		if d.Line != tt.line || d.Column != wantColumn {
			t.Fatalf("EOF location for %q = %d:%d, want %d:%d", tt.source, d.Line, d.Column, tt.line, wantColumn)
		}
	}

	source := []byte{'{', '\n', ' ', 0xff, '\n', '}'}
	_, err := Parse(source)
	var d *Diagnostic
	if !errors.As(err, &d) || d.Line != 2 || d.Column != 2 {
		t.Fatalf("invalid UTF-8 location = %+v, %v", d, err)
	}
	if strings.ContainsRune(err.Error(), '\ufffd') {
		t.Fatalf("invalid byte was not masked: %q", err)
	}
}

func TestDiagnosticUnknownFieldAndTypeErrorsAreSafeAndTyped(t *testing.T) {
	_, err := Parse([]byte(`{"private-schema-key":1}`))
	var d *Diagnostic
	if !errors.As(err, &d) || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %T %v", err, err)
	}
	if strings.Contains(err.Error(), "private-schema-key") || d.Line != 0 {
		t.Fatalf("unknown field disclosed or guessed a position: %+v %q", d, err)
	}

	_, err = Parse([]byte(`{"bar":{"height":"secret-value"}}`))
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &d) || !errors.As(err, &typeErr) {
		t.Fatalf("type diagnostic lost its cause: %T %v", err, err)
	}
	if strings.Contains(err.Error(), "secret-value") || typeErr.Offset == 0 || d.Line == 0 || d.Field != "bar.height" {
		t.Fatalf("unsafe or unlocated type error: %+v %q", d, err)
	}
}

func TestDiagnosticIsBoundedAndPathErrorRedactsStringArguments(t *testing.T) {
	line := strings.Repeat("x", 10_000)
	_, err := Parse([]byte("{\"bar\": @, \"padding\": \"" + line + "\"}"))
	var d *Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("not a diagnostic: %T %v", err, err)
	}
	if got := len(err.Error()); got > 4096 {
		t.Fatalf("diagnostic is %d bytes", got)
	}
	pathText := pathErr("bar.edge", "%q is not one of top, bottom, left, right", "private-edge").Error()
	if strings.Contains(pathText, "private-edge") || !strings.Contains(pathText, "bar.edge") || !strings.Contains(pathText, "not one of top") {
		t.Fatalf("unsafe path error: %q", pathText)
	}

	_, err = Parse([]byte("{\n" + strings.Repeat(" ", 10_000) + "@\n}"))
	if len(err.Error()) > 4096 || !strings.Contains(err.Error(), "…") {
		t.Fatalf("long source line was not safely cropped: %d bytes", len(err.Error()))
	}
	unsafePath := pathErr("plugins.private-owner.private-key.name", "%q is invalid", "secret").Error()
	if strings.Contains(unsafePath, "private-owner") || strings.Contains(unsafePath, "private-key") || strings.Contains(unsafePath, "secret") {
		t.Fatalf("unsafe path or value disclosed: %q", unsafePath)
	}
}

func TestDiagnosticFilenameSanitizesControlsAndBoundsOutput(t *testing.T) {
	data := []byte("{ @ }")
	err := diagnosticAt("/tmp/secret\nfile"+strings.Repeat("x", 800), data, 3, errors.New("private source data"))
	if strings.Contains(err.Error(), "\nfile") || strings.Contains(err.Error(), "private source data") || len(err.Error()) > 4096 {
		t.Fatalf("unsafe diagnostic: %q (%d bytes)", err, len(err.Error()))
	}
}

// A second complete value is "more than one JSON value" at that value, whatever
// fields it carries: the strict field check belongs to the configuration, not
// to whatever follows it.
func TestSecondValueWithFieldsIsAnExtraValue(t *testing.T) {
	_, err := Parse([]byte(`{"terminal-opacity":90} {"terminal-opacity":90}`))
	var d *Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("not a diagnostic: %T %v", err, err)
	}
	if d.Message != "more than one JSON value" || d.Line != 1 || d.Column != 25 {
		t.Fatalf("diagnostic = %+v, want more than one JSON value at 1:25", d)
	}
	if _, err := Parse([]byte(`{} x`)); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("trailing garbage: %v, want invalid JSON", err)
	}
}
