package i18n

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := map[string]string{
		"es":            "es",
		"es_ES":         "es",
		"es-419":        "es",
		"pt_BR.UTF-8":   "pt",
		"ja_JP.UTF-8":   "ja",
		"ko-KR":         "ko",
		"ru_RU.UTF-8":   "ru",
		"EN":            "en",
		" zh_CN.UTF-8 ": "",
		"C":             "",
		"POSIX":         "",
		"":              "",
		"de":            "",
		"es_MX@variant": "es",
	}
	for tag, want := range cases {
		if got := Match(tag); got != want {
			t.Errorf("Match(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestResolvePrefersSavedOverEnv(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "ja_JP.UTF-8")
	if got := Resolve("es"); got != "es" {
		t.Errorf("saved es should beat env ja, got %q", got)
	}
	if got := Resolve(""); got != "ja" {
		t.Errorf("empty saved should follow env LANG, got %q", got)
	}
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	if got := Resolve(""); got != "ru" {
		t.Errorf("LC_ALL should beat LANG, got %q", got)
	}
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "C")
	if got := Resolve(""); got != "en" {
		t.Errorf("C locale should fall back to en, got %q", got)
	}
	if got := Resolve("nonsense"); got != "en" {
		t.Errorf("unknown saved should fall back to en, got %q", got)
	}
}

func TestTLooksUpAndFallsBack(t *testing.T) {
	if got := T("es", "cluster.System", "System"); got != "Sistema" {
		t.Errorf("es cluster.System = %q", got)
	}
	if got := T("ja", "settings.option.session.language.", "Follow system"); got != "システム設定に従う" {
		t.Errorf("ja follow-system = %q", got)
	}
	if got := T("en", "cluster.System", "System"); got != "System" {
		t.Errorf("en (no catalog) must return source, got %q", got)
	}
	if got := T("ru", "no.such.Key", "source text"); got != "source text" {
		t.Errorf("missing key must return source, got %q", got)
	}
}

func TestNativeNamesAreSelfDescriptions(t *testing.T) {
	want := map[string]string{"en": "English", "es": "Español", "pt": "Português",
		"ja": "日本語", "ko": "한국어", "ru": "Русский"}
	for code, name := range want {
		if Native[code] != name {
			t.Errorf("Native[%q] = %q, want %q", code, Native[code], name)
		}
	}
}

// TestCatalogsAreConsistent keeps the five locale catalogs in lockstep: the
// same key set (Russian may add the plural forms the count switch needs),
// non-empty values, and identical placeholder counts per key.
func TestCatalogsAreConsistent(t *testing.T) {
	loads := map[string]map[string]string{}
	for _, code := range []string{"es", "pt", "ja", "ko", "ru"} {
		raw, err := catalogFS.ReadFile("catalog/" + code + ".json")
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		loads[code] = m
	}
	ruExtras := map[string]bool{
		"launcher.noun.result.few": true, "launcher.noun.result.many": true,
		"launcher.noun.app.few": true, "launcher.noun.app.many": true,
	}
	ph := regexp.MustCompile(`%[sdf]|%%`)
	for code, m := range loads {
		for k, v := range m {
			if v == "" {
				t.Errorf("%s: empty value for %q", code, k)
			}
		}
		base := loads["es"]
		if code == "ru" {
			base = map[string]string{}
			for k, v := range loads["es"] {
				base[k] = v
			}
			for k := range ruExtras {
				base[k] = m[k]
			}
		}
		if len(m) != len(base) {
			t.Fatalf("%s: %d keys, want %d", code, len(m), len(base))
		}
		for k := range base {
			if _, ok := m[k]; !ok {
				t.Errorf("%s: missing key %q", code, k)
			}
		}
		for k, v := range m {
			if len(ph.FindAllString(v, -1)) != len(ph.FindAllString(base[k], -1)) {
				t.Errorf("%s: placeholder count differs for %q: %q vs %q", code, k, v, base[k])
			}
		}
	}
}

func TestOnboardingPluginStringsAreLocalized(t *testing.T) {
	fallbacks := map[string]string{
		"onboarding.intro.plugins":            "Choose installed bar plugins to enable and place on the bar. Your choices save when you finish; install plugins later in Settings.",
		"onboarding.plugins.empty":            "No installed bar plugins yet. The plugin store has more.",
		"onboarding.action.plugin_store":      "Open plugin store",
		"onboarding.summary.plugins_selected": "%d selected",
		"onboarding.plugins.toggle":           "Place %s on the bar",
		"onboarding.plugins.widget":           "Bar widget: %s",
		"onboarding.title.plugins":            "Startup plugins",
	}
	for _, locale := range []string{"es", "pt", "ja", "ko", "ru"} {
		raw, err := catalogFS.ReadFile("catalog/" + locale + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(raw, &catalog); err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for key, fallback := range fallbacks {
			if got, ok := catalog[key]; !ok || got == "" || got == fallback {
				t.Errorf("%s: %s = %q, want localized text", locale, key, got)
			}
		}
	}
}
