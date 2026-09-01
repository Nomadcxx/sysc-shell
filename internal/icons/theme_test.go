package icons

import (
	"os"
	"path/filepath"
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

// Milestone 5 ships no SVG rasterizer, so an SVG-only theme must report no
// file rather than a path the decoder cannot read.
func TestResolverIgnoresVectorOnlyThemes(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Vector", "scalable/apps", "chat.svg")
	resolver := NewResolver("Vector", []string{root})

	if path, ok := resolver.Resolve("chat", 48); ok {
		t.Fatalf("an SVG resolved to %q; Milestone 5 decodes raster only", path)
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
