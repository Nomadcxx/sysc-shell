package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func fakeClipboard(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestReadSystemClipboardIsPlainTextBoundedAndTimed(t *testing.T) {
	fakeClipboard(t, "if [ \"$1\" = \"--list-types\" ]; then\n"+
		"printf 'text/html\\ntext/plain\\ntext/plain;charset=utf-8\\n'\n"+
		"elif [ \"$3\" = \"text/plain;charset=utf-8\" ]; then\n"+
		"printf 'line one\\nline two'\n"+
		"else exit 9; fi")
	got, err := readSystemClipboard(context.Background())
	if err != nil || got.Text != "line one\nline two" {
		t.Fatalf("read clipboard = %q, %v", got.Text, err)
	}

	fakeClipboard(t, fmt.Sprintf("if [ \"$1\" = \"--list-types\" ]; then\n"+
		"printf 'text/plain\\n'\n"+
		"elif [ \"$3\" = \"text/plain\" ]; then\n"+
		"printf '%%%ds' ''\n"+
		"else exit 9; fi", v1.MaxInputBytes+1))
	if _, err := readSystemClipboard(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized clipboard error = %v", err)
	}

	fakeClipboard(t, "if [ \"$1\" = \"--list-types\" ]; then exec sleep 3; fi; exit 9")
	started := time.Now()
	if _, err := readSystemClipboard(context.Background()); err == nil || time.Since(started) > 3*time.Second {
		t.Fatalf("slow clipboard call = %v after %v", err, time.Since(started))
	}
}

func TestReadSystemClipboardDoesNotImportRichTextOrUnknownCharset(t *testing.T) {
	fakeClipboard(t, "if [ \"$1\" = \"--list-types\" ]; then "+
		"printf 'text/html\\ntext/plain;charset=utf-16\\n'; else "+
		"printf 'unexpected read' >&2; exit 9; fi")
	if _, err := readSystemClipboard(context.Background()); err == nil || !strings.Contains(err.Error(), "plain-text") {
		t.Fatalf("unsupported clipboard types error = %v", err)
	}
}
