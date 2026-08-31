package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStockSeedsResolve(t *testing.T) {
	t.Parallel()
	names := StockNames()
	if len(names) < 10 {
		t.Fatalf("got %d families, want at least 10", len(names))
	}
	for _, name := range names {
		hex, ok := StockSeed(name)
		if !ok || len(hex) != 7 || hex[0] != '#' {
			t.Fatalf("stock %q -> %q", name, hex)
		}
	}
}

func TestStockSourceGeneratesViaMatugen(t *testing.T) {
	dir := t.TempDir()
	argsFile := fakeMatugenRecordingArgs(t, dir)
	g := Generator{CacheDir: dir, Matugen: filepath.Join(dir, "matugen")}
	if _, err := g.Generate(Source{Kind: "stock", Seed: "Blue"}, Options{Mode: "dark"}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	hex, _ := StockSeed("Blue")
	got := string(args)
	if !strings.Contains(got, "color") || !strings.Contains(got, "hex") || !strings.Contains(got, hex) {
		t.Fatalf("matugen args %q missing color hex %s", got, hex)
	}
}
