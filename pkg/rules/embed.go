package rules

import (
	"embed"
)

//go:embed resources/core/*.mdc
//go:embed resources/packs/*.mdc
var EmbeddedRules embed.FS
