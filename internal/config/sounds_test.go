package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSoundsAreOffByDefault(t *testing.T) {
	if got, want := Default().Sounds, (Sounds{Notification: false, Volume: 60}); got != want {
		t.Fatalf("Default().Sounds = %+v, want %+v", got, want)
	}
}

func TestSoundsBlockParsesAndValidates(t *testing.T) {
	c, err := Parse([]byte(`{"sounds":{"notification":true,"volume":35}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sounds != (Sounds{Notification: true, Volume: 35}) {
		t.Fatalf("Sounds = %+v", c.Sounds)
	}
	for _, bad := range []string{`{"sounds":{"volume":-1}}`, `{"sounds":{"volume":101}}`} {
		if _, err := Parse([]byte(bad)); err == nil || !strings.Contains(err.Error(), "sounds.volume") {
			t.Errorf("%s: err = %v, want mention of sounds.volume", bad, err)
		}
	}
	if _, err := Parse([]byte(`{"sounds":{"ring":true}}`)); err == nil {
		t.Error("an unknown sounds key was accepted")
	}
}

func TestSoundsRoundTripAndStaySparse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); strings.Contains(string(raw), `"sounds"`) {
		t.Fatalf("the default config wrote a sounds block:\n%s", raw)
	}
	c := Default()
	c.Sounds = Sounds{Notification: true, Volume: 80}
	if err := Write(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sounds != c.Sounds {
		t.Fatalf("round trip = %+v, want %+v", got.Sounds, c.Sounds)
	}
}
