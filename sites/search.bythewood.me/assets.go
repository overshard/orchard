package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

// Static assets are served under a URL carrying a hash of their contents.
//
// Without it a deploy ships a new stylesheet and script that browsers do not
// fetch, because the embedded filesystem has zero modification times, so
// http.FileServer sends no Last-Modified and no ETag and a browser is free to
// reuse what it has. That is exactly what happened: the try buttons were live
// and did nothing, because the page was running the previous app.js.
//
// The other sites solve this with Vite's content-hashed filenames. This one has
// no build step, so the hash goes in a query parameter and the answer is marked
// immutable, which is safe precisely because the URL changes when the bytes do.
type Assets struct {
	fsys fs.FS
	mu   sync.RWMutex
	tags map[string]string
}

func NewAssets(fsys fs.FS) *Assets {
	return &Assets{fsys: fsys, tags: map[string]string{}}
}

// URL returns the versioned path for an asset, for a template to write out.
func (a *Assets) URL(name string) string {
	clean := strings.TrimPrefix(name, "/")

	a.mu.RLock()
	tag, ok := a.tags[clean]
	a.mu.RUnlock()

	if !ok || Reloaded {
		// In development the file is re-read every time, so an edit shows on
		// reload rather than at the next restart.
		tag = a.hash(clean)
		a.mu.Lock()
		a.tags[clean] = tag
		a.mu.Unlock()
	}
	if tag == "" {
		return "/" + clean
	}
	return "/" + clean + "?v=" + tag
}

func (a *Assets) hash(name string) string {
	b, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// Handler serves the files. A request carrying a version is cached hard, since
// its URL cannot outlive its contents. One without is not cached at all, which
// covers anything linked by hand.
func (a *Assets) Handler() http.Handler {
	files := http.FileServer(http.FS(a.fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" && !Reloaded {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		// Content types the standard table gets wrong or leaves off.
		switch path.Ext(r.URL.Path) {
		case ".js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		files.ServeHTTP(w, r)
	})
}
