package icons

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// syntheticTheme writes a theme tree shaped like Adwaita on a real desktop:
// three themes in an inheritance chain, ten size directories and eight
// categories each, with a handful of names per category. It is the shape the
// old per-lookup walk cost roughly 1,400 stats and 2 ms in.
func syntheticTheme(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	for _, theme := range []string{"Adwaita", "AdwaitaLegacy", "hicolor"} {
		if theme != "hicolor" {
			dir := filepath.Join(root, theme)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				b.Fatal(err)
			}
			next := "AdwaitaLegacy"
			if theme == "AdwaitaLegacy" {
				next = "hicolor"
			}
			body := fmt.Sprintf("[Icon Theme]\nName=%s\nInherits=%s\n", theme, next)
			if err := os.WriteFile(filepath.Join(dir, "index.theme"), []byte(body), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		for _, size := range []string{"16x16", "22x22", "24x24", "32x32", "48x48", "64x64", "96x96", "128x128", "256x256", "512x512"} {
			for _, category := range []string{"apps", "actions", "devices", "mimetypes", "places", "status", "categories", "panel"} {
				dir := filepath.Join(root, theme, size, category)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					b.Fatal(err)
				}
				for i := range 6 {
					png := filepath.Join(dir, fmt.Sprintf("icon-%d.png", i))
					if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
						b.Fatal(err)
					}
				}
			}
		}
	}
	return root
}

func BenchmarkResolveWarm(b *testing.B) {
	resolver := NewResolver("Adwaita", []string{syntheticTheme(b)})
	// The tree is indexed once here; the benchmark measures a warm lookup.
	resolver.Resolve("icon-3", 48)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := resolver.Resolve("icon-3", 48); !ok {
			b.Fatal("a present icon stopped resolving")
		}
	}
}

func BenchmarkResolveMiss(b *testing.B) {
	resolver := NewResolver("Adwaita", []string{syntheticTheme(b)})
	resolver.Resolve("no-such-icon-xyz", 48)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := resolver.Resolve("no-such-icon-xyz", 48); ok {
			b.Fatal("an absent icon resolved")
		}
	}
}
