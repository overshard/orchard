//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle off disk, so `bun run dev` can
// rewrite build/dist under a running server. A build tag rather than a runtime
// check, because //go:embed needs the directory to exist at compile time.
func distFS() fs.FS {
	if d := os.Getenv("SITE_DIST"); d != "" {
		return os.DirFS(d)
	}
	return os.DirFS("build/dist")
}
