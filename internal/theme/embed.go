package theme

import (
	_ "embed"
)

//go:embed matugen/config.toml
var matugenConfig string

//go:embed matugen/tpl.json
var matugenTemplate string
