// Package web holds the HTTP pieces every site in this repo needs: resolving
// Vite's content-hashed filenames, request logging, security headers, and a
// server that shuts down cleanly.
package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
	"time"
)

// Assets resolves a Vite entry name to the content-hashed files Vite emitted,
// by reading dist/.vite/manifest.json.
type Assets struct {
	mu      sync.RWMutex
	entries map[string]manifestEntry

	// Set on a dev build, where Vite rewrites the bundle under a new content hash
	// every time a stylesheet is touched. The manifest is read once at boot, so
	// without this the running server keeps serving the script tag it booted with
	// and the browser gets a 404 it reports as a MIME type error, which sends you
	// looking in entirely the wrong place.
	//
	// A release build embeds the bundle and the manifest cannot change under it,
	// so this stays nil there and costs nothing.
	dist    fs.FS
	modTime time.Time
}

type manifestEntry struct {
	File    string   `json:"file"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	CSS     []string `json:"css"`
	Assets  []string `json:"assets"`
}

// LoadAssets reads the manifest out of a dist filesystem. A missing manifest is
// an error rather than a warning, because serving a page whose script tag points
// at a file that was never built is worse than refusing to start.
func LoadAssets(dist fs.FS) (*Assets, error) {
	raw, err := fs.ReadFile(dist, ".vite/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("read vite manifest: %w (did you run `make build` in frontend/?)", err)
	}

	var entries map[string]manifestEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse vite manifest: %w", err)
	}

	return &Assets{entries: entries}, nil
}

// WatchManifest makes an Assets re-read the manifest when Vite rewrites it. Only
// a dev build calls this: it is what makes `make run` survive a frontend rebuild
// without a restart.
func (a *Assets) WatchManifest(dist fs.FS) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dist = dist
	a.modTime = manifestModTime(dist)
}

func manifestModTime(dist fs.FS) time.Time {
	f, err := dist.Open(".vite/manifest.json")
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// refresh re-reads the manifest if it has been rewritten since it was last read.
// A stat per page render on a dev server, and nothing at all in production.
func (a *Assets) refresh() {
	a.mu.RLock()
	dist, was := a.dist, a.modTime
	a.mu.RUnlock()
	if dist == nil {
		return
	}

	now := manifestModTime(dist)
	if now.IsZero() || !now.After(was) {
		return
	}

	fresh, err := LoadAssets(dist)
	if err != nil {
		// A half-written manifest mid-build is not worth shouting about. The next
		// render picks up the finished one.
		return
	}

	a.mu.Lock()
	a.entries = fresh.entries
	a.modTime = now
	a.mu.Unlock()
}

func (a *Assets) lookup(entry string) (manifestEntry, bool) {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	e, ok := a.entries[entry]
	return e, ok
}

// Script returns the public URL of the JS bundle for a Vite entry, e.g.
// Script("index.js") -> "/static/base-aDClsiBF.js".
func (a *Assets) Script(entry string) string {
	e, ok := a.lookup(entry)
	if !ok {
		return ""
	}
	return "/static/" + e.File
}

// Hashed reports every file Vite emitted with a content hash in its name, which
// is the set safe to cache for a year. Files copied through from publicDir keep
// their original names and stay out of it, or an updated resume would never
// reach anybody.
func (a *Assets) Hashed() map[string]bool {
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()

	hashed := make(map[string]bool, len(a.entries)*2)
	for _, e := range a.entries {
		if e.File != "" {
			hashed[e.File] = true
		}
		for _, css := range e.CSS {
			hashed[css] = true
		}
		for _, asset := range e.Assets {
			hashed[asset] = true
		}
	}
	return hashed
}

// Styles returns the public URLs of every stylesheet Vite extracted from an
// entry. Plural because a code-split entry can pull in more than one.
func (a *Assets) Styles(entry string) []string {
	e, ok := a.lookup(entry)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(e.CSS))
	for _, css := range e.CSS {
		out = append(out, "/static/"+css)
	}
	return out
}
