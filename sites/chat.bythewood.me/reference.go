// Pictures a turn starts from: one he attached, or the last one in the
// conversation. The chat model cannot see any of them, so it only ever writes
// what should change, and the image model is handed the picture itself.
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// FLUX.2 reads a reference at up to about a megapixel and scales anything
// bigger down itself, so sending more is bytes nobody looks at.
const referencePixels = 1024 * 1024

// asReference decodes any picture a browser is likely to hand over and gives
// it back as a PNG no bigger than the model reads. sd-server cannot open webp,
// which is what most product pages serve.
func asReference(raw []byte) ([]byte, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("could not read the picture: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 16 || h < 16 {
		return nil, 0, 0, fmt.Errorf("the picture is too small to start from")
	}
	if w*h > referencePixels {
		s := math.Sqrt(float64(referencePixels) / float64(w*h))
		w, h = int(float64(w)*s), int(float64(h)*s)
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), w, h, nil
}

func pngSize(b []byte) (image.Config, error) {
	return png.DecodeConfig(bytes.NewReader(b))
}

// cropTo trims a picture drawn larger than asked back to the size asked for,
// since sd-server rounds each side up to a multiple of 16 and 1080 is not one.
func cropTo(b []byte, w, h int) []byte {
	cfg, err := pngSize(b)
	if err != nil || cfg.Width < w || cfg.Height < h || (cfg.Width == w && cfg.Height == h) {
		return b
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return b
	}
	x, y := (cfg.Width-w)/2, (cfg.Height-h)/2
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return b
	}
	var out bytes.Buffer
	if png.Encode(&out, sub.SubImage(image.Rect(x, y, x+w, y+h))) != nil {
		return b
	}
	return out.Bytes()
}

// An edit is drawn at two megapixels. At one the fabric on a product shot came
// back smeared, and the card manages two in about forty seconds.
const editPixels = 2 * 1024 * 1024

// sizeLike is a canvas of the given area in the shape w by h, so a wide
// product shot does not come back square.
func sizeLike(w, h, pixels int) (int, int) {
	aspect := float64(w) / float64(h)
	aspect = math.Max(1.0/3, math.Min(3, aspect))
	ch := math.Sqrt(float64(pixels) / aspect)
	return round16(ch * aspect), round16(ch)
}

func round16(v float64) int {
	return max(256, int(math.Round(v/16))*16)
}

// Starting points for this turn, carried on the context like incognito is,
// since the engine and the tools both need them and neither owns the turn.
type references struct {
	Attached []string
	Last     string
	// What he typed, without the note about the file wrapped round it.
	Message string
	// The newest picture he attached in this conversation, and what he said
	// with it, which is where a new scene starts from.
	Origin     string
	OriginSaid string
}

type referencesKey struct{}

func withReferences(ctx context.Context, r references) context.Context {
	return context.WithValue(ctx, referencesKey{}, r)
}

func referencesOf(ctx context.Context) references {
	r, _ := ctx.Value(referencesKey{}).(references)
	return r
}

// lastPicture is the newest picture in a stored conversation, drawn or
// attached, which is what "make it night" means.
func lastPicture(stored []Stored) string {
	for i := len(stored) - 1; i >= 0; i-- {
		m := stored[i]
		for j := len(m.Widgets) - 1; j >= 0; j-- {
			if m.Widgets[j].Kind == "image" && m.Widgets[j].Image != "" {
				return m.Widgets[j].Image
			}
		}
		for j := len(m.Files) - 1; j >= 0; j-- {
			if m.Files[j].Image != "" {
				return m.Files[j].Image
			}
		}
	}
	return ""
}

func lastAttached(stored []Stored) (string, string) {
	for i := len(stored) - 1; i >= 0; i-- {
		m := stored[i]
		for j := len(m.Files) - 1; j >= 0; j-- {
			if m.Files[j].Image != "" {
				return m.Files[j].Image, m.Display
			}
		}
	}
	return "", ""
}
