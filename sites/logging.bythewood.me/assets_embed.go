//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// `all:` is required since embed otherwise skips dot-prefixed entries like
// .vite/manifest.json, and the server would refuse to start without it.
//
//go:embed all:build/dist
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
