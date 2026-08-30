//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// A release build is one file with no SITE_* variables to set. `make build` runs
// Vite and typst first, since the directives below read those directories at
// compile time; `all:` is needed or embed skips .vite/manifest.json.
//
//go:embed all:build/dist
//go:embed build/pdfs
//go:embed build/og
var buildFS embed.FS

func sub(dir string) fs.FS {
	f, err := fs.Sub(buildFS, dir)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	return f
}

func distFS() fs.FS { return sub("build/dist") }
func pdfsFS() fs.FS { return sub("build/pdfs") }
func ogFS() fs.FS   { return sub("build/og") }
