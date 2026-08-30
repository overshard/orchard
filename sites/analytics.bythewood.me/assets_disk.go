//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A dev build reads assets off disk so Vite can rewrite them under a running
// server. It is a build tag and not a runtime check because //go:embed needs
// these directories to exist at compile time, and a fresh clone has neither.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
func mapsFS() fs.FS { return os.DirFS(dir("SITE_MAPS", "build/static_maps")) }
