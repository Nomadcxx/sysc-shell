package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestBarSurfaceDefaultsWhenTheKeysAreAbsent(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"edge":"top"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.Bar
	if b.Style != "frosted" || b.Shape != "attached" || b.FrostOpacity != 65 || b.PillOpacity != 70 {
		t.Errorf("surface = %q %q %d %d, want frosted attached 65 70",
			b.Style, b.Shape, b.FrostOpacity, b.PillOpacity)
	}
}

func TestBarSurfaceValuesAreValidated(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		doc, path, mention string
	}{
		{`{"bar":{"style":"glass"}}`, "bar.style", "frosted, solid, islands"},
		{`{"bar":{"shape":"hanging"}}`, "bar.shape", "attached, floating"},
		{`{"bar":{"frost-opacity":30}}`, "bar.frost-opacity", "40"},
		{`{"bar":{"pill-opacity":101}}`, "bar.pill-opacity", "100"},
		{`{"outputs":[{"connector":"DP-1","bar":{"style":"glass"}}]}`, ".bar.style", "frosted"},
	} {
		_, err := Parse([]byte(tc.doc))
		if err == nil {
			t.Errorf("%s was accepted", tc.doc)
			continue
		}
		if !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("%s: error %q should name %s and %s", tc.doc, err, tc.path, tc.mention)
		}
	}
}

func TestBarSurfaceWritesOnlyWhatDiffers(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Bar.Style = "islands"
	c.Bar.PillOpacity = 55
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"style"`, `"pill-opacity"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("written config lacks %s:\n%s", want, data)
		}
	}
	for _, unwanted := range []string{`"shape"`, `"frost-opacity"`, `"overhang"`} {
		if strings.Contains(string(data), unwanted) {
			t.Errorf("written config carries default or derived %s:\n%s", unwanted, data)
		}
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bar.Style != "islands" || got.Bar.PillOpacity != 55 || got.Bar.Shape != "attached" {
		t.Errorf("round trip = %q %d %q", got.Bar.Style, got.Bar.PillOpacity, got.Bar.Shape)
	}
}

func TestPerOutputBarInheritsTheSurface(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"style":"solid","frost-opacity":50},
		"outputs":[{"connector":"DP-1","bar":{"shape":"floating"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.ForConnector("DP-1")
	if b.Style != "solid" || b.FrostOpacity != 50 || b.Shape != "floating" {
		t.Errorf("DP-1 surface = %q %d %q, want solid 50 floating", b.Style, b.FrostOpacity, b.Shape)
	}
}

func TestOverhangFollowsTheShape(t *testing.T) {
	t.Parallel()
	b := Default().Bar
	if got := b.Overhang(); got != theme.FilletRadius {
		t.Errorf("attached overhang = %d, want the fillet %d", got, theme.FilletRadius)
	}
	b.Shape = "floating"
	if got := b.Overhang(); got != 0 {
		t.Errorf("floating overhang = %d, want 0", got)
	}
}
