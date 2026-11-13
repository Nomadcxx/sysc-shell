package displays

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/compositor"
)

func TestNativeValidatorSelectsCompositor(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"niri", "sysc-compositor"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '"+name+" rejected'\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	for _, kind := range []compositor.Kind{compositor.Niri, compositor.SyscCompositor} {
		validate, err := NativeValidator(kind)
		if err != nil {
			t.Fatal(err)
		}
		if err := validate(context.Background(), "/some config.kdl"); err == nil || !strings.Contains(err.Error(), kind.Executable()+" rejected") {
			t.Fatalf("validator error = %v", err)
		}
	}
}

func TestValidatorOutputIsBoundedWhileWritten(t *testing.T) {
	var out validatorOutput
	payload := strings.Repeat("x", maxValidateOutput*3)
	if n, err := out.Write([]byte(payload)); err != nil || n != len(payload) {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if out.Len() != maxValidateOutput {
		t.Fatalf("retained %d bytes", out.Len())
	}
}
