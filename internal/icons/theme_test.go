package icons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverPrefersTheRequestedSizeThenScalesDown(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "16x16/apps", "chat.png")
	writeIcon(t, root, "Adwaita", "48x48/apps", "chat.png")
	writeIcon(t, root, "Adwaita", "64x64/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})

	exact, ok := resolver.Resolve("chat", 48)
	if !ok || filepath.Base(filepath.Dir(filepath.Dir(exact))) != "48x48" {
		t.Fatalf("exact match = %q (%v)", exact, ok)
	}
	// 32 is offered by nobody: the smallest icon at least that large wins,
	// because scaling down keeps more detail than scaling up.
	bigger, ok := resolver.Resolve("chat", 32)
	if !ok || filepath.Base(filepath.Dir(filepath.Dir(bigger))) != "48x48" {
		t.Fatalf("upward match = %q (%v)", bigger, ok)
	}
	// Nothing is big enough for 128: take the largest available.
	largest, ok := resolver.Resolve("chat", 128)
	if !ok || filepath.Base(filepath.Dir(filepath.Dir(largest))) != "64x64" {
		t.Fatalf("downward match = %q (%v)", largest, ok)
	}
}

func TestResolverFollowsThemeInheritance(t *testing.T) {
	root := t.TempDir()
	writeTheme(t, root, "Papirus", "Adwaita,hicolor")
	writeIcon(t, root, "Adwaita", "48x48/apps", "mail.png")
	writeIcon(t, root, "hicolor", "48x48/apps", "fallback.png")
	resolver := NewResolver("Papirus", []string{root})

	if _, ok := resolver.Resolve("mail", 48); !ok {
		t.Fatal("an inherited theme was not searched")
	}
	if _, ok := resolver.Resolve("fallback", 48); !ok {
		t.Fatal("hicolor was not searched")
	}
	if _, ok := resolver.Resolve("absent", 48); ok {
		t.Fatal("an absent name resolved")
	}
}

func TestResolverSurvivesInheritanceCycles(t *testing.T) {
	root := t.TempDir()
	writeTheme(t, root, "Loop", "Mirror")
	writeTheme(t, root, "Mirror", "Loop")
	writeIcon(t, root, "Mirror", "22x22/apps", "chat.png")
	resolver := NewResolver("Loop", []string{root})

	if _, ok := resolver.Resolve("chat", 22); !ok {
		t.Fatal("a cyclic inheritance chain lost an icon")
	}
}

func TestResolverTierOrder(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Mix", "48x48/apps", "chat.png")
	writeIcon(t, root, "Mix", "96x96/apps", "chat.png")
	writeSVG(t, root, "Mix", "scalable/apps", "chat.svg")
	resolver := NewResolver("Mix", []string{root})

	if got, ok := resolver.Resolve("chat", 48); !ok || !strings.HasSuffix(got, "48x48/apps/chat.png") {
		t.Fatalf("exact raster = %q (%v), want the 48px png over the svg", got, ok)
	}
	if got, ok := resolver.Resolve("chat", 24); !ok || !strings.HasSuffix(got, "chat.svg") {
		t.Fatalf("svg tier = %q (%v), want the svg over a nearest raster", got, ok)
	}
	if got, ok := resolver.ResolveRaster("chat", 24); !ok || !strings.HasSuffix(got, "48x48/apps/chat.png") {
		t.Fatalf("raster fallback = %q (%v), want the nearest png with no svg", got, ok)
	}
}

func TestResolverTakesAnSvgOnlyTheme(t *testing.T) {
	root := t.TempDir()
	writeSVG(t, root, "Vector", "scalable/apps", "chat.svg")
	resolver := NewResolver("Vector", []string{root})
	if _, ok := resolver.Resolve("chat", 48); !ok {
		t.Fatal("an svg-only theme did not resolve")
	}
}

func writeSVG(t *testing.T, root, theme, category, name string) {
	t.Helper()
	dir := filepath.Join(root, theme, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<circle cx="12" cy="12" r="10" fill="#000000"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolverRefusesTraversalAndAcceptsAbsoluteRasters(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})

	if _, ok := resolver.Resolve("../../etc/passwd", 48); ok {
		t.Fatal("a name containing a path separator resolved")
	}
	if _, ok := resolver.Resolve("", 48); ok {
		t.Fatal("an empty name resolved")
	}

	// The notification spec lets an application send an absolute path.
	absolute := filepath.Join(root, "Adwaita", "48x48", "apps", "chat.png")
	if got, ok := resolver.Resolve(absolute, 48); !ok || got != absolute {
		t.Fatalf("absolute path = %q (%v)", got, ok)
	}
	missing := filepath.Join(root, "nope.png")
	if _, ok := resolver.Resolve(missing, 48); ok {
		t.Fatal("a missing absolute path resolved")
	}
}

func TestResolverFallsBackToUnthemedPixmaps(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "legacy.png"), pngBytes(t, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver("Adwaita", []string{root})
	if _, ok := resolver.Resolve("legacy", 48); !ok {
		t.Fatal("an unthemed pixmap was not found")
	}
}

func writeIcon(t *testing.T, root, theme, category, name string) {
	t.Helper()
	dir := filepath.Join(root, theme, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pngBytes(t, 8), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTheme(t *testing.T, root, theme, inherits string) {
	t.Helper()
	dir := filepath.Join(root, theme)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[Icon Theme]\nName=" + theme + "\nInherits=" + inherits + "\n"
	if err := os.WriteFile(filepath.Join(dir, "index.theme"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewResolverUsesTheConfiguredIconTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := filepath.Join(home, ".config", "gtk-3.0")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "[Settings]\ngtk-icon-theme-name=\"Tela\"\n"
	if err := os.WriteFile(filepath.Join(settings, "settings.ini"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeTheme(t, root, "Tela", "hicolor")
	writeIcon(t, root, "Tela", "48x48/apps", "chat.png")
	writeIcon(t, root, "hicolor", "48x48/apps", "fallback.png")

	resolver := NewResolver("", []string{root})
	if _, ok := resolver.Resolve("chat", 48); !ok {
		t.Fatal("the configured icon theme was not searched")
	}
	if _, ok := resolver.Resolve("fallback", 48); !ok {
		t.Fatal("hicolor was not searched after the configured theme")
	}
}
