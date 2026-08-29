//go:build embed

package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

// The release build carries the Vite bundle and the ~10MB of per-country
// topojson inside the executable, so the image has no /dist, no /static_maps,
// and neither SITE_DIST nor SITE_MAPS to set. It is still not a single-file
// image: analytics execs typst on the request path, so typst and its fonts have
// to be on disk beside it.
//
// `all:` is required on build/dist. Without it, embed skips every entry whose
// name starts with a dot, and .vite/manifest.json is one, so the server would
// refuse to start on a manifest it could not find.
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
