//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// `all:` is required: without it embed skips every entry whose name starts with
// a dot, and .vite/manifest.json is one.
//
//go:embed all:build/dist
var buildFS embed.FS

func distFS() fs.FS {
	sub, err := fs.Sub(buildFS, "build/dist")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	return sub
}
