package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sync"
)

// Cache busting for the hand written assets.
//
// This site writes app.css and app.js at fixed paths rather than the content
// hashed names Vite gives the other sites, so `immutable` would have Cloudflare
// hold the old file for a year. A hash of the bytes in the query string gives
// the same guarantee without a build step, since Cloudflare keys on the url.

var (
	assetOnce sync.Once
	assetVers map[string]string
)

// assetURL is the template function. A missing file falls through to the bare
// path rather than failing the render, since a broken stylesheet link is easier
// to read than a blank page.
func assetURL(path string) string {
	assetOnce.Do(loadAssetVersions)
	if v, ok := assetVers[path]; ok {
		return "/" + path + "?v=" + v
	}
	return "/" + path
}

func loadAssetVersions() {
	assetVers = map[string]string{}
	_ = fs.WalkDir(assets(), "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(assets(), p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		assetVers[p] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
}

// resetAssetVersions is what the development reload path calls, so editing CSS
// off disk still changes the url rather than serving the version read at boot.
func resetAssetVersions() {
	assetOnce = sync.Once{}
}
