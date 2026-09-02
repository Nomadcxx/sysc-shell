package ui

import "testing"

func TestEvdevTextMapsUSQWERTY(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key      uint32
		shift    bool
		want     string
		unmapped bool
	}{
		{key: 33, want: "f"},
		{key: 33, shift: true, want: "F"},
		{key: 49, want: "n"},
		{key: 57, want: " "},
		{key: 53, want: "/"},
		{key: 2, shift: true, want: "!"},
		{key: 14, unmapped: true}, // backspace is not text
		{key: 1, unmapped: true},  // escape
	}
	for _, tt := range tests {
		got, ok := EvdevText(tt.key, tt.shift)
		if tt.unmapped {
			if ok {
				t.Fatalf("key %d shift=%v mapped to %q, want unmapped", tt.key, tt.shift, got)
			}
			continue
		}
		if !ok || got != tt.want {
			t.Fatalf("key %d shift=%v = %q %v, want %q", tt.key, tt.shift, got, ok, tt.want)
		}
	}
}
