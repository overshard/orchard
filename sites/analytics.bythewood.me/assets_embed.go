//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// `all:` is required on build/dist since embed skips dot-prefixed entries
// without it, and the server won't start without .vite/manifest.json.
//
//go:embed all:build/dist
//go:embed build/static_maps
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
func mapsFS() fs.FS { return sub("build/static_maps") }
