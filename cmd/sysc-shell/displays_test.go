package main

import (
	"reflect"
	"testing"
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
