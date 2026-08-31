package theme

var stockSeeds = []struct{ name, hex string }{
	{"Blue", "#3d63dd"},
	{"Purple", "#7c4dff"},
	{"Green", "#2e7d32"},
	{"Orange", "#ef6c00"},
	{"Red", "#c62828"},
	{"Cyan", "#00838f"},
	{"Pink", "#c2185b"},
	{"Amber", "#ff8f00"},
	{"Coral", "#e64a19"},
	{"Monochrome", "#808080"},
}

func StockNames() []string {
	out := make([]string, len(stockSeeds))
	for i, s := range stockSeeds {
		out[i] = s.name
	}
	return out
}

func StockSeed(name string) (string, bool) {
	for _, s := range stockSeeds {
		if s.name == name {
			return s.hex, true
		}
	}
	return "", false
}
