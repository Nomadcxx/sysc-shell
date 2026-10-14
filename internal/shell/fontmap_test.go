package shell

import (
	"sync"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
)

func TestRegistryFontCatalogIsLazyAndPerRegistry(t *testing.T) {
	first, second := &Registry{}, &Registry{}
	if first.fontCatalog.catalog != nil || second.fontCatalog.catalog != nil {
		t.Fatal("font catalog was created before a text renderer was needed")
	}

	texts := make([]*render.TextRenderer, 2)
	errs := make([]error, len(texts))
	families := []string{"sans-serif", "serif"}
	var wg sync.WaitGroup
	for i := range texts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			texts[i], errs[i] = first.textRenderer(families[i])
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	catalog := first.fontCatalog.catalog
	if texts[0] == texts[1] {
		t.Fatal("Registry surfaces shared a TextRenderer")
	}
	if catalog == nil {
		t.Fatal("Registry did not retain its catalog")
	}

	_, err := second.textRenderer("sans-serif")
	otherCatalog := second.fontCatalog.catalog
	if err != nil {
		t.Fatal(err)
	}
	if catalog == otherCatalog {
		t.Fatal("separate Registries shared a mutable scanner catalog")
	}
}
