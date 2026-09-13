//go:build embed

package main

import (
	"embed"
	"io/fs"

	"house.bythewood.me/web"
)

// A release build compiles the Vite bundle in, which is what makes the binary the
// whole server. The directive cannot reference a path above its own package, so
// build/ lives inside the site.
//
//go:embed all:build/dist
var distEmbed embed.FS

func distFS() fs.FS {
	sub, err := fs.Sub(distEmbed, "build/dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Nothing to watch. The manifest is compiled in and cannot change under a running
// binary.
func watchAssets(*web.Assets, fs.FS) {}
