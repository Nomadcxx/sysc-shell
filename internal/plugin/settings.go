package plugin

import (
	"encoding/json"
	"fmt"
)

// CheckValues reports whether values match a declared schema.
//
// Unknown keys, the wrong type, a value outside declared bounds, or a select
// option that was not listed are rejected. The caller must not write the
// document when this fails: a rejected candidate leaves live configuration
// unchanged.
func CheckValues(schema []Setting, values map[string]any) error {
	byKey := make(map[string]Setting, len(schema))
	for _, s := range schema {
		byKey[s.Key] = s
	}
	for key, value := range values {
		s, ok := byKey[key]
		if !ok {
			return fmt.Errorf("plugin: setting %q is not declared", key)
		}
		if err := checkOne(s, value); err != nil {
			return err
		}
		if s.VisibleWhen != nil {
			parent, ok := values[s.VisibleWhen.Key]
			if !ok {
				parent = byKey[s.VisibleWhen.Key].Default
			}
			if !visibleEquals(parent, s.VisibleWhen.Equals) {
				// Hidden is still typed if the user stored it; the control
				// is what disappears, not the stored value's validity.
			}
		}
	}
	return nil
}

func checkOne(s Setting, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("plugin: setting %q: %w", s.Key, err)
	}
	decoded, err := decodeValue(raw, s.Type)
	if err != nil {
		return fmt.Errorf("plugin: setting %q: %w", s.Key, err)
	}
	if s.Type.numeric() {
		if err := s.inRange(decoded); err != nil {
			return fmt.Errorf("plugin: setting %q: %w", s.Key, err)
		}
	}
	if s.Type == SettingSelect {
		str, _ := decoded.(string)
		if !s.hasOption(str) {
			return fmt.Errorf("plugin: setting %q: %q is not an option", s.Key, str)
		}
	}
	return nil
}

func visibleEquals(have, want any) bool {
	a, _ := json.Marshal(have)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}
