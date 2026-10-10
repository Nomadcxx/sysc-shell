package ipc

import "testing"

func TestOnboardingIsKnownPanel(t *testing.T) {
	if _, ok := knownPanels["onboarding"]; !ok {
		t.Fatal("onboarding is not accepted by panel.open")
	}
}
