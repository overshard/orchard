package main

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
)

// The home screen icon is the favicon's frog, drawn here since the game has
// no image files. It sits in the middle eight of a twelve cell grid, so a
// launcher that masks the icon to a circle doesn't cut the frog.
var iconArt = []string{
	"........",
	"........",
	".wk..kw.",
	".gggggg.",
	".gggggg.",
	".gkkkkg.",
	"gg....gg",
	"........",
}

var iconInk = map[byte]string{'g': "#56b45f", 'w': "#fff6dc", 'k': "#0a0908"}

const iconGround = "#0e2c30"

func drawIcon(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fill(img, 0, 0, size, size, rgb(iconGround))
	at := func(cell int) int { return (cell + 2) * size / 12 }
	for y, row := range iconArt {
		for x := range len(row) {
			if ink, ok := iconInk[row[x]]; ok {
				fill(img, at(x), at(y), at(x+1)-at(x), at(y+1)-at(y), rgb(ink))
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func icon(size int) http.HandlerFunc {
	b := drawIcon(size)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b)
	}
}

// The web app manifest, so the pond installs to a home screen and opens
// without the browser round it. There's no service worker, it needs the
// network like the page does.
const manifest = `{
  "name": "Pond",
  "short_name": "Pond",
  "description": "A virtual pet frog of your own.",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "orientation": "portrait",
  "background_color": "#14120f",
  "theme_color": "#0a1b1f",
  "icons": [
    {"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
    {"src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
    {"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "maskable"},
    {"src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"}
  ]
}
`

func webManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write([]byte(manifest))
}
