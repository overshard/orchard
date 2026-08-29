//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle off disk, so `bun run dev` can
// rewrite build/dist under a running server.
//
// A release build embeds it; see assets_embed.go. This is a build tag rather
// than a runtime check because //go:embed fails at compile time on a directory
// that is not there, and on a fresh clone build/ is not.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
