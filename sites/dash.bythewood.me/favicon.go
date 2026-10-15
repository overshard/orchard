package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// An inline SVG instead of a file, since a build step for two elements would
// make more than it saves. The rail's mark in phosphor amber on its warm near
// black, drawn nearly to the edges or the margin swallows it at tab size.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
	`<rect width="32" height="32" rx="7" fill="#0d0b09"/>` +
	`<path d="M5.5 26.5V5.5h21z" fill="#ffb000"/>` +
	`</svg>`

// faviconVersion is a short content hash on the icon's query string, since
// Cloudflare holds the old icon for a day and a browser caches a favicon harder
// still.
var faviconVersion = func() string {
	sum := sha256.Sum256([]byte(faviconSVG))
	return hex.EncodeToString(sum[:4])
}()

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = strings.NewReader(faviconSVG).WriteTo(w)
}
