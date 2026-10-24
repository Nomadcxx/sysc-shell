package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

// A config file that stopped parsing belongs to the user, not to the shell. A
// settings edit has to refuse the write and report the parse error instead of
// replacing the file with the live config, which would drop every hand edit
// in it while the edit reported success.
func TestASettingsEditDoesNotOverwriteAConfigThatStoppedParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	hand := []byte("{\n  \"night-light\": {\n    \"night_kelvin\": 4200\n  },\n  \"nightKelvin\": 5000\n}\n")
	if err := os.WriteFile(path, hand, 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.BindPersist(path, nil)

	_, err := r.updateConfig(func(cfg *config.Config, _ func(config.Config) *settings.Registry) error {
		cfg.NightLight.NightKelvin = 3500
		return nil
	})
	if err == nil {
		t.Fatal("the edit reported success on a config file that does not parse")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(hand) {
		t.Fatalf("the config file was rewritten:\n%s", got)
	}
}
