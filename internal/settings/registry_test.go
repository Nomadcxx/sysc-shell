package settings

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestRegistryCoversAllSections(t *testing.T) {
	t.Parallel()
	r := Default()
	sections := []string{"Bar", "Widgets", "Appearance", "Panels", "Session", "Accessibility"}
	for _, s := range sections {
		if len(r.Section(s)) == 0 {
			t.Fatalf("section %s empty", s)
		}
	}
}

func TestEntryGetSetRoundTrip(t *testing.T) {
	t.Parallel()
	r := Default()
	e := r.ByPath("bar.height")
	if e == nil || e.Kind != KindInt {
		t.Fatal("bar.height must be KindInt")
	}
	cfg := config.Default()
	if err := e.Set(&cfg, "48"); err != nil {
		t.Fatal(err)
	}
	if got := e.Get(cfg); got != "48" {
		t.Fatalf("got %q, want 48", got)
	}
}

func TestSetRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	e := Default().ByPath("bar.edge")
	if e == nil {
		t.Fatal("missing bar.edge")
	}
	if err := e.Set(&cfg, "diagonal"); err == nil {
		t.Fatal("enum must reject")
	}
	e2 := Default().ByPath("bar.height")
	if err := e2.Set(&cfg, "not-a-number"); err == nil {
		t.Fatal("int must reject")
	}
}

func TestSearchMatchesLabels(t *testing.T) {
	t.Parallel()
	hits := Default().Search("motion")
	if len(hits) == 0 || hits[0].Path != "accessibility.reduced-motion" {
		t.Fatalf("search motion = %+v", hits)
	}
}
