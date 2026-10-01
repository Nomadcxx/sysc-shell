package theme

import "testing"

func TestGeneratorCustomSource(t *testing.T) {
	s := newTestStore(t)
	slug, err := s.Save(testFile(t))
	if err != nil {
		t.Fatal(err)
	}
	g := Generator{CacheDir: t.TempDir(), Custom: s}

	want, _ := testFile(t).Tokens("light", false)
	got, err := g.Generate(Source{Kind: "custom", Seed: slug}, Options{Mode: "light"})
	if err != nil || got != want {
		t.Fatalf("custom source: err=%v, tokens differ=%v", err, got != want)
	}

	fallback, err := g.Generate(Source{Kind: "custom", Seed: "missing"}, Options{Mode: "dark"})
	if err == nil {
		t.Fatal("unknown slug produced no error")
	}
	if fallback.Complete() != nil {
		t.Fatal("the failure path returned an incomplete palette")
	}

	if _, err := (Generator{CacheDir: t.TempDir()}).Generate(Source{Kind: "custom", Seed: slug}, Options{Mode: "dark"}); err == nil {
		t.Fatal("a generator with no store accepted a custom source")
	}
}
