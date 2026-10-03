package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func previewTestTokens(t *testing.T, name string) theme.Tokens {
	t.Helper()
	tok, ok := theme.NamedPalette(name, "dark", false)
	if !ok {
		t.Fatal("no palette " + name)
	}
	return tok
}

func TestThemePreviewTokensPaintsWithoutCommitting(t *testing.T) {
	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	committed := reg.tokens

	reg.mu.Lock()
	reg.themePreviewTokensLocked(previewTestTokens(t, "gruvbox"))
	reg.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for {
		reg.mu.Lock()
		painted := reg.previewTheme != nil
		reg.mu.Unlock()
		if painted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the preview was never painted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.tokens != committed {
		t.Fatal("the preview replaced the committed palette")
	}
	if !reg.previewing {
		t.Fatal("previewing flag not set")
	}
}

func TestThemePreviewTokensLastRequestWins(t *testing.T) {
	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	reg.mu.Lock()
	for _, n := range []string{"gruvbox", "nord", "dracula"} {
		reg.themePreviewTokensLocked(previewTestTokens(t, n))
	}
	reg.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	want, _ := theme.NamedPalette("dracula", "dark", false)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.previewTheme == nil {
		t.Fatal("nothing painted")
	}
	if reg.previewTheme.tokens != want {
		t.Fatal("an earlier preview painted after a later one")
	}
}

func TestThemePreviewHideRestoresAfterTokensPreview(t *testing.T) {
	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	reg.mu.Lock()
	reg.themePreviewTokensLocked(previewTestTokens(t, "nord"))
	reg.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	if out := reg.themePreviewHide(); out["previewing"] != false {
		t.Fatalf("hide = %v", out)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.previewing {
		t.Fatal("still previewing after hide")
	}
}
