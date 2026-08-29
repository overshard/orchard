//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// The release build carries the Vite bundle, every post PDF and every social
// card inside the executable. With content embedded unconditionally by main.go,
// ../../bin/blog.bythewood.me is the entire site: one file, nothing beside it,
// and none of the four SITE_* variables to set.
//
// Built by `make build`, which runs Vite and typst first, because the directive
// below reads these directories at compile time.
//
// `all:` is required on build/dist. Without it, embed skips every entry whose
// name starts with a dot, and .vite/manifest.json is one, so the server would
// refuse to start on a manifest it could not find.
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
