//go:build live

package integration

import "testing"

// TestGateFocusFallthrough is the live Niri check for design D3: open the
// session panel from a focused window, press Escape, and confirm keyboard
// focus returns to that window without a niri focus-window call.
func TestGateFocusFallthrough(t *testing.T) {
	t.Skip("live Niri: open session, Escape, confirm focus returns to the previous window")
}
