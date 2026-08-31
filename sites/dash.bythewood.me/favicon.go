package main

import (
	"net/http"
	"strings"
)

// An inline SVG rather than a file, since it is four elements and a build step
// for it would generate more than it saves. The shape is a sparkline, which is
// what the page is mostly made of.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
	`<rect width="32" height="32" rx="7" fill="#0d1117"/>` +
	`<path d="M5 22 L11 15 L16 19 L21 9 L27 13" fill="none" stroke="#3fb950" ` +
	`stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>` +
	`</svg>`

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = strings.NewReader(faviconSVG).WriteTo(w)
}
