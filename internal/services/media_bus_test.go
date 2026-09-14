package services

import "testing"

func TestSessionBusTracksInitialAndChangingMPRISOwners(t *testing.T) {
	b := &sessionBus{
		changes: make(chan nameChange, 2),
		owners:  map[string]string{},
	}

	b.handleNameOwnerChanged("org.mpris.MediaPlayer2.spotify", "", ":1.42")
	if got := b.ownerFor(":1.42"); got != "org.mpris.MediaPlayer2.spotify" {
		t.Fatalf("initial owner = %q, want spotify", got)
	}

	b.handleNameOwnerChanged("org.mpris.MediaPlayer2.spotify", ":1.42", ":1.43")
	if got := b.ownerFor(":1.42"); got != "" {
		t.Fatalf("old owner still mapped to %q", got)
	}
	if got := b.ownerFor(":1.43"); got != "org.mpris.MediaPlayer2.spotify" {
		t.Fatalf("replacement owner = %q, want spotify", got)
	}

	b.handleNameOwnerChanged("org.freedesktop.Notifications", ":1.43", ":1.44")
	if got := b.ownerFor(":1.43"); got != "org.mpris.MediaPlayer2.spotify" {
		t.Fatalf("non-MPRIS change disturbed owner map: %q", got)
	}
}
