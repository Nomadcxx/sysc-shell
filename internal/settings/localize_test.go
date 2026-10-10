package settings

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
)

func TestLanguageEntryCarriesNativeLabels(t *testing.T) {
	r := Default()
	e := r.ByPath("session.language")
	if e == nil {
		t.Fatal("session.language entry missing")
	}
	if len(e.OptionLabels) != len(e.Options) {
		t.Fatalf("OptionLabels (%d) not parallel to Options (%d)", len(e.OptionLabels), len(e.Options))
	}
	if e.OptionLabels[0] != "Follow system" || e.Options[0] != "" {
		t.Errorf("first option must be follow-system: %q/%q", e.Options[0], e.OptionLabels[0])
	}
	for i, code := range i18n.Codes {
		if e.Options[i+1] != code || e.OptionLabels[i+1] != i18n.Native[code] {
			t.Errorf("option %d = %q/%q, want %q/%q", i+1, e.Options[i+1], e.OptionLabels[i+1], code, i18n.Native[code])
		}
	}
	// Labels are display only: the setter accepts values, never names.
	cfg := config.Default()
	if err := e.Set(&cfg, "ja"); err != nil || cfg.Session.Language != "ja" {
		t.Errorf("value write failed: %v", err)
	}
	if err := e.Set(&cfg, "日本語"); err == nil {
		t.Errorf("a display label must not be writable as a value")
	}
}

func TestLocalizedTranslatesPresentationOnly(t *testing.T) {
	r := Default()
	cfg := config.Default()
	cfg.Session.Language = "es"
	loc := r.Localized(i18n.Resolve(cfg.Session.Language))
	e := loc.ByPath("session.language")
	if e.Label != "Idioma" {
		t.Errorf("es label = %q, want Idioma", e.Label)
	}
	if e.OptionLabels[0] != "Seguir el sistema" {
		t.Errorf("es follow-system label = %q", e.OptionLabels[0])
	}
	if e.OptionLabels[4] != "日本語" {
		t.Errorf("native names must not be translated: %q", e.OptionLabels[4])
	}
	l := loc.ByPath("session.locker")
	if l.Label != "Bloqueo de pantalla" {
		t.Errorf("es locker label = %q", l.Label)
	}
	// Identity strings stay canonical for actions, search, and writes.
	if e.Path != "session.language" || e.Section != "Session" || e.Group != "Language" {
		t.Errorf("localization moved an identity field: %+v", *e)
	}
	if got := e.Get(config.Config{Session: config.Session{Language: "ko"}}); got != "ko" {
		t.Errorf("accessor broken: %q", got)
	}
	// Unknown locales and English pass the source through.
	if Default().Localized("de").ByPath("session.language").Label != "Language" {
		t.Errorf("unknown locale must fall back to English")
	}
	if r.Localized("en") != r {
		t.Errorf("en should return the registry untouched")
	}
	// The English registry is unaffected by localization: the source stays put.
	if r.ByPath("session.language").Label != "Language" {
		t.Errorf("Localized mutated the source registry")
	}
}

func TestSearchFindsEnglishPathInAnyLocale(t *testing.T) {
	cfg := config.Default()
	cfg.Session.Language = "ja"
	loc := Default().Localized(i18n.Resolve(cfg.Session.Language))
	hits := loc.Search("locker")
	found := false
	for _, e := range hits {
		if e.Path == "session.locker" {
			found = true
		}
	}
	if !found {
		t.Errorf("path search must still hit 画面ロック rows: %+v", hits)
	}
}
