//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// distFS reads the Vite bundle off disk so `bun run dev` can rewrite build/dist
// under a running server. A build tag rather than a runtime check because
// //go:embed needs the directory to exist at compile time, and a fresh clone lacks it.
func distFS() fs.FS { return os.DirFS(env("SITE_DIST", "build/dist")) }
