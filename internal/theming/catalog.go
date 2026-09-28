package theming

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"text/template"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

//go:embed templates/*.tpl
var tplFS embed.FS

type CatalogT struct {
	names []string
	tpl   map[string]string
}

// completeTemplates names the templates whose output is real enough to write
// onto an application's live config path. Everything else is a colour stub
// and is gated off (GH #7); add a name here when its template is verified.
var completeTemplates = map[string]bool{
	"niri": true,
	// Terminal cluster: alacritty, foot, ghostty and kitty passed live checks
	// on Niri; wezterm and starship are covered by render and mechanism
	// tests only, the binaries are not installed here.
	"alacritty": true, "foot": true, "ghostty": true, "kitty": true,
	"starship": true, "wezterm": true,
	// Non-terminal cluster: btop and cava were checked by running them here;
	// helix, kcolorscheme and qt are test-only (binaries absent).
	"btop": true, "cava": true, "helix": true,
	"kcolorscheme": true, "qt": true,
}

func Complete(name string) bool { return completeTemplates[name] }

func Catalog() *CatalogT {
	c := &CatalogT{tpl: map[string]string{}}
	_ = fs.WalkDir(tplFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if path.Ext(p) != ".tpl" {
			return nil
		}
		name := strings.TrimSuffix(path.Base(p), ".tpl")
		b, err := tplFS.ReadFile(p)
		if err != nil {
			return err
		}
		c.names = append(c.names, name)
		c.tpl[name] = string(b)
		return nil
	})
	return c
}

func (c *CatalogT) Names() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.names...)
}

func (c *CatalogT) Template(name string) string {
	if c == nil {
		return ""
	}
	return c.tpl[name]
}

// Render fills one template from a palette. Mode names which half of the
// palette this is, and Source names where it came from, so a template can
// branch on either.
func Render(tpl string, tok theme.Tokens) string {
	return RenderWith(tpl, tok, "dark", "")
}

// RenderWith is Render with the mode and source metadata a caller knows.
//
// The token map comes from the palette's own role table rather than a list
// kept here, so a role added to the palette reaches every template without a
// second table to update. Only palette roles are exported: density, type,
// shape, opacity, elevation and motion are the shell's composition and mean
// nothing in another application's colour file.
func RenderWith(tpl string, tok theme.Tokens, mode, source string) string {
	t, err := template.New("t").Funcs(template.FuncMap{"darken": darken, "rgb": rgb}).Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return ""
	}
	data := tok.Export()
	if mode == "" {
		mode = "dark"
	}
	data["Mode"] = mode
	data["Source"] = source
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return ""
	}
	return buf.String()
}

// rgb renders a #RRGGBB colour as the decimal r,g,b triples KDE colour
// schemes use. An unparsable colour yields an empty value; the golden test
// fails loudly on empty rather than on a plausible wrong triple.
func rgb(hex string) string {
	c, err := theme.ParseColor(hex)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d,%d,%d", c.R, c.G, c.B)
}

// darken mixes a #RRGGBB colour toward black. A few application theme files
// want derived shades rather than another palette role.
func darken(hex string, amt float64) string {
	c, err := theme.ParseColor(hex)
	if err != nil {
		return hex
	}
	f := 1 - amt
	return theme.Color{
		R: uint8(float64(c.R) * f),
		G: uint8(float64(c.G) * f),
		B: uint8(float64(c.B) * f),
		A: c.A,
	}.Hex()
}
