//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// distFS reads the Vite bundle off disk so `bun run dev` can rewrite it under a
// running server. A release build embeds it instead; see assets_embed.go.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
