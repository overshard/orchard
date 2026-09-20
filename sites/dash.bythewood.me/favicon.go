package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// An inline SVG rather than a file, since it is two elements and a build step
// for it would generate more than it saves.
//
// The mark from the rail, set solid in the page's phosphor amber on the warm
// near black it sits on, so the tab and the wordmark are the same shape.
//
// The triangle is drawn nearly to the edges, since anything smaller leaves a
// dark margin that swallows the shape at tab size.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
	`<rect width="32" height="32" rx="7" fill="#0d0b09"/>` +
	`<path d="M5.5 26.5V5.5h21z" fill="#ffb000"/>` +
	`</svg>`

// faviconVersion is a short content hash, and the page links the icon with it on
// the query string. Cloudflare holds the previous icon for its full day after it
// changes and a browser caches a favicon harder still, so versioning the URL is
// what makes a change to the constant above arrive.
var faviconVersion = func() string {
	sum := sha256.Sum256([]byte(faviconSVG))
	return hex.EncodeToString(sum[:4])
}()

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = strings.NewReader(faviconSVG).WriteTo(w)
}
