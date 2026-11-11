package main

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The frog keeps a diary, one line a day written at the greeting about the
// day before, off these templates and whatever the tables say happened.
// Nothing here is made up, and a day nobody came by is only a quiet day.
type diaryPool struct {
	any []string
	by  map[string][]string
}

var diaryLines = map[string]diaryPool{
	"quiet": {
		any: []string{
			"Sat on the pad most of the day and watched the water go by, which is a good day by me.",
			"Nothing much happened yesterday and I liked all of it.",
			"I counted the ripples for a while and lost count somewhere after forty, so I started again.",
			"A dragonfly came by twice and I think it was the same one both times.",
			"Had a long think about flies and then a short nap, in that order.",
			"The pond was quiet so I was quiet too, and we got along fine.",
		},
		by: map[string][]string{
			"BOLD": {
				"Jumped off the pad and back on again about a hundred times, just to keep my legs in.",
				"I stared down a heron yesterday, or it might have been a stick, and either way it left.",
			},
			"SHY": {
				"Stayed under the lily for most of it and watched everything from there, which is the best seat on the pond.",
				"Somebody waved at me from the reeds and I waved back once they'd gone.",
			},
			"NIGHT OWL": {
				"Slept through the bright part and was up for the good part, when the stars come out on the water.",
				"The moon was on the pond again last night and I kept it company.",
			},
			"GLUTTON": {
				"I had three flies before noon and spent the afternoon thinking about a fourth.",
				"Spent the day working out what I'd eat if I could eat anything, and it's a long list now.",
			},
			"SINGER": {
				"Practised the low note until the reeds hummed along with me.",
				"I've got a new song and it only has the one word in it so far, but it's a good word.",
			},
			"DRIFTER": {
				"Floated from one side of the pond to the other and then back, since I had the time.",
				"I let the water take me wherever it liked and it mostly liked the middle.",
			},
		},
	},
	"egg": {any: []string{
		"I was an egg all day and I think I did it well.",
		"Mostly I floated, and once I turned all the way over, which felt like a lot.",
		"It's warm in here and I can hear the pond going on outside.",
		"I had a wriggle about and I'm fairly sure that's a tail coming.",
	}},
	"grew": {
		any: []string{
			"I woke up {a_stage} and had to check twice, but it's true.",
			"Everything fits a bit differently because I'm {a_stage} now.",
			"I'm {a_stage} as of yesterday and I've decided it suits me.",
		},
		by: map[string][]string{
			"BOLD":      {"I'm {a_stage} now, which I think makes me the biggest thing on this pad."},
			"SHY":       {"I'm {a_stage} now and I haven't told anybody yet, so you're the first."},
			"NIGHT OWL": {"I went to sleep one thing and got up {a_stage}, which is what I call a good night."},
			"GLUTTON":   {"I'm {a_stage} now, so there's more room in me for supper."},
			"SINGER":    {"I'm {a_stage} now and my voice came out lower than I expected, and I like it."},
			"DRIFTER":   {"I'm {a_stage} now and I got further across the pond than I ever have."},
		},
	},
	"heart": {
		any: []string{
			"You gave me a rub yesterday and I felt it right down to my feet, so I think we're better friends now.",
			"We got a bit closer yesterday, you and me, and I've been pleased about it ever since.",
			"I've worked out that I like it when you come by, and yesterday I liked it more than usual.",
		},
		by: map[string][]string{
			"BOLD":      {"I let you give me a rub yesterday and I don't let just anybody do that."},
			"SHY":       {"I didn't duck when you reached in yesterday, and I'm quite proud of that."},
			"NIGHT OWL": {"I stayed up for you yesterday and it was worth being tired for."},
			"GLUTTON":   {"A good rub is nearly as good as supper, and yesterday I had both."},
			"SINGER":    {"I sang a bit while you were rubbing my back yesterday, in case you didn't hear it."},
			"DRIFTER":   {"I came all the way over to you yesterday and stayed put for once."},
		},
	},
	"fish": {any: []string{
		"We went fishing and did better than we ever have, {score} on the board, and I held the net.",
		"It was our best day off the jetty so far with {score}, and I'm told I helped.",
		"We got {score} at fishing yesterday, which is a new best, and the fish were very good about it.",
	}},
	"biggest": {any: []string{
		"We landed {a_fish} bigger than {who}'s yesterday, so the pond record is ours now and I've told everybody.",
		"The biggest {fish} this pond has seen came up on our line yesterday, and {who} came over to look at it.",
		"Yesterday we took the {fish} record off {who}, and I measured it twice to be sure.",
	}},
	"catch": {any: []string{
		"We landed {a_fish} yesterday, the first one I've ever seen up close, and it looked right at me.",
		"There's {a_fish} in the fish book now, our first, and I helped by keeping very still.",
		"Up came {a_fish} on the line yesterday and we'd never had one before, so I had a good look.",
		"Our first {fish} yesterday, and I've been telling the lily about it since.",
	}},
	"record": {
		any: []string{
			"A new best at {game} yesterday, {score}, and I watched the whole thing.",
			"We got {score} at {game}, which we've never done before, and I croaked when it happened.",
			"We made {score} at {game} yesterday and that's the best yet, so I'm writing it down here.",
			"Yesterday was a good day for {game}, with {score} and a new best.",
		},
		by: map[string][]string{
			"BOLD":      {"We got {score} at {game}, a new best, and I think we can beat it today."},
			"SHY":       {"We got a new best at {game}, {score}, and I told the lily about it."},
			"NIGHT OWL": {"A new best at {game}, {score}, and I was awake for all of it."},
			"GLUTTON":   {"We got {score} at {game}, a new best, and I think that calls for a snack."},
			"SINGER":    {"I made up a song about getting {score} at {game}, and it rhymes if you sing it fast."},
			"DRIFTER":   {"A new best at {game}, {score}, and then I floated about feeling good."},
		},
	},
	"hat": {
		any: []string{
			"A {hat} turned up yesterday and I tried it on when nobody was looking.",
			"We found a {hat} yesterday, and I've been thinking about where I'd wear it.",
			"There's a {hat} in the bag now that wasn't there before, and I think it's my size.",
			"Yesterday had a new hat in it, a {hat}, so I'd call it a good one.",
		},
		by: map[string][]string{
			"BOLD": {"A {hat} came out yesterday and I'd like it known that I look good in it."},
			"SHY":  {"A {hat} turned up yesterday and it's big enough to hide under, which I like."},
		},
	},
	"friend": {any: []string{
		"{who} thinks more of us than they did the day before, and I think more of {who} too.",
		"We got a bit friendlier with {who} yesterday, and I got a wave on the way past.",
		"{who} called over to say hello yesterday, and I called back.",
		"I've decided {who} is a good neighbour, and yesterday settled it.",
	}},
	"memento": {any: []string{
		"{who}'s {thing} is ours to keep as of yesterday, which is the sort of thing you only give a best friend.",
		"We came home with {who}'s {thing} yesterday, and I've put it where it can see the water.",
		"{who} handed over the {thing} yesterday and said to look after it, so I am.",
	}},
	"set": {any: []string{
		"We finished the {set} set yesterday and now there's {thing} on the water, which I've been looking at ever since.",
		"There's {thing} on the pond as of yesterday, for finishing the {set} set, and it's staying for good.",
		"The {set} set is all found, so yesterday {thing} turned up on the water and I like it very much.",
	}},
	"pin": {any: []string{
		"We got a pin yesterday, the {badge} one, and it's up on the board with the others.",
		"There's a new pin on the board from yesterday, the {badge} one, and I think it's the best looking so far.",
		"Yesterday we earned the {badge} pin, and I went over and looked at it twice.",
	}},
	"keepsake": {any: []string{
		"We were given a {keepsake} yesterday to remember the day by, and I've put it where I can see it.",
		"There's a {keepsake} on the shelf now, from yesterday, and I keep going over to look at it.",
		"Yesterday left us a {keepsake}, so I won't be forgetting that one.",
		"I'm keeping the {keepsake} from yesterday for good, and nobody is to move it.",
	}},
	"rain": {any: []string{
		"It rained all over the pond yesterday and I sat out in every bit of it.",
		"It rained yesterday and the whole pond went spotty, which is my favourite kind of pond.",
		"The rain came down yesterday and I didn't go under the pad once.",
		"Yesterday was wet from end to end and I can't think of a thing wrong with that.",
	}},
	"frost": {any: []string{
		"There was frost on the pad yesterday so I slept in the mud, and the mud was lovely.",
		"It was cold enough yesterday that I could see my own croak.",
		"There was frost yesterday, so I tucked my feet under and had a long doze.",
	}},
	"snow": {any: []string{
		"It snowed on the pond yesterday and I caught one on my nose before it went.",
		"It snowed yesterday, which I'd only heard about before, and it's quieter than rain.",
		"Everything was white round the edges yesterday and I stayed in to look at it.",
	}},
	"storm": {any: []string{
		"There was thunder yesterday and I croaked back at it every time.",
		"A storm went over yesterday and the reeds all leaned the same way.",
		"It thundered yesterday and I counted between the flash and the bang like you're meant to.",
	}},
	"fog": {any: []string{
		"It was foggy yesterday and I couldn't see the far bank, so for a while the pond was all mine.",
		"The fog sat on the water yesterday and everything sounded closer than it was.",
		"Yesterday was misty and the spider webs all had beads on.",
	}},
	"hot": {any: []string{
		"It was hot yesterday so I kept my feet in the water and my head in the shade.",
		"Yesterday was a scorcher and the pad was warm right through.",
		"It was too hot to hop yesterday, so I floated, and I think floating is underrated.",
	}},
	"sun": {any: []string{
		"The sun was out yesterday and I found the warm spot on the pad before anybody else did.",
		"It was clear yesterday and I could see right to the bottom of the pond.",
		"Yesterday was bright from morning on and I sat in it for as long as it lasted.",
		"It was a clear day yesterday and the dragonflies were out showing off.",
	}},
}

// skyOf is the day's weather as the diary knows it, or "" for a day with
// nothing to say about it. Only the weather goes in, never the market.
func skyOf(r *reading) string {
	if r == nil {
		return ""
	}
	switch {
	case condHas(r, "thunder"):
		return "storm"
	case condHas(r, "snow", "flurr"):
		return "snow"
	case r.Rain >= 60 || condHas(r, "rain", "shower", "drizzle"):
		return "rain"
	case condHas(r, "fog", "mist", "haze"):
		return "fog"
	case r.HasTemp && r.Temp <= frostAt:
		return "frost"
	case r.HasTemp && r.Temp >= heatAt:
		return "hot"
	case condHas(r, "clear", "sunny", "fair"):
		return "sun"
	}
	return ""
}

// noteSky keeps today's weather for tomorrow's diaries, one row a day for
// every pond.
func noteSky(tx *sql.Tx, r *reading, now time.Time) error {
	sky := skyOf(r)
	if sky == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO skies (day, sky) VALUES (?, ?) ON CONFLICT (day) DO UPDATE SET sky = excluded.sky`, dayOf(now), sky)
	return err
}

type diaryFact struct {
	kind  string
	slots []string
}

func aFish(f fishKind) string {
	name := strings.ToLower(f.name)
	if strings.ContainsRune("aeiou", rune(name[0])) {
		return "an " + name
	}
	return "a " + name
}

func aStage(stage string) string {
	if stage == "elder" {
		return "an elder"
	}
	return "a " + stage
}

// diaryFacts is what the tables hold about one day, the biggest news first
// and a quiet day last.
func diaryFacts(tx *sql.Tx, p *pond, f frogRow, day string, from, to time.Time) ([]diaryFact, error) {
	var out []diaryFact
	born := time.Unix(f.HatchedAt, 0)
	if born.After(from) {
		from = born
	}
	was, _ := stageOf(from.Sub(born))
	is, _ := stageOf(to.Add(-time.Second).Sub(born))
	if is != was {
		out = append(out, diaryFact{"grew", []string{"{a_stage}", aStage(is)}})
	}
	if is == "egg" {
		return append(out, diaryFact{kind: "egg"}), nil
	}
	one := func(query string, args ...any) (string, error) {
		var s string
		err := tx.QueryRow(query, args...).Scan(&s)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return s, err
	}
	if d, err := one(`SELECT heart_day FROM frogs WHERE id = ?`, f.ID); err != nil {
		return nil, err
	} else if d == day {
		out = append(out, diaryFact{kind: "heart"})
	}
	// A pond record comes first, then a best at fishing, then any other best.
	rows, err := tx.Query(`SELECT game, score FROM records WHERE pond_id = ? AND day = ?
		ORDER BY substr(game, 1, 4) = 'rec_' DESC, game = 'fish' DESC, score DESC, game`, p.ID, day)
	if err != nil {
		return nil, err
	}
	told := false
	for rows.Next() {
		var game string
		var score int
		if err := rows.Scan(&game, &score); err != nil {
			rows.Close()
			return nil, err
		}
		if kind, ok := strings.CutPrefix(game, recordGame); ok {
			if f, ok := fishOf(kind); ok && !told {
				_, who := pondRecord(f)
				told = true
				out = append(out, diaryFact{"biggest", []string{"{a_fish}", aFish(f), "{fish}", strings.ToLower(f.name), "{who}", residents[who].name}})
			}
			continue
		}
		name := "Dive"
		if g, ok := arcadeOf(game); ok {
			name = g.name
		}
		fact := diaryFact{"record", []string{"{game}", name, "{score}", commas(score)}}
		if game == "fish" {
			fact.kind = "fish"
		}
		out = append(out, fact)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	first, err := tx.Query(`SELECT kind FROM fish WHERE pond_id = ? AND first_day = ?`, p.ID, day)
	if err != nil {
		return nil, err
	}
	var rarest *fishKind
	for first.Next() {
		var k string
		if err := first.Scan(&k); err != nil {
			first.Close()
			return nil, err
		}
		if f, ok := fishOf(k); ok && (rarest == nil || f.tier > rarest.tier || (f.tier == rarest.tier && f.pts > rarest.pts)) {
			rarest = &f
		}
	}
	if err := first.Close(); err != nil {
		return nil, err
	}
	if rarest != nil {
		out = append(out, diaryFact{"catch", []string{"{a_fish}", aFish(*rarest), "{fish}", strings.ToLower(rarest.name)}})
	}
	// A best friend's memento comes with a hat, and it's the bigger news.
	if k, err := one(`SELECT kind FROM mementos WHERE pond_id = ? AND day = ? ORDER BY kind LIMIT 1`, p.ID, day); err != nil {
		return nil, err
	} else if k != "" {
		for who, m := range mementos {
			if m.kind == k {
				out = append(out, diaryFact{"memento", []string{"{who}", residents[who].name, "{thing}", m.thing}})
			}
		}
	}
	if k, err := one(`SELECT kind FROM sets WHERE pond_id = ? AND day = ? ORDER BY kind LIMIT 1`, p.ID, day); err != nil {
		return nil, err
	} else if k != "" {
		for _, s := range sets {
			if s.kind == k {
				out = append(out, diaryFact{"set", []string{"{set}", setWord(s.name), "{thing}", strings.ToLower(s.gives)}})
			}
		}
	}
	rows, err = tx.Query(`SELECT shape, colour FROM hats WHERE pond_id = ? AND first_day = ?`, p.ID, day)
	if err != nil {
		return nil, err
	}
	var best *hat
	for rows.Next() {
		var h hat
		if err := rows.Scan(&h.Shape, &h.Colour); err != nil {
			rows.Close()
			return nil, err
		}
		if best == nil || h.tier() > best.tier() {
			best = &h
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if best != nil {
		out = append(out, diaryFact{"hat", []string{"{hat}", strings.ToLower(best.name())}})
	}
	var who int
	err = tx.QueryRow(`SELECT who FROM neighbours WHERE pond_id = ? AND heart_day = ? ORDER BY who LIMIT 1`, p.ID, day).Scan(&who)
	if err == nil && who >= 0 && who < len(residents) {
		out = append(out, diaryFact{"friend", []string{"{who}", residents[who].name}})
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if k, err := one(`SELECT kind FROM badges WHERE pond_id = ? AND day = ? ORDER BY kind LIMIT 1`, p.ID, day); err != nil {
		return nil, err
	} else if k != "" {
		for _, b := range badges {
			if b.kind == k {
				out = append(out, diaryFact{"pin", []string{"{badge}", strings.ToLower(b.name)}})
			}
		}
	}
	kept, err := tx.Query(`SELECT kind FROM keepsakes WHERE pond_id = ? AND day = ?`, p.ID, day)
	if err != nil {
		return nil, err
	}
	had := map[string]bool{}
	for kept.Next() {
		var k string
		if err := kept.Scan(&k); err != nil {
			kept.Close()
			return nil, err
		}
		had[k] = true
	}
	if err := kept.Close(); err != nil {
		return nil, err
	}
	var top *keepsake
	for i, k := range keepsakes {
		if had[k.kind] && (top == nil || k.tier > top.tier) {
			top = &keepsakes[i]
		}
	}
	if top != nil {
		out = append(out, diaryFact{"keepsake", []string{"{keepsake}", strings.ToLower(top.name)}})
	}
	if sky, err := one(`SELECT sky FROM skies WHERE day = ?`, day); err != nil {
		return nil, err
	} else if _, ok := diaryLines[sky]; ok {
		out = append(out, diaryFact{kind: sky})
	}
	return append(out, diaryFact{kind: "quiet"}), nil
}

// diaryWeek is how many days back a line isn't said again.
const diaryWeek = 7

// writeDiary puts down yesterday's line for the frog out front, once. It
// takes the biggest news that has a line the last week hasn't used.
func writeDiary(tx *sql.Tx, p *pond, f frogRow, now time.Time) error {
	n := now.In(pondZone)
	to := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, pondZone)
	from := to.AddDate(0, 0, -1)
	day := dayOf(from)
	if !time.Unix(f.HatchedAt, 0).Before(to) {
		return nil
	}
	var have int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM diary WHERE pond_id = ? AND day = ?`, p.ID, day).Scan(&have); err != nil || have > 0 {
		return err
	}
	recent := map[string]bool{}
	rows, err := tx.Query(`SELECT line FROM diary WHERE pond_id = ? ORDER BY day DESC LIMIT ?`, p.ID, diaryWeek)
	if err != nil {
		return err
	}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			rows.Close()
			return err
		}
		recent[l] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	facts, err := diaryFacts(tx, p, f, day, from, to)
	if err != nil {
		return err
	}
	nature := genomeOf(f.Seed, f.Mods).Nature
	turn := int(from.Unix()/86400) + int(p.ID)
	line := ""
	for _, fact := range facts {
		pool := append(append([]string{}, diaryLines[fact.kind].any...), diaryLines[fact.kind].by[nature]...)
		for i := range pool {
			l := strings.NewReplacer(fact.slots...).Replace(pool[(turn+i)%len(pool)])
			if !recent[l] {
				line = l
				break
			}
		}
		if line != "" {
			break
		}
	}
	if line == "" {
		return nil
	}
	_, err = tx.Exec(`INSERT INTO diary (pond_id, frog_id, day, line) VALUES (?, ?, ?, ?)`, p.ID, f.ID, day, line)
	return err
}

type diaryRow struct {
	Day  string `json:"day"`
	Line string `json:"line"`
}

type diaryPage struct {
	Lines []diaryRow `json:"lines"`
	// More says there are older lines than these.
	More bool `json:"more"`
}

const diaryPageSize = 30

// diaryOf is a frog's diary, newest first, a page at a time from before a day.
func (s *store) diaryOf(id, frogID int64, before string) (*diaryPage, error) {
	if before == "" {
		before = "9999"
	}
	rows, err := s.r.Query(`SELECT day, line FROM diary WHERE pond_id = ? AND frog_id = ? AND day < ?
		ORDER BY day DESC LIMIT ?`, id, frogID, before, diaryPageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &diaryPage{Lines: []diaryRow{}}
	for rows.Next() {
		var r diaryRow
		if err := rows.Scan(&r.Day, &r.Line); err != nil {
			return nil, err
		}
		if len(out.Lines) == diaryPageSize {
			out.More = true
			break
		}
		out.Lines = append(out.Lines, r)
	}
	return out, rows.Err()
}
