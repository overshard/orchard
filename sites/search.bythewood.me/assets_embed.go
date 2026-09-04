//go:build embed

package main

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var assetFS embed.FS

func assets() fs.FS { return assetFS }

const Reloaded = false
