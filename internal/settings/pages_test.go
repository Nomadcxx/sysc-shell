package settings

import (
	"slices"
	"testing"
)

func TestEveryEntryLandsOnAReachablePage(t *testing.T) {
	t.Parallel()
	rail := SectionNames()
	for _, e := range Default().All() {
		if !slices.Contains(rail, e.Section) {
			t.Errorf("%s: section %q is not in the rail", e.Path, e.Section)
			continue
		}
		pages := SectionPages(e.Section)
		if len(pages) == 0 && e.Page != "" {
			t.Errorf("%s: page %q in single-page section %q", e.Path, e.Page, e.Section)
		}
		if len(pages) > 0 && !slices.Contains(pages, e.Page) {
			t.Errorf("%s: page %q is not one of %s's pages %v", e.Path, e.Page, e.Section, pages)
		}
	}
}

func TestBarPagesAndPresentation(t *testing.T) {
	t.Parallel()
	if got := SectionPages("Bar"); !slices.Equal(got, []string{"Appearance", "Layout", "Displays"}) {
		t.Fatalf("Bar pages = %v", got)
	}
	r := Default()
	for _, path := range []string{"bar.enabled", "bar.edge", "bar.style", "bar.shape", "bar.frost-opacity", "bar.height", "bar.font-size"} {
		if e := r.ByPath(path); e == nil || e.Page != "Appearance" {
			t.Errorf("%s is not on Bar › Appearance: %+v", path, e)
		}
	}
	for _, path := range []string{"bar.style", "bar.shape"} {
		if e := r.ByPath(path); e.Present != PresentCards {
			t.Errorf("%s presents %v, want cards", path, e.Present)
		}
	}
	if slices.Contains(SectionNames(), "Displays") {
		t.Error("Displays is still a rail section; it moved to Bar › Displays")
	}
}

func TestClustersCoverTheRailOnce(t *testing.T) {
	t.Parallel()
	var flat []string
	for _, c := range SectionClusters() {
		if c.Name == "" || len(c.Sections) == 0 {
			t.Errorf("empty cluster %+v", c)
		}
		flat = append(flat, c.Sections...)
	}
	if !slices.Equal(flat, SectionNames()) {
		t.Fatalf("clusters flatten to %v, rail is %v", flat, SectionNames())
	}
}
