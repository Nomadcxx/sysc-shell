package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSessionLanguage(t *testing.T) {
	if cfg, err := Parse([]byte(`{"session":{"language":"ja"}}`)); err != nil {
		t.Fatalf("valid language rejected: %v", err)
	} else if cfg.Session.Language != "ja" {
		t.Errorf("Session.Language = %q, want ja", cfg.Session.Language)
	}
	if cfg, err := Parse([]byte(`{"session":{}}`)); err != nil || cfg.Session.Language != "" {
		t.Errorf("absent language must parse to follow-system: %q, %v", cfg.Session.Language, err)
	}
	// An explicit empty string is a saved "follow the session language" choice.
	if cfg, err := Parse([]byte(`{"session":{"language":""}}`)); err != nil || cfg.Session.Language != "" {
		t.Errorf("empty language must parse cleanly: %q, %v", cfg.Session.Language, err)
	}
	if _, err := Parse([]byte(`{"session":{"language":"de"}}`)); err == nil {
		t.Errorf("unsupported language must fail to parse")
	} else if !strings.Contains(err.Error(), "session.language") {
		t.Errorf("error should name the path, got %v", err)
	}
}

func TestWriteSessionLanguageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Default()
	cfg.Session.Language = "es"
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Session["language"] != "es" {
		t.Fatalf("written config missing session.language: %s", raw)
	}
	back, err := Load(path)
	if err != nil || back.Session.Language != "es" {
		t.Fatalf("round trip lost language: %q, %v", back.Session.Language, err)
	}
	// Untouched default stays sparse: no session block at all.
	cfg = Default()
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(raw), `"session"`) {
		t.Errorf("default config should not write a session block: %s", raw)
	}
}
