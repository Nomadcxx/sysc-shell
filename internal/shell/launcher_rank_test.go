package shell

import (
	"fmt"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
)

// alphabetEntries builds n entries named A0000..., B0001..., cycling the
// initial through the alphabet so the browse order is checkable by first
// letter the way the live list is.
func alphabetEntries(n int) []launcher.Entry {
	entries := make([]launcher.Entry, n)
	for i := range entries {
		name := fmt.Sprintf("%c%04d Entry", 'A'+i%26, i)
		entries[i] = launcher.Entry{
			ID:      fmt.Sprintf("entry-%04d.desktop", i),
			Name:    name,
			Comment: "generated",
			Argv:    []string{"true"},
		}
	}
	return entries
}

// The browse list must not be capped: 482 desktop entries used to arrive as
// 50, which read as a list that stopped in the E's.
func TestLauncherRankBrowseKeepsEveryEntry(t *testing.T) {
	t.Parallel()
	entries := alphabetEntries(482)
	got := launcherRank(entries, "", nil)
	if len(got) != len(entries) {
		t.Fatalf("browse returned %d of %d entries", len(got), len(entries))
	}
	if first, last := got[0].Entry.Name[:1], got[len(got)-1].Entry.Name[:1]; first != "A" || last != "Z" {
		t.Fatalf("browse spans %s..%s, want A..Z", first, last)
	}
}

// A whitespace-only query is still a browse: the library trims before scoring.
func TestLauncherRankBlankQueryIsBrowse(t *testing.T) {
	t.Parallel()
	entries := alphabetEntries(120)
	if got := launcherRank(entries, "   ", nil); len(got) != len(entries) {
		t.Fatalf("blank query returned %d of %d entries", len(got), len(entries))
	}
}

// A search is still capped: past 50 fuzzy matches the tail is noise.
func TestLauncherRankSearchCapsAtLimit(t *testing.T) {
	t.Parallel()
	entries := alphabetEntries(482)
	got := launcherRank(entries, "entry", nil)
	if len(got) != launcherSearchLimit {
		t.Fatalf("search returned %d results, want the %d cap", len(got), launcherSearchLimit)
	}
}

// Usage boost lifts an entry above the alphabetical run it would otherwise
// sit inside, and is clamped so it cannot bury a better textual match.
func TestLauncherRankAppliesCappedBoost(t *testing.T) {
	t.Parallel()
	entries := alphabetEntries(60)
	target := entries[40].ID
	boost := func(_, id string) int {
		if id == target {
			return 1000
		}
		return 0
	}
	got := launcherRank(entries, "", boost)
	if got[0].Entry.ID != target {
		t.Fatalf("boosted entry sorted at %q, want first", got[0].Entry.ID)
	}
	if got[0].Score != launcherUsageBoostCap {
		t.Fatalf("boost = %d, want it clamped to %d", got[0].Score, launcherUsageBoostCap)
	}
}

// The tie-break chain is score desc, lowercase name, name, then ID. Equal
// names must not order by scan order, or the browse list shuffles per rescan.
func TestLauncherRankTieBreaksDeterministically(t *testing.T) {
	t.Parallel()
	entries := []launcher.Entry{
		{ID: "z.desktop", Name: "app"},
		{ID: "a.desktop", Name: "app"},
		{ID: "m.desktop", Name: "App"},
	}
	got := launcherRank(entries, "", nil)
	want := []string{"m.desktop", "a.desktop", "z.desktop"}
	for i, id := range want {
		if got[i].Entry.ID != id {
			t.Fatalf("order[%d] = %q, want %q", i, got[i].Entry.ID, id)
		}
	}
}

// launcherRank is a copy of sysc-launch's own ranker with the browse cap
// removed. This pins the copy against the original: any drift in field
// weighting, boost cap, or tie-break shows up here, including drift the
// library introduces under a version bump.
func TestLauncherRankMatchesLibraryForSearches(t *testing.T) {
	// The library and shell guard fzf's package-global Init with separate
	// sync.Once values. Keep their parity check ahead of parallel rank tests.
	entries := append(launcherTestEntries(), alphabetEntries(120)...)
	for _, query := range []string{"fi", "fox", "term", "e", "entry", "zzz", "Files"} {
		want := rankViaService(t, entries, query, nil)
		got := rankViaService(t, entries, query, launcherRank)
		if len(got) != len(want) {
			t.Fatalf("query %q: %d results, library gives %d", query, len(got), len(want))
		}
		for i := range want {
			if got[i].Entry.ID != want[i].Entry.ID || got[i].Score != want[i].Score {
				t.Fatalf("query %q: result[%d] = %q/%d, library gives %q/%d",
					query, i, got[i].Entry.ID, got[i].Score, want[i].Entry.ID, want[i].Score)
			}
		}
	}
}

// rankViaService runs one query through a real service, which is the only way
// to reach the library's unexported ranker. A nil rank selects it.
func rankViaService(t *testing.T, entries []launcher.Entry, query string, rank func([]launcher.Entry, string, func(string, string) int) []launcher.Result) []launcher.Result {
	t.Helper()
	svc := launcher.NewService(launcher.ServiceConfig{
		Scan: func() []launcher.Entry { return entries },
		Rank: rank,
	})
	t.Cleanup(svc.Close)
	svc.Query(query)

	// The snapshot publish for the empty query can land before the query's
	// own publish, so take the last set that arrives before the feed settles.
	var last []launcher.Result
	deadline := time.After(2 * time.Second)
	for {
		select {
		case results := <-svc.Results():
			last = results
		case <-time.After(150 * time.Millisecond):
			if last == nil {
				t.Fatalf("query %q produced no results", query)
			}
			return last
		case <-deadline:
			t.Fatalf("query %q did not settle", query)
		}
	}
}
