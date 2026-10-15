//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle off disk so `bun run dev` can
// rewrite build/dist under a running server. It's a build tag instead of a
// runtime check because //go:embed needs the directory at compile time.
func distFS() fs.FS {
	if d := os.Getenv("SITE_DIST"); d != "" {
		return os.DirFS(d)
	}
	return os.DirFS("build/dist")
}
