package shell

import (
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/render"
)

// registryFontCatalog lazily shares the scanner and system index. Surface maps
// and faces remain private.
type registryFontCatalog struct {
	mu      sync.Mutex
	catalog *render.SystemFontCatalog
}

func (c *registryFontCatalog) textRenderer(family string) (*render.TextRenderer, error) {
	c.mu.Lock()
	if c.catalog == nil {
		c.catalog = render.NewSystemFontCatalog(render.DefaultFontCacheDir())
	}
	catalog := c.catalog
	c.mu.Unlock()
	fonts, err := catalog.NewFontMap(family)
	if err != nil {
		return nil, err
	}
	return render.NewTextRendererWithFontMap(fonts), nil
}

func (r *Registry) textRenderer(family string) (*render.TextRenderer, error) {
	return r.fontCatalog.textRenderer(family)
}
