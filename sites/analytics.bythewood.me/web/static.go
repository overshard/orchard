package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// Static serves the Vite build output at /static/.
//
// Files Vite content-hashed are cached for a year and never revalidated, which
// is what makes home-hosted bandwidth a non-issue: the origin serves each one
// once per Cloudflare colo and then stops being asked. A hashed name changes
// when the bytes change, so nothing ever needs invalidating.
//
// Everything else gets an hour. That covers files copied through from
// publicDir, which keep their original names: caching an unhashed resume PDF
// for a year would mean an update never reaching anyone. The set comes from
// the manifest rather than a hand-maintained list, so a public file added
// later cannot inherit the wrong policy by omission.
func Static(dist fs.FS, assets *Assets) http.Handler {
	server := http.FileServer(http.FS(dist))
	hashed := assets.Hashed()

	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")

		// .vite/manifest.json maps entry names to hashed files. It is how the
		// server resolves them, not something a browser has any use for.
		if strings.HasPrefix(clean, ".vite/") {
			http.NotFound(w, r)
			return
		}

		if hashed[clean] {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		server.ServeHTTP(w, r)
	}))
}
