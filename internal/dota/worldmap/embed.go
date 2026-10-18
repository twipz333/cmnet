package worldmap

import "embed"

//go:embed data/*.json
var dataFS embed.FS

// CurrentVersion is the geometry dataset this binary ships. Independent of Advisor version.
const CurrentVersion = "7.39-macro-v1"
