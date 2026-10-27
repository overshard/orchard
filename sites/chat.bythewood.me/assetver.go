package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sync"
)

// app.css and app.js sit at fixed paths instead of content hashed names, so
// `immutable` would have Cloudflare hold the old file for a year. A hash of the
// bytes in the query string fixes that, since Cloudflare keys on the url.

var (
	assetOnce sync.Once
	assetVers map[string]string
)

// A missing file falls through to the bare path instead of failing the render,
// since a broken stylesheet link is easier to read than a blank page.
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

// The development reload path calls this so a CSS edit off disk changes the url
// instead of serving the version hashed at boot.
func resetAssetVersions() {
	assetOnce = sync.Once{}
}
