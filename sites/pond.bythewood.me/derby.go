package main

import (
	"cmp"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"
)

// The snail derby. Every resident races a snail, three races of four a day,
// the same card in every pond. A race is rolled when a pond backs a snail by
// the same function that priced it, and the page only plays the script back.
const (
	derbyRaces = 3
	derbyField = 4
	// A free slip on every race, so nothing is staked from the purse.
	derbySlip = 6
	// Room for the longest odds on the board to pay what the board says.
	derbyCap = 150
	// The line is this far from the start, and a tick is a quarter second.
	derbyLine  = 1000
	derbyTicks = 120
	// Odds of this to one or longer are a long shot.
	derbyLong = 4
	// Within this of the line when the winner crosses is a photo finish.
	derbyPhoto = 20
	// How many races a card is priced off, and how many each step of
	// setting its spread takes.
	derbyPriced = 20000
	derbyTuned  = 2000
)

// The favourite wins about this often, whoever is in the race.
const derbyFav = 0.40

// A snail's chances are a tick each, and its pace makes up for them so no
// snail is better than another over a season. wet is how much it likes soft
// going and still is a snail that nothing stops.
type snail struct {
	name, shell, tip string
	pace             float64
	nap, leaf, hide  float64
	dash             float64
	wet              float64
	still            bool
	quirk            string
}

// snails[i] is residents[i]'s.
var snails = []snail{
	{"Old Shell", "#9aa6c2", "Never stops", -0.03, 0, 0, 0, 0, 0, true, "steady"},
	{"Biscuit", "#d8a25a", "Quick, but naps", 0.22, 0.02, 0, 0, 0, 0, false, ""},
	{"Bowler", "#7a5ab0", "Wears a hat", 0.02, 0, 0, 0.004, 0, 0, false, ""},
	{"Zoom", "#f29bb8", "Goes in bursts", -0.13, 0, 0, 0, 0.02, 0, false, ""},
	{"Thistle", "#a8424a", "Hates the wet", 0, 0, 0, 0, 0, -1, false, ""},
	{"Drizzle", "#6fc0e0", "Loves the wet", 0, 0, 0, 0, 0, 1.4, false, ""},
	{"Butterbean", "#e0d48a", "Can't pass a leaf", 0.19, 0, 0.03, 0, 0, 0, false, ""},
	{"Grandad", "#b8a27a", "Slow starter", 0.03, 0.003, 0, 0, 0, 0.3, false, "warms"},
	{"Rocket", "#ff8a4a", "Fast, then sleepy", 0.08, 0.008, 0, 0, 0.02, 0, false, "tires"},
	{"Mudlark", "#6a4428", "Loves the mud", 0, 0, 0, 0, 0, 2, false, ""},
	{"Petal", "#e07ab0", "Scared of birds", 0.1, 0, 0, 0.02, 0, 0, false, "shy"},
	{"Midnight", "#2a3a5a", "Anybody's guess", 0, 0, 0, 0, 0, 0, false, "wild"},
}

// What every snail does now and then on top of its own ways.
const snailNap, snailLeaf, snailHide, snailDash, snailSlime = 0.004, 0.005, 0.003, 0.008, 0.007

// A step a tick for a snail of plain pace, so a race runs about fifteen seconds.
const derbyStep = 17.5

// The going is how wet the log is, off the rain the day before.
var goings = []string{"firm", "good", "soft", "heavy"}

var goingSaid = []string{
	"The going is firm today, which the snails don't love.",
	"The going is good, a bit of dew on the log and that's all.",
	"The going is soft after the rain, and the snails are thrilled.",
	"It's heavy going after all that rain, which a snail calls perfect.",
}

// goingOf is 0 to 3 off how much rain fell the day before day.
func goingOf(q querier, day string) (int, error) {
	t, err := time.ParseInLocation("2006-01-02", day, pondZone)
	if err != nil {
		return 0, err
	}
	var wet int
	err = q.QueryRow(`SELECT COALESCE(SUM(wet), 0) FROM rain WHERE slot >= ? AND slot < ?`,
		rainSlot(t.AddDate(0, 0, -1)), rainSlot(t)).Scan(&wet)
	switch {
	case wet >= 36:
		return 3, err
	case wet >= 12:
		return 2, err
	case wet > 0:
		return 1, err
	}
	return 0, err
}

// The odds a board can chalk up, as so many to so many.
var derbyLadder = [][2]int{
	{1, 2}, {4, 7}, {8, 13}, {4, 6}, {8, 11}, {4, 5}, {5, 6}, {10, 11}, {1, 1}, {11, 10}, {6, 5},
	{5, 4}, {11, 8}, {6, 4}, {13, 8}, {7, 4}, {15, 8}, {2, 1}, {9, 4}, {5, 2}, {11, 4}, {3, 1},
	{10, 3}, {7, 2}, {4, 1}, {9, 2}, {5, 1}, {11, 2}, {6, 1}, {13, 2}, {7, 1}, {15, 2}, {8, 1},
	{9, 1}, {10, 1}, {11, 1}, {12, 1}, {14, 1}, {16, 1}, {20, 1},
}

// oddsOf is the longest odds on the board that are no better than fair for
// a chance of p, so a slip is worth a little under itself whatever it backs.
func oddsOf(p float64) [2]int {
	fair := (1 - p) / max(p, 0.001)
	best := derbyLadder[0]
	for _, o := range derbyLadder {
		if float64(o[0])/float64(o[1]) <= fair+1e-9 {
			best = o
		}
	}
	return best
}

func oddsSaid(o [2]int) string {
	if o[0] == o[1] {
		return "Evens"
	}
	return fmt.Sprintf("%d to %d", o[0], o[1])
}

// slipPays is what a winning slip comes back as, the slip and what it won.
func slipPays(o [2]int) int { return derbySlip * (o[0] + o[1]) / o[1] }

func longShot(o [2]int) bool { return o[0] >= derbyLong*o[1] }

// A card is one race: which snails, how quick each is on the day, and the
// chance of each winning off pricing it.
type derbyCard struct {
	snails []int
	pace   []float64
	chance []float64
	odds   [][2]int
	going  int
}

func (c *derbyCard) fav() int {
	f := 0
	for i := range c.chance {
		if c.chance[i] > c.chance[f] {
			f = i
		}
	}
	return f
}

func derbyRand(pepper []byte, what string) *rand.Rand {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte("derby|" + what))
	b := mac.Sum(nil)
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:16])))
}

// derbyDraw is the day's twelve snails in the order they race, four a race.
func derbyDraw(pepper []byte, day string) []int {
	return derbyRand(pepper, day).Perm(len(snails))
}

var derbyCards = struct {
	sync.Mutex
	m map[string]*derbyCard
}{m: map[string]*derbyCard{}}

// cardOf is race n of a day, 1 to 3, priced once and kept.
func cardOf(pepper []byte, day string, n, going int) *derbyCard {
	key := fmt.Sprintf("%x|%s|%d|%d", sha256.Sum256(pepper), day, n, going)
	derbyCards.Lock()
	defer derbyCards.Unlock()
	if c, ok := derbyCards.m[key]; ok {
		return c
	}
	if len(derbyCards.m) > 60 {
		clear(derbyCards.m)
	}
	c := dealCard(pepper, day, n, going)
	derbyCards.m[key] = c
	return c
}

// dealCard leans each snail by the going and its form on the day, then
// widens or narrows the gaps between them until the favourite wins about
// derbyFav of the time, so every race has a long shot worth talking about.
func dealCard(pepper []byte, day string, n, going int) *derbyCard {
	draw := derbyDraw(pepper, day)
	c := &derbyCard{snails: slices.Clone(draw[(n-1)*derbyField : n*derbyField]), going: going}
	form := derbyRand(pepper, fmt.Sprintf("%s|form|%d", day, n))
	lean := make([]float64, derbyField)
	mean := 0.0
	for i, k := range c.snails {
		lean[i] = snails[k].wet*float64(going-1)*0.03 + (form.Float64()-0.5)*0.08
		mean += lean[i] / derbyField
	}
	sd := 0.0
	for _, l := range lean {
		sd += (l - mean) * (l - mean) / derbyField
	}
	sd = max(math.Sqrt(sd), 0.001)
	spread := func(k float64) {
		c.pace = make([]float64, derbyField)
		for i := range c.pace {
			c.pace[i] = (1 + snails[c.snails[i]].pace) * (1 + k*(lean[i]-mean)/sd)
		}
	}
	tune := fmt.Sprintf("%s|tune|%d", day, n)
	lo, hi := 0.0, 0.5
	for range 10 {
		mid := (lo + hi) / 2
		spread(mid)
		if price(c, derbyRand(pepper, tune), derbyTuned).fav() < derbyFav {
			lo = mid
		} else {
			hi = mid
		}
	}
	spread((lo + hi) / 2)
	p := price(c, derbyRand(pepper, fmt.Sprintf("%s|price|%d", day, n)), derbyPriced)
	c.chance = p.chance
	c.odds = make([][2]int, derbyField)
	for i, ch := range c.chance {
		c.odds[i] = oddsOf(ch)
	}
	return c
}

type priced struct{ chance []float64 }

func (p priced) fav() float64 { return slices.Max(p.chance) }

func price(c *derbyCard, r *rand.Rand, n int) priced {
	wins := make([]int, derbyField)
	for range n {
		wins[runDerby(c, r, false).Win]++
	}
	out := priced{make([]float64, derbyField)}
	for i, w := range wins {
		out.chance[i] = float64(w) / float64(n)
	}
	return out
}

// An event is something a snail did from tick T for D ticks. S is -1 for
// something that happened to the whole race.
type derbyEvent struct {
	T int    `json:"t"`
	S int    `json:"s"`
	K string `json:"k"`
	D int    `json:"d"`
}

type derbyRace struct {
	// Pos is each lane's place along the log every tick, from 0 at the start.
	Pos    [][]int      `json:"pos"`
	Events []derbyEvent `json:"events"`
	Win    int          `json:"win"`
	// Ticks is how long the race ran, the winner crossing in the last one.
	Ticks int  `json:"ticks"`
	Photo bool `json:"photo"`
	// Order is the lanes in the order they finished, or would have.
	Order []int `json:"order"`
}

// runDerby is one race of a card. Pricing skips keeping the script.
func runDerby(c *derbyCard, r *rand.Rand, keep bool) *derbyRace {
	out := &derbyRace{Win: -1}
	var pos, last [derbyField]float64
	var busy, fast, slick [derbyField]int
	if keep {
		out.Pos = make([][]int, derbyField)
		for i := range out.Pos {
			out.Pos[i] = append(make([]int, 0, 64), 0)
		}
	}
	note := func(t, s int, k string, d int) {
		if keep {
			out.Events = append(out.Events, derbyEvent{t, s, k, d})
		}
	}
	bird := -1
	if r.Float64() < 0.3 {
		bird = 10 + r.IntN(30)
		note(bird, -1, "bird", 6)
	}
	for t := 1; t <= derbyTicks; t++ {
		late := t > 70
		for i, k := range c.snails {
			s := &snails[k]
			last[i] = pos[i]
			if t == bird && !s.still && busy[i] == 0 && r.Float64() < 0.85 {
				d := 4 + r.IntN(4)
				busy[i], fast[i] = d, 0
				note(t, i, "hide", d)
			}
			if busy[i] > 0 {
				busy[i]--
				continue
			}
			// Both of a tick's draws come off one number, which keeps pricing quick.
			x := r.Uint64()
			u, w := float64(x>>40)/(1<<24), float64(x&(1<<24-1))/(1<<24)
			switch {
			case s.still || t < 3 || late:
			case fast[i] == 1 && s.quirk == "tires" && u < 0.4:
				d := 8 + r.IntN(8)
				busy[i], fast[i] = d, 0
				note(t, i, "worn", d)
				continue
			case u < s.nap+snailNap:
				d := 6 + r.IntN(12)
				busy[i] = d
				note(t, i, "nap", d)
				continue
			case u < s.nap+snailNap+s.leaf+snailLeaf:
				d := 4 + r.IntN(6)
				busy[i] = d
				note(t, i, "leaf", d)
				continue
			case u < s.nap+snailNap+s.leaf+snailLeaf+s.hide+snailHide:
				d := 3 + r.IntN(5)
				busy[i] = d
				note(t, i, "hide", d)
				continue
			case fast[i] == 0 && u < s.nap+snailNap+s.leaf+snailLeaf+s.hide+snailHide+s.dash+snailDash:
				fast[i] = 4 + r.IntN(5)
				note(t, i, "dash", fast[i])
			case fast[i] == 0 && slick[i] == 0 && u < s.nap+snailNap+s.leaf+snailLeaf+s.hide+snailHide+s.dash+snailDash+snailSlime:
				slick[i] = 3
				note(t, i, "slime", 3)
			}
			lo, hi := 0.6, 1.4
			switch s.quirk {
			case "steady":
				lo, hi = 0.9, 1.1
			case "wild":
				lo, hi = 0.2, 1.8
			}
			v := derbyStep * c.pace[i] * (lo + (hi-lo)*w)
			if s.quirk == "warms" {
				v *= 0.75 + 0.5*min(1, float64(t)/50)
			}
			if fast[i] > 0 {
				v *= 2.1
				fast[i]--
			}
			if slick[i] > 0 {
				v *= 1.6
				slick[i]--
			}
			pos[i] += v
		}
		if keep {
			for i := range pos {
				out.Pos[i] = append(out.Pos[i], int(math.Round(pos[i])))
			}
		}
		// Two over the line in one tick go by who crossed first between ticks.
		first := 2.0
		for i := range pos {
			if pos[i] >= derbyLine {
				if f := (derbyLine - last[i]) / (pos[i] - last[i]); f < first {
					first, out.Win = f, i
				}
			}
		}
		if out.Win < 0 && t < derbyTicks {
			continue
		}
		if out.Win < 0 {
			out.Win = slices.Index(pos[:], slices.Max(pos[:]))
			first = 1
		}
		out.Ticks = t
		if !keep {
			return out
		}
		at := make([]float64, derbyField)
		for i := range pos {
			at[i] = last[i] + (pos[i]-last[i])*first
		}
		out.Order = []int{0, 1, 2, 3}
		slices.SortStableFunc(out.Order, func(a, b int) int {
			switch {
			case a == out.Win:
				return -1
			case b == out.Win:
				return 1
			}
			return cmp.Compare(at[b], at[a])
		})
		out.Photo = at[out.Order[1]] >= derbyLine-derbyPhoto
		return out
	}
	return out
}

// ---------------------------------------------------------------- commentary

// A call is a line the commentary puts up under the log at tick T.
type derbyCall struct {
	T    int    `json:"t"`
	Line string `json:"line"`
}

// The hand written calls, by what happened. {snail} is the snail's name and
// {owner} the resident it belongs to.
var derbyLines = map[string][]string{
	"off": {
		"And they're off, or at least most of them are.",
		"They're away, in a manner of speaking.",
		"And they're off, slowly, very slowly.",
		"The flag is down and the race is on, give or take.",
	},
	"nap": {
		"{snail} has nodded off right there in the middle of the log.",
		"{snail} is asleep, and they're never going to wake up in time.",
		"{snail} is having a little sleep, is that even allowed?",
		"{snail} has stopped for forty winks and {owner} can't watch.",
		"{snail} has curled up for a snooze with the whole bank shouting.",
	},
	"wake": {
		"{snail} has woken up and looks very surprised to be in a race.",
		"{snail} is awake again and wants to know what they missed.",
		"Somebody nudged {snail} awake, and off we go again.",
	},
	"worn": {
		"{snail} went off far too quick and now needs a lie down.",
		"All that rushing about and {snail} has gone for a nap.",
	},
	"leaf": {
		"{snail} has stopped for a lettuce leaf.",
		"{snail} has found a leaf and that's the end of that for a bit.",
		"{snail} is having a snack, in the middle of a race.",
		"{owner} is begging {snail} to leave the leaf alone.",
	},
	"ate": {
		"{snail} has finished the leaf and is back in the race, a bit rounder.",
		"Leaf gone, {snail} is off again with a full tummy.",
	},
	"hide": {
		"{snail} has gone into the shell, there must be a bird.",
		"{snail} has hidden away and won't come out for anybody.",
		"Something spooked {snail}, and in the shell it goes.",
	},
	"out": {
		"{snail} pokes an eye out, and decides it's safe.",
		"Out comes {snail}, very carefully.",
	},
	"bird": {
		"A blackbird has landed on the log, everybody into your shells.",
		"There's a thrush on the log, and the field has gone very quiet.",
	},
	"dash": {
		"{snail} has found another gear, look at that go.",
		"Here comes {snail}, flat out, which isn't very fast.",
		"{snail} is really moving now, you can nearly see it.",
		"{owner} is up and shouting as {snail} picks up speed.",
	},
	"slime": {
		"{snail} has hit a slick patch and is sliding along nicely.",
		"{snail} is on somebody's slime trail and loving it.",
	},
	"lead": {
		"{snail} takes the lead, look at that.",
		"{snail} hits the front, and {owner} is jumping up and down.",
		"{snail} is in front now, who saw that coming.",
		"It's {snail} out in front, by a whisker.",
	},
	"cheer": {
		"{owner} is shouting {snail} on from the bank.",
		"{owner} has both hands in the air for {snail}.",
		"You can hear {owner} from here, come on {snail}.",
		"{owner} can't look, but {snail} is going well.",
	},
	"back": {
		"{snail} is at the back, taking in the view.",
		"{snail} brings up the rear, in no hurry at all.",
		"{snail} is last, but {owner} says it's all part of the plan.",
	},
	"half": {
		"Halfway down the log and it's {snail} from the rest.",
		"We're halfway, and {snail} is showing them the way.",
	},
	"close": {
		"It's going to be close, it's going to be very close.",
		"Neck and neck as they come to the line, if snails had necks.",
	},
	"win": {
		"{snail} wins it, what a snail.",
		"{snail} gets there first, and {owner} is over the moon.",
		"{snail} crosses the line, what a race.",
	},
	"photo": {
		"It's a photo, and the photo says {snail}.",
		"By the thinnest bit of slime, {snail} wins it.",
	},
	"upset": {
		"{snail} wins at long odds, and the bank has gone very quiet.",
		"Nobody had {snail}, well nearly nobody.",
		"A long shot comes in, {snail} wins and {owner} can't believe it.",
	},
	"fav": {
		"The favourite does it, {snail} wins and nobody is surprised.",
		"{snail} was the one to beat and nobody did.",
	},
}

// Lines are called at least callGap ticks apart, and a quiet spell longer
// than callQuiet gets something from the bank.
const callGap, callQuiet = 6, 11

// derbyCalls puts a line to the race as it goes, and the end is always called.
func derbyCalls(c *derbyCard, race *derbyRace, r *rand.Rand) []derbyCall {
	used := map[string]bool{}
	say := func(k string, lane int) string {
		pool := slices.DeleteFunc(slices.Clone(derbyLines[k]), func(l string) bool { return used[l] })
		if len(pool) == 0 {
			pool = derbyLines[k]
		}
		l := pool[r.IntN(len(pool))]
		used[l] = true
		if lane >= 0 {
			l = strings.ReplaceAll(l, "{snail}", snails[c.snails[lane]].name)
			l = strings.ReplaceAll(l, "{owner}", residents[c.snails[lane]].name)
		}
		return l
	}
	type said struct {
		t, lane, rank int
		k             string
	}
	var heard []said
	rank := map[string]int{"bird": 4, "nap": 3, "worn": 3, "leaf": 3, "hide": 2, "lead": 1, "dash": 1, "wake": 1, "ate": 0, "out": 0, "slime": 0}
	after := map[string]string{"nap": "wake", "worn": "wake", "leaf": "ate", "hide": "out"}
	for _, e := range race.Events {
		// The bird is called, and not everybody it sent into their shells.
		bird := e.K == "hide" && slices.ContainsFunc(race.Events, func(b derbyEvent) bool { return b.K == "bird" && b.T == e.T })
		if !bird {
			heard = append(heard, said{e.T, e.S, rank[e.K], e.K})
		}
		if k, ok := after[e.K]; ok && !bird {
			heard = append(heard, said{e.T + e.D, e.S, rank[k], k})
		}
	}
	lead := -1
	for t := 1; t < race.Ticks; t++ {
		best := 0
		for i := range race.Pos {
			if race.Pos[i][t] > race.Pos[best][t] {
				best = i
			}
		}
		if best != lead && t > 6 {
			heard = append(heard, said{t, best, rank["lead"], "lead"})
		}
		lead = best
	}
	slices.SortStableFunc(heard, func(a, b said) int { return a.t - b.t })
	out := []derbyCall{{0, say("off", -1)}}
	end := race.Ticks - 5
	close := -1
	for t := max(1, race.Ticks-8); t < race.Ticks; t++ {
		a, b := race.Pos[race.Order[0]][t], race.Pos[race.Order[1]][t]
		if a > derbyLine-200 && b > derbyLine-200 && abs(a-b) < 40 {
			close = t
			break
		}
	}
	if close > 0 {
		end = close
	}
	half := false
	// fill puts the bank's view of the race into a quiet spell up to tick to.
	fill := func(to int) {
		for t := out[len(out)-1].T + callQuiet - 4; to-t >= callGap && t < end; t += callQuiet - 2 {
			order := slices.Clone(race.Order)
			slices.SortStableFunc(order, func(a, b int) int { return race.Pos[b][t] - race.Pos[a][t] })
			k, lane := "cheer", order[r.IntN(2)]
			switch {
			case !half && race.Pos[order[0]][t] > derbyLine*2/5 && race.Pos[order[0]][t] < derbyLine*3/5:
				half, k, lane = true, "half", order[0]
			case r.IntN(3) == 0:
				k, lane = "back", order[derbyField-1]
			}
			out = append(out, derbyCall{t, say(k, lane)})
		}
	}
	for i, h := range heard {
		if h.t >= end || h.t-out[len(out)-1].T < callGap {
			continue
		}
		// Something better coming straight after wins the slot.
		if slices.ContainsFunc(heard[i+1:], func(o said) bool { return o.t-h.t < 4 && o.rank > h.rank && o.t < end }) {
			continue
		}
		fill(h.t)
		out = append(out, derbyCall{h.t, say(h.k, h.lane)})
	}
	if close > 0 {
		fill(close)
		out = append(out, derbyCall{close, say("close", -1)})
	} else {
		fill(race.Ticks)
	}
	k := "win"
	switch {
	case race.Photo:
		k = "photo"
	case longShot(c.odds[race.Win]):
		k = "upset"
	case race.Win == c.fav():
		k = "fav"
	}
	return append(out, derbyCall{race.Ticks, say(k, race.Win)})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------------------------------------------------------------- the pond's side

type derbyRow struct {
	race, pick, win, paid int
	odds                  string
}

func derbyRows(q querier, id int64, day string) ([]derbyRow, error) {
	rows, err := q.Query(`SELECT race, pick, win, paid, odds FROM derby WHERE pond_id = ? AND day = ? ORDER BY race`, id, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []derbyRow
	for rows.Next() {
		var r derbyRow
		if err := rows.Scan(&r.race, &r.pick, &r.win, &r.paid, &r.odds); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type derbyRunner struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Seed  uint32 `json:"seed"`
	Hat   int    `json:"hat"`
	Shell string `json:"shell"`
	Tip   string `json:"tip"`
	Odds  string `json:"odds"`
	Pays  int    `json:"pays"`
	Fav   bool   `json:"fav"`
	Long  bool   `json:"long"`
	// Look is a few things the page draws on this snail.
	Look string `json:"look"`
}

type derbyRan struct {
	Race int    `json:"race"`
	Pick string `json:"pick"`
	Win  string `json:"win"`
	Odds string `json:"odds"`
	Paid int    `json:"paid"`
}

type derbyView struct {
	Day string `json:"day"`
	// Race is the next race to run, 1 to Of, and 0 once they've all run.
	Race  int           `json:"race"`
	Of    int           `json:"of"`
	Going string        `json:"going"`
	Said  string        `json:"said"`
	Card  []derbyRunner `json:"card"`
	Ran   []derbyRan    `json:"ran"`
	Won   int           `json:"won"`
	Paid  int           `json:"paid"`
	Cap   int           `json:"cap"`
	Slip  int           `json:"slip"`
	Aims  []aimView     `json:"aims"`
}

func runnersOf(c *derbyCard) []derbyRunner {
	out := make([]derbyRunner, derbyField)
	fav := c.fav()
	for i, k := range c.snails {
		s, r := snails[k], residents[k]
		out[i] = derbyRunner{s.name, r.name, r.seed, r.hat, s.shell, s.tip, oddsSaid(c.odds[i]),
			slipPays(c.odds[i]), i == fav, longShot(c.odds[i]), s.quirk}
		if s.name == "Bowler" {
			out[i].Look = "hat"
		}
	}
	return out
}

func derbyViewOf(q querier, id int64, now time.Time, pepper []byte) (derbyView, error) {
	day := dayOf(now)
	v := derbyView{Day: day, Of: derbyRaces, Cap: derbyCap, Slip: derbySlip}
	going, err := goingOf(q, day)
	if err != nil {
		return v, err
	}
	v.Going, v.Said = goings[going], goingSaid[going]
	ran, err := derbyRows(q, id, day)
	if err != nil {
		return v, err
	}
	for _, r := range ran {
		v.Ran = append(v.Ran, derbyRan{r.race, snails[r.pick].name, snails[r.win].name, r.odds, r.paid})
		v.Paid += r.paid
		if r.pick == r.win {
			v.Won++
		}
	}
	if len(ran) < derbyRaces {
		v.Race = len(ran) + 1
		v.Card = runnersOf(cardOf(pepper, day, v.Race, going))
	}
	had, err := aimsMet(q, id, day)
	if err != nil {
		return v, err
	}
	v.Aims = aimViews(had, day, "derby")
	return v, nil
}

var errRaceGone = errors.New("that race has been run")

type derbyBacked struct {
	// Run is only for the frame round the game, which wants one.
	Run    string        `json:"run"`
	Race   int           `json:"race"`
	Pick   int           `json:"pick"`
	Picked bool          `json:"picked"`
	Card   []derbyRunner `json:"card"`
	Script *derbyRace    `json:"script"`
	Calls  []derbyCall   `json:"calls"`
	// Won is what the slip came back as, and Paid what that and any aims put
	// in the purse inside the day's cap.
	Won    int      `json:"won"`
	Paid   int      `json:"paid"`
	Rested int      `json:"rested"`
	Aims   []string `json:"aims,omitempty"`
	Pond   *view    `json:"pond"`
}

// backSnail runs today's next race with the pond's slip on lane pick, or on
// the favourite when pick is out of range. The race is rolled and paid here
// and the page only plays it back.
func (s *store) backSnail(id int64, day string, race, pick int) (*derbyBacked, error) {
	out := &derbyBacked{Race: race, Pick: pick, Picked: pick >= 0 && pick < derbyField}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		today := dayOf(now)
		if day != today || race < 1 || race > derbyRaces {
			return errRaceGone
		}
		ran, err := derbyRows(tx, p.ID, today)
		if err != nil {
			return err
		}
		if race != len(ran)+1 {
			return errRaceGone
		}
		going, err := goingOf(tx, today)
		if err != nil {
			return err
		}
		c := cardOf(s.pepper, today, race, going)
		if !out.Picked {
			out.Pick = c.fav()
		}
		var seed [32]byte
		crand.Read(seed[:])
		r := rand.New(rand.NewChaCha8(seed))
		out.Script = runDerby(c, r, true)
		out.Calls = derbyCalls(c, out.Script, r)
		out.Card = runnersOf(c)
		out.Run = fmt.Sprintf("%s/%d", today, race)
		odds := c.odds[out.Pick]
		won, wins := 0, 0
		for _, x := range ran {
			if x.pick == x.win {
				wins++
			}
		}
		counts := map[string]int{"wins": wins}
		if out.Script.Win == out.Pick {
			won = slipPays(odds)
			counts["wins"]++
			if longShot(odds) {
				counts["long"] = 1
			}
		}
		out.Won = won
		if err := addFlow(tx, p, "played", 1); err != nil {
			return err
		}
		aims, more, err := meetAims(tx, p.ID, "derby", 0, counts, now)
		if err != nil {
			return err
		}
		out.Aims = aims
		paid := 0
		for _, x := range ran {
			paid += x.paid
		}
		out.Paid, out.Rested = p.payRun(won+more, derbyCap-paid, now)
		p.Pebbles += out.Paid
		if _, err := tx.Exec(`DELETE FROM derby WHERE pond_id = ? AND day <> ?`, p.ID, today); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO derby (pond_id, day, race, pick, win, paid, odds) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.ID, today, race, c.snails[out.Pick], c.snails[out.Script.Win], out.Paid, oddsSaid(odds))
		return err
	})
	out.Pond = v
	return out, err
}
