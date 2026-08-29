//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// The release build carries the Vite bundle inside the executable, so there is
// no /dist in the image and no SITE_DIST to set. Built by `make build`, which
// runs Vite first because the directive below reads build/dist at compile time.
//
// Unlike the two FROM scratch sites this does not make the image a single
// file: status execs the lighthouse CLI and typst, so bun, chromium, typst and
// node_modules still have to be on disk beside it. What it does remove is the
// half that was only ever there because Go was not carrying it.
//
// `all:` is load bearing. Without it embed skips every entry whose name starts
// with a dot, and .vite/manifest.json is exactly that, so the server would
// refuse to start on a manifest it could not find.
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
