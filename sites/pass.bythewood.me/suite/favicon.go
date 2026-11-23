package suite

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
)

// iconSum covers every tool's icon, so suite-head can version the link
// without knowing which tool it's on.
var iconSum = func() string {
	h := sha256.New()
	names, _ := fs.Glob(embedded, "files/icon-*.svg")
	for _, name := range names {
		b, _ := fs.ReadFile(embedded, name)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)[:6])
}()

// FaviconURL is what suite-head links the icon by.
func FaviconURL() string { return "/favicon.svg?v=" + iconSum }

// Favicon serves a tool's icon, mounted at /favicon.svg and /favicon.ico
// since feed readers and some browsers ask for the .ico without reading the page.
func Favicon(tool string) http.Handler {
	b, err := fs.ReadFile(embedded, "files/icon-"+tool+".svg")
	if err != nil {
		panic("suite: no icon for " + tool)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		if r.URL.Query().Get("v") == iconSum {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		_, _ = w.Write(b)
	})
}
