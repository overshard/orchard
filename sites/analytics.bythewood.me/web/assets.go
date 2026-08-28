// Package web holds the pieces every site in this repo needs: resolving
// Vite's content-hashed filenames, request logging, security headers, and a
// server that shuts down cleanly.
//
// It exists because the Rust era shipped a near-identical render.rs,
// middleware.rs and templates.rs in five separate repositories, and every fix
// had to be made five times. One repo means one copy.
package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

// Assets resolves a Vite entry name to the content-hashed files Vite actually
// emitted, by reading dist/.vite/manifest.json.
//
// Hashed filenames are the whole reason /static/* can be cached immutably at
// the Cloudflare edge: the name changes when the bytes change, so nothing ever
// needs invalidating. The tradeoff is that templates cannot hardcode a
// filename, which is what this type is for.
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

// LoadAssets reads the manifest out of a dist filesystem. It is an error for
// the manifest to be missing: serving a page whose script tag points at a file
// that was never built is worse than refusing to start.
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

// Hashed reports every file Vite emitted with a content hash in its name.
//
// This is what makes immutable caching safe to apply automatically rather than
// from a hand-maintained list. Files copied through from publicDir (images,
// the resume PDF, favicons) keep their original names, so caching one of those
// for a year would mean an updated resume never reaching anybody. Reading the
// set from the manifest means a new public file added later cannot quietly
// inherit the wrong policy.
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
// entry. Plural because a code-split entry can pull in more than one, even
// though these sites currently emit exactly one.
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
