//go:build !embed

package main

import (
	"io/fs"
	"os"

	"house.bythewood.me/web"
)

// A dev build reads the Vite bundle off disk, so a CSS change needs a browser
// reload and not a Go restart.
func distFS() fs.FS { return os.DirFS("build/dist") }

// And the manifest is watched, because Vite renames the bundle on every rebuild
// and the server would otherwise keep serving the name it booted with.
func watchAssets(a *web.Assets, dist fs.FS) { a.WatchManifest(dist) }
