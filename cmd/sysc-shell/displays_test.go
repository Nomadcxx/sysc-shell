package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/compositor"
)

func TestRestoreDisplaysRequested(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want []string
		ok   bool
	}{
		{"no arguments", nil, nil, false},
		{"normal startup", []string{"--verbose"}, nil, false},
		{"token", []string{restoreDisplaysFlag, "abc123"}, []string{"abc123"}, true},
		{"extra arguments", []string{restoreDisplaysFlag, "abc123", "extra"}, []string{"abc123", "extra"}, true},
		// A bare flag still selects the recovery path: refusing to recognise it
		// would start the whole shell instead, which is the one outcome the
		// helper must never produce.
		{"bare flag", []string{restoreDisplaysFlag}, nil, true},
		{"another mode's argument", []string{"ipc", "panel.open", restoreDisplaysFlag}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := restoreDisplaysRequested(tc.argv)
			if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("restoreDisplaysRequested(%q) = %q, %v; want %q, %v", tc.argv, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// The helper accepts exactly one token. A token whose ticket is already gone
// is a settled preview, which the helper finishes without an error.
func TestRunRestoreDisplaysAcceptsOneToken(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	for _, bad := range [][]string{nil, {""}, {"0123456789abcdef0123456789abcdef", "extra"}} {
		if err := runRestoreDisplays(context.Background(), bad); err == nil {
			t.Errorf("runRestoreDisplays(%q) accepted a malformed invocation", bad)
		}
	}
	if err := runRestoreDisplays(context.Background(), []string{"0123456789abcdef0123456789abcdef"}); err != nil {
		t.Fatalf("a settled token was refused: %v", err)
	}
}

// Each compositor reads only its own confirmed root.
func TestSavedDisplaysRootIsPerCompositor(t *testing.T) {
	saved := config.Displays{NiriConfigPath: "/n/config.kdl"}
	if got := savedDisplaysRoot(compositor.SyscCompositor, saved); got != "" {
		t.Errorf("sysc-compositor reused the Niri root %q", got)
	}
	saved.SyscConfigPath = "/s/config.kdl"
	if got := savedDisplaysRoot(compositor.SyscCompositor, saved); got != "/s/config.kdl" {
		t.Errorf("sysc-compositor root = %q", got)
	}
	if got := savedDisplaysRoot(compositor.Niri, saved); got != "/n/config.kdl" {
		t.Errorf("niri root = %q", got)
	}
}
