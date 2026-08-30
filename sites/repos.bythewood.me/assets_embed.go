//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// The release build carries the Vite bundle inside the executable, so
// ../../bin/repos.bythewood.me is the whole server. The repositories it serves
// are the one thing that stays outside it, on a volume.
//
// `all:` is required. Without it, embed skips every entry whose name starts
// with a dot, and .vite/manifest.json is one, so the server would refuse to
// start on a manifest it could not find.
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
