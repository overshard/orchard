//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// `all:` is required: without it embed skips dotted entries, and
// .vite/manifest.json is one, so the server would refuse to start.
//
//go:embed all:build/dist
var buildFS embed.FS

func distFS() fs.FS {
	f, err := fs.Sub(buildFS, "build/dist")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	return f
}
