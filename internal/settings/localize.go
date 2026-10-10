package settings

import "github.com/Nomadcxx/sysc-shell/internal/i18n"

// languageOptions are the stored values for session.language; languageLabels
// are their display names. Languages are shown in their own tongue in every
// locale — 日本語 reads as 日本語 in an English or a Russian UI — so the
// native names are not catalog keys; only "Follow system" is translated.
var languageOptions = append([]string{i18n.FollowSystem}, i18n.Codes...)

var languageLabels = buildLanguageLabels()

func buildLanguageLabels() []string {
	labels := make([]string, 0, len(i18n.Codes)+1)
	labels = append(labels, "Follow system")
	for _, code := range i18n.Codes {
		labels = append(labels, i18n.Native[code])
	}
	return labels
}

// Localized returns a registry whose display text follows the given locale,
// resolved by the caller (see i18n.Resolve). Only presentation strings move:
// Path, Section, Group, Page, Options, and the Get/Set accessors keep their
// canonical English tokens, so actions, writes, and lookups are unaffected
// by translation. Unknown keys and locales fall back to the English source,
// string by string.
func (r *Registry) Localized(locale string) *Registry {
	if locale == "" || locale == "en" {
		return r
	}
	out := &Registry{entries: make([]Entry, len(r.entries))}
	for i, e := range r.entries {
		e.Label = i18n.T(locale, "settings.label."+e.Path, e.Label)
		e.Describe = i18n.T(locale, "settings.describe."+e.Path, e.Describe)
		if e.EmptyLabel != "" {
			e.EmptyLabel = i18n.T(locale, "settings.empty."+e.Path, e.EmptyLabel)
		}
		if len(e.OptionLabels) == len(e.Options) && len(e.OptionLabels) > 0 {
			labels := make([]string, len(e.OptionLabels))
			for j, label := range e.OptionLabels {
				labels[j] = i18n.T(locale, "settings.option."+e.Path+"."+e.Options[j], label)
			}
			e.OptionLabels = labels
		}
		out.entries[i] = e
	}
	return out
}
