package config

import (
	"fmt"
	"os"
	"slices"
	"testing"
)

func TestTrayPreferencesRoundTripThroughStrictConfig(t *testing.T) {
	cfg, err := Parse([]byte(`{"tray":{"hidden":["id:chat"],"pinned":["id:mail"],"order":["id:mail","id:chat"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Tray.Hidden, []string{"id:chat"}) ||
		!slices.Equal(cfg.Tray.Pinned, []string{"id:mail"}) ||
		!slices.Equal(cfg.Tray.Order, []string{"id:mail", "id:chat"}) {
		t.Fatalf("tray preferences = %+v", cfg.Tray)
	}
	path := t.TempDir() + "/config.json"
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Tray.Hidden, cfg.Tray.Hidden) || !slices.Equal(got.Tray.Pinned, cfg.Tray.Pinned) ||
		!slices.Equal(got.Tray.Order, cfg.Tray.Order) {
		t.Fatalf("round trip = %+v, want %+v", got.Tray, cfg.Tray)
	}
}

func TestWriteRejectsInvalidTrayPreferencesBeforeReplacing(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	for i := 0; i <= maxTrayPreferences; i++ {
		cfg.Tray.Hidden = append(cfg.Tray.Hidden, fmt.Sprintf("id:item-%d", i))
	}
	if err := Write(path, cfg); err == nil {
		t.Fatal("Write accepted preferences that Load must reject")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(after, before) {
		t.Fatal("invalid tray preferences replaced the prior-good file")
	}
}

func TestTrayPreferencesRejectDuplicatesAndUnboundedInput(t *testing.T) {
	bad := []string{
		`{"tray":{"hidden":["id:a","id:a"]}}`,
		`{"tray":{"pinned":[""]}}`,
	}
	for _, input := range bad {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("Parse accepted %s", input)
		}
	}
}
