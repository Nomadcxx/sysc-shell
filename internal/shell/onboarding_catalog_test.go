package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// onboardingKeyPatterns find every catalog key the wizard's source asks for.
// Keys are written out in full at their call site so this scan sees them.
var onboardingKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`"(onboarding\.[a-z0-9_.]+[a-z0-9_])"`),
	regexp.MustCompile(`onbText\(h, "([a-z0-9_.]+)"`),
	regexp.MustCompile(`onbActionButton\(h, "[^"]+", "([a-z0-9_.]+)"`),
	regexp.MustCompile(`key, (?:fallback|english) :?= "([a-z0-9_.]+)"`),
}

// onboardingDynamicPrefix is the one key family built at run time: the
// native name of a CJK language, one key per locale.
const onboardingDynamicPrefix = "onboarding.font.language."

func TestOnboardingCatalogsMatchTheKeysTheWizardUses(t *testing.T) {
	used := map[string]bool{}
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, pattern := range onboardingKeyPatterns {
			for _, m := range pattern.FindAllStringSubmatch(string(raw), -1) {
				key := m[1]
				if i > 0 {
					key = "onboarding." + key
				}
				used[key] = true
			}
		}
	}
	if len(used) < 40 {
		t.Fatalf("found %d onboarding keys; the scan is broken", len(used))
	}
	for _, locale := range []string{"es", "pt", "ja", "ko", "ru"} {
		raw, err := os.ReadFile(filepath.Join("..", "i18n", "catalog", locale+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(raw, &catalog); err != nil {
			t.Fatal(err)
		}
		var missing, stale []string
		for key := range used {
			if _, ok := catalog[key]; !ok {
				missing = append(missing, key)
			}
		}
		for key := range catalog {
			if strings.HasPrefix(key, "onboarding.") && !used[key] && !strings.HasPrefix(key, onboardingDynamicPrefix) {
				stale = append(stale, key)
			}
		}
		sort.Strings(missing)
		sort.Strings(stale)
		if len(missing) > 0 {
			t.Errorf("%s lacks %d onboarding keys: %s", locale, len(missing), strings.Join(missing, " "))
		}
		if len(stale) > 0 {
			t.Errorf("%s keeps %d unused onboarding keys: %s", locale, len(stale), strings.Join(stale, " "))
		}
	}
}
