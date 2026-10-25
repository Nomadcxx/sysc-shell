package config

import (
	"os"
	"strings"
	"testing"
)

func TestTheLockKeyOSDIsOnByDefault(t *testing.T) {
	if !Default().Panels.OSDLockKeys {
		t.Fatal("the built-in configuration turned the lock-key OSD off")
	}
}

func TestTheLockKeyOSDSurvivesAWriteAndAReload(t *testing.T) {
	cfg, err := Parse([]byte(`{"panels":{"osd_lock_keys":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Panels.OSDLockKeys {
		t.Fatal("the parsed configuration kept the lock-key OSD on")
	}
	path := t.TempDir() + "/config.json"
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "osd_lock_keys") {
		t.Fatalf("the written config does not mention the lock-key OSD: %s", data)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Panels.OSDLockKeys {
		t.Fatal("the lock-key OSD came back on after a reload")
	}
}
