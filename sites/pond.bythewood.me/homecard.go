package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"sync"
)

// The frog on the home page's card belongs to nobody, so the card never
// shows a visitor's.
const homeCardSeed = 20261109

// drawHomeCard is the frog card's frame with what the pond is where a frog's
// numbers go.
func drawHomeCard() *image.RGBA {
	drawing.Lock()
	defer drawing.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 1200, 630))
	fill(img, 0, 0, 1200, 630, cardBg)

	const sx, sy, sw, sh0, px = 48, 48, 576, 534, 10
	fill(img, sx-1, sy-1, sw+2, sh0+2, cardLine)
	fill(img, sx, sy, sw, sh0, cardVoid)
	for i, c := range water {
		fill(img, sx, sy+i*sh0/4, sw, sh0/4+1, c)
	}
	ox, oy := sx+(sw-spriteW*px)/2, sy+sh0-spriteH*px-70
	lilyPad(img, ox, oy, px)
	g := lookFor(homeCardSeed, nil)
	drawSprite(img, spriteCells(g, "frog", 12, 0), spriteColours(g, hat{}), ox, oy, px)
	for i := 0; i < sh0; i += 4 {
		for x := sx; x < sx+sw; x++ {
			c := img.RGBAAt(x, sy+i)
			img.SetRGBA(x, sy+i, color.RGBA{c.R * 15 / 16, c.G * 15 / 16, c.B * 15 / 16, 255})
		}
	}
	bracket(img, sx-1, sy-1, false)
	bracket(img, sx+sw, sy+sh0, true)

	x := 672
	text(img, face(fontBold, 20), cardAmber, x, 92, "POND", 4)
	fill(img, x, 112, 480, 1, cardLine)
	text(img, face(fontBold, 64), cardText, x, 230, "A frog of", 0)
	text(img, face(fontBold, 64), cardAmber, x, 304, "your own", 0)
	text(img, face(fontRegular, 22), cardDim, x, 384, "Raise it, find it hats, and take", 0)
	text(img, face(fontRegular, 22), cardDim, x, 416, "it diving for pebbles.", 0)
	text(img, face(fontRegular, 18), cardAmberLo, x, 504, "NO ACCOUNT, JUST SIX WORDS", 3)
	fill(img, x, 548, 480, 1, cardLine)
	text(img, face(fontRegular, 18), cardDim, x, 580, "pond.bythewood.me", 0)
	return img
}

var homeCardPNG = sync.OnceValues(func() ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, drawHomeCard())
	return buf.Bytes(), err
})

func homeCard(w http.ResponseWriter, r *http.Request) {
	b, err := homeCardPNG()
	if err != nil {
		slog.Error("pond", slog.String("path", r.URL.Path), slog.Any("err", err))
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "no card", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(b)
}
