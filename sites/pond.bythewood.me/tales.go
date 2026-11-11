package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dreams, gossip and the gazette. Each has a hand written pool that's always
// there, and the nightly model adds to it when it ran.

var dreamPool = []string{
	"It dreamt the moon fell in the pond and {resident} tried to eat it.",
	"It dreamt it could fly, but only very low and only over puddles.",
	"It dreamt every fly in the world was a little bit slower.",
	"It dreamt it was a pebble, and honestly it liked it.",
	"It dreamt {resident} opened a shop that only sold left shoes.",
	"It dreamt the pond was full of soup and nobody minded.",
	"It dreamt it won a race it never entered.",
	"It dreamt a heron asked it for directions and it gave bad ones.",
	"It dreamt the lily pads were trampolines.",
	"It dreamt it had a very small hat on a very big head.",
	"It dreamt it was the weatherman, and it said rain every day.",
	"It dreamt {resident} could talk to fish, and the fish were rude.",
	"It dreamt the old log was hollow all the way to the sea.",
	"It dreamt about you, and you were holding a cricket.",
	"It dreamt it fell asleep in a dream and woke up here.",
	"It dreamt of a pond so big it had its own pond in the middle.",
	"It dreamt the wheel kept spinning and every slice was a snack.",
	"It dreamt {resident} sang one note for a whole night.",
	"It dreamt it was a tadpole again and had to learn everything twice.",
	"It dreamt the stars came down to drink from the pond.",
	"It dreamt a snail beat it in a race and was very nice about it.",
	"It dreamt all its hats had little hats of their own.",
}

var gossipPool = []string{
	"Heard {name} has been practising a new croak when nobody's around.",
	"Says {name} ate two dinners yesterday and denied the second one.",
	"Swears {name} has a secret stash of crickets under the old log.",
	"Thinks {name} has a crush on somebody, but won't say who.",
	"Heard {name} fell off a lily pad and pretended it meant to.",
	"Says {name} has been up very late counting pebbles.",
	"Heard {name} is learning to cook, and it isn't going well.",
	"Says {name} hasn't taken its hat off in a week.",
	"Thinks {name} is planning something big for the garden.",
	"Heard {name} was seen talking to a heron, which is a bold choice.",
	"Says {name} keeps a list of everybody's favourite dish.",
	"Heard {name} sneezed so hard it scared off the dragonfly.",
	"Says {name} has started napping on the wrong side of the pond.",
	"Thinks {name} has been rehearsing for the frog choir.",
	"Heard {name} lost a game of tadpoles to a real tadpole.",
	"Says {name} found something shiny and won't show anybody.",
	"Heard {name} tried to sell a rock at the curio shop and got told no.",
	"Says {name} has a cousin in a much fancier pond.",
}

// The gazette's tone follows the mood. The weather moods get a headline of
// their own on top.
func toneOf(mood string) string {
	switch mood {
	case moodSombre:
		return "quiet"
	case moodGloom1, moodGloom2, moodGloom3, moodJitters:
		return "grey"
	}
	return "fair"
}

var gazettePool = map[string][]string{
	"fair": {
		"{resident} spotted humming in broad daylight, neighbours delighted",
		"Lily pad traffic at an all time high, says {resident}",
		"Local frog claims to have seen the biggest fly yet",
		"{resident} hosts supper on the bank, everybody brings a dish",
		"Little Tully gets a new box of hats in and won't say what's in it",
		"Dragonfly sighting has the whole pond looking up",
		"{resident} and {other} settle long running argument over the best rock",
		"Garden beds doing very well this week, gardeners say",
		"Choir practice runs late again, nobody complains",
		"{resident} finally finishes reading a very long leaf",
	},
	"grey": {
		"A slow day on the pond as frogs keep close to the bank",
		"{resident} says the water will clear soon and asks everyone to stay calm",
		"Beetles enjoying the murky water, frogs less so",
		"{resident} spends the day under a pad, friends bring snacks",
		"Big Bean offers a warm cup of something to anyone who asks",
		"{resident} and {other} sit together and agree it'll pass",
		"Choir sings a slow one this evening",
		"Quiet day at the stall as neighbours stay in",
	},
	"quiet": {
		"A quiet day on the pond",
		"Neighbours keep each other company",
		"{resident} sits with {other} by the water",
		"The choir rests tonight",
		"Lanterns lit along the bank this evening",
	},
	moodRain: {
		"Rain at last, and the whole pond is out in it",
		"{resident} declares it the best day of the year, again",
	},
	moodShowers: {
		"Old Clover promises rain tomorrow, pond gets excited early",
	},
	moodFrost: {
		"Frost on the pads, most of the pond sleeps in",
		"{resident} wears two hats at once against the cold",
	},
	moodHeat: {
		"Heat sends everybody to the deep end",
		"{resident} spends the whole day floating, calls it work",
	},
	moodPollen: {
		"Pollen everywhere, pond sneezing as one",
	},
}

// otherThan picks a resident that isn't i, off h.
func otherThan(i int, h uint32) int {
	j := int(h % uint32(len(residents)-1))
	if j >= i {
		j++
	}
	return j
}

func fillNames(line string, a, b int) string {
	return strings.NewReplacer("{resident}", residents[a].name, "{other}", residents[b].name, "{name}", residents[b].name).Replace(line)
}

// dreamOf is what a frog dreamt last night, the same all day.
func dreamOf(seed uint32, day string) string {
	pool := book.pool("dream", "", day, dreamPool)
	h := dayHash(day, "dream"+strconv.FormatUint(uint64(seed), 10))
	a := int((h / 7) % uint32(len(residents)))
	return fillNames(pool[h%uint32(len(pool))], a, otherThan(a, h/101))
}

// gossipOf is resident i's rumour for the day, about somebody else.
func gossipOf(i int, day string) string {
	pool := book.pool("gossip", "", day, gossipPool)
	h := dayHash(day, "gossip"+strconv.Itoa(i))
	return fillNames(pool[h%uint32(len(pool))], i, otherThan(i, h/13))
}

type gazette struct {
	Lines    []string `json:"lines"`
	Business string   `json:"business,omitempty"`
	Issue    int      `json:"issue"`
	Date     string   `json:"date"`
	Sky      string   `json:"sky"`
	Photo    int      `json:"photo"`
	Pictured bool     `json:"pictured"`
	// The pond's own new bests from the day before.
	Records []string `json:"records,omitempty"`
	// Notice is this pond's own news, a place that opened today.
	Notice string `json:"notice,omitempty"`
	// Sweep is how this pond stands with the day's bug sweep.
	Sweep *bugNote `json:"sweep,omitempty"`
}

// noteRecord keeps a day's best new best at a game. A game's first score
// isn't one, since there was nothing to beat.
func noteRecord(tx *sql.Tx, id int64, game string, score, was int, now time.Time) error {
	if was <= 0 {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO records (pond_id, game, day, score) VALUES (?, ?, ?, ?)
		ON CONFLICT (pond_id, game, day) DO UPDATE SET score = MAX(score, excluded.score)`, id, game, dayOf(now), score)
	return err
}

// recordsOf is yesterday's new bests as lines for the paper.
func recordsOf(q querier, id int64, name string, now time.Time) ([]string, error) {
	rows, err := q.Query(`SELECT game, score FROM records WHERE pond_id = ? AND day = ? ORDER BY game`, id, dayOf(now.AddDate(0, 0, -1)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var game string
		var score int
		if err := rows.Scan(&game, &score); err != nil {
			return nil, err
		}
		if kind, ok := strings.CutPrefix(game, recordGame); ok {
			if f, ok := fishOf(kind); ok {
				_, who := pondRecord(f)
				out = append(out, fmt.Sprintf("%s landed %s of %s inches yesterday and took the pond record off %s.",
					name, aFish(f), inches(score), residents[who].name))
			}
			continue
		}
		what := "Dive"
		if g, ok := arcadeOf(game); ok {
			what = g.name
		}
		out = append(out, fmt.Sprintf("%s set a new best at %s yesterday, %s.", name, what, commas(score)))
	}
	return out, rows.Err()
}

// The first issue went out the day the gazette did.
const gazetteFirst = "2026-11-10"

var skyWords = map[string]string{
	moodPlain: "Fair", moodBright: "Sunny", moodGloom1: "Grey", moodGloom2: "Grey",
	moodGloom3: "Dark", moodJitters: "Windy", moodRain: "Rain", moodShowers: "Rain due",
	moodFrost: "Frost", moodHeat: "Hot", moodPollen: "Pollen", moodSombre: "Still",
}

func gazetteFor(mood string, now time.Time) gazette {
	today := dayOf(now)
	lines, photo, named := gazetteWithPhoto(mood, today)
	return gazette{Lines: lines, Photo: photo, Pictured: named, Issue: int(dayNumber(today)-dayNumber(gazetteFirst)) + 1,
		Date: now.In(pondZone).Format("Monday, January 2, 2006"), Sky: skyWords[mood]}
}

// gazetteOf is the day's three headlines, the same in every pond, with a
// weather mood's own headline first when it has one.
func gazetteOf(mood, day string) []string {
	out, _, _ := gazetteWithPhoto(mood, day)
	return out
}

// gazetteWithPhoto also says which resident the lead's photo is of, and
// whether the lead names it.
func gazetteWithPhoto(mood, day string) ([]string, int, bool) {
	var out []string
	photo, named := -1, false
	used := map[string]bool{}
	add := func(pool []string, salt string) {
		h := dayHash(day, salt)
		for k := 0; k < len(pool); k++ {
			l := pool[(h+uint32(k))%uint32(len(pool))]
			if !used[l] {
				used[l] = true
				a := int((h / 3) % uint32(len(residents)))
				if photo < 0 {
					photo, named = a, strings.Contains(l, "{resident}")
				}
				out = append(out, fillNames(l, a, otherThan(a, h/17)))
				return
			}
		}
	}
	if own, ok := gazettePool[mood]; ok {
		add(book.pool("gazette", mood, day, own), "gazette-weather")
	}
	tone := toneOf(mood)
	pool := book.pool("gazette", tone, day, gazettePool[tone])
	for k := 0; len(out) < 3 && k < 3; k++ {
		add(pool, "gazette"+strconv.Itoa(k))
	}
	return out, photo, named
}

// marketHeadline is the gazette's business story, off whichever business
// moved furthest in pebbles today. Like every other line it carries no number.
func marketHeadline(firms []firmView, day string) string {
	var best *firmView
	bestMove := 0.0
	for i := range firms {
		f := &firms[i]
		if f.Prev == 0 {
			continue
		}
		m := float64(f.Price-f.Prev) / float64(f.Prev)
		if m*m > bestMove*bestMove {
			best, bestMove = f, m
		}
	}
	if best == nil || bestMove > -0.01 && bestMove < 0.01 {
		return "A quiet day on the market board, traders mostly napping"
	}
	h := dayHash(day, "business")
	var pool []string
	switch {
	case bestMove >= 0.05:
		pool = []string{"{firm} has a day to remember, shareholders throw a party",
			"{firm} shoots up the board and nobody can quite say why"}
	case bestMove > 0:
		pool = []string{"{firm} edges up and its shareholders look smug",
			"A good day for {firm}, says everybody who owns some"}
	case bestMove <= -0.05:
		pool = []string{"{firm} tumbles down the board, shareholders take a long swim",
			"A rough day for {firm}, and the board is very pink"}
	default:
		pool = []string{"{firm} slips a little, owners say it's nothing",
			"{firm} has an off day and blames the weather"}
	}
	return strings.ReplaceAll(pool[h%uint32(len(pool))], "{firm}", best.Name)
}
