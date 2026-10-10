package i18n

import "testing"

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
