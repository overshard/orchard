package main

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"time"
	_ "time/tzdata"
)

const (
	starterPebbles = 20

	// A present needs this long since the last one, and stops getting better
	// after a week.
	giftGap  = 4 * time.Hour
	giftBest = 7 * 24 * time.Hour

	spinEvery     = 4 * time.Hour
	maxFreeSpins  = 2
	paidSpinCost  = 25
	paidSpinsADay = 5

	boxCost = 40

	// The first runs of a day pay a pebble a point, later ones a quarter, and
	// the cap is on the day whatever the runs were.
	diveFullRuns = 3
	diveCap      = 120
	diveMaxScore = 400

	// Ponds laid across the whole site in an hour, so a script cannot fill the
	// disk with eggs.
	pondsAnHour = 240
)

// Days roll over on this clock for everybody.
var pondZone = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return loc
}()

func dayOf(t time.Time) string { return t.In(pondZone).Format("2006-01-02") }

// stageOf counts from the day the egg was laid, and day 1 is that day.
func stageOf(age time.Duration) (string, int) {
	day := int(age/(24*time.Hour)) + 1
	switch {
	case day <= 1:
		return "egg", max(day, 1)
	case day <= 8:
		return "tadpole", day
	case day <= 12:
		return "froglet", day
	case day <= 28:
		return "frog", day
	}
	return "elder", day
}

// ---------------------------------------------------------------- hats

var hatNames = []string{"", "Top hat", "Party cone", "Crown", "Leaf", "Bobble hat", "Daisy", "Cowboy hat", "Wizard hat", "Toadstool", "Horned helmet", "Chef's hat", "Propeller cap", "Halo", "Bow", "Straw hat"}

// A shape's tier in its plain colours, by the same index.
var hatPlainTier = []int{0, 2, 1, 3, 0, 0, 0, 1, 2, 1, 2, 1, 1, 3, 0, 0}

var hatColours = []string{"", "Moss ", "Berry ", "Golden "}

const (
	hatShapes  = 15
	hatGolden  = 3
	tierCount  = 5
	hatsInGame = hatShapes * 4
)

// What a spare is worth at the going rate. Wren pays a quarter of it.
var hatRate = []int{20, 60, 160, 500, 1600}

type hat struct {
	Shape  int `json:"shape"`
	Colour int `json:"colour"`
}

func (h hat) valid() bool {
	return h.Shape >= 1 && h.Shape <= hatShapes && h.Colour >= 0 && h.Colour <= hatGolden
}

func (h hat) tier() int {
	if h.Colour == hatGolden {
		return tierCount - 1
	}
	t := hatPlainTier[h.Shape]
	if h.Colour > 0 {
		t++
	}
	return min(t, tierCount-1)
}

func (h hat) name() string { return hatColours[h.Colour] + hatNames[h.Shape] }

func wrenPays(h hat) int { return hatRate[h.tier()] / 4 }

var hatsByTier = func() [tierCount][]hat {
	var by [tierCount][]hat
	for s := 1; s <= hatShapes; s++ {
		for c := 0; c <= hatGolden; c++ {
			h := hat{s, c}
			by[h.tier()] = append(by[h.tier()], h)
		}
	}
	return by
}()

// Out of 100, common first. The hats page prints these.
var boxOdds = [tierCount]int{50, 28, 14, 6, 2}

func rollBox() hat {
	r, acc := rand.IntN(100), 0
	for t, w := range boxOdds {
		acc += w
		if r < acc {
			return hatOfTier(t)
		}
	}
	return hatOfTier(0)
}

func hatOfTier(t int) hat { return hatsByTier[t][rand.IntN(len(hatsByTier[t]))] }

// ---------------------------------------------------------------- things

// Everything a pond can hold that is not a pebble or a hat.
var itemNames = map[string]string{
	"cricket":   "Cricket",
	"agate":     "River agate",
	"quartz":    "Smoky quartz",
	"geode":     "Geode",
	"dye_blue":  "Blue dye",
	"dye_gold":  "Golden dye",
	"dye_berry": "Strawberry dye",
	"dye_olive": "Olive dye",
	"dye_brown": "Wood brown dye",
	"dye_grey":  "Grey dye",
	"snail":     "Snail shell",
	"cap":       "Bottle cap",
	"feather":   "Heron feather",
	"amethyst":  "Amethyst",
	"calcite":   "White calcite",
	"pyrite":    "Fool's gold",
	"banded":    "Banded agate",
	"fossil":    "Fossil shell",
}

// A prize is pebbles, a thing or a hat, and only one of the three is set.
type prize struct {
	Pebbles int    `json:"pebbles,omitempty"`
	Item    string `json:"item,omitempty"`
	Hat     *hat   `json:"hat,omitempty"`
	HatName string `json:"hat_name,omitempty"`
	HatTier int    `json:"hat_tier"`
	Spare   bool   `json:"spare,omitempty"`
	Text    string `json:"text"`
}

// ---------------------------------------------------------------- the wheel

// Sixteen equal slices, so a prize's share of the wheel is its odds. The page
// draws the wheel from this list.
var wheelSlices = []string{"P25", "CRI", "P40", "AGA", "P25", "GEO", "P40", "CRI", "P25", "HAT", "P40", "AGA", "P25", "QUA", "CRI", "DYE"}

var wheelDyes = []string{"dye_blue", "dye_gold", "dye_berry", "dye_olive", "dye_brown", "dye_grey"}

// The hat and the dye on the wheel change with the day and are the same for
// everybody that day. Most days the hat is a common one and now and then rare.
func wheelOfDay(day string) (hat, string) {
	h := fnv.New32a()
	h.Write([]byte("wheel " + day))
	n := h.Sum32()
	tier := 0
	switch r := n % 100; {
	case r >= 85:
		tier = 2
	case r >= 55:
		tier = 1
	}
	pool := hatsByTier[tier]
	return pool[(n/100)%uint32(len(pool))], wheelDyes[(n/100000)%uint32(len(wheelDyes))]
}

func wheelPrize(slice, day string) prize {
	dayHat, dayDye := wheelOfDay(day)
	switch slice {
	case "P25":
		return prize{Pebbles: 25, Text: "25 pebbles."}
	case "P40":
		return prize{Pebbles: 40, Text: "40 pebbles."}
	case "CRI":
		return prize{Item: "cricket", Text: "A cricket, and it's yours."}
	case "AGA":
		return prize{Item: "agate", Text: "A river agate, and it's yours."}
	case "QUA":
		return prize{Item: "quartz", Text: "A smoky quartz, and it's yours."}
	case "GEO":
		return prize{Item: "geode", Text: "A geode. Crack it open from your purse."}
	case "DYE":
		return prize{Item: dayDye, Text: "A bottle of " + strings.ToLower(itemNames[dayDye]) + ", for an egg some day."}
	}
	return prize{Hat: &dayHat}
}

// Out of 100.
var geodeInside = []struct {
	w    int
	item string
	text string
}{
	{40, "amethyst", "Amethyst, a good purple one."},
	{28, "calcite", "Plain white calcite, still sparkly."},
	{18, "pyrite", "Fool's gold in a crust."},
	{10, "banded", "Banded agate all the way through."},
	{4, "fossil", "A tiny fossil shell, which is the rare one."},
}

func rollGeode() prize {
	r, acc := rand.IntN(100), 0
	for _, g := range geodeInside {
		acc += g.w
		if r < acc {
			return prize{Item: g.item, Text: "Crack. " + g.text}
		}
	}
	return prize{Item: geodeInside[0].item, Text: "Crack. " + geodeInside[0].text}
}

// ---------------------------------------------------------------- the old log

type find struct {
	Pebbles int    `json:"pebbles"`
	Text    string `json:"text"`
	w       int
}

// Weights out of 100.
var finds = []find{
	{3, "A beetle, and it got away. It dropped 3 pebbles.", 22},
	{6, "A wet leaf with 6 pebbles under it.", 22},
	{10, "10 pebbles in the mud.", 20},
	{14, "A snail who owed somebody 14 pebbles.", 14},
	{20, "20 pebbles and a bottle cap.", 12},
	{35, "A tin with 35 pebbles in it.", 7},
	{80, "Somebody's lost purse, 80 pebbles.", 3},
}

func rollFind() find {
	r, acc := rand.IntN(100), 0
	for _, f := range finds {
		acc += f.w
		if r < acc {
			return f
		}
	}
	return finds[0]
}

// ---------------------------------------------------------------- presents

type gift struct {
	pebbles int
	item    string
	text    string
	hatTier int
	isHat   bool
	// A trinket of this tier plus one, so zero is none.
	trinket int
}

var (
	giftsDay = []gift{
		{pebbles: 15}, {pebbles: 25}, {pebbles: 40},
		{item: "cricket", text: "A cricket, still kicking."},
		{item: "snail", text: "A snail shell it found on the bank."},
		{item: "cap", text: "A bottle cap, very shiny."},
		{item: "agate", text: "A bit of river agate."},
		{trinket: 1},
	}
	giftsDays = []gift{
		{pebbles: 60}, {pebbles: 80},
		{item: "cricket", text: "A cricket, still kicking."},
		{item: "agate", text: "A bit of river agate."},
		{item: "quartz", text: "A lump of smoky quartz."},
		{item: "feather", text: "A heron feather, which is a good one."},
		{item: "wood", text: "A chip of petrified wood off the bank."},
		{isHat: true, hatTier: 0},
		{trinket: 1}, {trinket: 2},
	}
	giftsWeek = []gift{
		{pebbles: 120},
		{item: "feather", text: "A heron feather, which is a good one."},
		{item: "quartz", text: "A lump of smoky quartz."},
		{item: "geode", text: "A geode it couldn't open."},
		{item: "wood", text: "A chip of petrified wood off the bank."},
		{item: "moonstone", text: "A moonstone. It won't say where from."},
		{isHat: true, hatTier: 1}, {isHat: true, hatTier: 1},
		{trinket: 2}, {trinket: 3},
	}
)

// rollGift picks a present for somebody gone this long.
func rollGift(gap time.Duration) prize {
	table := giftsDay
	switch {
	case gap >= giftBest:
		table = giftsWeek
	case gap >= 24*time.Hour:
		table = giftsDays
	}
	g := table[rand.IntN(len(table))]
	switch {
	case g.trinket > 0:
		t := trinketOfTier(g.trinket - 1)
		return prize{Item: t.kind, Text: t.name + ", for round the pad."}
	case g.isHat:
		h := hatOfTier(g.hatTier)
		return prize{Hat: &h}
	case g.item != "":
		return prize{Item: g.item, Text: g.text}
	}
	return prize{Pebbles: g.pebbles, Text: fmt.Sprintf("%d pebbles, in a neat little pile.", g.pebbles)}
}

// ---------------------------------------------------------------- greeting

var natureLines = map[string]string{
	"BOLD":      "It jumps straight at the glass.",
	"SHY":       "It peeks out from under a lily first, then comes all the way over.",
	"NIGHT OWL": "It was asleep, and it got up for this.",
	"GLUTTON":   "It arrives with its mouth full.",
	"SINGER":    "It's been saving a song.",
	"DRIFTER":   "It was halfway across the pond and turned round.",
}

// greetLine is only ever glad. Nothing here gets sad or cross with absence.
func greetLine(g genome, stage string, away time.Duration) string {
	name := titled(g.Name)
	switch stage {
	case "egg":
		return name + " turns over in its jelly when you lean in. It knows it's you."
	case "tadpole":
		if away < time.Hour {
			return name + " swims up to the glass. Oh good, it's you again."
		}
		return name + " is at the glass before the page has finished loading, tail going the whole time."
	}
	nature := natureLines[g.Nature]
	switch {
	case away < time.Hour:
		return name + " looks up. Oh good, it's you again."
	case away < 3*24*time.Hour:
		return name + " is over before the page has finished loading. " + nature
	case away < 21*24*time.Hour:
		return name + " does three laps of the pad. You weren't here and now you are, and that's all it has to say about it. " + nature
	}
	return name + " comes over slower than it used to and gets there all the same. " + nature
}

// Nine ghost frogs swim every run, and each is dealt the number of gates it
// will clear before it hits one. The page plays them out and the server keeps
// the deal, so a place on the podium is worked out here.
const (
	diveGhosts = 9
	// A new pond starts on this form, and form never deals below it.
	diveFormStart = 8.0
	diveFormFloor = 4.0
)

// diveForm follows the gates of recent runs, so ghosts come in closer while a
// player keeps going out early and spread further as they do well.
func diveForm(form float64, gates int) float64 {
	return form*0.65 + float64(gates)*0.35
}

// dealGhosts sets the three best ghosts around the pond's form and the rest
// well under it, so matching your form is about even odds for first.
func dealGhosts(form float64) []int {
	base := max(form, diveFormFloor)
	band := func(lo, hi float64) int { return int(base * (lo + rand.Float64()*(hi-lo))) }
	clears := []int{band(0.85, 1.15), band(0.65, 0.95), band(0.5, 0.8)}
	for len(clears) < diveGhosts {
		clears = append(clears, band(0, 0.6))
	}
	rand.Shuffle(len(clears), func(i, j int) { clears[i], clears[j] = clears[j], clears[i] })
	return clears
}

// divePlace is where a run that cleared this many gates came among the
// ghosts, first being 1. A ghost that went out at the same gate is behind.
func divePlace(gates int, ghosts []int) int {
	place := 1
	for _, c := range ghosts {
		if c > gates {
			place++
		}
	}
	return place
}

// Extra pebbles for first, second and third, each once a day. First also
// gets a hat.
var podiumPebbles = [3]int{25, 25, 10}

// divePays is what a run earns before the day's cap. Run counts from 1.
func divePays(score, run int) int {
	if run > diveFullRuns {
		return score / 4
	}
	return score
}

// A run cannot score faster than gates and bugs arrive at top speed, which is
// a little over two a second.
func divePlausible(score int, took time.Duration) bool {
	return score >= 0 && score <= diveMaxScore && float64(score) <= took.Seconds()*2.5+1
}
