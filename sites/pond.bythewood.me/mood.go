package main

import (
	"strconv"
	"strings"
	"time"
)

// The mood of the day is the real day boiled down to one word, which tints
// the water and picks what the frog says. No number from dash ever reaches a
// line, so the frog never reads out a price or a temperature.
const (
	moodPlain   = "plain"
	moodBright  = "bright"
	moodGloom1  = "gloom1"
	moodGloom2  = "gloom2"
	moodGloom3  = "gloom3"
	moodJitters = "jitters"
	moodRain    = "rain"
	moodShowers = "showers"
	moodFrost   = "frost"
	moodHeat    = "heat"
	moodPollen  = "pollen"
	moodSombre  = "sombre"
)

const (
	heatAt   = 90
	jitterAt = 22
)

// moodOf picks one mood, the heaviest first, so a big news day is sombre
// whatever the weather.
func moodOf(r *reading, v dayView) string {
	if r == nil {
		return moodPlain
	}
	switch {
	case r.Major:
		return moodSombre
	case r.HasSP && r.SP <= -2.5:
		return moodGloom3
	case v.Rain:
		return moodRain
	case r.HasHL && r.Low <= frostAt, r.HasTemp && r.Temp <= frostAt:
		return moodFrost
	case r.HasHL && r.High >= heatAt:
		return moodHeat
	case r.HasSP && r.SP <= -1.5:
		return moodGloom2
	case r.VIX >= jitterAt:
		return moodJitters
	case r.HasSP && r.SP <= -0.5:
		return moodGloom1
	case r.Pollen >= 2:
		return moodPollen
	case r.Tmr != nil && r.Tmr.Rain >= 60:
		return moodShowers
	case r.HasSP && r.SP >= 0.5:
		return moodBright
	}
	return moodPlain
}

// Each line follows the greeting. A sombre day gets quiet lines and no jokes.
var moodLines = map[string][]string{
	moodPlain: {
		"It's an ordinary sort of day and it seems happy with that.",
		"Nothing much is going on and it's taking full advantage.",
		"It's been sitting on the pad watching the water move.",
		"It caught a fly earlier and hasn't stopped thinking about it.",
		"It had a quiet morning and would like a quiet afternoon too.",
		"It's in no hurry today, so you shouldn't be either.",
	},
	moodBright: {
		"The water's clear and it's in a very good mood about it.",
		"It's a bright day and it keeps hopping for no reason.",
		"Everything seems to be going right on the pond today.",
		"It found a sunny spot and isn't giving it up.",
		"It's been humming to itself all morning.",
		"It's a good day, it can feel it in its toes.",
	},
	moodGloom1: {
		"It's a little grey today, so it's keeping near the bank.",
		"The water's gone a bit murky and it's not sure how it feels about that.",
		"It's having a slow one and would like some company.",
		"It's not a great day but it's glad you came.",
		"The beetles are out and it's keeping an eye on them.",
	},
	moodGloom2: {
		"It's a grey day and it's spent most of it under the pad.",
		"The water's murky and it's staying close to you for a bit.",
		"It came up from the mud just to say hello, and it means it.",
		"It's having a rough one, so be nice to it.",
		"It's sitting very still and waiting for the water to clear.",
	},
	moodGloom3: {
		"It's a heavy day on the pond and it went down to the bottom early.",
		"It's been in the mud most of the day, but it came up for you.",
		"The water's dark and it's glad to see a friendly face.",
		"It says days like this pass, and it would know.",
	},
	moodJitters: {
		"The wind's got everybody a bit jumpy, it included.",
		"It keeps looking over its shoulder at the reeds.",
		"It's been jumpy all day, so it's glad it's only you.",
		"Every gust sends it under the pad for a second.",
	},
	moodRain: {
		"Rain, so it's in the best mood it's been in all week.",
		"It's raining and it has been sitting out in it on purpose.",
		"The rain's out and the whole pond is singing, it loudest.",
		"It's catching raindrops and missing most of them.",
		"Rain, so it's going to stay in with {show} once you've gone.",
	},
	moodShowers: {
		"There's rain coming tomorrow and it can't stop talking about it.",
		"It can smell rain on the way and it's very pleased.",
		"It's saving its best song for tomorrow's rain.",
	},
	moodFrost: {
		"It's cold enough for frost and it's mostly in the mud.",
		"It's cold out, so it's moving slow and staying low.",
		"It came up from the mud and it's still a bit sleepy.",
		"Frost on the bank, so it's keeping its toes tucked in.",
		"It's too cold to do much, so it's playing {game} under the pad.",
	},
	moodHeat: {
		"It's hot, so it's sitting in the shade with just its eyes out.",
		"It's too warm to hop, it says, so it's floating.",
		"It's been in the deep end all afternoon keeping cool.",
		"It's hot out and it's not moving unless it has to.",
	},
	moodPollen: {
		"There's pollen all over the water and it keeps sneezing.",
		"Everything's gone yellow and it's not a fan.",
		"It sneezed itself off the pad earlier and it's fine.",
	},
	moodSombre: {
		"It's a quiet day on the pond.",
		"It's sitting close by today.",
		"It's keeping you company for a while.",
		"It's been quiet all day and it's glad you're here.",
	},
}

var hobbyLines = map[string][]string{
	"tv": {
		"It stayed up watching {show} and wants to tell you all about it.",
		"It's halfway through {show} and won't say what happens.",
		"It fell asleep in front of {show} again.",
		"It thinks you'd like {show}. It's very sure.",
	},
	"game": {
		"It's been playing {game} all morning and it's very nearly good at it.",
		"It's stuck on a hard bit of {game} and needs a break.",
		"It beat somebody at {game} and it's still smug about it.",
		"It keeps saying one more go of {game}.",
	},
}

// Titles off dash with any of these at the start of a word are left out, so
// the frog never watches true crime or plays a war game.
var titleBlocklist = []string{
	"war", "crime", "criminal", "murder", "kill", "dead", "death", "blood",
	"casualt", "sex", "adult", "nsfw", "hentai", "gore", "terror", "cartel",
	"mob", "gun", "sniper", "hitman", "serial", "homicide", "massacre",
	"slaughter", "nazi", "zombie", "horror", "hell", "evil", "battle",
	"combat", "assassin", "strike",
}

var tagBlocklist = []string{"SEXUAL CONTENT", "NUDITY", "GORE", "VIOLENT", "HORROR", "MATURE"}

// fitTitle tidies a title off dash and says whether the frog can mention it.
func fitTitle(name string, tags []string) (string, bool) {
	name = strings.NewReplacer("™", "", "®", "", "©", "").Replace(name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len(name) > 40 {
		return "", false
	}
	for _, t := range tags {
		for _, b := range tagBlocklist {
			if strings.EqualFold(t, b) {
				return "", false
			}
		}
	}
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	for _, w := range words {
		for _, b := range titleBlocklist {
			if strings.HasPrefix(w, b) {
				return "", false
			}
		}
	}
	// A line can never carry a number, and a title with a digit in it is
	// usually a sequel or a year anyway.
	if strings.ContainsAny(name, "0123456789%") {
		return "", false
	}
	return name, true
}

func dayNumber(day string) uint32 {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return 0
	}
	return uint32(t.Unix() / 86400)
}

// pickLine rotates through a pool one step a day from where the frog's seed
// puts it, so a frog never says the same thing two days running. Lines with
// a slot the day can't fill are skipped.
func pickLine(pool []string, seed uint32, day string, shows, games []string) string {
	var ok []string
	for _, l := range pool {
		if strings.Contains(l, "{show}") && len(shows) == 0 || strings.Contains(l, "{game}") && len(games) == 0 {
			continue
		}
		ok = append(ok, l)
	}
	if len(ok) == 0 {
		return ""
	}
	n := seed + dayNumber(day)
	l := ok[n%uint32(len(ok))]
	if len(shows) > 0 {
		l = strings.ReplaceAll(l, "{show}", shows[(seed/7)%uint32(len(shows))])
	}
	if len(games) > 0 {
		l = strings.ReplaceAll(l, "{game}", games[(seed/7)%uint32(len(games))])
	}
	return l
}

type hobby struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Line  string `json:"line"`
}

// hobbyOf is what the frog is watching or playing today, on about two days
// in five, and never on a sombre day.
func hobbyOf(r *reading, mood string, seed uint32, nature, day string) *hobby {
	if r == nil || mood == moodSombre {
		return nil
	}
	h := dayHash(day, "hobby"+strconv.FormatUint(uint64(seed), 10))
	var kind string
	var list []string
	switch h % 10 {
	case 0, 1:
		kind, list = "tv", r.Shows
	case 2, 3:
		kind, list = "game", r.Games
	}
	if len(list) == 0 {
		return nil
	}
	title := list[(h/10)%uint32(len(list))]
	pool := book.natured(map[string]string{"tv": "watching", "game": "playing"}[kind], "", nature, day, hobbyLines[kind])
	line := pool[(h/1000)%uint32(len(pool))]
	line = strings.NewReplacer("{show}", title, "{game}", title).Replace(line)
	return &hobby{Kind: kind, Title: title, Line: line}
}

// moodLine is what follows the greeting today, the hobby when there is one.
func moodLine(r *reading, mood string, seed uint32, nature, day string) string {
	if h := hobbyOf(r, mood, seed, nature, day); h != nil {
		return h.Line
	}
	var shows, games []string
	if r != nil {
		shows, games = r.Shows, r.Games
	}
	return pickLine(book.natured("greeting", mood, nature, day, moodLines[mood]), seed, day, shows, games)
}

// ---------------------------------------------------------------- the weatherman

// Old Clover gives tomorrow's forecast every day and thinks rain is the best
// news there is.
const weatherman = "Old Clover"

func feelOf(high int) string {
	switch {
	case high >= 88:
		return "hot"
	case high >= 74:
		return "warm"
	case high >= 60:
		return "mild"
	case high >= 46:
		return "cool"
	}
	return "cold"
}

func forecastLine(t *tomorrow, day string) string {
	if t == nil {
		return ""
	}
	sky := strings.ToLower(t.Cond)
	wet := t.Rain >= 50 || strings.Contains(sky, "rain") || strings.Contains(sky, "shower") ||
		strings.Contains(sky, "drizzle") || strings.Contains(sky, "thunder")
	n := dayHash(day, "forecast")
	if wet {
		rain := []string{
			weatherman + " says rain tomorrow, and says it like it's the best news all week.",
			weatherman + " says rain tomorrow and has already picked a spot to sit in it.",
			"Rain tomorrow, says " + weatherman + ", who hasn't stopped grinning since.",
		}
		return rain[n%uint32(len(rain))]
	}
	feel := feelOf(t.High)
	if strings.Contains(sky, "snow") {
		return weatherman + " says snow tomorrow, and is suspicious of it."
	}
	line := weatherman + " says tomorrow is " + feel
	switch {
	case strings.Contains(sky, "fog"):
		line += " and foggy first thing"
	case strings.Contains(sky, "clear"):
		line += " and clear"
	case strings.Contains(sky, "cloud"):
		line += " and cloudy"
	}
	if t.Low <= frostAt {
		line += ", with frost overnight"
	}
	dry := []string{". No rain, which is a shame.", ". Dry, sadly.", ". No rain in it, and Clover's sorry about that."}
	return line + dry[n%uint32(len(dry))]
}

// ---------------------------------------------------------------- the fortune lily

// A pond's fortune is set at midnight and holds all day. Most are only a
// line, and the lucky ones tip the log or the wheel a little.
const (
	luckNone  = ""
	luckLog   = "log"
	luckWheel = "wheel"
)

type fortune struct {
	Luck string
	Line string
}

var fortunes = []fortune{
	{luckLog, "The fortune lily says the old log is kind to you today."},
	{luckLog, "The fortune lily says look under things today."},
	{luckWheel, "The fortune lily says the wheel leans your way today."},
	{luckWheel, "The fortune lily says give it a spin, you'll see."},
	{luckNone, "The fortune lily says a quiet day is a good day."},
	{luckNone, "The fortune lily says somebody in town is thinking of you."},
	{luckNone, "The fortune lily says bring a snack."},
	{luckNone, "The fortune lily says don't trust the heron."},
	{luckNone, "The fortune lily says today is for sitting still."},
	{luckNone, "The fortune lily says you'll find something round."},
}

func fortuneOf(pondID int64, day string) fortune {
	return fortunes[dayHash(day, "fortune"+strconv.FormatInt(pondID, 10))%uint32(len(fortunes))]
}
