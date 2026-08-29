//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle and the Natural Earth topojson off
// disk, so `bun run dev` can rewrite build/dist under a running server.
//
// A release build embeds both; see assets_embed.go. This is a build tag rather
// than a runtime check because //go:embed needs those directories to exist at
// compile time, and on a fresh clone neither does.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
func mapsFS() fs.FS { return os.DirFS(dir("SITE_MAPS", "build/static_maps")) }
