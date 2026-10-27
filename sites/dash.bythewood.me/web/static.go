package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// Static serves the Vite build output at /static/. Hashed files get a year since
// the name changes when the bytes do, and files copied from publicDir under
// their own names get an hour and an ETag to revalidate against.
func Static(dist fs.FS, assets *Assets) http.Handler {
	server := Tagged(dist, NoDirs(http.FileServer(http.FS(dist))))
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

// Tagged sets an ETag on each file in root with no modification time, which is
// every file in an embed.FS, and http.FileServer answers If-None-Match with it.
// A file on disk keeps Last-Modified, since it can change under a dev server.
func Tagged(root fs.FS, next http.Handler) http.Handler {
	tags := map[string]string{}
	_ = fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err != nil || !info.ModTime().IsZero() {
			return nil
		}
		b, err := fs.ReadFile(root, path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		tags[path] = `"` + hex.EncodeToString(sum[:12]) + `"`
		return nil
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, ok := tags[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			w.Header().Set("ETag", tag)
		}
		next.ServeHTTP(w, r)
	})
}
