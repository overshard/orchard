//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// The release build carries the Vite bundle inside the executable, so what
// gets deployed is one file with nothing beside it and no SITE_DIST to set.
// Built with `make build`, which passes -tags embed after Vite has run.
//
// `all:` is load bearing. Without it embed skips every entry whose name starts
// with a dot, and .vite/manifest.json is exactly that, so the server would
// refuse to start on a manifest it could not find: a silent omission that only
// shows up at runtime.
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
