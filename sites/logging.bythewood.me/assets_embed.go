//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// The release build carries the Vite bundle inside the executable, so the image
// has no /dist and no SITE_DIST to set. It is still not a single-file image:
// this site execs typst on the request path, so typst and its fonts have to be
// on disk beside it.
//
// `all:` is required. Without it, embed skips every entry whose name starts
// with a dot, and .vite/manifest.json is one, so the server would refuse to
// start on a manifest it could not find.
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
