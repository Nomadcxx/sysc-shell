// Package i18n resolves the UI language and translates interface text.
//
// Resolution follows the usual locale chain: an explicit language saved in
// the user's config beats the environment (LC_ALL, then LC_MESSAGES, then
// LANG), and anything unknown — including a language the catalog does not
// cover — falls back to English. Only the interface is localized here;
// keyboard layout and input method stay separate concerns.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Codes lists the language codes the shell recognizes, in the order the
// Settings picker shows them. English always anchors the catalog.
var Codes = []string{"en", "es", "pt", "ja", "ko", "ru"}

// Native maps a language code to its own-language name, used in pickers so
// every language is recognizable regardless of the UI's current language.
var Native = map[string]string{
	"en": "English",
	"es": "Español",
	"pt": "Português",
	"ja": "日本語",
	"ko": "한국어",
	"ru": "Русский",
}

// FollowSystem is the saved value meaning "no explicit choice: use the
// session language". It is not a catalog key.
const FollowSystem = ""

// Match maps a locale tag to a supported code: BCP-47 style (-, _),
// CLDR-style regional suffixes (es_419), POSIX style (pt_BR.UTF-8), or a
// bare code. Unknown or unsupported tags return "".
func Match(tag string) string {
	t := strings.ToLower(strings.TrimSpace(tag))
	for i := 0; i < len(t); i++ {
		if t[i] == '_' || t[i] == '-' || t[i] == '.' || t[i] == '@' {
			t = t[:i]
			break
		}
	}
	for _, c := range Codes {
		if c == t {
			return c
		}
	}
	return ""
}

// Resolve picks the UI locale: the explicit saved language when set and
// supported, else the first supported language in the environment locale
// chain, else English.
func Resolve(configured string) string {
	if c := Match(configured); c != "" {
		return c
	}
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if c := Match(os.Getenv(env)); c != "" {
			return c
		}
	}
	return "en"
}

//go:embed catalog/*.json
var catalogFS embed.FS

var (
	catalogOnce sync.Once
	catalogs    map[string]map[string]string
)

func loadCatalogs() {
	catalogs = map[string]map[string]string{}
	entries, err := catalogFS.ReadDir("catalog")
	if err != nil {
		return
	}
	for _, e := range entries {
		raw, err := catalogFS.ReadFile("catalog/" + e.Name())
		if err != nil {
			continue
		}
		var table map[string]string
		if err := json.Unmarshal(raw, &table); err != nil {
			panic(fmt.Sprintf("i18n: bad catalog %s: %v", e.Name(), err))
		}
		catalogs[strings.TrimSuffix(e.Name(), ".json")] = table
	}
}

// T returns the translation of key for locale, falling back to english (the
// literal source text) when the locale or the key is unknown. Missing
// translations degrade to English per string, never crash.
func T(locale, key, english string) string {
	catalogOnce.Do(loadCatalogs)
	if table, ok := catalogs[locale]; ok {
		if s, ok := table[key]; ok && s != "" {
			return s
		}
	}
	return english
}
