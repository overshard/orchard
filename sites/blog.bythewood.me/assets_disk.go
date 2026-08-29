//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A development build reads the Vite bundle, the post PDFs and the social
// cards off disk, so `bun run dev` can rewrite build/dist under a running
// server and `make pdfs` can regenerate one file without a recompile.
//
// The release build embeds all three; see assets_embed.go. The split is a
// build tag rather than a runtime check because the embed directive needs
// those directories to exist at compile time, and on a fresh clone none of
// them do: a single-file binary must not be the thing that makes `go build`
// fail before anyone has run Vite or typst once.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
func pdfsFS() fs.FS { return os.DirFS(dir("SITE_PDFS", "build/pdfs")) }
func ogFS() fs.FS   { return os.DirFS(dir("SITE_OG", "build/og")) }
