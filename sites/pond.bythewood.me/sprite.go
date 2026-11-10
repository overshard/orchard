package main

import (
	"image"
	"image/color"
	"math"
)

// This is the drawing half of frontend/src/frog.js, for the picture a share
// link unfurls with. It keeps that file's names and its order of draws, and a
// fixture test holds the two together.

const (
	spriteW = 44
	spriteH = 34
	ground  = 30
)

var morphEye = map[string]string{
	"GREEN": "#e8c552", "OLIVE": "#d9a441", "WOOD BROWN": "#d6a34a", "GREY": "#cfc7a8",
	"GOLDEN": "#fff3d0", "RED EYE": "#e2412d", "STRAWBERRY": "#f3e4c2", "BLUE": "#e8c552", "ALBINO": "#e87f86",
}

var morphLimb = map[string]float64{"RED EYE": 24, "STRAWBERRY": 222}

type spot struct{ x, y, r float64 }

// look is everything about a frog the drawing needs and the genome doesn't.
type look struct {
	morph                                 morph
	hue, sat, lit, bw, bh, eye, gap, freq float64
	phase                                 float64
	pupil, pattern                        string
	pseed                                 int
	spots                                 []spot
}

func lookOf(seed uint32) look {
	r := mulberry32(seed)
	roll, acc := r()*1000, 0
	m := morphs[0]
	for _, c := range morphs {
		acc += c.W
		if roll < float64(acc) {
			m = c
			break
		}
	}
	span := func(a [2]float64) float64 { return a[0] + float64(r()*(a[1]-a[0])) }
	l := look{morph: m}
	l.hue, l.sat, l.lit = span(m.H), span(m.S), span(m.L)
	l.bw = 11 + float64(r()*3.5)
	l.bh = 7.2 + float64(r()*2.6)
	l.eye = 2.6 + float64(r()*1.5)
	l.gap = 0.5 + float64(r()*0.2)
	l.pupil = pick(r, pupils)
	if m.Mask && r() < 0.6 {
		l.pattern = "MASK"
	} else {
		l.pattern = pick(r, patterns)
	}
	l.freq = 0.55 + float64(r()*0.5)
	l.phase = r() * 6.28
	// Nature, pitch twice, rate and notes, which only the call uses.
	for range 5 {
		r()
	}
	l.pseed = int(r() * 9999)
	r()
	r()
	n := 4 + int(r()*6)
	for range n {
		var s spot
		s.x = (r()*2 - 1) * 0.85
		s.y = float64((r()*2-1)*0.9) - 0.2
		s.r = 1 + float64(r()*1.6)
		l.spots = append(l.spots, s)
	}
	return l
}

func hash2(x, y, s int) float64 {
	sum := int64(x)*374761393 + int64(y)*668265263 + int64(s)*974711
	n := uint32(sum) * 1274126177
	n ^= n >> 15
	return float64(n%1000) / 1000
}

func ell(x, y, cx, cy, rx, ry float64) bool {
	a, b := (x-cx)/rx, (y-cy)/ry
	return a*a+b*b <= 1
}

// hatCell is frog.js's hatCell, hx across from the middle of the head and hy
// up from the crown. 10 is the hat's main colour and 11 its trim.
func hatCell(shape int, hx, hy float64) uint8 {
	ax := math.Abs(hx)
	var v uint8
	switch shape {
	case 1:
		if hy > 0 && hy < 1.2 && ax < 5 {
			v = 10
		} else if hy >= 1.2 && hy < 6.5 && ax < 3 {
			v = 10
			if hy < 2.4 {
				v = 11
			}
		}
	case 2:
		if hy > 0 && hy < 7 && ax < (7-hy)*0.5 {
			v = 10
			if int(math.Floor(hy))%3 == 1 {
				v = 11
			}
		}
	case 3:
		if hy > 0 && hy < 2 && ax < 4 {
			v = 10
		} else if hy >= 2 && hy < 4 && ax < 4 && int(math.Floor(ax))%3 != 1 {
			v = 10
		}
		if v != 0 && hy < 1 && ax < 0.6 {
			v = 11
		}
	case 4:
		if ell(hx, hy, 0.5, 1.6, 6, 1.9) {
			v = 10
			if math.Abs(hy-1.6) < 0.5 {
				v = 11
			}
		}
	case 5:
		if hy > 0 && ell(hx, hy, 0, 0, 4.7, 4.6) {
			v = 10
			if hy < 1.3 {
				v = 11
			}
		}
		if ell(hx, hy, 0, 5.4, 1.5, 1.5) {
			v = 11
		}
	case 6:
		if ell(hx, hy, 2.3, 2.8, 1.4, 1.4) || ell(hx, hy, -2.3, 2.8, 1.4, 1.4) || ell(hx, hy, 0, 5.1, 1.4, 1.4) || ell(hx, hy, 0, 0.6, 1.4, 1.4) {
			v = 10
		}
		if ell(hx, hy, 0, 2.8, 1.3, 1.3) {
			v = 11
		}
	case 7:
		if hy > 0 && hy < 1.2 && ax < 7.6 {
			v = 10
		} else if hy >= 1.2 && hy < 2.3 && ax > 6 && ax < 7.6 {
			v = 10
		} else if hy >= 1.2 && hy < 5.2 && ax < 3.4 && !(hy > 4.3 && ax < 0.9) {
			v = 10
			if hy < 2.3 {
				v = 11
			}
		}
	case 8:
		if hy > 0 && hy < 1.2 && ax < 6 {
			v = 10
		} else if hy >= 1.2 && hy < 8.6 && ax < (8.8-hy)*0.48 {
			v = 10
			if hy < 2.4 || (math.Abs(hx+1-math.Mod(hy, 3)) < 0.6 && hy > 3.2) {
				v = 11
			}
		}
	case 9:
		if hy > 0 && hy < 1 && ax < 2 {
			v = 11
		}
		if hy >= 0.9 && ell(hx, hy, 0, 1, 6.6, 4.4) {
			v = 10
			if ell(hx, hy, -3, 2.8, 1.2, 1.1) || ell(hx, hy, 2.6, 3.4, 1.2, 1.1) || ell(hx, hy, 0, 4.4, 0.9, 0.8) {
				v = 11
			}
		}
	case 10:
		if hy > 0 && ell(hx, hy, 0, 0, 4.7, 4.1) {
			v = 10
			if hy < 1.3 {
				v = 11
			}
		}
		if ell(ax, hy, 5.5, 2.4, 1.3, 1.5) || ell(ax, hy, 6.5, 4.5, 1, 1.5) {
			v = 11
		}
	case 11:
		if hy > 0 && hy < 2.6 && ax < 3.6 {
			v = 10
			if hy > 2 {
				v = 11
			}
		}
		if ell(hx, hy, 0, 4.7, 5.2, 2.6) || ell(ax, hy, 3, 5.6, 2.4, 2) {
			v = 10
		}
	case 12:
		if hy > 0 && ell(hx, hy, 0, 0, 4.5, 3.7) {
			v = 10
			if int(math.Floor((hx+6.6)/2.2))%2 != 0 {
				v = 11
			}
		}
		if hy >= 3.6 && hy < 5 && ax < 0.6 {
			v = 11
		}
		if hy >= 5 && hy < 6 && ax < 4.2 {
			v = 10
		}
	case 13:
		if ell(hx, hy, 0, 4.4, 5.2, 1.6) && !ell(hx, hy, 0, 4.4, 3.4, 0.6) {
			v = 10
			if hy > 4.6 {
				v = 11
			}
		}
	case 14:
		if hy > 0.4 && hy < 4.8 && ax < 5.6 && math.Abs(hy-2.6) < ax*0.5+0.3 {
			v = 10
		}
		if ax < 1.1 && math.Abs(hy-2.6) < 1.1 {
			v = 11
		}
	case 15:
		if hy > 0 && hy < 1.1 && ax < 8.2 {
			v = 10
		}
		if hy >= 1 && ell(hx, hy, 0, 1, 4.2, 3.4) {
			v = 10
			if hy < 2.1 {
				v = 11
			}
		}
	}
	return v
}

// frogCells is a still frog, no blink, breath or croak. Cell classes are
// frog.js's: 1 body, 2 belly, 3 pattern, 4 eye, 5 pupil, 6 dark, 7 sac, 8 limb,
// 9 outline, 10 and 11 hat.
func frogCells(g look, scale float64, tail bool, hatShape int) []uint8 {
	c := make([]uint8, spriteW*spriteH)
	cx, s := float64(spriteW)/2, scale
	bw, bh := g.bw*s, g.bh*s
	by := ground - bh - 0.6
	er, eg := math.Max(1.6, g.eye*s), bw*g.gap
	ey := by - bh*0.98
	for py := range spriteH {
		for px := range spriteW {
			x, y := float64(px)+0.5, float64(py)+0.5
			dx := math.Abs(x - cx)
			var v uint8
			if ell(dx, y, bw*0.92, by+bh*0.42, bw*0.42, bh*0.66) {
				v = 8
			}
			if ell(dx, y, bw*1.12, ground-0.7, bw*0.4, 1.3) {
				v = 8
			}
			if tail && ell(x, y, cx+bw+3.5, ground-1.4, 4.2, 1.2) {
				v = 1
			}
			onBody := ell(x, y, cx, by, bw, bh) || ell(x, y, cx, by-bh*0.5, bw*0.82, bh*0.72)
			if onBody {
				v = 1
			}
			if ell(dx, y, eg, ey, er+1.2, er+1.2) {
				v, onBody = 1, true
			}
			if onBody {
				p := false
				switch g.pattern {
				case "SPOTS":
					for _, sp := range g.spots {
						ddx, ddy := x-(cx+sp.x*bw), y-(by+sp.y*bh*0.7)
						if ddx*ddx+ddy*ddy < sp.r*sp.r*s*s {
							p = true
							break
						}
					}
				case "BANDS":
					p = math.Sin((x-cx)*g.freq+g.phase) > 0.5 && y < by+bh*0.3
				case "SPECKLED":
					p = hash2(px, py, g.pseed) < 0.17
				case "MASK":
					p = y > ey+er*0.3 && y < ey+er*0.3+2.2*s && dx > eg*0.35
				case "BACK STRIPE":
					p = dx < 1.1*s && y < by+bh*0.1
				}
				if p {
					v = 3
				}
			}
			if ell(x, y, cx, by+bh*0.48, bw*0.6, bh*0.5) {
				v = 2
			}
			if ell(dx, y, bw*0.44, by+bh*0.62, 1.3*s+0.3, bh*0.42) {
				v = 8
			}
			if ell(dx, y, bw*0.46, ground-0.7, 2.7*s, 1.3) {
				v = 8
			}
			if dx < bw*0.6 {
				my := by - bh*0.34 - 1.3*s*math.Pow(dx/(bw*0.6), 2)
				if math.Abs(y-my) < 0.55 {
					v = 6
				}
			}
			if s > 0.8 && math.Abs(dx-1.5) < 0.6 && math.Abs(y-(by-bh*0.72)) < 0.6 {
				v = 6
			}
			if ell(dx, y, eg, ey, er, er) {
				v = 4
				rx, ry := er*0.52, er*0.52
				switch g.pupil {
				case "BAR":
					rx, ry = er*0.82, er*0.36
				case "SLIT":
					rx, ry = er*0.34, er*0.82
				}
				if ell(dx, y, eg, ey, rx, ry) {
					v = 5
				}
			}
			if hatShape != 0 {
				if h := hatCell(hatShape, x-cx, by-bh*1.2-y); h != 0 {
					v = h
				}
			}
			c[py*spriteW+px] = v
		}
	}
	return c
}

func tadCells(legs bool, hatShape int) []uint8 {
	c := make([]uint8, spriteW*spriteH)
	const cx, cy = 16.0, 20.0
	for py := range spriteH {
		for px := range spriteW {
			x, y := float64(px)+0.5, float64(py)+0.5
			var v uint8
			if x > cx+3 && x < cx+24 {
				u := (x - cx - 3) / 21
				ty := cy + math.Sin(x*0.55)*2.2*u
				if math.Abs(y-ty) < 3.2*(1-u)+0.5 {
					v = 8
				}
			}
			if ell(x, y, cx, cy, 7, 5) {
				v = 1
			}
			if ell(x, y, cx, cy+2, 4.5, 2.4) {
				v = 2
			}
			if legs && ell(x, y, cx+4, cy+5.2, 2.2, 1.1) {
				v = 8
			}
			if ell(x, y, cx-3.5, cy-1.5, 1.8, 1.8) {
				v = 4
			}
			if ell(x, y, cx-3.8, cy-1.5, 0.9, 0.9) {
				v = 5
			}
			if hatShape != 0 {
				if h := hatCell(hatShape, x-cx+1, cy-4.4-y); h != 0 {
					v = h
				}
			}
			c[py*spriteW+px] = v
		}
	}
	return c
}

func eggCells(hatShape int) []uint8 {
	c := make([]uint8, spriteW*spriteH)
	const cx, cy = float64(spriteW) / 2, 19.0
	for py := range spriteH {
		for px := range spriteW {
			x, y := float64(px)+0.5, float64(py)+0.5
			var v uint8
			d := math.Sqrt((x-cx)*(x-cx) + (y-cy)*(y-cy))
			if d < 10 {
				v = 7
			}
			if d < 10 && d > 8.8 {
				v = 8
			}
			if ell(x, y, cx, cy+0.5, 4.2, 3.6) {
				v = 1
			}
			if ell(x, y, cx-1.2, cy-0.6, 1, 1) {
				v = 5
			}
			if hatShape != 0 {
				if h := hatCell(hatShape, x-cx, cy-9.4-y); h != 0 {
					v = h
				}
			}
			c[py*spriteW+px] = v
		}
	}
	return c
}

// spriteCells is frog.js's cells for a frog sitting still.
func spriteCells(g look, stage string, day int, hatShape int) []uint8 {
	switch stage {
	case "egg":
		return eggCells(hatShape)
	case "tadpole":
		return tadCells(day >= 6, hatShape)
	case "froglet":
		return frogCells(g, 0.62, true, hatShape)
	case "elder":
		return frogCells(g, 1.06, false, hatShape)
	}
	return frogCells(g, 0.94, false, hatShape)
}

var (
	hatMain = [][2]string{{}, {"#1b1a22", "#b3222f"}, {"#e8559a", "#ffe27a"}, {"#f2c230", "#d8402f"}, {"#4f9a4a", "#2f6b33"}, {"#d8402f", "#f3e4c2"}, {"#f3e4c2", "#f2c230"}, {"#8a5a2b", "#3a2412"}, {"#4b3fa8", "#ffe27a"}, {"#d8402f", "#fff6dc"}, {"#9aa0a6", "#f3e4c2"}, {"#fff6dc", "#c9ccd0"}, {"#3d8fd6", "#f2c230"}, {"#ffe27a", "#fff6dc"}, {"#e8559a", "#b3306d"}, {"#e3c57a", "#d8402f"}}
	hatWays = [][2]string{{}, {"#4f9a4a", "#cfe8a0"}, {"#b03a8c", "#ffd0ea"}, {"#f2c230", "#fff6dc"}}
)

func rgb(s string) color.RGBA {
	var c [3]uint8
	for i := range c {
		hi, lo := s[1+2*i], s[2+2*i]
		c[i] = nib(hi)<<4 | nib(lo)
	}
	return color.RGBA{c[0], c[1], c[2], 255}
}

func nib(b byte) uint8 {
	if b >= 'a' {
		return b - 'a' + 10
	}
	return b - '0'
}

// hsl rounds the way the page's toFixed(0) does and converts the way CSS does.
func hsl(h, s, l float64) color.RGBA {
	h = math.Round(h)
	s = math.Round(math.Max(0, math.Min(100, s))) / 100
	l = math.Round(math.Max(0, math.Min(100, l))) / 100
	h = math.Mod(math.Mod(h, 360)+360, 360)
	a := s * math.Min(l, 1-l)
	f := func(n float64) uint8 {
		k := math.Mod(n+h/30, 12)
		return uint8(math.Round(255 * (l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1)))))
	}
	return color.RGBA{f(0), f(8), f(4), 255}
}

func spriteColours(g look, h hat) map[uint8]color.RGBA {
	col := map[uint8]color.RGBA{
		1: hsl(g.hue, g.sat, g.lit), 2: hsl(g.hue+18, g.sat*0.45, math.Min(92, g.lit+30)),
		3: hsl(g.hue-8, g.sat, g.lit-17), 4: rgb(morphEye[g.morph.Name]), 5: rgb("#0a0908"),
		6: hsl(g.hue, g.sat*0.6, 12), 7: hsl(g.hue+24, g.sat*0.4, math.Min(94, g.lit+38)),
		8: hsl(g.hue, g.sat, g.lit-8), 9: hsl(g.hue, g.sat*0.5, 9),
	}
	if limb, ok := morphLimb[g.morph.Name]; ok {
		col[8] = hsl(limb, 70, 50)
	}
	if g.morph.Name == "ALBINO" {
		col[5] = rgb("#b3222f")
	}
	if h.Shape != 0 {
		pair := hatMain[h.Shape]
		if h.Colour != 0 {
			pair = hatWays[h.Colour]
		}
		col[10], col[11] = rgb(pair[0]), rgb(pair[1])
	}
	return col
}

// drawSprite paints cells at px pixels a cell with a one cell outline round
// whatever is filled, the same as the page's draw.
func drawSprite(img *image.RGBA, c []uint8, col map[uint8]color.RGBA, ox, oy, px int) {
	for y := range spriteH {
		for x := range spriteW {
			v := c[y*spriteW+x]
			if v == 0 {
				near := (x > 0 && c[y*spriteW+x-1] != 0) || (x < spriteW-1 && c[y*spriteW+x+1] != 0) ||
					(y > 0 && c[(y-1)*spriteW+x] != 0) || (y < spriteH-1 && c[(y+1)*spriteW+x] != 0)
				if !near {
					continue
				}
				v = 9
			}
			fill(img, ox+x*px, oy+y*px, px, px, col[v])
		}
	}
}

func fill(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	r := image.Rect(x, y, x+w, y+h).Intersect(img.Bounds())
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		for xx := r.Min.X; xx < r.Max.X; xx++ {
			img.SetRGBA(xx, yy, c)
		}
	}
}
