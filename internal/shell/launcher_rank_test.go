package shell

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
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

// Released usage history must lift a searched app without overtaking a
// materially stronger text match.
func TestLauncherRankKeepsBetterTextAboveUsageHistory(t *testing.T) {
	entries := []launcher.Entry{
		{ID: "fox.desktop", Name: "Fox", Argv: []string{"fox"}},
		{ID: "firefox.desktop", Name: "Firefox", Argv: []string{"firefox"}},
	}
	query := "fox"
	base := launcherRank(entries, query, nil)
	if len(base) != 2 || base[0].Entry.ID != "fox.desktop" {
		t.Fatalf("base text order = %+v, want exact Fox first", base)
	}
	baseScores := map[string]int{}
	for _, result := range base {
		baseScores[result.Entry.ID] = result.Score
	}
	if gap := baseScores["fox.desktop"] - baseScores["firefox.desktop"]; gap <= launcherUsageBoostCap {
		t.Fatalf("fixture text score gap = %d, want more than usage cap %d", gap, launcherUsageBoostCap)
	}

	history := launcher.OpenHistory(filepath.Join(t.TempDir(), "history.gob"), nil)
	svc := launcher.NewService(launcher.ServiceConfig{
		Scan:    func() []launcher.Entry { return entries },
		History: history,
		Rank:    launcherRank,
		Run:     func(context.Context, []string) error { return nil },
	})
	t.Cleanup(svc.Close)
	svc.Query(query)
	waitForRankServiceResults(t, svc)
	for i := 0; i < 20; i++ {
		if err := svc.Activate("firefox.desktop", ""); err != nil {
			t.Fatalf("record usage %d: %v", i+1, err)
		}
	}
	svc.Query(query)
	got := waitForRankServiceResults(t, svc)
	if len(got) != len(entries) {
		t.Fatalf("usage-adjusted result count = %d, want %d", len(got), len(entries))
	}
	if got[0].Entry.ID != "fox.desktop" {
		t.Fatalf("usage-adjusted order = %+v, materially stronger text match must remain first", got)
	}
	foundFirefox := false
	for _, result := range got {
		if result.Entry.ID == "firefox.desktop" {
			foundFirefox = true
			if result.Score <= baseScores[result.Entry.ID] {
				t.Fatalf("history did not raise Firefox score: got %d, base %d", result.Score, baseScores[result.Entry.ID])
			}
		}
	}
	if !foundFirefox {
		t.Fatal("usage-boosted Firefox result is missing")
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

func TestLauncherPreferencesHideAppsAndTheirActions(t *testing.T) {
	entries := []launcher.Entry{
		{ID: "hidden.desktop", Name: "Secret Tool", Actions: []launcher.Action{{ID: "secret", Name: "Open Vault"}}},
		{ID: "visible.desktop", Name: "Visible Tool", Actions: []launcher.Action{{ID: "open", Name: "Open Notes"}}},
	}
	prefs := &launcherAppPreferenceSnapshot{hidden: map[string]struct{}{"hidden.desktop": {}}}

	browse := launcherRankWithPreferences(entries, "", nil, prefs)
	if len(browse) != 1 || browse[0].Entry.ID != "visible.desktop" {
		t.Fatalf("browse rows = %+v, want only visible.desktop", browse)
	}
	search := launcherRankWithPreferences(entries, "vault", nil, prefs)
	if len(search) != 0 {
		t.Fatalf("hidden app action search rows = %+v, want none", search)
	}
	visibleAction := launcherRankWithPreferences(entries, "notes", nil, prefs)
	if len(visibleAction) != 1 || visibleAction[0].Action != "open" {
		t.Fatalf("visible action rows = %+v, want the visible app action", visibleAction)
	}
}

func TestLauncherPreferencesFavouriteOnlyBreaksScoreTies(t *testing.T) {
	equal := []launcher.Entry{
		{ID: "a.desktop", Name: "Same Tool"},
		{ID: "z.desktop", Name: "Same Tool"},
	}
	prefs := &launcherAppPreferenceSnapshot{favourites: map[string]struct{}{"z.desktop": {}}}
	got := launcherRankWithPreferences(equal, "", nil, prefs)
	if len(got) != 2 || got[0].Entry.ID != "z.desktop" {
		t.Fatalf("equal-score order = %+v, want favourite z.desktop first", got)
	}

	betterMatch := []launcher.Entry{
		{ID: "fox.desktop", Name: "Fox"},
		{ID: "firefox.desktop", Name: "Firefox"},
	}
	prefs = &launcherAppPreferenceSnapshot{favourites: map[string]struct{}{"firefox.desktop": {}}}
	got = launcherRankWithPreferences(betterMatch, "fox", nil, prefs)
	if len(got) != 2 || got[0].Entry.ID != "fox.desktop" {
		t.Fatalf("better-text order = %+v, want Fox before favourite Firefox", got)
	}
}

func TestLauncherRankReadsPreferencesAfterConfigReload(t *testing.T) {
	reg := newPanelRegistry(t)
	entries := []launcher.Entry{
		{ID: "hidden.desktop", Name: "Hidden Tool"},
		{ID: "visible.desktop", Name: "Visible Tool"},
	}
	next := config.Default()
	next.Applications.Hidden = []string{"hidden.desktop"}
	prepared, err := reg.PrepareConfig(next, nil)
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	reg.launcherMu.Lock()
	providerGeneration := reg.launcherProviderGeneration
	reg.launcherMu.Unlock()
	got := reg.rankLauncher(providerGeneration, entries, "", nil)
	if len(got) != 1 || got[0].Entry.ID != "visible.desktop" {
		t.Fatalf("rank after config reload = %+v, want only visible.desktop", got)
	}
}

// Application rows must match the library's scores and ordering up to the
// shell's search cap. Category and terminal entries guard version bump drift.
func TestLauncherRankMatchesLibraryForSearches(t *testing.T) {
	// The library and shell guard fzf's package-global Init with separate
	// sync.Once values. Keep their parity check ahead of parallel rank tests.
	entries := append(launcherTestEntries(), alphabetEntries(120)...)
	entries = append(entries,
		launcher.Entry{ID: "gimp.desktop", Name: "Gimp", Categories: []string{"Graphics"}},
		launcher.Entry{ID: "editor.desktop", Name: "Graphics Editor"},
		launcher.Entry{ID: "htop.desktop", Name: "Htop", Argv: []string{"/usr/bin/kitty", "-e", "htop"}, ExecArgv: []string{"htop"}, Terminal: true},
	)
	for _, query := range []string{"Graphics", "kitty", "usr", "htop", "fi", "fox", "term", "e", "entry", "zzz", "Files"} {
		want := rankViaService(t, entries, query, nil)
		want = want[:min(len(want), launcherSearchLimit)]
		// The library's rank produces no desktop-action rows; compare
		// application rows only.
		var got []launcher.Result
		for _, r := range rankViaService(t, entries, query, launcherRank) {
			if r.Action == "" {
				got = append(got, r)
			}
		}
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
	return waitForRankServiceResults(t, svc)
}

// The snapshot publish for the empty query can land before the query's own
// publish, so take the last set that arrives before the feed settles.
func waitForRankServiceResults(t *testing.T, svc *launcher.Service) []launcher.Result {
	t.Helper()
	var last []launcher.Result
	deadline := time.After(2 * time.Second)
	for {
		select {
		case results := <-svc.Results():
			last = results.Results
		case <-time.After(150 * time.Millisecond):
			if last == nil {
				t.Fatal("launcher service produced no results")
			}
			return last
		case <-deadline:
			t.Fatal("launcher service results did not settle")
		}
	}
}

func TestActionRowsRankBelowTheirAppAndOnlyWhenSearching(t *testing.T) {
	entries := []launcher.Entry{{
		ID: "brave.desktop", Name: "Brave", IconName: "brave", Argv: []string{"brave"},
		Actions: []launcher.Action{
			{ID: "new-window", Name: "New Window", Argv: []string{"brave", "--new-window"}},
			{ID: "new-private-window", Name: "New Incognito Window", IconName: "incognito", Argv: []string{"brave", "--incognito"}},
		},
	}}
	got := launcherRank(entries, "brave", nil)
	if len(got) != 3 || got[0].Action != "" || got[1].Action == "" || got[2].Action == "" {
		t.Fatalf("app first then its actions: %+v", got)
	}
	if got[1].Entry.Name != "Brave · New Window" && got[2].Entry.Name != "Brave · New Window" {
		t.Fatalf("action row name: %+v", got)
	}
	for _, r := range got[1:] {
		if r.Entry.Comment != "Desktop action" {
			t.Fatalf("comment %q", r.Entry.Comment)
		}
		if r.Action == "new-private-window" && r.Entry.IconName != "incognito" {
			t.Fatal("an action's own icon is used when it names one")
		}
	}
	if rows := launcherRank(entries, "", nil); len(rows) != 1 {
		t.Fatalf("browse shows %d rows, want the app only", len(rows))
	}
	if rows := launcherRank(entries, "incognito", nil); len(rows) != 1 || rows[0].Action != "new-private-window" {
		t.Fatalf("action-only match: %+v", rows)
	}
}
