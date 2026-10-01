package config

import "testing"

// The screenshot launcher is the org.sysc.screenshot plugin now. A stale
// first-party item must fail loudly, as every removed item does, rather than
// silently drop a widget.
func TestFirstPartyScreenshotBarItemIsGone(t *testing.T) {
	if _, ok := knownItems["screenshot"]; ok {
		t.Fatal(`"screenshot" is still a known bar item; the launcher is a plugin placement now`)
	}
	if _, err := Parse([]byte(`{"bar":{"items":{"right":[{"id":"screenshot"}]}}}`)); err == nil {
		t.Fatal("a stale first-party screenshot item loaded")
	}
	if _, err := Parse([]byte(`{"bar":{"items":{"right":[{"id":"plugin","plugin":"org.sysc.screenshot","entry":"bar","instance":"screenshot-1"}]}}}`)); err != nil {
		t.Fatalf("the plugin placement must load: %v", err)
	}
}
