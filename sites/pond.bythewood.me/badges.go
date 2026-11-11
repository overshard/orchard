package main

// A badge is a small goal somewhere in the game, kept with the day it was
// earned and drawn as a pin on a board. None is for a streak, and each can
// be earned by one pond on its own.
type badge struct {
	kind, name string
	// how is what earns it, said as a thing to do, and to the place it's done.
	how, to string
	// at is how far along the pond is and how far it has to go.
	at func(t *tally) (have, of int)
}

// tally is everything a badge can ask about, counted once off the view, so
// checking the badges after a move reads nothing more.
type tally struct {
	fish, bigFish, records        int
	podium, won, played, games    int
	stage, frogs, hearts, jobBest int
	hoods, pets                   int
	hats, goldHats, trinkets      int
	rocks, keepsakes, sets        int
	power                         int
	gravel, garden, built         int
	dishes, lodgers, furniture    int
	friends, mementos             int
	stall, share                  int
	days, sites, key              int
	// The best go at bug sweep in pads and the best round of pebble drop.
	swept, dropped int
	// Crickets and fireflies brought in at work, and targets at full mastery.
	crickets, fireflies, mastered int
}

var stageRank = map[string]int{"egg": 0, "tadpole": 1, "froglet": 2, "frog": 3, "elder": 4}

func one(b bool) int {
	if b {
		return 1
	}
	return 0
}

func tallyOf(v *view, p *pond) *tally {
	t := &tally{power: v.Share.Power.Total, hearts: v.Bond.Hearts, days: v.Visited, key: one(p.KeySavedAt.Valid)}
	for _, f := range v.Fishing.All {
		t.fish += one(f.Count > 0)
		t.bigFish += one(f.Count > 0 && f.Tier >= 3)
		t.records += one(f.Yours)
	}
	t.podium = one(v.Dive.Podium[0] || v.Dive.Podium[1] || v.Dive.Podium[2])
	t.won = one(v.Dive.Podium[0])
	t.games, t.played = len(v.Arcade)+1, one(v.Dive.Best > 0)
	for _, g := range v.Arcade {
		t.played += one(g.Best > 0)
		switch g.Key {
		case "sweep":
			t.swept = g.Best
		case "drop":
			t.dropped = g.Best
		}
	}
	for _, f := range v.Frogs {
		t.stage = max(t.stage, stageRank[f.Stage])
		t.frogs += one(f.Stage != "egg")
		if f.Work == nil {
			continue
		}
		for _, l := range f.Work.Levels {
			t.jobBest = max(t.jobBest, l.Level)
		}
		for _, m := range f.Work.Targets {
			j, _ := jobOf(m.Job)
			tg, _ := j.target(m.Target)
			switch tg.kind {
			case "cricket":
				t.crickets += m.Got
			case "firefly":
				t.fireflies += m.Got
			}
			t.mastered += one(m.Level == masteryTop)
		}
	}
	for _, h := range v.Hoods {
		t.hoods += one(h.Got)
	}
	for _, h := range v.Hats {
		t.hats += one(h.Count > 0)
		t.goldHats += one(h.Count > 0 && h.Tier == tierCount-1)
	}
	for _, x := range v.Trinkets.All {
		t.trinkets += one(x.Found)
		t.pets += one(x.Found && x.Pet != "")
	}
	for _, r := range v.Rocks {
		t.rocks += one(r.Found)
	}
	for _, k := range v.Keepsakes {
		t.keepsakes += one(k.First != "")
	}
	for _, s := range v.Sets {
		t.sets += one(s.Done)
	}
	b := v.Bank
	t.gravel, t.garden = b.Gravel.Level, b.Garden.Level
	t.built = one(b.Gravel.Level > 0) + one(b.Garden.Level > 0) + one(b.Dye.Level > 0) + one(b.Burrows.Level > 0)
	t.dishes, t.lodgers, t.furniture = len(v.Kitchen.Cookbook), len(b.Burrows.Lodgers), len(v.Furniture)
	for _, n := range v.Neighbours.All {
		t.friends += one(n.Level >= 1)
	}
	for _, m := range v.Mementos {
		t.mementos += one(m.Day != "")
	}
	t.stall = one(p.Tried&triedStall != 0 || len(v.Stall.Rows) > 0 || v.Stall.Takings > 0)
	t.share = one(p.Tried&triedShare != 0 || v.Market.In > 0)
	for _, s := range v.Sites {
		t.sites += one(s.Got)
	}
	return t
}

func upTo(n func(t *tally) int, of int) func(t *tally) (int, int) {
	return func(t *tally) (int, int) { return min(n(t), of), of }
}

// lilyLord is the rung of the power ladder the badge of that name is for.
var lilyLord = ranks[2].at

// Stored by kind, so a new badge only ever goes on the end.
var badges = []badge{
	{"first_fish", "First bite", "Land a fish", "fish", upTo(func(t *tally) int { return t.fish }, 1)},
	{"fish_10", "Half the book", "Land ten kinds of fish", "fish", upTo(func(t *tally) int { return t.fish }, 10)},
	{"fish_big", "The big one", "Land an epic fish or better", "fish", upTo(func(t *tally) int { return t.bigFish }, 1)},
	{"record", "Record breaker", "Beat a pond record", "fish", upTo(func(t *tally) int { return t.records }, 1)},
	{"podium", "On the podium", "Finish in the top three in Dive", "dive", upTo(func(t *tally) int { return t.podium }, 1)},
	{"dive_win", "First home", "Win a race in Dive", "dive", upTo(func(t *tally) int { return t.won }, 1)},
	{"every_disk", "Tried the lot", "Play every game once", "play", func(t *tally) (int, int) { return t.played, t.games }},
	{"hatch", "Out of the egg", "Hatch a frog", "pond", upTo(func(t *tally) int { return t.stage }, 1)},
	{"grown", "All grown up", "Raise a tadpole into a frog", "pond", upTo(func(t *tally) int { return t.stage / 3 }, 1)},
	{"elder", "An elder", "Keep a frog until it's an elder", "pond", upTo(func(t *tally) int { return t.stage / 4 }, 1)},
	{"chorus", "A chorus", "Have three frogs at the pond", "reeds", upTo(func(t *tally) int { return t.frogs }, 3)},
	{"heart_1", "First heart", "Get to one heart with your frog", "pond", upTo(func(t *tally) int { return t.hearts }, 1)},
	{"heart_5", "Five hearts", "Get to five hearts with your frog", "pond", upTo(func(t *tally) int { return t.hearts }, bondHearts)},
	{"job_10", "Learning the ropes", "Get a frog to level 10 at a job", "reeds", upTo(func(t *tally) int { return t.jobBest }, 10)},
	{"job_50", "Old hand", "Get a frog to level 50 at a job", "reeds", upTo(func(t *tally) int { return t.jobBest }, 50)},
	{"job_99", "Master", "Get a frog to level 99 at a job", "reeds", upTo(func(t *tally) int { return max(t.jobBest, t.hoods*maxLevel) }, maxLevel)},
	{"pet", "A friend from work", "Have a frog bring a pet home from work", "reeds", upTo(func(t *tally) int { return t.pets }, 1)},
	{"hats_10", "Ten hats", "Find ten hats", "collect", upTo(func(t *tally) int { return t.hats }, 10)},
	{"hats_30", "Thirty hats", "Find thirty hats", "collect", upTo(func(t *tally) int { return t.hats }, 30)},
	{"hat_gold", "Something golden", "Find a legendary hat", "collect", upTo(func(t *tally) int { return t.goldHats }, 1)},
	{"trinkets_10", "Ten trinkets", "Find ten trinkets", "collect", upTo(func(t *tally) int { return t.trinkets }, 10)},
	{"rocks_5", "Rock hound", "Find five kinds of rock", "collect", upTo(func(t *tally) int { return t.rocks }, 5)},
	{"keepsakes_5", "Days to remember", "Keep five kinds of keepsake", "collect", upTo(func(t *tally) int { return t.keepsakes }, 5)},
	{"set_1", "A set", "Finish a set", "collect", upTo(func(t *tally) int { return t.sets }, 1)},
	{"sets_all", "Every set", "Finish every set", "collect", func(t *tally) (int, int) { return t.sets, len(sets) }},
	{"lily_lord", "Lily lord", "Get to 5,000 frog power", "settings", func(t *tally) (int, int) { return min(t.power, lilyLord), lilyLord }},
	{"bar", "Gravel bar", "Build the gravel bar", "bank", upTo(func(t *tally) int { return t.gravel }, 1)},
	{"garden", "A full garden", "Build the garden as far as it goes", "garden", upTo(func(t *tally) int { return t.garden }, buildLevels)},
	{"cook_1", "First dish", "Cook a dish", "garden", upTo(func(t *tally) int { return t.dishes }, 1)},
	{"cook_10", "Ten dishes", "Cook ten different dishes", "garden", upTo(func(t *tally) int { return t.dishes }, 10)},
	{"builder", "Builder", "Build the gravel bar, the garden, the dye works and a burrow", "bank", upTo(func(t *tally) int { return t.built }, 4)},
	{"lodger", "A lodger", "Take in a lodger", "bank", upTo(func(t *tally) int { return t.lodgers }, 1)},
	{"friend_1", "A friend", "Get to one heart with a neighbour", "neighbours", upTo(func(t *tally) int { return t.friends }, 1)},
	{"friends_all", "Good neighbour", "Get to one heart with every neighbour", "neighbours", func(t *tally) (int, int) { return t.friends, len(residents) }},
	{"best_friend", "Best friend", "Get to five hearts with a neighbour", "neighbours", upTo(func(t *tally) int { return t.mementos }, 1)},
	{"stall", "Stallholder", "List something on the stall", "stall", upTo(func(t *tally) int { return t.stall }, 1)},
	{"share", "Shareholder", "Buy a share on the market", "market", upTo(func(t *tally) int { return t.share }, 1)},
	{"furniture", "Home comforts", "Buy three things from Tansy", "shops", upTo(func(t *tally) int { return t.furniture }, 3)},
	{"days_30", "Thirty days", "Spend thirty days at the pond", "pond", upTo(func(t *tally) int { return t.days }, 30)},
	{"days_100", "A hundred days", "Spend a hundred days at the pond", "pond", upTo(func(t *tally) int { return t.days }, 100)},
	{"sites", "Out and about", "Have your frog turn up on another site", "collect", upTo(func(t *tally) int { return t.sites }, 1)},
	{"key", "Key kept safe", "Save your pond key", "key", upTo(func(t *tally) int { return t.key }, 1)},
	{"sweep_clear", "Clean sweep", "Clear a grid in bug sweep", "sweep", upTo(func(t *tally) int { return t.swept / bugSafe }, 1)},
	{"drop_five", "Five in the middle", "Land all five pebbles in the middle pot", "drop", upTo(func(t *tally) int { return t.dropped / dropMost }, 1)},
	{"crickets_100", "A hundred crickets", "Catch a hundred crickets at work", "job", upTo(func(t *tally) int { return t.crickets }, 100)},
	{"first_firefly", "First firefly", "Catch a firefly at work", "job", upTo(func(t *tally) int { return t.fireflies }, 1)},
	{"mastered", "Mastered", "Get a target to mastery 10", "job", upTo(func(t *tally) int { return t.mastered }, 1)},
}

type badgeView struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	How  string `json:"how"`
	Have int    `json:"have"`
	Of   int    `json:"of"`
	// Got stays true once it's earned, whatever the count does after.
	Got bool   `json:"got"`
	Day string `json:"day,omitempty"`

	to string
}

// badgesOf works every badge out from a tally. days is the day each one
// already earned was kept.
func badgesOf(t *tally, days map[string]string) []badgeView {
	out := make([]badgeView, 0, len(badges))
	for _, b := range badges {
		have, of := b.at(t)
		_, kept := days[b.kind]
		if kept {
			have = of
		}
		out = append(out, badgeView{b.kind, b.name, b.how + ".", have, of, have >= of, days[b.kind], b.to})
	}
	return out
}
