package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func modelListing(id, name, source, category, version string) store.Listing {
	release := catalog.Release{
		Version: version, Protocol: v1.Version{Major: 1, Minor: 0},
		Capabilities: []string{"panels"}, Requires: catalog.Requires{Commands: []string{"notify-send"}},
		Assets: map[string]catalog.Asset{"linux-amd64": {
			URL: "https://example.com/plugin.tar.gz", SHA256: strings.Repeat("a", 64), Size: 1,
		}},
	}
	entry := catalog.Entry{
		ID: id, Name: name, Author: "Nomad", Description: name + " description",
		Category: category, Homepage: "https://example.com", Release: release,
	}
	return store.Listing{
		Source: source, CatalogCommit: strings.Repeat("b", 40), Entry: entry,
		Resolution: store.Resolution{Release: &entry.Release, AssetKey: "linux-amd64", Compat: store.Compatible},
		Status:     store.StatusAvailable,
	}
}

func TestBrowseListingsFiltersTextSourceCategoryAndInstalled(t *testing.T) {
	alpha := modelListing("org.sysc.alpha", "Alpha Clock", "sysc", "utilities", "1.0.0")
	alpha.Entry.Author = "Ada Lovelace"
	beta := modelListing("org.other.beta", "Beta", "community", "monitoring", "2.0.0")
	installed := modelListing("org.sysc.installed", "Installed", "sysc", "utilities", "1.0.0")
	installed.Installed = &store.Record{Source: "sysc", Version: "1.0.0"}

	cases := []struct {
		name  string
		query BrowseQuery
		want  []string
	}{
		{name: "name search", query: BrowseQuery{Text: "CLOCK"}, want: []string{"org.sysc.alpha"}},
		{name: "author search", query: BrowseQuery{Text: "ada lovelace"}, want: []string{"org.sysc.alpha"}},
		{name: "description search", query: BrowseQuery{Text: "beta description"}, want: []string{"org.other.beta"}},
		{name: "source and category", query: BrowseQuery{Source: "sysc", Category: "utilities"}, want: []string{"org.sysc.alpha", "org.sysc.installed"}},
		{name: "hide installed", query: BrowseQuery{HideInstalled: true}, want: []string{"org.sysc.alpha", "org.other.beta"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := browseListings([]store.Listing{alpha, beta, installed}, tc.query)
			ids := make([]string, len(got))
			for i := range got {
				ids[i] = got[i].Entry.ID
			}
			if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("ids = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestBrowseListingsSortsDatesAndCategoriesWithStableTies(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan2 := jan1.Add(24 * time.Hour)
	a := modelListing("org.sysc.a", "Alpha", "sysc", "z", "1.0.0")
	a.Entry.UpdatedAt, a.Entry.AddedAt = jan2, time.Time{}
	a.Installed = &store.Record{Source: "sysc", Version: "1.0.0"}
	b := modelListing("org.sysc.b", "Beta", "sysc", "a", "1.0.0")
	b.Entry.UpdatedAt, b.Entry.AddedAt = time.Time{}, jan1
	c := modelListing("org.sysc.c", "Charlie", "sysc", "m", "1.0.0")
	c.Entry.UpdatedAt, c.Entry.AddedAt = jan1, jan2

	cases := []struct {
		name  string
		query BrowseQuery
		want  []string
	}{
		{"name", BrowseQuery{Sort: SortName}, []string{"org.sysc.a", "org.sysc.b", "org.sysc.c"}},
		{"name descending", BrowseQuery{Sort: SortName, Desc: true}, []string{"org.sysc.c", "org.sysc.b", "org.sysc.a"}},
		{"updated missing last", BrowseQuery{Sort: SortUpdated}, []string{"org.sysc.c", "org.sysc.a", "org.sysc.b"}},
		{"updated descending missing last", BrowseQuery{Sort: SortUpdated, Desc: true}, []string{"org.sysc.a", "org.sysc.c", "org.sysc.b"}},
		{"added missing last", BrowseQuery{Sort: SortAdded}, []string{"org.sysc.b", "org.sysc.c", "org.sysc.a"}},
		{"added descending missing last", BrowseQuery{Sort: SortAdded, Desc: true}, []string{"org.sysc.c", "org.sysc.b", "org.sysc.a"}},
		{"category", BrowseQuery{Sort: SortCategory}, []string{"org.sysc.b", "org.sysc.c", "org.sysc.a"}},
		{"installed first", BrowseQuery{Sort: SortInstalledFirst}, []string{"org.sysc.a", "org.sysc.b", "org.sysc.c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := browseListings([]store.Listing{a, b, c}, tc.query)
			ids := make([]string, len(got))
			for i := range got {
				ids[i] = got[i].Entry.ID
			}
			if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("ids = %v, want %v", ids, tc.want)
			}
		})
	}

	if got := SortName.Next().Next().Next().Next().Next(); got != SortName {
		t.Fatalf("sort cycle returned %q", got)
	}
}

func TestBrowseListingsBreaksTiesBySourceAndID(t *testing.T) {
	a := modelListing("org.z.same", "Same", "zeta", "utilities", "1.0.0")
	b := modelListing("org.a.same", "Same", "alpha", "utilities", "1.0.0")
	got := browseListings([]store.Listing{a, b}, BrowseQuery{Sort: SortName})
	if len(got) != 2 || got[0].Source != "alpha" || got[1].Source != "zeta" {
		t.Fatalf("tie order = %+v", got)
	}
}

func TestPluginStoreCardModelsBadgesActionAndMedia(t *testing.T) {
	l := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	l.Entry.Screenshot = &catalog.Screenshot{URL: "https://example.com/timer.png", SHA256: strings.Repeat("c", 64)}
	l.Entry.Deprecated = true
	l.Installed = &store.Record{Source: "sysc", Version: "1.0.0"}
	l.LocalDir = "/home/user/plugins/timer"
	l.Status = store.StatusShadowed
	l.UpdateAvailable = true
	media := map[string]store.MediaState{strings.Repeat("c", 64): {Path: "/cache/timer.png"}}
	card := cardFor(l, media)
	if card.Title != "Timer" || card.Byline != "v1.4.0 · Nomad" || card.Action != ActionUpdate {
		t.Fatalf("card = %+v", card)
	}
	if card.Screenshot.SHA256 != strings.Repeat("c", 64) || card.ScreenshotPath != "/cache/timer.png" || card.Glyph == "" {
		t.Fatalf("card media = %+v", card)
	}
	labels := map[string]bool{}
	for _, badge := range card.Badges {
		labels[badge.Label] = true
	}
	for _, want := range []string{"Official", "Deprecated", "Local override"} {
		if !labels[want] {
			t.Errorf("badges %v miss %q", labels, want)
		}
	}
}

func TestPluginStoreCardAndDetailUsePinnedMaterialGlyphs(t *testing.T) {
	for _, category := range catalog.Categories {
		if glyph := categoryGlyph(category); glyph == "" || !render.ValidMaterialIcon(glyph) {
			t.Errorf("category %q glyph %q is not in the embedded subset", category, glyph)
		}
	}
	if categoryGlyph("new-category") == "" {
		t.Fatal("unknown category has no fallback glyph")
	}
}

func TestPluginStoreDetailCarriesPinnedConsentAndStartWarning(t *testing.T) {
	l := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	l.Entry.License = "MIT"
	l.Entry.ReleaseNotes = "https://example.com/releases/1.4.0"
	l.Entry.Screenshot = &catalog.Screenshot{URL: "https://example.com/timer.png", SHA256: strings.Repeat("c", 64)}
	l.Entry.Release.Capabilities = []string{"panels", "settings"}
	l.Entry.Release.Requires.Commands = []string{"notify-send"}
	l.Resolution.Release = &l.Entry.Release
	l.Resolution.AssetKey = "linux-amd64"
	media := map[string]store.MediaState{strings.Repeat("c", 64): {Path: "/cache/timer.png"}}
	detail := detailFor(l, true, media, "## Timer\n\nREADME text")
	if detail.Action != ActionInstall || !detail.NeedsConsent || !detail.StartImmediately {
		t.Fatalf("detail action/consent = %+v", detail)
	}
	if detail.Version != "1.4.0" || detail.SHA256Prefix != strings.Repeat("a", 12) || detail.CatalogCommit != l.CatalogCommit {
		t.Fatalf("detail identity = %+v", detail)
	}
	if detail.Readme != "## Timer\n\nREADME text" || detail.ScreenshotPath != "/cache/timer.png" {
		t.Fatalf("detail media = %+v", detail)
	}
	if !containsString(detail.ConsentLines, "runs as your user with full file and network access") ||
		!containsString(detail.ConsentLines, "will start immediately") {
		t.Fatalf("consent lines = %v", detail.ConsentLines)
	}

	l.Installed = &store.Record{Source: "sysc", Version: "1.3.0", Capabilities: []string{"panels", "settings"}, Requires: []string{"notify-send"}}
	l.Status = store.StatusUpdateAvailable
	l.UpdateAvailable = true
	detail = detailFor(l, false, nil, "")
	if detail.Action != ActionUpdate || detail.NeedsConsent {
		t.Fatalf("unchanged-capability update = %+v", detail)
	}
	l.Entry.Release.Capabilities = []string{"panels"}
	detail = detailFor(l, false, nil, "")
	if !detail.NeedsConsent {
		t.Fatalf("capability-changing update did not require consent: %+v", detail)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
