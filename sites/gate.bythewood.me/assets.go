package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"

	"gate.bythewood.me/web"
)

//go:embed static
var staticFS embed.FS

// assetVers hashes each file so its url changes with it, since Cloudflare keys
// on the url and the files are served immutable.
var assetVers = func() map[string]string {
	out := map[string]string{}
	_ = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := fs.ReadFile(staticFS, p)
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
	return out
}()

func assetURL(path string) string {
	if v, ok := assetVers[path]; ok {
		return "/" + path + "?v=" + v
	}
	return "/" + path
}

func static() http.Handler {
	files := web.NoDirs(http.FileServerFS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}
