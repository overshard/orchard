package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Power only ever goes up, since a frog's rarity is fixed, a found hat or
// trinket is never lost and the best Dive is a best. Trinkets count the same
// as hats by tier.
var (
	hatPower = [tierCount]int{20, 50, 120, 300, 800}
	ranks    = []struct {
		at   int
		name string
	}{{0, "NEW TO THE POND"}, {900, "POND REGULAR"}, {1500, "BANK BOSS"}, {2600, "MARSH BARON"}, {4500, "SWAMP LEGEND"}}
)

type power struct {
	Total         int    `json:"total"`
	Rank          string `json:"rank"`
	Frog          int    `json:"frog"`
	Hats          int    `json:"hats"`
	HatsHeld      int    `json:"hats_held"`
	Trinkets      int    `json:"trinkets"`
	TrinketsFound int    `json:"trinkets_found"`
	Rocks         int    `json:"rocks"`
	RocksFound    int    `json:"rocks_found"`
	Keepsakes     int    `json:"keepsakes"`
	KeepsakesHad  int    `json:"keepsakes_found"`
	Jobs          int    `json:"jobs"`
	JobsTotal     int    `json:"jobs_total"`
	Dive          int    `json:"dive"`
	DiveBest      int    `json:"dive_best"`
}

// had is every trinket, rock and keepsake kind the pond has ever had, since
// none of them can be lost from the shelves.
// jobsTotal is the pond's best level at every job added up, and each level past
// the first is worth three.
func powerOf(r rarity, held []hat, had map[string]bool, diveBest, jobsTotal int) power {
	p := power{Frog: int(math.Round(120 * math.Log2(float64(r.N)))), DiveBest: diveBest, Dive: diveBest * 25}
	p.JobsTotal = max(jobsTotal, len(jobs))
	p.Jobs = (p.JobsTotal - len(jobs)) * 3
	for _, h := range held {
		p.Hats += hatPower[h.tier()]
	}
	p.HatsHeld = len(held)
	for _, t := range trinkets {
		if had[t.kind] {
			p.Trinkets += hatPower[t.tier]
			p.TrinketsFound++
		}
	}
	for _, k := range rocks {
		if had[k.kind] {
			p.Rocks += hatPower[k.tier]
			p.RocksFound++
		}
	}
	for _, k := range keepsakes {
		if had[k.kind] {
			p.Keepsakes += hatPower[k.tier]
			p.KeepsakesHad++
		}
	}
	p.Total = p.Frog + p.Hats + p.Trinkets + p.Rocks + p.Keepsakes + p.Jobs + p.Dive
	for _, k := range ranks {
		if p.Total >= k.at {
			p.Rank = k.name
		}
	}
	return p
}

func heldHats(q querier, id int64) ([]hat, error) {
	rows, err := q.Query(`SELECT shape, colour FROM hats WHERE pond_id = ? AND count > 0`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var held []hat
	for rows.Next() {
		var h hat
		if err := rows.Scan(&h.Shape, &h.Colour); err != nil {
			return nil, err
		}
		held = append(held, h)
	}
	return held, rows.Err()
}

// slug is the share address for a pond's frog out front. The code is the
// pond's public id, which has nothing to do with its key.
func slug(name, publicID string) string {
	return strings.ReplaceAll(strings.ToLower(name), " ", "-") + "-" + publicID
}

type shown struct {
	Frog  frogView
	Power power
	Slug  string
	URL   string
	// What's set out round the pad, by spot, "" for an empty one.
	Spots []string
}

func (s *store) shownBy(publicID string) (*shown, error) {
	var id int64
	err := s.r.QueryRow(`SELECT id FROM ponds WHERE public_id = ?`, publicID).Scan(&id)
	if err != nil {
		return nil, errNoPond
	}
	p, err := loadPond(s.r, id)
	if err != nil {
		return nil, err
	}
	held, err := heldHats(s.r, id)
	if err != nil {
		return nil, err
	}
	had, err := everHad(s.r, id)
	if err != nil {
		return nil, err
	}
	xp, err := loadXP(s.r, id)
	if err != nil {
		return nil, err
	}
	spots, err := spotsOut(s.r, p)
	if err != nil {
		return nil, err
	}
	f := frogOf(p, s.now())
	sl := slug(f.Name, p.PublicID)
	return &shown{Frog: f, Power: powerOf(f.Rarity, held, had, p.DiveBest, bestLevels(xp)), Slug: sl, URL: baseURL + "/f/" + sl, Spots: spots}, nil
}

// publicIDOf takes the code off the end of a share slug.
func publicIDOf(sl string) (string, bool) {
	i := strings.LastIndexByte(sl, '-')
	code := sl[i+1:]
	if len(code) != 16 || strings.Trim(code, "0123456789abcdef") != "" {
		return "", false
	}
	return code, true
}

func (s *site) lookup(w http.ResponseWriter, r *http.Request) (*shown, bool) {
	code, ok := publicIDOf(r.PathValue("slug"))
	if !ok {
		s.notFound(w, r)
		return nil, false
	}
	sh, err := s.store.shownBy(code)
	if errors.Is(err, errNoPond) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		slog.Error("pond", slog.String("path", r.URL.Path), slog.Any("err", err))
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "something went wrong in the pond", http.StatusInternalServerError)
		return nil, false
	}
	return sh, true
}

// A grown frog is the frog's own word for what it is, and an egg keeps its
// look to itself the same way it does on the pond.
func describe(f frogView) string {
	stage := titled(f.Stage)
	if f.Stage == "egg" {
		return stage + ", day " + fmt.Sprint(f.Day) + ". What hatches is a surprise."
	}
	return stage + ", day " + fmt.Sprint(f.Day) + ". " + titled(f.Morph) + ", " + strings.ToLower(f.Pattern) + " back, " + strings.ToLower(f.Nature) + "."
}

func (s *site) sharePage(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if r.PathValue("slug") != sh.Slug {
		http.Redirect(w, r, "/f/"+sh.Slug, http.StatusMovedPermanently)
		return
	}
	name := titled(sh.Frog.Name)
	p := s.pageFor("src/share.js", name, fmt.Sprintf("%s, a %s frog with %s power, %s.",
		name, strings.ToLower(sh.Frog.Rarity.Label), commas(sh.Power.Total), strings.ToLower(sh.Power.Rank)))
	p.Share = sh
	p.Describe = describe(sh.Frog)
	// The card's address changes with the power, so an unfurl that was cached
	// at an old number is fetched again.
	p.Image = fmt.Sprintf("%s/f/%s/card.png?p=%d", baseURL, sh.Slug, sh.Power.Total)
	s.renderer.Render(w, http.StatusOK, "share.html", p)
}

func (s *site) shareCard(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, drawCard(sh)); err != nil {
		slog.Error("pond", slog.String("path", r.URL.Path), slog.Any("err", err))
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "no card", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}

func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// ---------------------------------------------------------------- the card

//go:embed fonts/JetBrainsMono-Regular.ttf
var monoRegular []byte

//go:embed fonts/JetBrainsMono-Bold.ttf
var monoBold []byte

var (
	fontRegular = mustFont(monoRegular)
	fontBold    = mustFont(monoBold)
	faces       = map[string]font.Face{}

	// A face keeps a scratch buffer, so one card is drawn at a time.
	drawing sync.Mutex
)

func mustFont(b []byte) *opentype.Font {
	f, err := opentype.Parse(b)
	if err != nil {
		panic(err)
	}
	return f
}

func face(f *opentype.Font, size float64) font.Face {
	key := fmt.Sprintf("%p %g", f, size)
	if fc, ok := faces[key]; ok {
		return fc
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faces[key] = fc
	return fc
}

var (
	cardBg      = rgb("#0d0b09")
	cardVoid    = rgb("#0a0908")
	cardLine    = rgb("#47392a")
	cardText    = rgb("#ece5d8")
	cardDim     = rgb("#9c8f79")
	cardAmber   = rgb("#ffb000")
	cardAmberLo = rgb("#b37b00")
	tierColours = [tierCount]color.RGBA{rgb("#bcaf99"), rgb("#5ddc82"), rgb("#6cb8ff"), rgb("#cf9bff"), rgb("#ffb000")}
	water       = []color.RGBA{rgb("#123a3d"), rgb("#103336"), rgb("#0e2c30"), rgb("#0c262a")}
)

// text draws a line with extra space between letters, the caps tracking the
// page uses, and returns where it ended.
func text(img *image.RGBA, f font.Face, c color.RGBA, x, y int, s string, track int) int {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, y)}
	for _, r := range s {
		d.DrawString(string(r))
		d.Dot.X += fixed.I(track)
	}
	return d.Dot.X.Round()
}

func textWidth(f font.Face, s string, track int) int {
	return font.MeasureString(f, s).Round() + track*len([]rune(s))
}

// drawCard is the 1200 by 630 picture a share link unfurls with: the frog on
// its water in a screen on the left and its numbers on the right.
func drawCard(sh *shown) *image.RGBA {
	drawing.Lock()
	defer drawing.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 1200, 630))
	fill(img, 0, 0, 1200, 630, cardBg)

	// The screen, 44 by 34 cells at 10 pixels with water round it.
	const sx, sy, sw, sh0, px = 48, 48, 576, 534, 10
	fill(img, sx-1, sy-1, sw+2, sh0+2, cardLine)
	fill(img, sx, sy, sw, sh0, cardVoid)
	for i, c := range water {
		fill(img, sx, sy+i*sh0/4, sw, sh0/4+1, c)
	}
	f := sh.Frog
	ox, oy := sx+(sw-spriteW*px)/2, sy+sh0-spriteH*px-70
	grown := f.Stage == "froglet" || f.Stage == "frog" || f.Stage == "elder"
	// Trinkets sit where the pond page puts them, in its 352 by 272 scene.
	at := func(spot, x, y int) {
		if spot < len(sh.Spots) {
			drawTrinket(img, sh.Spots[spot], ox+x*px/8, oy+y*px/8, px/2)
		}
	}
	at(2, 160, 12)
	if grown {
		lilyPad(img, ox, oy, px)
	}
	g := lookFor(f.Seed, f.Mods)
	drawSprite(img, spriteCells(g, f.Stage, f.Day, f.Hat.Shape), spriteColours(g, f.Hat), ox, oy, px)
	if grown {
		at(0, 40, 236)
		at(1, 280, 236)
	}
	for i := 0; i < sh0; i += 4 {
		for x := sx; x < sx+sw; x++ {
			c := img.RGBAAt(x, sy+i)
			img.SetRGBA(x, sy+i, color.RGBA{c.R * 15 / 16, c.G * 15 / 16, c.B * 15 / 16, 255})
		}
	}
	bracket(img, sx-1, sy-1, false)
	bracket(img, sx+sw, sy+sh0, true)

	// The numbers.
	x := 672
	text(img, face(fontBold, 20), cardAmber, x, 92, "POND", 4)
	text(img, face(fontRegular, 16), cardDim, x+80, 92, "FROG CARD", 3)
	fill(img, x, 112, 480, 1, cardLine)

	name := f.Name
	size := 56.0
	for size > 30 && textWidth(face(fontBold, size), name, 0) > 480 {
		size -= 2
	}
	text(img, face(fontBold, size), cardText, x, 182, name, 0)
	end := text(img, face(fontBold, 20), tierColours[f.Rarity.Tier], x, 226, f.Rarity.Label, 3)
	text(img, face(fontRegular, 20), cardDim, end+12, 226, "1 IN "+commas(f.Rarity.N), 2)
	text(img, face(fontRegular, 18), cardDim, x, 262, strings.ToUpper(f.Stage)+", DAY "+fmt.Sprint(f.Day), 2)

	text(img, face(fontRegular, 18), cardAmberLo, x, 340, "FROG POWER", 3)
	text(img, face(fontBold, 120), cardAmber, x-6, 460, commas(sh.Power.Total), 0)
	text(img, face(fontBold, 22), cardText, x, 504, sh.Power.Rank, 3)

	fill(img, x, 548, 480, 1, cardLine)
	text(img, face(fontRegular, 18), cardDim, x, 580, "pond.bythewood.me", 0)
	return img
}

func drawTrinket(img *image.RGBA, kind string, x, y, s int) {
	ic, ok := trinketIcons[kind]
	if !ok {
		return
	}
	for r, row := range ic.rows {
		for c := range len(row) {
			if hex, ok := ic.col[row[c]]; ok {
				fill(img, x+c*s, y+r*s, s, s, rgb(hex))
			}
		}
	}
}

// bracket draws an amber corner, top left when flip is false and bottom right
// when it's true, with (x, y) the corner pixel.
func bracket(img *image.RGBA, x, y int, flip bool) {
	const n, t = 22, 3
	if flip {
		x, y = x-n+1, y-n+1
		fill(img, x, y+n-t, n, t, cardAmber)
		fill(img, x+n-t, y, t, n, cardAmber)
		return
	}
	fill(img, x, y, n, t, cardAmber)
	fill(img, x, y, t, n, cardAmber)
}

// lilyPad is the pond page's pad, in the same place under the frog's feet,
// drawn from the page's 352 by 272 scene scaled to px pixels a cell.
func lilyPad(img *image.RGBA, ox, oy, px int) {
	for y := 222; y < 268; y += 4 {
		for x := 20; x < 332; x += 4 {
			a, b := float64(x+2-176)/150, float64(y+2-246)/20
			d := a*a + b*b
			if d > 1 || (x > 286 && math.Abs(float64(y-240)) < float64(x-286)*0.3) {
				continue
			}
			c := rgb("#3f9a4d")
			switch {
			case d > 0.82:
				c = rgb("#1f5a2c")
			case y < 236:
				c = rgb("#56b45f")
			}
			fill(img, ox+x*px/8, oy+y*px/8, px/2, px/2, c)
		}
	}
}
