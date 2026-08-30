//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// A dev build reads assets off disk so Vite and `make pdfs` can rewrite them
// under a running server. It is a build tag and not a runtime check because
// //go:embed needs these directories at compile time, and a fresh clone has none.
func distFS() fs.FS { return os.DirFS(dir("SITE_DIST", "build/dist")) }
func pdfsFS() fs.FS { return os.DirFS(dir("SITE_PDFS", "build/pdfs")) }
func ogFS() fs.FS   { return os.DirFS(dir("SITE_OG", "build/og")) }
