package main

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// Pad putt's nine holes come off a plain hash of the date, the same for
// every pond, since nobody gains by knowing them. A hole's pads run from the
// frog's at the bottom to the flag's last.
const (
	puttW, puttH = 200, 320
	puttHoles    = 9
	// The longest hop, and how far the windiest day can push one.
	puttReach, puttGust = 120, 20
	puttMost            = puttHoles * puttBirdie
	puttBirdie          = 12
)

type puttPad struct {
	X int `json:"x"`
	Y int `json:"y"`
	R int `json:"r"`
}

type puttHole struct {
	Pads []puttPad `json:"pads"`
	Par  int       `json:"par"`
	// Wind is how far it pushes a hop of the full reach, in pixels.
	Wind [2]int `json:"wind"`
}

// puttDeal is the day's holes with the wind as strong as it is right now,
// calm, breezy or windy, blowing a way of its own over each hole.
func puttDeal(day string, wind int) []puttHole {
	sum := sha256.Sum256([]byte("putt " + day))
	r := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])))
	out := make([]puttHole, puttHoles)
	for i := range out {
		out[i] = puttHoleOf(r, i)
		a := float64(r.IntN(8)) * math.Pi / 4
		push := float64(wind) * puttGust / 2
		out[i].Wind = [2]int{int(math.Round(math.Cos(a) * push)), int(math.Round(math.Sin(a) * push))}
		// Par is the fewest hops a frog that allows for the wind can make it in.
		out[i].Par = puttPar(out[i].Pads, puttReach-4-int(push))
	}
	return out
}

// A safe hop is one that lands whatever the wind does to it, so every hole
// can be played on a windy day by a frog that allows for the gust.
const puttSafe = puttReach - puttGust - 6

func puttHoleOf(r *rand.Rand, i int) puttHole {
	for {
		tee := puttPad{puttW / 2, puttH - 30, 13}
		flag := puttPad{24 + r.IntN(puttW-48), 34 + r.IntN(40), 11}
		if r.IntN(3) == 0 {
			flag.Y = 110 + r.IntN(50)
		}
		// Later holes can put the flag off to one side.
		if i >= 3 && r.IntN(2) == 0 {
			flag.X = 26 + r.IntN(30)
			if r.IntN(2) == 0 {
				flag.X = puttW - flag.X
			}
		}
		pads := []puttPad{tee}
		at := tee
		for tries := 0; dist(at, flag) > float64(puttSafe-10) && tries < 200; tries++ {
			dx, dy := float64(flag.X-at.X), float64(flag.Y-at.Y)
			d := math.Hypot(dx, dy)
			step := float64(55 + r.IntN(puttSafe-60))
			side := float64(r.IntN(61) - 30)
			next := puttPad{
				X: at.X + int(dx/d*step-dy/d*side),
				Y: at.Y + int(dy/d*step+dx/d*side),
				R: 8 + r.IntN(5),
			}
			next.X = min(puttW-14, max(14, next.X))
			next.Y = min(puttH-60, max(30, next.Y))
			if dist(at, next) > puttSafe || dist(at, next) < 30 {
				continue
			}
			pads = append(pads, next)
			at = next
		}
		for range 3 + r.IntN(4) {
			p := puttPad{14 + r.IntN(puttW-28), 30 + r.IntN(puttH-110), 7 + r.IntN(6)}
			if clear(p, append(pads, flag)) {
				pads = append(pads, p)
			}
		}
		h := puttHole{Pads: append(pads, flag)}
		if !clearAll(h.Pads) {
			continue
		}
		if h.Par = puttPar(h.Pads, puttSafe); h.Par > 0 {
			return h
		}
	}
}

func dist(a, b puttPad) float64 { return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)) }

func clear(p puttPad, pads []puttPad) bool {
	for _, q := range pads {
		if dist(p, q) < float64(p.R+q.R+8) {
			return false
		}
	}
	return true
}

func clearAll(pads []puttPad) bool {
	for i, p := range pads {
		if !clear(p, pads[i+1:]) {
			return false
		}
	}
	return true
}

// puttPar is the fewest hops from the first pad to the last, each no longer
// than reach, or 0 when the flag can't be reached.
func puttPar(pads []puttPad, reach int) int {
	hops := make([]int, len(pads))
	for i := range hops {
		hops[i] = -1
	}
	hops[0] = 0
	queue := []int{0}
	for len(queue) > 0 {
		a := queue[0]
		queue = queue[1:]
		for b := range pads {
			if hops[b] < 0 && dist(pads[a], pads[b]) <= float64(reach) {
				hops[b] = hops[a] + 1
				queue = append(queue, b)
			}
		}
	}
	return max(0, hops[len(pads)-1])
}
