package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// Static serves the Vite build output at /static/.
//
// Content-hashed files are cached for a year and never revalidated: the name
// changes when the bytes change, so nothing needs invalidating, and the origin
// serves each one once per Cloudflare colo. Everything else gets an hour, which
// covers files copied through from publicDir that keep their original names,
// where a year would mean an updated resume PDF never reaching anyone.
func Static(dist fs.FS, assets *Assets) http.Handler {
	server := http.FileServer(http.FS(dist))
	hashed := assets.Hashed()

	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")

		// The manifest is how the server resolves entry names. A browser
		// has no use for it.
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
