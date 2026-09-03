package wallpaper

import (
	"errors"
	"testing"
)

func newTestStore() *Store {
	s := &Store{}
	s.SetConnectors([]string{"DP-1", "DP-3"})
	return s
}

// commitAll runs every request in jobs to success with the given preview.
func commitAll(t *testing.T, s *Store, jobs []Job, preview string) {
	t.Helper()
	for _, r := range jobs {
		if !s.Commit(r, preview) {
			t.Fatalf("commit %s gen %d refused", r.Connector, r.Gen)
		}
	}
}

func TestAssignIndependentOutputs(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-1", "/w/a.png", KindImage), "")
	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")

	one, ok := s.Assignment("DP-1")
	if !ok || one.Path != "/w/a.png" || one.Kind != KindImage {
		t.Fatalf("DP-1 = %+v, %v", one, ok)
	}
	three, ok := s.Assignment("DP-3")
	if !ok || three.Path != "/w/b.mp4" || three.Kind != KindVideo {
		t.Fatalf("DP-3 = %+v, %v", three, ok)
	}
	if three.PreviewPath != "/c/b.jpg" {
		t.Fatalf("preview = %q", three.PreviewPath)
	}
}

func TestAssignAllExpands(t *testing.T) {
	s := newTestStore()
	jobs := s.Apply(AllOutputs, "/w/a.png", KindImage)
	if len(jobs) != 2 {
		t.Fatalf("all expanded to %d requests, want 2", len(jobs))
	}
	seen := map[string]bool{}
	for _, r := range jobs {
		seen[r.Connector] = true
	}
	if !seen["DP-1"] || !seen["DP-3"] {
		t.Fatalf("all covered %v, want each connected output", seen)
	}
	commitAll(t, s, jobs, "")
	for _, c := range []string{"DP-1", "DP-3"} {
		if a, ok := s.Assignment(c); !ok || a.Path != "/w/a.png" {
			t.Fatalf("%s = %+v, %v", c, a, ok)
		}
	}
}

func TestAssignStaleGeneration(t *testing.T) {
	s := newTestStore()
	first := s.Apply("DP-1", "/w/slow.png", KindImage)
	second := s.Apply("DP-1", "/w/fast.png", KindImage)
	if first[0].Gen >= second[0].Gen {
		t.Fatalf("generation did not advance: %d then %d", first[0].Gen, second[0].Gen)
	}
	commitAll(t, s, second, "")
	if s.Commit(first[0], "") {
		t.Fatal("a stale generation must not commit")
	}
	if a, _ := s.Assignment("DP-1"); a.Path != "/w/fast.png" {
		t.Fatalf("stale commit clobbered the newer apply: %q", a.Path)
	}
}

func TestAssignDisconnectKeepsAssignment(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")
	s.Disconnect("DP-3")

	if a, ok := s.Assignment("DP-3"); !ok || a.Path != "/w/b.mp4" {
		t.Fatalf("disconnect dropped the assignment: %+v %v", a, ok)
	}
	if rt := s.Runtime("DP-3"); rt.State != StateStatic || rt.Socket != "" {
		t.Fatalf("disconnect kept runtime: %+v", rt)
	}
	if jobs := s.Apply(AllOutputs, "/w/a.png", KindImage); len(jobs) != 1 {
		t.Fatalf("all covered %d outputs, want only the connected one", len(jobs))
	}
}

func TestAssignReconnectReplays(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")
	s.Disconnect("DP-3")

	jobs := s.Reconnect("DP-3")
	if len(jobs) != 1 || jobs[0].Path != "/w/b.mp4" || jobs[0].Kind != KindVideo {
		t.Fatalf("reconnect = %+v, want a replay of the saved assignment", jobs)
	}
	if jobs := s.Reconnect("HDMI-A-1"); len(jobs) != 0 {
		t.Fatalf("an unassigned output must stay untouched, got %+v", jobs)
	}
}

func TestAssignPartialAll(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-3", "/w/prior.mp4", KindVideo), "/c/prior.jpg")

	jobs := s.Apply(AllOutputs, "/w/new.png", KindImage)
	for _, r := range jobs {
		if r.Connector == "DP-3" {
			s.Fail(r, errors.New("engine refused"))
			continue
		}
		s.Commit(r, "")
	}

	if a, _ := s.Assignment("DP-1"); a.Path != "/w/new.png" {
		t.Fatalf("DP-1 should have taken the apply: %q", a.Path)
	}
	if a, _ := s.Assignment("DP-3"); a.Path != "/w/prior.mp4" {
		t.Fatalf("a failed output must keep its prior assignment, got %q", a.Path)
	}
	if rt := s.Runtime("DP-3"); rt.State != StateError || rt.Err == "" {
		t.Fatalf("a failed output must report the error: %+v", rt)
	}
}

func TestAssignSeedPath(t *testing.T) {
	s := newTestStore()
	if s.SeedPath() != "" {
		t.Fatalf("a fresh store has no seed, got %q", s.SeedPath())
	}

	commitAll(t, s, s.Apply("DP-1", "/w/a.png", KindImage), "")
	if s.SeedPath() != "/w/a.png" {
		t.Fatalf("image seed = %q, want the image path", s.SeedPath())
	}

	commitAll(t, s, s.Apply("DP-1", "/w/b.mp4", KindVideo), "/c/b.jpg")
	if s.SeedPath() != "/c/b.jpg" {
		t.Fatalf("video seed = %q, want the still", s.SeedPath())
	}

	commitAll(t, s, s.Apply("DP-1", "/w/c.mkv", KindVideo), "")
	if s.SeedPath() != "/c/b.jpg" {
		t.Fatalf("a video with no still must leave the seed, got %q", s.SeedPath())
	}
}
