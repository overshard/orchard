package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// Static serves the Vite build output at /static/. Hashed files get a year since
// the name changes when the bytes do, and files copied from publicDir under
// their own names get an hour.
func Static(dist fs.FS, assets *Assets) http.Handler {
	server := NoDirs(http.FileServer(http.FS(dist)))
	hashed := assets.Hashed()

	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")

		// Server-side only, a browser has no use for it.
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
