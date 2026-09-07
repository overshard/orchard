package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sync"
)

// Cache busting for the hand written assets.
//
// The Vite sites here get content hashed filenames, so `immutable` on their
// static handler is safe. This site writes app.css and app.js by hand at fixed
// paths, and `immutable` on a fixed path means Cloudflare holds the old file for
// a year and a stylesheet change is invisible at the edge while being correct in
// the container. A hash of the bytes in the query string gives the same
// guarantee without a build step, since Cloudflare keys its cache on the whole
// url.

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
