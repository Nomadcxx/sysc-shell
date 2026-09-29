package services

import (
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestIdleInhibitorsTracker(t *testing.T) {
	track := idleInhibitors{byCookie: map[uint32]ScreenSaverInhibitor{}}

	first := track.inhibit(":1.5", "mpv", "playing video")
	second := track.inhibit(":1.9", "firefox", "watching")
	if first == second || first == 0 || second == 0 {
		t.Fatalf("cookies must be nonzero and unique: %d %d", first, second)
	}

	// A foreign sender must not release someone else's cookie.
	if track.uninhibit(":1.5", second) {
		t.Fatal("foreign sender uninhibit accepted")
	}
	// An unknown cookie is rejected too.
	if track.uninhibit(":1.5", second+100) {
		t.Fatal("unknown cookie uninhibit accepted")
	}
	// The owning sender can release its own cookie.
	if !track.uninhibit(":1.9", second) {
		t.Fatal("owning sender uninhibit rejected")
	}

	inhibitors := track.list()
	if len(inhibitors) != 1 || inhibitors[0].App != "mpv" {
		t.Fatalf("List after release = %+v, want only mpv", inhibitors)
	}

	// Cookies are never reused, even after the holder is gone.
	if third := track.inhibit(":1.9", "firefox", "watching again"); third == second {
		t.Fatalf("cookie %d was reused", third)
	}

	// A vanished bus name drops exactly its own inhibitors.
	if dropped := track.dropSender(":1.9"); dropped != 1 {
		t.Fatalf("DropSender = %d, want 1", dropped)
	}
	inhibitors = track.list()
	if len(inhibitors) != 1 || inhibitors[0].Cookie != first {
		t.Fatalf("List after DropSender = %+v, want only mpv", inhibitors)
	}
	if !track.uninhibit(":1.5", first) {
		t.Fatal("last uninhibit rejected")
	}
	if len(track.list()) != 0 {
		t.Fatal("tracker should be empty")
	}
}

func TestIdleInhibitorsListSorted(t *testing.T) {
	track := idleInhibitors{byCookie: map[uint32]ScreenSaverInhibitor{}}
	for range 5 {
		track.inhibit(":1.5", "app", "reason")
	}
	got := track.list()
	if !slices.IsSortedFunc(got, func(a, b ScreenSaverInhibitor) int {
		return int(a.Cookie) - int(b.Cookie)
	}) {
		t.Fatalf("List = %+v, want ascending by cookie", got)
	}
}

func TestScreenSaverServiceMethods(t *testing.T) {
	var published [][]ScreenSaverInhibitor
	s := &ScreenSaverService{
		tracker:   idleInhibitors{byCookie: map[uint32]ScreenSaverInhibitor{}},
		onChanged: func(list []ScreenSaverInhibitor) { published = append(published, list) },
	}

	cookie, dbusErr := s.Inhibit(dbus.Sender(":1.5"), "mpv", "playing video")
	if dbusErr != nil {
		t.Fatalf("Inhibit: %v", dbusErr)
	}
	if dbusErr := s.UnInhibit(dbus.Sender(":1.9"), cookie); dbusErr == nil {
		t.Fatal("foreign sender UnInhibit accepted")
	}
	active, dbusErr := s.GetActive()
	if dbusErr != nil || !active {
		t.Fatalf("GetActive = %v,%v, want true,nil", active, dbusErr)
	}
	if dbusErr := s.UnInhibit(dbus.Sender(":1.5"), cookie); dbusErr != nil {
		t.Fatalf("owning sender UnInhibit: %v", dbusErr)
	}
	active, dbusErr = s.GetActive()
	if dbusErr != nil || active {
		t.Fatalf("GetActive after release = %v,%v, want false,nil", active, dbusErr)
	}
	if dbusErr := s.SimulateUserActivity(); dbusErr != nil {
		t.Fatalf("SimulateUserActivity: %v", dbusErr)
	}
	// Inhibit and the successful UnInhibit each published a snapshot; the
	// rejected foreign release published nothing.
	if len(published) != 2 {
		t.Fatalf("published %d snapshots, want 2", len(published))
	}
	if len(published[0]) != 1 || len(published[1]) != 0 {
		t.Fatalf("snapshots = %+v", published)
	}
}
