package shell

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestConfigErrorPersistsUntilSuccessfulReload(t *testing.T) {
	reg := newPanelRegistry(t)
	refused := &config.Diagnostic{Field: "bar.height", Message: "value is not positive"}
	reg.RecordConfigReload(refused)
	first := reg.Status()
	message, ok := first["config_error"].(string)
	stamp, hasStamp := first["config_error_at"].(string)
	if !ok || !hasStamp || message == "" || stamp == "" {
		t.Fatalf("no refusal: %+v", first)
	}
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		t.Fatalf("config_error_at = %q: %v", stamp, err)
	}
	if !strings.Contains(message, "bar.height") || !strings.Contains(message, "value is not positive") {
		t.Fatalf("incomplete diagnostic: %q", message)
	}

	reg.RecordConfigReload(&config.Diagnostic{Field: "bar.height", Message: "value is not positive"})
	if got := reg.Status()["config_error_at"]; got != stamp {
		t.Fatalf("identical refusal reset timestamp: got %v, want %s", got, stamp)
	}

	reg.RecordConfigReload(nil)
	last := reg.Status()
	if last["config_error"] != "" || last["config_error_at"] != "" {
		t.Fatalf("recovery left stale refusal: %+v", last)
	}
}

func TestConfigErrorRejectsUnknownTextAndBoundsDiagnostic(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.RecordConfigReload(errors.New("secret-token"))
	status := reg.Status()
	message, ok := status["config_error"].(string)
	if !ok || message == "" || strings.Contains(message, "secret-token") {
		t.Fatalf("unknown error leaked or missing: %q", message)
	}

	reg.RecordConfigReload(&config.Diagnostic{
		Field:   "bar.height",
		Message: strings.Repeat("x", 8192),
	})
	message, _ = reg.Status()["config_error"].(string)
	if len(message) > 4096 {
		t.Fatalf("diagnostic exceeded the bound: %d bytes", len(message))
	}
}
