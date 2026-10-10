package shell

import (
	"os"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func BenchmarkBarRepaint(b *testing.B) {
	cfg := config.Default()
	if path := os.Getenv("SYSC_BENCH_CONFIG"); path != "" {
		var err error
		cfg, err = config.Load(path)
		if err != nil {
			b.Fatal(err)
		}
	}
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-1")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(bar.stopAnimation)
	bar.apply(barView{Now: time.Now(), Title: "Synthetic window title",
		Pills: []workspacePill{{ID: 1, Index: 1, Name: "one", Focused: true}}})
	w, h := 3440, cfg.Bar.SurfaceExtent()
	if err := bar.Configure(w, h, 120); err != nil {
		b.Fatal(err)
	}
	pixels := make([]byte, w*h*4)
	for b.Loop() {
		if err := bar.Render(pixels, w, h, w*4); err != nil {
			b.Fatal(err)
		}
	}
}
