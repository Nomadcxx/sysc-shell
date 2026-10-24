package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// TestUpdateConfigRefusesToWriteOverAConfigThatDoesNotParse is the data-loss
// guard. A file the shell cannot read is a file the shell must not write
// over: the edit would drop every other line the user wrote and the panel
// would report success.
func TestUpdateConfigRefusesToWriteOverAConfigThatDoesNotParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	hand := `{"theme":{"preset":"compact"},"night-light":{"night_kelvin":3000,"nightKelvin":3000}}`
	if err := os.WriteFile(path, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.BindPersist(path, nil)

	_, err := r.updateConfig(func(cfg *config.Config, _ func(config.Config) *settings.Registry) error {
		cfg.NightLight.DayKelvin = 6200
		return nil
	})
	if err == nil {
		t.Fatal("the edit over a config that does not parse reported success")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error = %v, want it to name the field the file got wrong", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != hand {
		t.Errorf("the file was rewritten:\n%s\nwant:\n%s", after, hand)
	}
}

// TestUpdateConfigKeepsTheLiveConfigWhenTheFileIsGone covers the other half
// of the refusing variant. A file that is temporarily absent must not
// replace the live settings with startup defaults, the way a reload's
// LoadRequired does not.
func TestUpdateConfigKeepsTheLiveConfigWhenTheFileIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg := config.Default()
	cfg.Theme.Preset = theme.PresetCompact
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.BindPersist(path, nil)

	if _, err := r.updateConfig(func(cfg *config.Config, _ func(config.Config) *settings.Registry) error {
		cfg.NightLight.DayKelvin = 6200
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "compact") {
		t.Errorf("the written config dropped the live theme preset:\n%s", after)
	}
}
