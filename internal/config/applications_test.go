package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestApplicationPreferencesRoundTrip(t *testing.T) {
	cfg, err := Parse([]byte(`{"applications":{"favourites":["org.gnome.Nautilus","firefox"],"hidden":["old-editor"],"default-terminal":"foot"}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := ApplicationPreferences{
		Favourites:      []string{"org.gnome.Nautilus", "firefox"},
		Hidden:          []string{"old-editor"},
		DefaultTerminal: "foot",
	}
	if !slices.Equal(cfg.Applications.Favourites, want.Favourites) ||
		!slices.Equal(cfg.Applications.Hidden, want.Hidden) ||
		cfg.Applications.DefaultTerminal != want.DefaultTerminal {
		t.Fatalf("applications = %+v, want %+v", cfg.Applications, want)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	readApplications := func(path string) map[string]json.RawMessage {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]json.RawMessage
		if err := json.Unmarshal(raw, &root); err != nil {
			t.Fatal(err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(root["applications"], &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	decodeIDs := func(raw json.RawMessage) []string {
		t.Helper()
		var got []string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	assertWritten := func(path string) {
		t.Helper()
		got := readApplications(path)
		if !slices.Equal(decodeIDs(got["favourites"]), want.Favourites) ||
			!slices.Equal(decodeIDs(got["hidden"]), want.Hidden) {
			t.Fatalf("written application lists = %s, want %+v", got, want)
		}
		var terminal string
		if err := json.Unmarshal(got["default-terminal"], &terminal); err != nil {
			t.Fatal(err)
		}
		if terminal != want.DefaultTerminal {
			t.Fatalf("written default terminal = %q, want %q", terminal, want.DefaultTerminal)
		}
	}
	assertWritten(path)

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	second := filepath.Join(t.TempDir(), "config.json")
	if err := Write(second, loaded); err != nil {
		t.Fatalf("Write loaded config: %v", err)
	}
	assertWritten(second)
}

func TestApplicationPreferencesRejectInvalidDesktopIDs(t *testing.T) {
	tooLong := strings.Repeat("a", maxApplicationDesktopIDSize+1)
	tooMany := make([]string, maxApplicationPreferenceIDs+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("app-%04d", i)
	}
	encodedMany, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
	}{
		{name: "empty favourite", body: `{"applications":{"favourites":[""]}}`},
		{name: "empty hidden id", body: `{"applications":{"hidden":[""]}}`},
		{name: "path separator", body: `{"applications":{"favourites":["bad/id"]}}`},
		{name: "control character", body: `{"applications":{"hidden":["bad\u0000id"]}}`},
		{name: "overlong id", body: fmt.Sprintf(`{"applications":{"hidden":[%q]}}`, tooLong)},
		{name: "duplicate favourite", body: `{"applications":{"favourites":["firefox","firefox"]}}`},
		{name: "duplicate hidden", body: `{"applications":{"hidden":["firefox","firefox"]}}`},
		{name: "favourite and hidden overlap", body: `{"applications":{"favourites":["firefox"],"hidden":["firefox"]}}`},
		{name: "empty terminal id", body: `{"applications":{"default-terminal":""}}`},
		{name: "malformed terminal id", body: `{"applications":{"default-terminal":"bad/id"}}`},
		{name: "too many ids", body: `{"applications":{"favourites":` + string(encodedMany) + `}}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.body)); err == nil {
				t.Fatalf("Parse accepted invalid application preference: %s", tt.body)
			}
		})
	}
}

func TestWriteRejectsInvalidApplicationPreferenceIDs(t *testing.T) {
	cfg := Default()
	cfg.Applications.Favourites = []string{"bad/id"}
	if err := Write(filepath.Join(t.TempDir(), "config.json"), cfg); err == nil {
		t.Fatal("Write accepted an invalid desktop ID")
	}

	cfg = Default()
	cfg.Applications.Favourites = []string{"firefox"}
	cfg.Applications.Hidden = []string{"firefox"}
	if err := Write(filepath.Join(t.TempDir(), "config.json"), cfg); err == nil {
		t.Fatal("Write accepted a desktop ID in both favourites and hidden")
	}
}
