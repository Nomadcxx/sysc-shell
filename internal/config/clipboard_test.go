package config

import "testing"

func TestClipboardIsKnownAndShipsOnTheDefaultBar(t *testing.T) {
	t.Parallel()

	if _, ok := knownItems["clipboard"]; !ok {
		t.Fatal("clipboard is not a known bar item")
	}
	cfg := Default()
	var found bool
	for _, item := range cfg.Bar.Right {
		if item.ID == "clipboard" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default right bar = %+v, want clipboard", cfg.Bar.Right)
	}

	parsed, err := Parse([]byte(`{"bar":{"items":{"right":["clipboard"]}}}`))
	if err != nil {
		t.Fatalf("clipboard config: %v", err)
	}
	if len(parsed.Bar.Right) != 1 || parsed.Bar.Right[0].ID != "clipboard" {
		t.Fatalf("parsed clipboard item = %+v", parsed.Bar.Right)
	}
}
