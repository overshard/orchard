//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle off disk, so `bun run dev` can
// rewrite build/dist under a running server and a CSS change costs a browser
// reload rather than a restart.
//
// The release build embeds it instead; see assets_embed.go. The split is a
// build tag rather than a runtime check because the embed directive needs the
// directory to exist at compile time, and on a fresh clone it does not: a
// single-file binary must not be the thing that makes `go build` fail before
// anyone has run Vite once.
func distFS() fs.FS {
	if d := os.Getenv("SITE_DIST"); d != "" {
		return os.DirFS(d)
	}
	return os.DirFS("build/dist")
}
