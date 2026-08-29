//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle and the Natural Earth topojson off
// disk, so `bun run dev` can rewrite build/dist under a running server and a
// CSS change costs a browser reload rather than a restart.
//
// The release build embeds both; see assets_embed.go. The split is a build tag
// rather than a runtime check because the embed directive needs those
// directories to exist at compile time, and on a fresh clone neither does: a
// single-file binary must not be the thing that makes `go build` fail before
// anyone has run Vite once.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
func mapsFS() fs.FS { return os.DirFS(dir("SITE_MAPS", "build/static_maps")) }
