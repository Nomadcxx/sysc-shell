package theming

import (
	"bytes"
	"embed"
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

func Render(tpl string, tok theme.Tokens) string {
	t, err := template.New("t").Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return ""
	}
	data := map[string]string{
		"Surface": tok.Surface, "SurfaceContainer": tok.SurfaceContainer,
		"OnSurface": tok.OnSurface, "OnSurfaceVariant": tok.OnSurfaceVariant,
		"Primary": tok.Primary, "OnPrimary": tok.OnPrimary,
		"PrimaryContainer": tok.PrimaryContainer, "OnPrimaryContainer": tok.OnPrimaryContainer,
		"Outline": tok.Outline, "Error": tok.Error, "OnError": tok.OnError,
		"Mode": "dark",
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return ""
	}
	return buf.String()
}
