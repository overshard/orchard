//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// distFS reads the Vite bundle off disk so `bun run dev` can rewrite build/dist
// under a running server. It's a build tag since //go:embed needs the directory
// at compile time and a fresh clone lacks it.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
