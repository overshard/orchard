// Package web holds the HTTP pieces every site in this repo needs: resolving
// Vite's content-hashed filenames, request logging, security headers, and a
// server that shuts down cleanly.
package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

// Assets resolves a Vite entry name to the content-hashed files Vite emitted,
// by reading dist/.vite/manifest.json.
type Assets struct {
	entries map[string]manifestEntry
	once    sync.Once
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

// Script returns the public URL of the JS bundle for a Vite entry, e.g.
// Script("index.js") -> "/static/base-aDClsiBF.js".
func (a *Assets) Script(entry string) string {
	e, ok := a.entries[entry]
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
	e, ok := a.entries[entry]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(e.CSS))
	for _, css := range e.CSS {
		out = append(out, "/static/"+css)
	}
	return out
}
