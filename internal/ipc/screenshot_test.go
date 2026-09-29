package ipc

import (
	"errors"
	"strings"
	"testing"
)

func TestScreenshotMethod(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		line     string
		handler  error
		wantMode string
		want     string
	}{
		{"region", `{"id":1,"method":"screenshot","params":{"mode":"region"}}`, nil, "region", `"ok":true`},
		{"screen", `{"id":1,"method":"screenshot","params":{"mode":"screen"}}`, nil, "screen", `"ok":true`},
		{"window", `{"id":1,"method":"screenshot","params":{"mode":"window"}}`, nil, "window", `"ok":true`},
		{"unknown mode", `{"id":1,"method":"screenshot","params":{"mode":"scroll"}}`, nil, "", `"error":"unknown screenshot mode"`},
		{"no mode", `{"id":1,"method":"screenshot"}`, nil, "", `"error":"unknown screenshot mode"`},
		{"handler refuses", `{"id":1,"method":"screenshot","params":{"mode":"region"}}`, errors.New("a selector is already open"), "region", `"error":"a selector is already open"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got string
			s := NewServer("", Handlers{Screenshot: func(mode string) error {
				got = mode
				return tc.handler
			}})
			out := string(s.handleLine(tc.line))
			if !strings.Contains(out, tc.want) {
				t.Fatalf("reply %s, want %s", out, tc.want)
			}
			if got != tc.wantMode {
				t.Fatalf("handler got mode %q, want %q", got, tc.wantMode)
			}
		})
	}
	if out := string(NewServer("", Handlers{}).handleLine(`{"id":1,"method":"screenshot","params":{"mode":"region"}}`)); !strings.Contains(out, "screenshot handler unset") {
		t.Fatalf("unset handler reply %s", out)
	}
}
