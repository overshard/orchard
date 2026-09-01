//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// distFS reads the Vite bundle off disk so a running server picks up rebuilds.
// A build tag rather than a runtime check, because //go:embed fails at compile
// time on a missing directory and a fresh clone has no build/.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
